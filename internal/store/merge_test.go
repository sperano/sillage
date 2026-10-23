package store

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func TestMergeRunFillsWhatTheLaterReadLacks(t *testing.T) {
	stored := model.Run{
		Key: uidKey("uid-1"), TaskName: "task-7", Pod: "pod-7", Job: "job-7", Agent: "agent",
		Project: "widgets", Role: model.RoleImplementation, Created: ptr(at(0)), Message: "running",
		Model: "model-a", Usage: &model.Usage{CostUSD: "0.1"},
		Interval: model.Interval{Start: model.KnownAt(at(1)), End: model.OpenEnd()},
		Outcome:  model.OutcomeRunning,
		Evidence: []model.Evidence{kelosEvidence("tasks/agents/task-7", "from the Task")},
	}
	later := model.Run{
		Key: uidKey("uid-1"), Role: model.RoleUnknown, Model: "model-b", Usage: &model.Usage{CostUSD: "0.4"},
		Interval: model.Interval{Start: model.KnownAt(at(1)), End: model.KnownAt(at(9))},
		Outcome:  model.OutcomeSucceeded,
		Evidence: []model.Evidence{kelosEvidence("taskrecords/agents/uid-1", "Task gone")},
	}
	got := mergeRun(stored, later)
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Pod != "pod-7" || got.Job != "job-7" || got.TaskName != "task-7" || got.Created == nil ||
		got.Message != "running" || got.Agent != "agent" || got.Project != "widgets" {
		t.Errorf("merged = %+v, want the Task's fields kept", got)
	}
	if got.Role != model.RoleImplementation || got.Model != "model-b" || got.Usage.CostUSD != "0.4" {
		t.Errorf("merged role %s, model %s, cost %s; want the known role and the later model and usage",
			got.Role, got.Model, got.Usage.CostUSD)
	}
	if got.Outcome != model.OutcomeSucceeded || got.Interval.End.State != model.BoundKnown {
		t.Errorf("merged outcome = %s, want the later read's", got.Outcome)
	}
	if len(got.Evidence) != 2 {
		t.Errorf("evidence = %+v, want both objects'", got.Evidence)
	}
}

func TestMergeRunKeepsKnownOutcome(t *testing.T) {
	uid := finishedRun("uid-1", 0, 10, model.OutcomeFailed)
	got := mergeRun(placeholderRun("pod-7"), uid)
	if got.Key != uid.Key || got.Outcome != model.OutcomeFailed || got.Pod != "pod-7" {
		t.Errorf("merged = %+v, want uid-1, failed, with the placeholder's pod", got)
	}
	got = mergeRun(uid, placeholderRun("pod-7"))
	if got.Outcome != model.OutcomeFailed || got.Interval != uid.Interval {
		t.Errorf("an announcement replaced the known outcome: %+v", got)
	}
}

func TestMergeRunEvidenceOverlayWins(t *testing.T) {
	base := []model.Evidence{kelosEvidence("a", "old"), kelosEvidence("b", "b")}
	got := mergeEvidence(base, []model.Evidence{kelosEvidence("a", "new")})
	if len(got) != 2 || got[0].Note != "new" || got[1].Ref != "b" {
		t.Errorf("evidence = %+v, want a (new) and b", got)
	}
}

func gap(start, end int, reason string) model.Gap {
	return model.Gap{Start: at(start), End: at(end), Reason: reason}
}

// afterRead is a gap from just after a read at minute read to minute end.
func afterRead(read, end int, reason string) model.Gap {
	return model.Gap{Start: at(read).Add(time.Nanosecond), End: at(end), Reason: reason}
}

func cov(read *time.Time, gaps ...model.Gap) model.Coverage {
	return model.Coverage{Source: model.SourceKelos, Instance: testCluster, LastSuccessfulRead: read, Gaps: gaps}
}

func TestMergeCoverage(t *testing.T) {
	tests := []struct {
		name             string
		stored, incoming model.Coverage
		wantRead         *time.Time
		wantGaps         []model.Gap
	}{
		{
			"a later read's TTL gap shrinks to what the earlier read missed",
			cov(ptr(at(10)), gap(0, 5, "expired")), cov(ptr(at(20)), gap(0, 15, "expired later")),
			ptr(at(20)), []model.Gap{gap(0, 5, "expired later"), afterRead(10, 15, "expired later")},
		},
		{
			"a gap after the earlier read stays",
			cov(ptr(at(10))), cov(ptr(at(30)), gap(12, 18, "watch expired")),
			ptr(at(30)), []model.Gap{gap(12, 18, "watch expired")},
		},
		{
			"a gap straddling the earlier read keeps its part after it",
			cov(ptr(at(10))), cov(ptr(at(30)), gap(5, 20, "down")),
			ptr(at(30)), []model.Gap{afterRead(10, 20, "down")},
		},
		{
			"an older read arriving late",
			cov(ptr(at(20)), gap(0, 15, "expired")), cov(ptr(at(10)), gap(0, 5, "expired")),
			ptr(at(20)), []model.Gap{gap(0, 5, "expired"), afterRead(10, 15, "expired")},
		},
		{
			"the same read twice",
			cov(ptr(at(10)), gap(0, 5, "expired")), cov(ptr(at(10)), gap(0, 5, "expired")),
			ptr(at(10)), []model.Gap{gap(0, 5, "expired")},
		},
		{
			"nothing stored was read",
			cov(nil), cov(ptr(at(10)), gap(0, 5, "expired")),
			ptr(at(10)), []model.Gap{gap(0, 5, "expired")},
		},
		{
			"the incoming coverage was never read",
			cov(ptr(at(10)), gap(0, 5, "expired")), cov(nil),
			ptr(at(10)), []model.Gap{gap(0, 5, "expired")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeCoverage(tt.stored, tt.incoming)
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			if !got.LastSuccessfulRead.Equal(*tt.wantRead) {
				t.Errorf("last read = %v, want %v", got.LastSuccessfulRead, tt.wantRead)
			}
			if len(got.Gaps) != len(tt.wantGaps) {
				t.Fatalf("gaps = %+v, want %+v", got.Gaps, tt.wantGaps)
			}
			for i, g := range tt.wantGaps {
				if !got.Gaps[i].Start.Equal(g.Start) || !got.Gaps[i].End.Equal(g.End) || got.Gaps[i].Reason != g.Reason {
					t.Errorf("gap %d = %+v, want %+v", i, got.Gaps[i], g)
				}
			}
		})
	}
}

func TestMergeCoverageKnownStaysKnown(t *testing.T) {
	older := cov(ptr(at(10)), gap(0, 5, "expired"))
	newer := cov(ptr(at(20)), gap(0, 15, "expired"))
	got := mergeCoverage(older, newer)
	for minute := range 21 {
		known := older.Known(at(minute)) || newer.Known(at(minute))
		if got.Known(at(minute)) != known {
			t.Errorf("minute %d: merged known = %v, want %v", minute, got.Known(at(minute)), known)
		}
	}
}

func TestActivity(t *testing.T) {
	if _, ok := activity(model.Records{}); ok {
		t.Error("no records have an activity time")
	}
	seen := bucket("Doing", 0)
	seen.LastObservedAt = ptr(at(40))
	recs := model.Records{
		Events: []model.Event{comment(7, "comments/1", 10), seen},
		Runs:   []model.Run{finishedRun("uid-1", 0, 30, model.OutcomeSucceeded)},
	}
	if got, ok := activity(recs); !ok || !got.Equal(at(40)) {
		t.Errorf("activity = %v, %v; want minute 40", got, ok)
	}
}
