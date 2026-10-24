package kelos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/sperano/sillage/internal/model"
)

const (
	// defaultReconnectDelay is the pause before reopening a watch the API
	// server closed, and before relisting after an expired one, so that a
	// server closing or expiring every watch at once is not hammered.
	defaultReconnectDelay = time.Second
	// GapWatchExpired is the reason of the gap a relist records on the
	// Kelos coverage after a watch expired: the deletions and transient
	// phases between the last event and the relist are unknown.
	GapWatchExpired = "watch expired"
)

// errExpired marks a watch whose resourceVersion the API server no longer
// serves (410 Gone): only a relist can resume.
var errExpired = errors.New("kelos: watch expired")

// Watcher lists the configured namespace's Tasks and TaskRecords once,
// then watches both from the lists' resourceVersions, committing what each
// event changed to a Sink with the checkpoint it brings the watch to. A
// watch the API server closes is reopened from the checkpoint; a watch that
// expired is replaced by a relist and a "watch expired" gap. One Watcher
// runs per namespace, and its Run is not safe for concurrent use.
type Watcher struct {
	client         dynamic.Interface
	cfg            Config
	sink           Sink
	now            func() time.Time
	reconnectDelay time.Duration
	// sleep waits a delay, or until ctx is done.
	sleep func(ctx context.Context, d time.Duration) error
}

// NewWatcher returns a watcher of cfg's namespace through client,
// committing to sink.
func NewWatcher(client dynamic.Interface, cfg Config, sink Sink) (*Watcher, error) {
	if client == nil || sink == nil {
		return nil, errors.New("kelos: watcher without a client or a sink")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Watcher{
		client: client, cfg: cfg, sink: sink,
		now: time.Now, reconnectDelay: defaultReconnectDelay, sleep: sleep,
	}, nil
}

// Run watches until ctx is done or a request or a commit fails, and
// returns why. It resumes from the sink's checkpoint when there is one,
// and lists first otherwise. A failure leaves the last committed
// checkpoint, so running it again loses nothing unless that checkpoint
// expired meanwhile, which then shows as a gap.
func (w *Watcher) Run(ctx context.Context) error {
	st, err := w.start(ctx)
	if err != nil {
		return err
	}
	for {
		err := w.watch(ctx, st)
		if !errors.Is(err, errExpired) {
			return err
		}
		if err := w.pause(ctx); err != nil {
			return err
		}
		if st, err = w.relist(ctx, st); err != nil {
			return err
		}
	}
}

// start restores the committed checkpoint, or lists when there is none.
func (w *Watcher) start(ctx context.Context) (*state, error) {
	data, ok, err := w.sink.Checkpoint(ctx, w.cfg.checkpointKey())
	if err != nil {
		return nil, fmt.Errorf("kelos: load checkpoint: %w", err)
	}
	if !ok {
		return w.relist(ctx, nil)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("kelos: decode checkpoint: %w", err)
	}
	return stateOf(cp)
}

// stream is one resource's watch.
type stream struct {
	gvr schema.GroupVersionResource
	// version and seenAt point to the state's resourceVersion of gvr and
	// the time its watch last received something.
	version *string
	seenAt  *time.Time
	watcher watch.Interface
}

// watch follows both resources' watches until ctx is done, one expires
// (errExpired), or an event fails.
func (w *Watcher) watch(ctx context.Context, st *state) error {
	tasks := &stream{gvr: taskGVR, version: &st.tasksVersion, seenAt: &st.tasksSeenAt}
	records := &stream{gvr: recordGVR, version: &st.recordsVersion, seenAt: &st.recordsSeenAt}
	streams := []*stream{tasks, records}
	// Stop is idempotent, so a watch next already stopped can be stopped
	// again here.
	defer func() {
		for _, s := range streams {
			if s.watcher != nil {
				s.watcher.Stop()
			}
		}
	}()
	for _, s := range streams {
		wi, err := w.open(ctx, s)
		if err != nil {
			return err
		}
		s.watcher = wi
	}
	for {
		var err error
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-tasks.watcher.ResultChan():
			err = w.next(ctx, st, tasks, ev, ok)
		case ev, ok := <-records.watcher.ResultChan():
			err = w.next(ctx, st, records, ev, ok)
		}
		if err != nil {
			return err
		}
	}
}

// next handles one receive from s: an event, or the server closing the
// watch, which is reopened from the checkpoint after a pause.
func (w *Watcher) next(ctx context.Context, st *state, s *stream, ev watch.Event, ok bool) error {
	if ok {
		return w.handle(ctx, st, s, ev)
	}
	s.watcher.Stop()
	if err := w.pause(ctx); err != nil {
		return err
	}
	wi, err := w.open(ctx, s)
	if err != nil {
		return err
	}
	s.watcher = wi
	return nil
}

// pause waits reconnectDelay, or until ctx is done.
func (w *Watcher) pause(ctx context.Context) error {
	return w.sleep(ctx, w.reconnectDelay)
}

// sleep waits d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// open starts a watch of s's resource from its resourceVersion, with
// bookmarks, so that a quiet namespace still advances the checkpoint.
func (w *Watcher) open(ctx context.Context, s *stream) (watch.Interface, error) {
	opts := metav1.ListOptions{ResourceVersion: *s.version, AllowWatchBookmarks: true}
	wi, err := w.client.Resource(s.gvr).Namespace(w.cfg.Namespace).Watch(ctx, opts)
	if err != nil {
		return nil, w.watchError(s.gvr, err)
	}
	return wi, nil
}

// watchError wraps a watch's error, marking an expired one errExpired.
func (w *Watcher) watchError(gvr schema.GroupVersionResource, err error) error {
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
		return fmt.Errorf("%w: %s in %s: %w", errExpired, gvr.Resource, w.cfg.Namespace, err)
	}
	return fmt.Errorf("kelos: watch %s in %s: %w", gvr.Resource, w.cfg.Namespace, err)
}

// handle applies one event to the state and commits what it changed.
func (w *Watcher) handle(ctx context.Context, st *state, s *stream, ev watch.Event) error {
	switch ev.Type {
	case watch.Error:
		return w.watchError(s.gvr, apierrors.FromObject(ev.Object))
	case watch.Bookmark:
		u, err := asUnstructured(s.gvr, ev.Object)
		if err != nil {
			return err
		}
		s.advance(u, w.now())
		return w.commit(ctx, st, nil, nil, nil)
	case watch.Added, watch.Modified, watch.Deleted:
		u, err := asUnstructured(s.gvr, ev.Object)
		if err != nil {
			return err
		}
		at := w.now()
		// A half held past the retention joins nothing, however long
		// since the last commit pruned it.
		st.prune(at)
		changed, obs, err := w.cfg.apply(st, s.gvr, changeOf[ev.Type], *u, at)
		if err != nil {
			return err
		}
		s.advance(u, at)
		return w.commit(ctx, st, changed, []model.Event{obs}, nil)
	}
	return fmt.Errorf("kelos: watch %s: unexpected event type %q", s.gvr.Resource, ev.Type)
}

// advance records that the stream received the object at at, and moves
// its resume point to the object's resourceVersion. An object without one
// leaves the resume point: an empty version would restart the watch at the
// current state, skipping what came between.
func (s *stream) advance(u *unstructured.Unstructured, at time.Time) {
	*s.seenAt = at
	if rv := u.GetResourceVersion(); rv != "" {
		*s.version = rv
	}
}

// asUnstructured returns a watch event's object, which the dynamic client
// decodes as unstructured.
func asUnstructured(gvr schema.GroupVersionResource, obj runtime.Object) (*unstructured.Unstructured, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("kelos: watch %s: unexpected object %T", gvr.Resource, obj)
	}
	return u, nil
}

// commit builds the runs of the changed attempts, prunes the state and
// commits them with the observations, the coverage and the checkpoint.
func (w *Watcher) commit(ctx context.Context, st *state, changed []*attempt, obs []model.Event, gaps []model.Gap) error {
	recs, err := w.cfg.records(sortedAttempts(changed))
	if err != nil {
		return err
	}
	read := st.knownUntil()
	recs.Events = append([]model.Event{}, obs...)
	recs.Coverage = []model.Coverage{{
		Source: model.SourceKelos, Instance: w.cfg.Cluster, LastSuccessfulRead: &read, Gaps: gaps,
	}}
	if err := validateBatch(recs); err != nil {
		return err
	}
	st.prune(st.lastSeen())
	data, err := json.Marshal(st.checkpoint(w.cfg))
	if err != nil {
		return fmt.Errorf("kelos: encode checkpoint: %w", err)
	}
	if err := w.sink.Commit(ctx, w.cfg.checkpointKey(), recs, data); err != nil {
		return fmt.Errorf("kelos: commit: %w", err)
	}
	return nil
}

// validateBatch validates the events and coverage a batch adds to its
// runs and correlations, which records validated.
func validateBatch(recs model.Records) error {
	var errs []error
	for _, e := range recs.Events {
		errs = append(errs, e.Validate())
	}
	for _, c := range recs.Coverage {
		errs = append(errs, c.Validate())
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("kelos: %w", err)
	}
	return nil
}
