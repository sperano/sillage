package kelos

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/sperano/sillage/internal/model"
)

func TestWatcherListsThenWatchesFromTheListVersion(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning, testStart))
	h.lists.set(recordsResource, "11")
	h.start(t, 0)
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
	for _, rw := range []struct{ resource, want string }{{tasksResource, "10"}, {recordsResource, "11"}} {
		resource, want := rw.resource, rw.want
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
		TasksVersion: "20", RecordsVersion: "21",
		TasksSeenAt: testCreated, RecordsSeenAt: testCreated,
		Tasks: []WatchedTask{{Task: task}}, Records: []WatchedRecord{},
	}
}

// A restarted watcher resumes both watches from the checkpoint without
// listing, and still joins a TaskRecord to the Task it held.
func TestWatcherResumesFromCheckpoint(t *testing.T) {
	cp := checkpointOf(t)
	h := newHarness(t, &cp)
	h.start(t, 0)
	for _, rw := range []struct{ resource, want string }{{tasksResource, "20"}, {recordsResource, "21"}} {
		resource, want := rw.resource, rw.want
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

// A record the watch delivers before its Task, as a backlog replayed after
// a restart does, is held until the Task arrives, then joins it.
func TestWatcherJoinsARecordBeforeItsTask(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10")
	h.lists.set(recordsResource, "11")
	h.start(t, 0)
	h.commit(t)
	tasks, records := h.watches.next(t, tasksResource).w, h.watches.next(t, recordsResource).w
	records.Add(recordObj(taskA, uidA, "12"))
	b := h.commit(t)
	if run := onlyRun(t, b.Records); !strings.Contains(run.Evidence[0].Note, awaitingTaskNote) {
		t.Errorf("record-only run evidence = %q, want the Task not held yet", run.Evidence[0].Note)
	}
	if cp := b.Checkpoint; len(cp.Records) != 1 || cp.Records[0].AwaitingTaskSince == nil {
		t.Errorf("checkpoint = %+v, want the record held for its Task", cp)
	}
	tasks.Add(taskObj(taskA, uidA, "13", PhaseRunning, testStart))
	assertJoined(t, h.commit(t))
	tasks.Delete(taskObj(taskA, uidA, "14", PhaseSucceeded, testStart, testEnd))
	b = h.commit(t)
	assertJoined(t, b)
	if cp := b.Checkpoint; len(cp.Tasks)+len(cp.Records) != 0 {
		t.Errorf("checkpoint = %+v, want the complete run released", cp)
	}
}

// The held record survives a restart.
func TestWatcherJoinsAHeldRecordAfterARestart(t *testing.T) {
	since := testCreated
	cp := Checkpoint{TasksVersion: "20", RecordsVersion: "21", TasksSeenAt: testCreated, RecordsSeenAt: testCreated,
		Tasks:   []WatchedTask{},
		Records: []WatchedRecord{{Record: *decodedRecord(t, uidA), AwaitingTaskSince: &since}}}
	h := newHarness(t, &cp)
	h.start(t, 0)
	h.watches.next(t, tasksResource).w.Add(taskObj(taskA, uidA, "22", PhaseRunning, testStart))
	h.watches.next(t, recordsResource)
	assertJoined(t, h.commit(t))
}

// assertJoined checks the batch's run merges the Task and its record.
func assertJoined(t *testing.T, b batch) {
	t.Helper()
	run := onlyRun(t, b.Records)
	if run.Outcome != model.OutcomeSucceeded || run.Usage == nil || run.Created == nil || len(run.Evidence) != 2 {
		t.Errorf("run = %+v, want the Task and its record joined", run)
	}
}

// A deleted TaskRecord changes no run: the run keeps the record's values.
func TestWatcherRecordDeletionKeepsTheRun(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseSucceeded, testStart, testEnd))
	h.lists.set(recordsResource, "11", recordObj(taskA, uidA, "6"))
	h.start(t, 0)
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

// A deleted Task held past the join retention joins nothing: a record
// arriving later builds a run of its own, which the store completes.
func TestWatcherRecordAfterTheRetentionJoinsNothing(t *testing.T) {
	cp := checkpointOf(t)
	cp.Tasks[0].Deleted = &Deletion{At: testObservedAt.Add(-joinRetention - testStep), Seen: true}
	h := newHarness(t, &cp)
	h.start(t, 0)
	h.watches.next(t, tasksResource)
	h.watches.next(t, recordsResource).w.Add(recordObj(taskA, uidA, "22"))
	b := h.commit(t)
	if run := onlyRun(t, b.Records); len(run.Evidence) != 1 || !strings.Contains(run.Evidence[0].Note, awaitingTaskNote) {
		t.Errorf("run = %+v, want the record alone", run)
	}
	if cp := b.Checkpoint; len(cp.Tasks) != 0 || len(cp.Records) != 1 {
		t.Errorf("checkpoint = %+v, want the expired Task released and the record held", cp)
	}
}

// A bookmark advances the checkpoint and the coverage, and nothing else.
func TestWatcherBookmarkAdvancesTheCheckpoint(t *testing.T) {
	h := newHarness(t, nil)
	listed, tasks, records := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
	bookmark := object("Task", "", "")
	bookmark.SetResourceVersion("50")
	tasks.Action(watch.Bookmark, bookmark)
	b := h.commit(t)
	if len(b.Records.Runs)+len(b.Records.Events) != 0 {
		t.Errorf("bookmark batch = %+v, want no record", b.Records)
	}
	if b.Checkpoint.TasksVersion != "50" || !b.Checkpoint.TasksSeenAt.After(listed.Checkpoint.TasksSeenAt) {
		t.Errorf("checkpoint = %+v, want version 50 and a later event time", b.Checkpoint)
	}
	// The records watch still stands at the list: the namespace is known
	// up to the slower watch.
	if read := b.Records.Coverage[0].LastSuccessfulRead; !read.Equal(*listed.Records.Coverage[0].LastSuccessfulRead) {
		t.Errorf("coverage read at %v, want the list's %v", read, listed.Records.Coverage[0].LastSuccessfulRead)
	}
	recordsBookmark := object("TaskRecord", "", "")
	recordsBookmark.SetResourceVersion("51")
	records.Action(watch.Bookmark, recordsBookmark)
	b = h.commit(t)
	if read := b.Records.Coverage[0].LastSuccessfulRead; !read.Equal(b.Checkpoint.TasksSeenAt) {
		t.Errorf("coverage read at %v, want the Task bookmark's %v", read, b.Checkpoint.TasksSeenAt)
	}
}

// Any other failure stops the watcher with the committed checkpoint
// unchanged, for the caller to run it again.
func TestWatcherStopsOnFailure(t *testing.T) {
	tests := []struct {
		name string
		fail func(*harness, *watch.FakeWatcher)
		want string
	}{
		{"refused watch", func(h *harness, w *watch.FakeWatcher) {
			forbidden := apierrors.NewForbidden(schema.GroupResource{Group: apiGroup, Resource: tasksResource}, "", errors.New("no watch verb"))
			h.watches.fail(tasksResource, forbidden)
			w.Stop()
		}, "watch tasks in agents"},
		{"error event", func(_ *harness, w *watch.FakeWatcher) {
			w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: http.StatusInternalServerError, Message: "etcd unavailable"})
		}, "etcd unavailable"},
		{"commit", func(h *harness, w *watch.FakeWatcher) {
			h.sink.setErr(errors.New("database down"))
			w.Modify(taskObj(taskA, uidA, "12", PhaseSucceeded))
		}, "kelos: commit: database down"},
		{"task without uid", func(_ *harness, w *watch.FakeWatcher) {
			w.Modify(taskObj(taskA, "", "12", PhaseSucceeded))
		}, "no uid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			_, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
			tt.fail(h, tasks)
			err := h.stopped(t)
			if err == nil || errors.Is(err, errExpired) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Run error = %v, want one containing %q", err, tt.want)
			}
			if cp := h.sink.stored(t); cp.TasksVersion != "10" {
				t.Errorf("checkpoint at %s, want the list's 10", cp.TasksVersion)
			}
		})
	}
}

// Cancelling stops the watcher while it waits for events, and while it
// pauses before reopening a closed watch.
func TestWatcherStopsWhenCancelled(t *testing.T) {
	tests := []struct {
		name  string
		close bool
	}{
		{"waiting for events", false},
		{"pausing to reconnect", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning))
			h.lists.set(recordsResource, "11")
			w, err := NewWatcher(h.client, testConfig(), h.sink)
			if err != nil {
				t.Fatal(err)
			}
			paused := make(chan struct{})
			w.sleep = func(ctx context.Context, _ time.Duration) error {
				close(paused)
				<-ctx.Done()
				return ctx.Err()
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			h.commit(t)
			tasks := h.watches.next(t, tasksResource).w
			h.watches.next(t, recordsResource)
			if tt.close {
				tasks.Stop()
				select {
				case <-paused:
				case <-time.After(testTimeout):
					t.Fatal("the watcher did not pause")
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("Run error = %v, want context.Canceled", err)
				}
			case <-time.After(testTimeout):
				t.Fatal("the watcher did not stop")
			}
		})
	}
}

// A list without a resourceVersion gives nothing to watch from: the
// watcher stops rather than watch from the current state, which would skip
// what came between.
func TestWatcherRefusesAListWithoutVersion(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "", taskObj(taskA, uidA, "5", PhaseRunning))
	h.lists.set(recordsResource, "11")
	h.start(t, 0)
	if err := h.stopped(t); err == nil || !strings.Contains(err.Error(), "no resourceVersion") {
		t.Errorf("Run error = %v, want the list refused", err)
	}
}

// An event without a resourceVersion leaves the resume point.
func TestWatcherKeepsVersionOfAnEventWithoutOne(t *testing.T) {
	h := newHarness(t, nil)
	_, tasks, _ := h.listed(t, taskObj(taskA, uidA, "5", PhaseRunning))
	tasks.Action(watch.Bookmark, object("Task", "", ""))
	if b := h.commit(t); b.Checkpoint.TasksVersion != "10" {
		t.Errorf("checkpoint at %q after a bookmark without a version, want the list's 10", b.Checkpoint.TasksVersion)
	}
}

func TestNewWatcherRejects(t *testing.T) {
	sink := newFakeSink(t, nil)
	bad := testConfig()
	bad.Cluster = ""
	tests := []struct {
		name   string
		client *dynamicfake.FakeDynamicClient
		cfg    Config
		sink   Sink
	}{
		{"nil client", nil, testConfig(), sink},
		{"nil sink", newFakeClient(), testConfig(), nil},
		{"invalid config", newFakeClient(), bad, sink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client dynamic.Interface
			if tt.client != nil {
				client = tt.client
			}
			if _, err := NewWatcher(client, tt.cfg, tt.sink); err == nil {
				t.Error("NewWatcher accepted it")
			}
		})
	}
}

// The resources a Watcher reads are exactly the two RBAC names.
func TestWatcherResources(t *testing.T) {
	got := []string{taskGVR.Resource, recordGVR.Resource}
	if want := []string{"tasks", "taskrecords"}; !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
}

// The default sleep waits its delay, and stops early when cancelled.
func TestSleep(t *testing.T) {
	if err := sleep(t.Context(), 0); err != nil {
		t.Errorf("sleep = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled sleep = %v, want context.Canceled", err)
	}
}
