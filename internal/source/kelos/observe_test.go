package kelos

import (
	"fmt"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestObservationIDsTheChange(t *testing.T) {
	cfg := testConfig()
	task := decodedTask(t, uidA, PhaseRunning)
	e := cfg.observeTask(task, changeModified, testObservedAt)
	if e.IDKind != model.IDSource || e.ID != "tasks/"+uidA+"@5/modified" {
		t.Errorf("id = %s %q", e.IDKind, e.ID)
	}
	if e.Related.Run == nil || e.Related.Run.UID != uidA || e.Evidence.Ref != cfg.taskRef(taskA) {
		t.Errorf("observation = %+v, want it on the Task's run", e)
	}
	if listed := cfg.observeTask(task, changeListed, testObservedAt); listed.ID == e.ID {
		t.Error("a listed and a modified observation share an id")
	}
	if err := e.Validate(); err != nil {
		t.Error(err)
	}
}

// A record's observation is on its Task's run, under the record's own id.
func TestRecordObservationIsOnItsTasksRun(t *testing.T) {
	e := testConfig().observeRecord(decodedRecord(t, uidA), changeAdded, testObservedAt)
	if e.Related.Run.UID != uidA || e.ID != "taskrecords/record-"+uidA+"@6/added" ||
		e.Details["object"] != objectRecord || e.Details["phase"] != string(PhaseSucceeded) {
		t.Errorf("observation = %+v", e)
	}
}

// Without a resourceVersion, the id is a fingerprint of when the object
// was received.
func TestObservationWithoutResourceVersion(t *testing.T) {
	task := decodedTask(t, uidA, PhaseRunning)
	task.ResourceVersion = ""
	e := testConfig().observeTask(task, changeAdded, testObservedAt)
	if e.IDKind != model.IDFingerprint || e.Details["resourceVersion"] != "" {
		t.Errorf("observation = %+v, want a fingerprint id", e)
	}
	if err := e.Validate(); err != nil {
		t.Error(err)
	}
	if later := testConfig().observeTask(task, changeAdded, testObservedAt.Add(testStep)); later.ID == e.ID {
		t.Error("two receptions share a fingerprint")
	}
	task.Status.PodName = "pod-retry"
	if retried := testConfig().observeTask(task, changeAdded, testObservedAt); retried.ID == e.ID {
		t.Error("two pods received at one instant share a fingerprint")
	}
}

// retryVersion is the resourceVersion of the Task once it names its first
// pod, after the one decodedTask gives it.
const retryVersion = 7

// Each observation of a Task names the pod its status named then, so that
// a retried Job's first pod is kept once the second replaced it; a
// TaskRecord names none.
func TestObservationNamesTheTasksPod(t *testing.T) {
	cfg := testConfig()
	task := decodedTask(t, uidA, PhaseRunning)
	if e := cfg.observeTask(task, changeAdded, testObservedAt); e.Details["pod"] != "" {
		t.Errorf("pod = %q before the Task named one", e.Details["pod"])
	}
	var pods, ids []string
	for i, pod := range []string{"pod-first", "pod-retry"} {
		task.Status.PodName = pod
		task.ResourceVersion = fmt.Sprint(retryVersion + i)
		e := cfg.observeTask(task, changeModified, testObservedAt)
		pods, ids = append(pods, e.Details["pod"]), append(ids, e.ID)
	}
	if pods[0] != "pod-first" || pods[1] != "pod-retry" {
		t.Errorf("pods = %q, want each observation's own", pods)
	}
	if ids[0] == ids[1] {
		t.Errorf("both pods' observations have id %q: the store would keep one", ids[0])
	}
	if e := cfg.observeRecord(decodedRecord(t, uidA), changeAdded, testObservedAt); e.Details["pod"] != "" {
		t.Errorf("a record's observation names pod %q", e.Details["pod"])
	}
}
