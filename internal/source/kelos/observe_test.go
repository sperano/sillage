package kelos

import (
	"strings"
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
	if e.Related.Run.UID != uidA || !strings.HasPrefix(e.ID, "taskrecords/record-"+uidA+"@6/") ||
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
}
