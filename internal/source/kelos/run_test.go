package kelos

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sperano/sillage/internal/model"
)

// A Task with its record: one run keyed by UID, the Task's creation, pod,
// job and message, the record's interval, outcome, effective model and
// usage, and evidence from both, each naming the fields it supplied.
func TestTaskWithRecord(t *testing.T) {
	recs := storyRecords(t, "task-with-record")
	run := onlyRun(t, recs)
	want := model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: "00000000-0000-4000-8000-000000000201"}
	if run.Key != want {
		t.Errorf("key = %v, want %v", run.Key, want)
	}
	if run.Created == nil || run.Pod == "" || run.Job == "" || run.Message == "" {
		t.Errorf("created %v, pod %q, job %q, message %q: want all from the Task", run.Created, run.Pod, run.Job, run.Message)
	}
	if run.Agent != "agent-opus" || run.Project != "widgets" || run.Role != model.RoleImplementation {
		t.Errorf("agent %q, project %q, role %q", run.Agent, run.Project, run.Role)
	}
	if d, ok := run.Interval.Duration(); !ok || d != 24*time.Minute+30*time.Second || run.Outcome != model.OutcomeSucceeded {
		t.Errorf("interval %v (%v), outcome %q", d, ok, run.Outcome)
	}
	// The record's effective model wins over the Task's requested one.
	if run.Model != "model-effective" {
		t.Errorf("model = %q, want the record's", run.Model)
	}
	if u := run.Usage; u == nil || u.CostUSD != "1.234" || *u.InputTokens != 48000 || *u.OutputTokens != 5200 {
		t.Errorf("usage = %+v, want cost 1.234 (from 1234m) and both token counts", u)
	}
	checkNote(t, noteOf(t, run, "tasks/agents/"), "created (metadata.creationTimestamp)", "pod (status.podName)",
		"job (status.jobName)", "message (status.message)", "agent (metadata.labels[vikunja.example.com/variant])",
		lifecycleNote)
	checkNote(t, noteOf(t, run, "taskrecords/agents/"), "outcome (spec.phase Succeeded)", "start (spec.startTime)",
		"end (spec.completionTime)", "model (spec.model)", "usage (spec.usage)", lifecycleNote)
	if len(recs.Correlations) != 1 || recs.Correlations[0].To.ID != "vikunja.example.com/tasks/201" {
		t.Errorf("correlations = %+v, want one to task 201", recs.Correlations)
	}
}

// A record only: the Task was cleaned up. The run still has its interval,
// outcome, model, usage and labels; the Task-only fields are unknown and
// the evidence says why.
func TestRecordOnly(t *testing.T) {
	recs := storyRecords(t, "record-only")
	run := onlyRun(t, recs)
	if run.Created != nil || run.Pod != "" || run.Job != "" || run.Message != "" {
		t.Errorf("created %v, pod %q, job %q, message %q: want none without the Task", run.Created, run.Pod, run.Job, run.Message)
	}
	if run.TaskName != "vikunja-fu-widgets-agent-sonnet-202" || run.Role != model.RoleTaskFollowUp || run.Agent != "agent-sonnet" {
		t.Errorf("name %q, role %q, agent %q, want them from the record", run.TaskName, run.Role, run.Agent)
	}
	if run.Outcome != model.OutcomeFailed || run.Model != "model-b" || run.Usage == nil || run.Usage.CostUSD != "" {
		t.Errorf("outcome %q, model %q, usage %+v", run.Outcome, run.Model, run.Usage)
	}
	if refs := evidenceRefs(run); len(refs) != 1 {
		t.Errorf("evidence = %v, want the record only", refs)
	}
	checkNote(t, noteOf(t, run, "taskrecords/agents/"), "taskName (spec.taskRef.name)",
		"role (metadata.labels[vikunja.example.com/role] task-followup)", noTaskNote, lifecycleNote)
	if len(recs.Correlations) != 1 || recs.Correlations[0].Evidence[0].Ref != run.Evidence[0].Ref {
		t.Errorf("correlations = %+v, want one from the record's label", recs.Correlations)
	}
}

// A Task only: it reported no usage, so it has no record. Its lifecycle
// comes from its status, its model from the deprecated spec.model.
func TestTaskOnly(t *testing.T) {
	run := onlyRun(t, storyRecords(t, "task-only"))
	if run.Role != model.RoleDiscussion {
		t.Errorf("role = %q, want discussion from the name", run.Role)
	}
	if run.Outcome != model.OutcomeSucceeded || run.Interval.End.State != model.BoundKnown {
		t.Errorf("outcome %q, end %v", run.Outcome, run.Interval.End)
	}
	if run.Usage != nil || run.Model != "model-legacy" {
		t.Errorf("usage %+v, model %q", run.Usage, run.Model)
	}
	checkNote(t, noteOf(t, run, "tasks/agents/"), "outcome (status.phase Succeeded)", "start (status.startTime)",
		"model (spec.model)", noRecordNote, lifecycleNote)
}

// Unfinished Tasks have an open end. A waiting Task has no start yet; its
// model is set by a WorkerPool, which is not read, so it stays unknown.
func TestTasksUnfinished(t *testing.T) {
	recs := storyRecords(t, "tasks-unfinished")
	if len(recs.Runs) != 2 || len(recs.Correlations) != 0 {
		t.Fatalf("got %d runs and %d correlations, want 2 and none", len(recs.Runs), len(recs.Correlations))
	}
	running, waiting := recs.Runs[0], recs.Runs[1]
	if running.Role != model.RolePRFollowUp || running.Outcome != model.OutcomeRunning ||
		running.Interval.Start.State != model.BoundKnown || running.Interval.End.State != model.BoundOpen {
		t.Errorf("running: role %q, outcome %q, interval %+v", running.Role, running.Outcome, running.Interval)
	}
	if waiting.Role != model.RoleAutomation || waiting.Outcome != model.OutcomeRunning ||
		waiting.Interval.Start.State != model.BoundUnknown || waiting.Model != "" {
		t.Errorf("waiting: role %q, outcome %q, interval %+v, model %q", waiting.Role, waiting.Outcome, waiting.Interval, waiting.Model)
	}
	// Its creation is its only time, and is labelled as a lifecycle time.
	checkNote(t, noteOf(t, waiting, "tasks/agents/"), "outcome (status.phase Waiting)", lifecycleNote)
}

// Two attempts with the same Task name stay two runs, each with its own
// UID; the name joins nothing.
func TestSameNameRetry(t *testing.T) {
	recs := storyRecords(t, "same-name-retry")
	if len(recs.Runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(recs.Runs))
	}
	failed, retry := recs.Runs[0], recs.Runs[1]
	if failed.TaskName != retry.TaskName || failed.Key == retry.Key {
		t.Errorf("names %q and %q, keys %v and %v: want one name, two keys", failed.TaskName, retry.TaskName, failed.Key, retry.Key)
	}
	if failed.Outcome != model.OutcomeFailed || failed.Pod != "" || len(failed.Evidence) != 1 {
		t.Errorf("first attempt: outcome %q, pod %q, evidence %v: want failed, from its record alone",
			failed.Outcome, failed.Pod, evidenceRefs(failed))
	}
	if retry.Outcome != model.OutcomeSucceeded || retry.Pod == "" || len(retry.Evidence) != 2 {
		t.Errorf("retry: outcome %q, pod %q, evidence %v: want succeeded, from its Task and record",
			retry.Outcome, retry.Pod, evidenceRefs(retry))
	}
	if len(recs.Correlations) != 2 {
		t.Errorf("got %d correlations, want one per attempt", len(recs.Correlations))
	}
}

func TestLifecyclePhases(t *testing.T) {
	start, end := metav1.NewTime(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)), metav1.NewTime(time.Date(2026, 3, 1, 10, 5, 0, 0, time.UTC))
	tests := []struct {
		phase   Phase
		end     *metav1.Time
		outcome model.Outcome
		endKind model.BoundState
	}{
		{PhaseSucceeded, &end, model.OutcomeSucceeded, model.BoundKnown},
		{PhaseFailed, &end, model.OutcomeFailed, model.BoundKnown},
		{PhaseFailed, nil, model.OutcomeFailed, model.BoundUnknown},
		{PhaseRunning, nil, model.OutcomeRunning, model.BoundOpen},
		{PhasePending, nil, model.OutcomeRunning, model.BoundOpen},
		{PhaseWaiting, nil, model.OutcomeRunning, model.BoundOpen},
		{"", nil, model.OutcomeRunning, model.BoundOpen},
		{"Suspended", nil, model.OutcomeUnknown, model.BoundUnknown},
		{"Suspended", &end, model.OutcomeUnknown, model.BoundKnown},
	}
	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			b := &runBuilder{a: &attempt{uid: "u"}}
			b.setLifecycle(tt.phase, &start, tt.end, "status.", b.fromTask)
			if b.run.Outcome != tt.outcome || b.run.Interval.End.State != tt.endKind {
				t.Errorf("outcome %q, end %q, want %q, %q", b.run.Outcome, b.run.Interval.End.State, tt.outcome, tt.endKind)
			}
		})
	}
}

// A record with no time at all supplies no lifecycle time, and is not
// labelled as if it had.
func TestNoTimeNoLifecycleNote(t *testing.T) {
	b := &runBuilder{cfg: testConfig(), a: &attempt{uid: "u", record: &TaskRecord{Spec: TaskRecordSpec{Phase: PhaseFailed}}}}
	b.lifecycle()
	if b.timesFromRecord {
		t.Error("a record without times marked as having supplied one")
	}
}

func TestModelSources(t *testing.T) {
	tests := []struct {
		name   string
		task   *Task
		record *TaskRecord
		want   string
	}{
		{"record wins", &Task{Spec: TaskSpec{Worker: &WorkerSpec{Model: "w"}}}, &TaskRecord{Spec: TaskRecordSpec{Model: "r"}}, "r"},
		{"record without model", &Task{Spec: TaskSpec{Worker: &WorkerSpec{Model: "w"}}}, &TaskRecord{}, "w"},
		{"worker over legacy", &Task{Spec: TaskSpec{Worker: &WorkerSpec{Model: "w"}, Model: "l"}}, nil, "w"},
		{"legacy", &Task{Spec: TaskSpec{Worker: &WorkerSpec{}, Model: "l"}}, nil, "l"},
		{"pool unknown", &Task{Spec: TaskSpec{WorkerPoolRef: &WorkerPoolReference{Name: "p"}, Model: "l"}}, nil, ""},
		{"none", &Task{}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &runBuilder{a: &attempt{uid: "u", task: tt.task, record: tt.record}}
			b.model()
			if b.run.Model != tt.want {
				t.Errorf("model = %q, want %q", b.run.Model, tt.want)
			}
		})
	}
}
