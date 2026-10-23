package store

import (
	"errors"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const testRetentionDays = 30

func TestPruneKeepsEverythingByDefault(t *testing.T) {
	s := newTestStore(t, Retention{})
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/1", 0)}})
	n, err := s.Prune(t.Context(), at(0).AddDate(100, 0, 0))
	if err != nil || n != 0 {
		t.Fatalf("Prune = %d, %v; want nothing pruned", n, err)
	}
	mustStory(t, s, taskRoot(7))
}

// TestPruneWholeStories prunes the story inactive past the retention, and
// keeps a run it shares with a story still active.
func TestPruneWholeStories(t *testing.T) {
	retention := Retention{keep: testRetentionDays * day}
	s := newTestStore(t, retention)
	ctx := t.Context()
	shared := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	old, active := taskRoot(7), taskRoot(8)
	mustSave(t, s, old, model.Records{Events: []model.Event{comment(7, "comments/1", 0)}, Runs: []model.Run{shared}})
	recent := at(0).AddDate(0, 0, testRetentionDays+1)
	activeComment := comment(8, "comments/2", 0)
	activeComment.SourceTime = &recent
	mustSave(t, s, active, model.Records{Events: []model.Event{activeComment}, Runs: []model.Run{shared}})
	if err := s.PutCheckpoint(ctx, Checkpoint{Source: model.SourceVikunja, Instance: testVikunja,
		Scope: "root", Value: "x", Root: &old}); err != nil {
		t.Fatal(err)
	}

	n, err := s.Prune(ctx, recent.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v; want one story pruned", n, err)
	}
	if _, err := s.Story(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("pruned story: %v, want ErrNotFound", err)
	}
	if story := mustStory(t, s, active); len(story.Runs) != 1 || len(story.Events) != 1 {
		t.Errorf("active story = %+v, want its comment and the shared run", story.Records)
	}
	if _, err := s.Checkpoint(ctx, model.SourceVikunja, testVikunja, "root"); !errors.Is(err, ErrNotFound) {
		t.Errorf("checkpoint of the pruned root: %v, want ErrNotFound", err)
	}
	var events int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("%d event rows, want the pruned story's deleted", events)
	}
}
