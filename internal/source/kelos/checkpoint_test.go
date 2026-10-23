package kelos

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func decodedTask(t *testing.T, uid string, phase Phase) *Task {
	t.Helper()
	task, err := decode[Task](*taskObj(taskA, uid, "5", phase, testStart))
	if err != nil {
		t.Fatal(err)
	}
	return &task
}

func decodedRecord(t *testing.T, uid string) *TaskRecord {
	t.Helper()
	r, err := decode[TaskRecord](*recordObj(taskA, uid, "6"))
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

// A checkpoint persisted as JSON restores the state it was taken from.
func TestCheckpointRoundTrip(t *testing.T) {
	st := &state{
		attempts: map[types.UID]*attempt{
			uidA: {uid: uidA, task: decodedTask(t, uidA, PhaseSucceeded), record: decodedRecord(t, uidA)},
			uidB: {uid: uidB, task: decodedTask(t, uidB, PhaseRunning), deleted: &Deletion{At: testEnd, Seen: true}},
			"c":  {uid: "c", record: decodedRecord(t, "c"), awaitingTaskSince: testEnd},
		},
		tasksVersion: "20", recordsVersion: "21", tasksSeenAt: testEnd, recordsSeenAt: testStart,
	}
	data, err := json.Marshal(st.checkpoint(testConfig()))
	if err != nil {
		t.Fatal(err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.Tasks[0].Task.UID != uidA {
		t.Errorf("checkpoint = %+v, want its Tasks in uid order", cp)
	}
	got, err := stateOf(cp)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.checkpoint(testConfig()), st.checkpoint(testConfig())) {
		t.Errorf("restored state differs:\n%+v\nwant\n%+v", got.checkpoint(testConfig()), st.checkpoint(testConfig()))
	}
}

func TestStateOfRejectsInconsistentCheckpoint(t *testing.T) {
	seen := func(cp Checkpoint) Checkpoint {
		cp.TasksSeenAt, cp.RecordsSeenAt = testStart, testStart
		return cp
	}
	tests := []struct {
		name string
		cp   Checkpoint
	}{
		{"task without uid", seen(Checkpoint{Tasks: []WatchedTask{{Task: *decodedTask(t, "", PhaseRunning)}}})},
		{"record without task", seen(Checkpoint{Records: []WatchedRecord{{Record: *decodedRecord(t, uidA)}}})},
		{"no watch times", Checkpoint{RecordsSeenAt: testStart}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := stateOf(tt.cp); err == nil || !strings.Contains(err.Error(), "checkpoint") {
				t.Errorf("stateOf error = %v", err)
			}
		})
	}
}

// prune releases what no later event can change, and holds a deleted Task
// without a record for the retention only.
func TestStatePrune(t *testing.T) {
	deletedAt := testEnd
	st := &state{attempts: map[types.UID]*attempt{
		"live":           {task: decodedTask(t, "live", PhaseRunning)},
		"record-only":    {record: decodedRecord(t, "record-only")},
		"deleted":        {task: decodedTask(t, "deleted", PhaseRunning), deleted: &Deletion{At: deletedAt}},
		"deleted-record": {task: decodedTask(t, "deleted-record", PhaseSucceeded), record: decodedRecord(t, "deleted-record"), deleted: &Deletion{At: deletedAt}},
		"awaiting":       {record: decodedRecord(t, "awaiting"), awaitingTaskSince: deletedAt},
	}}
	st.prune(deletedAt.Add(joinRetention))
	if got := len(st.attempts); got != 3 || st.attempts["live"] == nil || st.attempts["deleted"] == nil || st.attempts["awaiting"] == nil {
		t.Errorf("kept %v, want the live Task and the two recent halves", st.attempts)
	}
	st.prune(deletedAt.Add(joinRetention + time.Nanosecond))
	if st.attempts["deleted"] != nil || st.attempts["awaiting"] != nil {
		t.Error("kept a half past the retention")
	}
}

// The checkpoint never persists annotations, which can hold the prompt,
// except the correlation annotations: a deleted Task it holds still names
// what started it.
func TestCheckpointKeepsOnlyCorrelationAnnotations(t *testing.T) {
	task := decodedTask(t, uidA, PhaseRunning)
	task.Annotations = map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": "{\"spec\":{\"prompt\":\"secret plan\"}}",
		testDomain + "/pr":     "41",
		testDomain + "/prompt": "secret plan",
	}
	st := &state{
		attempts:    map[types.UID]*attempt{uidA: {uid: uidA, task: task}},
		tasksSeenAt: testEnd, recordsSeenAt: testEnd,
	}
	data, err := json.Marshal(st.checkpoint(testConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret plan") || strings.Contains(string(data), "last-applied") {
		t.Errorf("checkpoint persists annotations: %s", data)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	restored, err := stateOf(cp)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.attempts[uidA].task.Annotations; !reflect.DeepEqual(got, map[string]string{testDomain + "/pr": "41"}) {
		t.Errorf("restored annotations = %v, want only the pr annotation", got)
	}
	if len(task.Annotations) != 3 {
		t.Error("checkpoint modified the state's Task")
	}
}

// A Task with no correlation annotation keeps none.
func TestCheckpointWithoutCorrelationAnnotations(t *testing.T) {
	task := decodedTask(t, uidA, PhaseRunning)
	task.Annotations = map[string]string{"example.com/note": "x"}
	st := &state{attempts: map[types.UID]*attempt{uidA: {uid: uidA, task: task}}}
	data, err := json.Marshal(st.checkpoint(testConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "annotations") {
		t.Errorf("checkpoint persists annotations: %s", data)
	}
}
