package kelos

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/sperano/sillage/internal/model"
)

// change is how a list or watch saw an object.
type change string

// The changes of a run.observed event's "change" detail.
const (
	changeListed   change = "listed"
	changeAdded    change = "added"
	changeModified change = "modified"
	changeDeleted  change = "deleted"
)

// changeOf maps the watch events that carry an object to their change.
var changeOf = map[watch.EventType]change{
	watch.Added:    changeAdded,
	watch.Modified: changeModified,
	watch.Deleted:  changeDeleted,
}

const (
	// The values of a run.observed event's "object" detail.
	objectTask   = "task"
	objectRecord = "taskrecord"
	// observedTimeNote labels a run.observed event's time.
	observedTimeNote = "observed time is when sillage received it, not when the object changed; " +
		"the run's times are the controller's lifecycle times"
)

// observed is one object as a list or watch saw it.
type observed struct {
	resource, object, ref string
	meta                  metav1.ObjectMeta
	// run is the UID of the Task the object belongs to.
	run   types.UID
	phase Phase
	// pod is the Task's status.podName: its latest pod, empty for a
	// TaskRecord, which names none.
	pod    string
	change change
	at     time.Time
}

func (c Config) observeTask(t *Task, ch change, at time.Time) model.Event {
	return c.observation(observed{
		resource: tasksResource, object: objectTask, ref: c.taskRef(t.Name),
		meta: t.ObjectMeta, run: t.UID, phase: t.Status.Phase, pod: t.Status.PodName, change: ch, at: at,
	})
}

func (c Config) observeRecord(r *TaskRecord, ch change, at time.Time) model.Event {
	return c.observation(observed{
		resource: recordsResource, object: objectRecord, ref: c.recordRef(r.Name),
		meta: r.ObjectMeta, run: r.Spec.TaskRef.UID, phase: r.Spec.Phase, change: ch, at: at,
	})
}

// observation is a run.observed event, with the Task's pod at the time,
// so that every pod a Task had is kept, not only the latest one its
// status names: the Job's retry replaces the first pod with a second. Its
// id is the object's UID and
// resourceVersion, which the API server gives each change, with the
// change: a relist observing an unchanged object gives it the id it had,
// and a resumed watch never repeats an event. Without a resourceVersion
// the id is a fingerprint of the time it was received.
func (c Config) observation(o observed) model.Event {
	run := model.RunKey{Cluster: c.Cluster, Namespace: c.Namespace, UID: string(o.run)}
	rv := o.meta.ResourceVersion
	e := model.Event{
		Source: model.SourceKelos, Instance: c.Cluster,
		ID:     fmt.Sprintf("%s/%s@%s/%s", o.resource, o.meta.UID, rv, o.change),
		IDKind: model.IDSource, Kind: model.KindRunObserved,
		ObservedAt: o.at,
		Actor:      model.Account{Kind: model.ActorUnknown},
		Related:    model.Related{Run: &run},
		Details:    map[string]string{"object": o.object, "change": string(o.change)},
		Evidence: model.Evidence{
			Source: model.SourceKelos, Instance: c.Cluster, Ref: o.ref,
			Note: fmt.Sprintf("%s at resourceVersion %s; %s", o.change, rv, observedTimeNote),
		},
	}
	if rv == "" {
		e.ID = model.Fingerprint(o.at, o.resource+"/"+string(o.meta.UID), string(o.change), string(o.phase))
		e.IDKind = model.IDFingerprint
		e.Evidence.Note = fmt.Sprintf("%s without a resourceVersion; %s", o.change, observedTimeNote)
	} else {
		e.Details["resourceVersion"] = rv
	}
	if o.phase != "" {
		e.Details["phase"] = string(o.phase)
	}
	if o.pod != "" {
		e.Details["pod"] = o.pod
	}
	return e
}

// listed is an observation of every object a list read.
func (c Config) listed(s Snapshot) []model.Event {
	out := make([]model.Event, 0, len(s.Tasks)+len(s.Records))
	for i := range s.Tasks {
		out = append(out, c.observeTask(&s.Tasks[i], changeListed, s.ObservedAt))
	}
	for i := range s.Records {
		out = append(out, c.observeRecord(&s.Records[i], changeListed, s.ObservedAt))
	}
	return out
}

// apply applies one watch event's object to the state. It returns the
// attempt whose run changed, if any, and the event's observation.
func (c Config) apply(st *state, gvr schema.GroupVersionResource, ch change, u unstructured.Unstructured,
	at time.Time) ([]*attempt, model.Event, error) {
	if gvr == taskGVR {
		return c.applyTask(st, ch, u, at)
	}
	return c.applyRecord(st, ch, u, at)
}

// applyTask sets the Task's attempt to its new state. A deleted Task stays,
// marked deleted, with its last state: its run keeps the Task's fields,
// and a TaskRecord arriving later still joins it.
func (c Config) applyTask(st *state, ch change, u unstructured.Unstructured, at time.Time) ([]*attempt, model.Event, error) {
	t, err := decode[Task](u)
	if err != nil {
		return nil, model.Event{}, err
	}
	if t.UID == "" {
		return nil, model.Event{}, fmt.Errorf("kelos: task %s: no uid", c.taskRef(t.Name))
	}
	a := st.attempt(t.UID)
	a.task = &t
	if ch == changeDeleted {
		a.deleted = &Deletion{At: at, Seen: true}
	}
	return []*attempt{a}, c.observeTask(&t, ch, at), nil
}

// applyRecord sets the attempt's TaskRecord. A record whose Task the
// state does not hold is held for that Task, which a backlog replayed
// after a restart can deliver after it. A deleted record leaves its run as
// it was: the run keeps the record's values. Only the observation says it
// is gone.
func (c Config) applyRecord(st *state, ch change, u unstructured.Unstructured, at time.Time) ([]*attempt, model.Event, error) {
	r, err := decode[TaskRecord](u)
	if err != nil {
		return nil, model.Event{}, err
	}
	uid := r.Spec.TaskRef.UID
	if uid == "" {
		return nil, model.Event{}, fmt.Errorf("kelos: taskrecord %s: no spec.taskRef.uid", c.recordRef(r.Name))
	}
	obs := c.observeRecord(&r, ch, at)
	if ch == changeDeleted {
		return nil, obs, nil
	}
	a := st.attempt(uid)
	if a.task == nil && a.awaitingTaskSince.IsZero() {
		a.awaitingTaskSince = at
	}
	a.record = &r
	return []*attempt{a}, obs, nil
}
