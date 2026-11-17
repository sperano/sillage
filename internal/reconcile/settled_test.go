package reconcile

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// settledStory is task testTaskID's story with events and runs, read
// after all of them.
func settledStory(events []model.Event, runs ...model.Run) model.Story {
	return model.Story{Root: testRoot(), Records: model.Records{
		Events: events, Runs: runs,
		PullRequests: []model.PullRequest{{Key: testPRKey, Branch: testBranch}},
	}}
}

var testPRKey = model.PRKey{Instance: testGitHubHost, Repository: testRepo.String(), Number: 42}

// storyEvent is an event of kind at minutes after testStart, on the task
// or, for a pull request's, on testPRKey.
func storyEvent(kind model.EventKind, minutes int) model.Event {
	at := testStart.Add(time.Duration(minutes) * time.Minute)
	e := model.Event{Kind: kind, SourceTime: &at, ObservedAt: at}
	switch kind {
	case model.KindTaskDone, model.KindTaskCompletionObserved:
		task := *testRoot().Task
		e.Related.Task = &task
	default:
		key := testPRKey
		e.Related.PR = &key
	}
	return e
}

// seenOpen is a read that saw the task not done, minutes after testStart.
func seenOpen(minutes int) model.Event {
	e := storyEvent(model.KindTaskCompletionObserved, minutes)
	e.SourceTime = nil
	e.Details = map[string]string{model.DetailDone: model.DoneFalse}
	return e
}

func run(end model.Bound, outcome model.Outcome) model.Run {
	return model.Run{Interval: model.Interval{Start: model.KnownAt(testStart), End: end}, Outcome: outcome}
}

func TestSettled(t *testing.T) {
	done, opened := storyEvent(model.KindTaskDone, 90), storyEvent(model.KindPROpened, 10)
	merged, closed := storyEvent(model.KindPRMerged, 60), storyEvent(model.KindPRClosed, 60)
	reopened := storyEvent(model.KindPRReopened, 70)
	finished := run(model.KnownAt(testStart.Add(time.Hour)), model.OutcomeSucceeded)
	for _, tc := range []struct {
		name string
		s    model.Story
		want bool
	}{
		{"done, merged, finished", settledStory([]model.Event{opened, merged, done}, finished), true},
		{"closed unmerged", settledStory([]model.Event{opened, closed, done}, finished), true},
		{"task not done", settledStory([]model.Event{opened, merged}, finished), false},
		{"task reopened", settledStory([]model.Event{opened, merged, done, seenOpen(100)}, finished), false},
		{"pull request open", settledStory([]model.Event{opened, done}, finished), false},
		{"pull request reopened", settledStory([]model.Event{opened, closed, reopened, done}, finished), false},
		{"pull request state unknown", settledStory([]model.Event{done}, finished), false},
		{"run running", settledStory([]model.Event{opened, merged, done}, run(model.OpenEnd(), model.OutcomeRunning)), false},
		{"run failed, end unknown", settledStory([]model.Event{opened, merged, done},
			run(model.UnknownBound(), model.OutcomeFailed)), true},
		{"run outcome and end unknown", settledStory([]model.Event{opened, merged, done},
			run(model.UnknownBound(), model.OutcomeUnknown)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := settled(tc.s, testStart.Add(24*time.Hour)); got != tc.want {
				t.Errorf("settled = %v, want %v", got, tc.want)
			}
		})
	}
}
