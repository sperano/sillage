package kelos

import (
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/sperano/sillage/internal/model"
)

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
// time, and the coverage gets a "watch expired" gap from where the slower
// watch stood to the relist.
func TestWatcherRelistsAnExpiredWatch(t *testing.T) {
	tests := []struct {
		name   string
		expire func(*harness, *watch.FakeWatcher)
	}{
		{"error event", func(_ *harness, w *watch.FakeWatcher) { w.Error(expiredStatus()) }},
		{"refused watch", func(h *harness, w *watch.FakeWatcher) {
			h.watches.fail(tasksResource, apierrors.NewResourceExpired("too old resource version"))
			w.Stop()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testRelist(t, tt.expire)
		})
	}
}

func testRelist(t *testing.T, expire func(*harness, *watch.FakeWatcher)) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseRunning), taskObj(taskB, uidB, "6", PhaseRunning))
	h.lists.set(recordsResource, "11")
	h.start(t, 0)
	h.commit(t)
	tasks := h.watches.next(t, tasksResource).w
	h.watches.next(t, recordsResource)
	tasks.Modify(taskObj(taskA, uidA, "12", PhaseRunning, testStart))
	// The records watch has received nothing since the list: the gap
	// starts there, not at the later Task event.
	recordsSeen := h.commit(t).Checkpoint.RecordsSeenAt

	h.lists.set(tasksResource, "30", taskObj(taskA, uidA, "25", PhaseSucceeded, testStart, testEnd))
	h.lists.set(recordsResource, "31")
	expire(h, tasks)
	b := h.commit(t)
	gaps := b.Records.Coverage[0].Gaps
	relist := *b.Records.Coverage[0].LastSuccessfulRead
	if len(gaps) != 1 || gaps[0].Reason != GapWatchExpired || !gaps[0].Start.Equal(recordsSeen) || !gaps[0].End.Equal(relist) {
		t.Errorf("gaps = %+v, want watch expired from %v to %v", gaps, recordsSeen, relist)
	}
	if len(gaps) == 1 && b.Records.Coverage[0].Known(gaps[0].Start.Add(gaps[0].End.Sub(gaps[0].Start)/2)) {
		t.Error("the expired watch's gap is reported as known instead of unknown")
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
	for _, rw := range []struct{ resource, want string }{{tasksResource, "30"}, {recordsResource, "31"}} {
		resource, want := rw.resource, rw.want
		if call := h.watches.next(t, resource); call.opts.ResourceVersion != want {
			t.Errorf("%s watch after the relist from %q, want %s", resource, call.opts.ResourceVersion, want)
		}
	}
}

// A TaskRecord deleted while the watch was expired keeps its values in the
// run of its Task, still listed after the relist.
func TestWatcherRelistKeepsADeletedRecord(t *testing.T) {
	h := newHarness(t, nil)
	h.lists.set(tasksResource, "10", taskObj(taskA, uidA, "5", PhaseSucceeded, testStart, testEnd))
	h.lists.set(recordsResource, "11", recordObj(taskA, uidA, "6"))
	h.start(t, 0)
	h.commit(t)
	tasks := h.watches.next(t, tasksResource).w
	h.watches.next(t, recordsResource)
	h.lists.set(recordsResource, "31")
	tasks.Error(expiredStatus())
	b := h.commit(t)
	assertJoined(t, b)
	if cp := b.Checkpoint; len(cp.Tasks) != 1 || len(cp.Records) != 1 {
		t.Errorf("checkpoint = %+v, want the record still joined to its live Task", cp)
	}
}
