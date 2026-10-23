package kelos

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/sperano/sillage/internal/model"
)

func TestWatcherListsThenWatchesFromTheListVersion(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning, testStart))
	h.lists.set(recordsResource, "11")
	h.start(t)
	b := h.commit(t)
	if run := onlyRun(t, b.Records); run.Outcome != model.OutcomeRunning || run.Key.UID != uidA {
		t.Errorf("run = %+v, want %s running", run, uidA)
	}
	if len(b.Records.Correlations) != 1 {
		t.Errorf("correlations = %+v, want the task label's", b.Records.Correlations)
	}
	e := onlyEvent(t, b)
	if e.Kind != model.KindRunObserved || e.Details["change"] != "listed" || e.Details["phase"] != "Running" ||
		!e.ObservedAt.Equal(testObservedAt) || e.SourceTime != nil {
		t.Errorf("observation = %+v", e)
	}
	cov := b.Records.Coverage
	if len(cov) != 1 || !cov[0].LastSuccessfulRead.Equal(testObservedAt) || len(cov[0].Gaps) != 0 {
		t.Errorf("coverage = %+v, want read at the list, no gap", cov)
	}
	if cp := b.Checkpoint; cp.TasksVersion != "10" || cp.RecordsVersion != "11" || len(cp.Tasks) != 1 {
		t.Errorf("checkpoint = %+v", cp)
	}
	for resource, want := range map[string]string{tasksResource: "10", recordsResource: "11"} {
		opts := h.watches.next(t, resource).opts
		if opts.ResourceVersion != want || !opts.AllowWatchBookmarks {
			t.Errorf("%s watch options = %+v, want from %s with bookmarks", resource, opts, want)
		}
	}
	for _, a := range h.client.Actions() {
		if verb := a.GetVerb(); verb != "list" && verb != "watch" {
			t.Errorf("action %s %s: the watcher only lists and watches", verb, a.GetResource().Resource)
		}
	}
}

// A lister sees one phase; the watch sees each, as an observation with
// the time it was received, while the run's times stay the controller's.
func TestWatcherObservesPhaseTransitions(t *testing.T) {
	h := newHarness(t, nil)
	_, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhasePending))
	steps := []struct {
		obj     *unstructured.Unstructured
		outcome model.Outcome
	}{
		{taskObj(taskA, uidA, "12", PhaseWaiting), model.OutcomeWaiting},
		{taskObj(taskA, uidA, "13", PhaseRunning, testStart), model.OutcomeRunning},
		{taskObj(taskA, uidA, "14", PhaseSucceeded, testStart, testEnd), model.OutcomeSucceeded},
	}
	ids := map[string]bool{}
	var last model.Run
	for i, step := range steps {
		tasks.Modify(step.obj)
		b := h.commit(t)
		run, e := onlyRun(t, b.Records), onlyEvent(t, b)
		phase := step.obj.Object["status"].(map[string]any)["phase"]
		if run.Outcome != step.outcome || e.Details["phase"] != phase || e.Details["change"] != "modified" {
			t.Errorf("step %d: outcome %s, observation %v, want %s in %s", i, run.Outcome, e.Details, step.outcome, phase)
		}
		if want := testObservedAt.Add(time.Duration(i+1) * testStep); !e.ObservedAt.Equal(want) || e.SourceTime != nil {
			t.Errorf("step %d: observed at %v (source time %v), want %v and none", i, e.ObservedAt, e.SourceTime, want)
		}
		if !strings.Contains(e.Evidence.Note, observedTimeNote) {
			t.Errorf("step %d: evidence note %q does not label the observed time", i, e.Evidence.Note)
		}
		if b.Checkpoint.TasksVersion != step.obj.GetResourceVersion() {
			t.Errorf("step %d: checkpoint at %s, want %s", i, b.Checkpoint.TasksVersion, step.obj.GetResourceVersion())
		}
		ids[e.ID], last = true, run
	}
	if len(ids) != len(steps) {
		t.Errorf("observation ids %v are not distinct", ids)
	}
	if iv := last.Interval; !knownAt(iv.Start, testStart) || !knownAt(iv.End, testEnd) {
		t.Errorf("interval = %+v, want the controller's start and completion", iv)
	}
}

func knownAt(b model.Bound, t time.Time) bool {
	return b.State == model.BoundKnown && b.At != nil && b.At.Equal(t)
}

// checkpointOf is the checkpoint a watcher holding a Running Task at
// resourceVersions 20 and 21 would have committed.
func checkpointOf(t *testing.T) Checkpoint {
	t.Helper()
	task, err := decode[Task](*taskObj(taskA, uidA, "19", PhaseRunning, testStart))
	if err != nil {
		t.Fatal(err)
	}
	return Checkpoint{
		Cluster: testCluster, Namespace: testNamespace, TasksVersion: "20", RecordsVersion: "21",
		LastEventAt: testCreated, Tasks: []WatchedTask{{Task: task}}, Records: []TaskRecord{},
	}
}

// A restarted watcher resumes both watches from the checkpoint without
// listing, and still joins a TaskRecord to the Task it held.
func TestWatcherResumesFromCheckpoint(t *testing.T) {
	cp := checkpointOf(t)
	h := newHarness(t, &cp)
	h.start(t)
	for resource, want := range map[string]string{tasksResource: "20", recordsResource: "21"} {
		call := h.watches.next(t, resource)
		if call.opts.ResourceVersion != want {
			t.Errorf("%s watch from %q, want the checkpoint's %s", resource, call.opts.ResourceVersion, want)
		}
		if resource == recordsResource {
			call.w.Add(recordObj(taskA, uidA, "22"))
		}
	}
	b := h.commit(t)
	run := onlyRun(t, b.Records)
	if run.Outcome != model.OutcomeSucceeded || len(run.Evidence) != 2 || run.Created == nil {
		t.Errorf("run = %+v, want the record joined to the checkpoint's Task", run)
	}
	if b.Checkpoint.RecordsVersion != "22" || b.Checkpoint.TasksVersion != "20" || len(b.Checkpoint.Records) != 1 {
		t.Errorf("checkpoint = %+v", b.Checkpoint)
	}
	if n := h.lists.count(); n != 0 {
		t.Errorf("listed %d times, want a resumed watch only", n)
	}
}

// A watch the server closes is reopened from the last event's
// resourceVersion.
func TestWatcherReconnectsFromTheLastEvent(t *testing.T) {
	h := newHarness(t, nil)
	_, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
	tasks.Modify(taskObj(taskA, uidA, "12", PhaseSucceeded, testStart, testEnd))
	h.commit(t)
	tasks.Stop()
	if call := h.watches.next(t, tasksResource); call.opts.ResourceVersion != "12" {
		t.Errorf("reconnected from %q, want 12", call.opts.ResourceVersion)
	}
	if n := h.lists.count(); n != 2 {
		t.Errorf("%d list requests, want the first two only", n)
	}
}

// expiredStatus is the ERROR event the API server sends on a watch whose
// resourceVersion it compacted.
func expiredStatus() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Status", "status": "Failure",
		"code": int64(410), "reason": string(metav1.StatusReasonExpired), "message": "too old resource version",
	}}
}

// An expired watch, reported in the stream or by refusing the watch,
// relists. A Task gone from the list keeps its run as deleted at an unknown
// time, and the coverage gets a "watch expired" gap from the last event to
// the relist.
func TestWatcherRelistsAnExpiredWatch(t *testing.T) {
	for name, expire := range map[string]func(*harness, *watch.FakeWatcher){
		"error event": func(_ *harness, w *watch.FakeWatcher) { w.Error(expiredStatus()) },
		"refused watch": func(h *harness, w *watch.FakeWatcher) {
			h.watches.fail(tasksResource, apierrors.NewResourceExpired("too old resource version"))
			w.Stop()
		},
	} {
		t.Run(name, func(t *testing.T) {
			testRelist(t, expire)
		})
	}
}

func testRelist(t *testing.T, expire func(*harness, *watch.FakeWatcher)) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning), taskObj(taskB, uidB, "6", PhaseRunning))
	h.lists.set(recordsResource, "11")
	h.start(t)
	h.commit(t)
	tasks := h.watches.next(t, tasksResource).w
	h.watches.next(t, recordsResource)
	tasks.Modify(taskObj(taskA, uidA, "12", PhaseRunning, testStart))
	lastEvent := h.commit(t).Checkpoint.LastEventAt

	h.lists.set(tasksResource, "30", taskObj(taskA, uidA, "25", PhaseSucceeded, testStart, testEnd))
	h.lists.set(recordsResource, "31")
	expire(h, tasks)
	b := h.commit(t)
	gaps := b.Records.Coverage[0].Gaps
	relist := *b.Records.Coverage[0].LastSuccessfulRead
	if len(gaps) != 1 || gaps[0].Reason != GapWatchExpired || !gaps[0].Start.Equal(lastEvent) || !gaps[0].End.Equal(relist) {
		t.Errorf("gaps = %+v, want watch expired from %v to %v", gaps, lastEvent, relist)
	}
	runs := map[string]model.Run{}
	for _, r := range b.Records.Runs {
		runs[r.Key.UID] = r
	}
	if runs[uidA].Outcome != model.OutcomeSucceeded {
		t.Errorf("relisted run = %+v, want succeeded", runs[uidA])
	}
	gone := runs[uidB]
	if gone.Outcome != model.OutcomeUnknown || gone.Created == nil || !strings.Contains(gone.Evidence[0].Note, deletedRelistNote) {
		t.Errorf("run of the Task gone from the list = %+v, want kept, deleted at an unknown time", gone)
	}
	if cp := b.Checkpoint; cp.TasksVersion != "30" || len(cp.Tasks) != 2 || cp.Tasks[1].Deleted == nil || cp.Tasks[1].Deleted.Seen {
		t.Errorf("checkpoint = %+v, want the relist's version and the deleted Task kept", cp)
	}
	for resource, want := range map[string]string{tasksResource: "30", recordsResource: "31"} {
		if call := h.watches.next(t, resource); call.opts.ResourceVersion != want {
			t.Errorf("%s watch after the relist from %q, want %s", resource, call.opts.ResourceVersion, want)
		}
	}
}

// A deleted Task keeps its run, with an unknown outcome when it never
// finished, and a TaskRecord arriving later completes it.
func TestWatcherKeepsADeletedTasksRun(t *testing.T) {
	h := newHarness(t, nil)
	_, tasks, records := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning, testStart))
	tasks.Delete(taskObj(taskA, uidA, "12", PhaseRunning, testStart))
	b := h.commit(t)
	run := onlyRun(t, b.Records)
	if run.Outcome != model.OutcomeUnknown || run.Interval.End.State != model.BoundUnknown ||
		run.Created == nil || !strings.Contains(run.Evidence[0].Note, deletedSeenNote) {
		t.Errorf("run after the deletion = %+v, want kept with an unknown end", run)
	}
	if e := onlyEvent(t, b); e.Details["change"] != "deleted" {
		t.Errorf("observation = %v, want the deletion", e.Details)
	}
	if cp := b.Checkpoint; len(cp.Tasks) != 1 || cp.Tasks[0].Deleted == nil || !cp.Tasks[0].Deleted.Seen {
		t.Errorf("checkpoint = %+v, want the deleted Task held for its record", cp)
	}
	records.Add(recordObj(taskA, uidA, "13"))
	b = h.commit(t)
	run = onlyRun(t, b.Records)
	if run.Outcome != model.OutcomeSucceeded || run.Usage == nil || run.Created == nil || len(run.Evidence) != 2 {
		t.Errorf("run with the record = %+v, want the deleted Task and its record merged", run)
	}
	if cp := b.Checkpoint; len(cp.Tasks) != 0 || len(cp.Records) != 0 {
		t.Errorf("checkpoint = %+v, want the complete run released", cp)
	}
}

// A deleted TaskRecord changes no run: the run keeps the record's values.
func TestWatcherRecordDeletionKeepsTheRun(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseSucceeded, testStart, testEnd))
	h.lists.set(recordsResource, "11", recordObj(taskA, uidA, "6"))
	h.start(t)
	h.commit(t)
	h.watches.next(t, tasksResource)
	h.watches.next(t, recordsResource).w.Delete(recordObj(taskA, uidA, "12"))
	b := h.commit(t)
	if len(b.Records.Runs) != 0 {
		t.Errorf("runs = %+v, want none changed", b.Records.Runs)
	}
	if e := onlyEvent(t, b); e.Details["change"] != "deleted" || e.Details["object"] != objectRecord {
		t.Errorf("observation = %v", e.Details)
	}
	if cp := b.Checkpoint; cp.RecordsVersion != "12" || len(cp.Records) != 1 {
		t.Errorf("checkpoint = %+v, want the record still joined to its live Task", cp)
	}
}

// A bookmark advances the checkpoint and the coverage, and nothing else.
func TestWatcherBookmarkAdvancesTheCheckpoint(t *testing.T) {
	h := newHarness(t, nil)
	listed, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
	bookmark := object("Task", "", "")
	bookmark.SetResourceVersion("50")
	tasks.Action(watch.Bookmark, bookmark)
	b := h.commit(t)
	if len(b.Records.Runs)+len(b.Records.Events) != 0 {
		t.Errorf("bookmark batch = %+v, want no record", b.Records)
	}
	if b.Checkpoint.TasksVersion != "50" || !b.Checkpoint.LastEventAt.After(listed.Checkpoint.LastEventAt) {
		t.Errorf("checkpoint = %+v, want version 50 and a later event time", b.Checkpoint)
	}
	if read := b.Records.Coverage[0].LastSuccessfulRead; !read.Equal(b.Checkpoint.LastEventAt) {
		t.Errorf("coverage read at %v, want the bookmark's %v", read, b.Checkpoint.LastEventAt)
	}
}

// Any other failure stops the watcher with the committed checkpoint
// unchanged, for the caller to run it again.
func TestWatcherStopsOnFailure(t *testing.T) {
	failures := map[string]struct {
		fail func(*harness, *watch.FakeWatcher)
		want string
	}{
		"refused watch": {func(h *harness, w *watch.FakeWatcher) {
			h.watches.fail(tasksResource, apierrors.NewForbidden(schema.GroupResource{Group: apiGroup, Resource: tasksResource}, "", errors.New("no watch verb")))
			w.Stop()
		}, "watch tasks in agents"},
		"error event": {func(_ *harness, w *watch.FakeWatcher) {
			w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 500, Message: "etcd unavailable"})
		}, "etcd unavailable"},
		"commit": {func(h *harness, w *watch.FakeWatcher) {
			h.sink.mu.Lock()
			h.sink.err = errors.New("database down")
			h.sink.mu.Unlock()
			w.Modify(taskObj(taskA, uidA, "12", PhaseSucceeded))
		}, "kelos: commit: database down"},
		"task without uid": {func(_ *harness, w *watch.FakeWatcher) {
			w.Modify(taskObj(taskA, "", "12", PhaseSucceeded))
		}, "no uid"},
	}
	for name, f := range failures {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			_, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
			f.fail(h, tasks)
			err := h.stopped(t)
			if err == nil || errors.Is(err, errExpired) || !strings.Contains(err.Error(), f.want) {
				t.Errorf("Run error = %v, want one containing %q", err, f.want)
			}
			if cp, _, _ := h.sink.Checkpoint(t.Context(), testCluster, testNamespace); cp.TasksVersion != "10" {
				t.Errorf("checkpoint at %s, want the list's 10", cp.TasksVersion)
			}
		})
	}
}

func TestWatcherStopsWhenCancelled(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning))
	w, err := NewWatcher(h.client, testConfig(), h.sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}

func TestNewWatcherRejects(t *testing.T) {
	sink := newFakeSink(t, nil)
	bad := testConfig()
	bad.Cluster = ""
	for name, f := range map[string]func() (*Watcher, error){
		"nil client":     func() (*Watcher, error) { return NewWatcher(nil, testConfig(), sink) },
		"nil sink":       func() (*Watcher, error) { return NewWatcher(newFakeClient(), testConfig(), nil) },
		"invalid config": func() (*Watcher, error) { return NewWatcher(newFakeClient(), bad, sink) },
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s: NewWatcher accepted it", name)
		}
	}
}

// The resources a Watcher reads are exactly the two RBAC names.
func TestWatcherResources(t *testing.T) {
	got := []string{taskGVR.Resource, recordGVR.Resource}
	if want := []string{"tasks", "taskrecords"}; !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
}
