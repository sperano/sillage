package store

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

const testPod = "vikunja-widgets-agent-7-x8z2w"

// taskRun is a run read from its Task while it runs.
func taskRun(uid string) model.Run {
	return model.Run{
		Key: uidKey(uid), TaskName: "vikunja-widgets-agent-7", Pod: testPod, Job: "job-7",
		Agent: "agent", Role: model.RoleImplementation, Created: ptr(at(0)), Message: "running",
		Interval: model.Interval{Start: model.KnownAt(at(1)), End: model.OpenEnd()},
		Outcome:  model.OutcomeRunning,
		Evidence: []model.Evidence{kelosEvidence("tasks/agents/vikunja-widgets-agent-7", "status")},
	}
}

// announcement announces the run of testPod on task 7 at minute.
func announcement(minute int) model.Event {
	e := comment(7, "comments/900", minute)
	e.Kind = model.KindRunAnnounced
	e.Actor = model.Account{Login: "gordon", Kind: model.ActorService}
	e.Details = map[string]string{"pod": testPod, "taskName": "vikunja-widgets-agent-7"}
	return e
}

// TestRunSurvivesTaskCleanup reads a run from its Task, then from its
// TaskRecord once the Task is gone, then only from its announcement once
// both are gone: the story keeps one run, with what each read knew.
func TestRunSurvivesTaskCleanup(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	ann := announcement(0)
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{ann},
		Runs:         []model.Run{taskRun("uid-1")},
		Correlations: []model.Correlation{link(ann.Ref(), uidKey("uid-1").Ref())},
	})
	mustSave(t, s, root, model.Records{Runs: []model.Run{finishedRun("uid-1", 1, 30, model.OutcomeSucceeded)}})
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{ann},
		Runs:         []model.Run{placeholderRun(testPod)},
		Correlations: []model.Correlation{link(ann.Ref(), podRunKey(testPod).Ref())},
	})

	story := mustStory(t, s, root)
	if len(story.Runs) != 1 {
		t.Fatalf("runs = %+v, want one", story.Runs)
	}
	r := story.Runs[0]
	if r.Key != uidKey("uid-1") || r.Pod != testPod || r.Job != "job-7" || r.Created == nil {
		t.Errorf("run = %+v, want uid-1 with the pod, job and creation its Task gave", r)
	}
	if r.Outcome != model.OutcomeSucceeded || r.Interval.End.State != model.BoundKnown {
		t.Errorf("run outcome = %s, end %s; want the TaskRecord's success", r.Outcome, r.Interval.End.State)
	}
	if len(r.Evidence) != 3 {
		t.Errorf("evidence = %+v, want the Task's, the TaskRecord's and the announcement's", r.Evidence)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].To != uidKey("uid-1").Ref() {
		t.Errorf("correlations = %+v, want the announcement linked to uid-1", story.Correlations)
	}
}

// TestPlaceholderRekeyedByUID stores a run known only from its
// announcement, then reads its Task: the placeholder becomes the UID's run,
// and what named it follows.
func TestPlaceholderRekeyedByUID(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	ann := announcement(0)
	ann.Related.Run = ptr(podRunKey(testPod))
	mustSave(t, s, root, model.Records{
		Events: []model.Event{ann},
		Runs:   []model.Run{placeholderRun(testPod)},
		Correlations: []model.Correlation{
			link(ann.Ref(), podRunKey(testPod).Ref()),
			link(podRunKey(testPod).Ref(), taskKey(7).Ref()),
		},
	})
	other := taskRoot(8)
	mustSave(t, s, other, model.Records{
		Runs:         []model.Run{taskRun("uid-1")},
		Correlations: []model.Correlation{link(uidKey("uid-1").Ref(), taskKey(7).Ref())},
	})

	story := mustStory(t, s, root)
	if len(story.Runs) != 1 || story.Runs[0].Key != uidKey("uid-1") {
		t.Fatalf("runs = %+v, want the placeholder re-keyed as uid-1", story.Runs)
	}
	if story.Runs[0].Outcome != model.OutcomeRunning {
		t.Errorf("outcome = %s, want the Task's", story.Runs[0].Outcome)
	}
	if got := story.Events[0].Related.Run; got == nil || *got != uidKey("uid-1") {
		t.Errorf("announcement names run %v, want uid-1", got)
	}
	if len(story.Correlations) != 2 {
		t.Fatalf("correlations = %+v, want two", story.Correlations)
	}
	for _, c := range story.Correlations {
		if c.From != uidKey("uid-1").Ref() && c.To != uidKey("uid-1").Ref() {
			t.Errorf("correlation %s does not name uid-1", c)
		}
	}
	// The other root's story holds the folded duplicate link too.
	if got := mustStory(t, s, other).Correlations; len(got) != 1 {
		t.Errorf("other root correlations = %+v, want one", got)
	}
}

// TestPlaceholderFoldedIntoStoredUID covers a UID first read without its
// pod: when a later read names the pod, the placeholder stored meanwhile
// is folded into the UID's run.
func TestPlaceholderFoldedIntoStoredUID(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	noPod := finishedRun("uid-1", 1, 30, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{Runs: []model.Run{noPod}})
	mustSave(t, s, taskRoot(8), model.Records{Runs: []model.Run{placeholderRun(testPod)}})
	mustSave(t, s, root, model.Records{Runs: []model.Run{taskRun("uid-1")}})

	for _, r := range []model.Root{root, taskRoot(8)} {
		story := mustStory(t, s, r)
		if len(story.Runs) != 1 || story.Runs[0].Key != uidKey("uid-1") {
			t.Errorf("%v runs = %+v, want only uid-1", r.Task, story.Runs)
		}
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM runs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d run rows, want 1", n)
	}
}
