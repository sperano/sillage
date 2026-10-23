package store

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func TestRunActivity(t *testing.T) {
	if _, ok := runActivity(placeholderRun("pod-1")); ok {
		t.Error("a run of unknown bounds has an activity time")
	}
	run := finishedRun("uid-1", 0, 30, model.OutcomeSucceeded)
	run.Created = ptr(at(40))
	if got, ok := runActivity(run); !ok || !got.Equal(at(40)) {
		t.Errorf("runActivity = %v, %v; want minute 40", got, ok)
	}
}

func mustLastActivity(t *testing.T, s *Store, root model.Root) time.Time {
	t.Helper()
	got, err := s.Root(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastActivity == nil {
		t.Fatalf("%v has no last activity", root)
	}
	return *got.LastActivity
}

func TestLastActivityOfObservationsIsTheLastSeen(t *testing.T) {
	s := newTestStore(t, Retention{})
	seen := bucket("Doing", 0)
	seen.LastObservedAt = ptr(at(40))
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/1", 10), seen}})
	if got := mustLastActivity(t, s, taskRoot(7)); !got.Equal(at(40)) {
		t.Errorf("last activity = %v, want minute 40", got)
	}
}

// TestLastActivityFollowsSharedRecords saves, under another root, a newer
// read of a record task 7's story holds: task 7's last activity follows
// it, and nothing else of that save.
func TestLastActivityFollowsSharedRecords(t *testing.T) {
	later := comment(8, "comments/2", 50)
	t.Run("live edit replaced", func(t *testing.T) {
		s := newTestStore(t, Retention{})
		mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{liveEdit(0)}})
		mustSave(t, s, pageRoot(57), model.Records{Events: []model.Event{liveEdit(10)}})
		mustSave(t, s, taskRoot(8), model.Records{Events: []model.Event{later}})
		if got := mustLastActivity(t, s, taskRoot(7)); !got.Equal(at(10)) {
			t.Errorf("last activity = %v, want minute 10, the re-render's", got)
		}
	})
	t.Run("run ended", func(t *testing.T) {
		s := newTestStore(t, Retention{})
		running := finishedRun("uid-1", 20, 0, model.OutcomeUnknown)
		running.Interval.End = model.UnknownBound()
		mustSave(t, s, taskRoot(7), model.Records{Runs: []model.Run{running}})
		mustSave(t, s, taskRoot(8), model.Records{
			Events: []model.Event{later},
			Runs:   []model.Run{finishedRun("uid-1", 20, 30, model.OutcomeSucceeded)},
		})
		if got := mustLastActivity(t, s, taskRoot(7)); !got.Equal(at(30)) {
			t.Errorf("last activity = %v, want minute 30, the run's end", got)
		}
	})
}

// TestLastActivityNeverFalls saves task 7 again with only older records:
// its last activity stays the newer save's.
func TestLastActivityNeverFalls(t *testing.T) {
	s := newTestStore(t, Retention{})
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/2", 50)}})
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/1", 10)}})
	if got := mustLastActivity(t, s, taskRoot(7)); !got.Equal(at(50)) {
		t.Errorf("last activity = %v, want minute 50, the newer save's", got)
	}
}
