package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// coverageOf returns the story's coverage of source instance, failing t
// without one.
func coverageOf(t *testing.T, story model.Story, source model.Source, instance string) model.Coverage {
	t.Helper()
	for _, c := range story.Coverage {
		if c.Source == source && c.Instance == instance {
			return c
		}
	}
	t.Fatalf("no %s/%s coverage in %+v", source, instance, story.Coverage)
	return model.Coverage{}
}

func vikunjaCoverage(minute int) model.Coverage {
	return model.Coverage{Source: model.SourceVikunja, Instance: testVikunja, LastSuccessfulRead: ptr(at(minute))}
}

// TestCommitReplacesCoverage commits, after a read at minute 10, a gap
// that starts before it: one the collector reports late, such as a run
// announced at minute 5 that no Kelos read found. Save's merge keeps only
// what the read at minute 10 had not reached; Commit keeps it whole, and
// keeps the coverage of the instances it does not carry.
func TestCommitReplacesCoverage(t *testing.T) {
	late := cov(ptr(at(20)), gap(5, 20, "announced only"))
	tests := []struct {
		name  string
		write func(*Store, model.Root) error
		want  []model.Gap
	}{
		{"commit", func(s *Store, root model.Root) error {
			return s.Commit(t.Context(), Commit{Batch: &Batch{Root: root, Records: model.Records{
				Coverage: []model.Coverage{late},
			}}})
		}, []model.Gap{gap(5, 20, "announced only")}},
		{"save", func(s *Store, root model.Root) error {
			return s.Save(t.Context(), Batch{Root: root, Records: model.Records{Coverage: []model.Coverage{late}}})
		}, []model.Gap{afterRead(10, 20, "announced only")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t, Retention{})
			root := taskRoot(7)
			mustSave(t, s, root, model.Records{Coverage: []model.Coverage{cov(ptr(at(10))), vikunjaCoverage(10)}})
			if err := tt.write(s, root); err != nil {
				t.Fatal(err)
			}
			story := mustStory(t, s, root)
			got := coverageOf(t, story, model.SourceKelos, testCluster)
			if !got.LastSuccessfulRead.Equal(at(20)) || !slices.EqualFunc(got.Gaps, tt.want, sameGap) {
				t.Errorf("kelos coverage = %+v, want read at minute 20 with gaps %+v", got, tt.want)
			}
			if v := coverageOf(t, story, model.SourceVikunja, testVikunja); !v.LastSuccessfulRead.Equal(at(10)) {
				t.Errorf("vikunja coverage = %+v, want it kept", v)
			}
		})
	}
}

func sameGap(a, b model.Gap) bool {
	return a.Start.Equal(b.Start) && a.End.Equal(b.End) && a.Reason == b.Reason
}

// TestCommitStoresRecordsByTheSaveRules commits a placeholder run, then
// the run keyed by UID that names its pod: the run is re-keyed, as Save
// re-keys it, and a later commit that lacks it keeps it.
func TestCommitStoresRecordsByTheSaveRules(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	commit := func(recs model.Records) {
		t.Helper()
		if err := s.Commit(t.Context(), Commit{Batch: &Batch{Root: root, Records: recs}}); err != nil {
			t.Fatal(err)
		}
	}
	commit(model.Records{Runs: []model.Run{placeholderRun("pod-1")}})
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	run.Pod = "pod-1"
	commit(model.Records{Runs: []model.Run{run}})
	commit(model.Records{Events: []model.Event{comment(7, "comments/1", 6)}})
	story := mustStory(t, s, root)
	if len(story.Runs) != 1 || story.Runs[0].Key != uidKey("uid-1") {
		t.Errorf("runs = %+v, want the one keyed by UID", story.Runs)
	}
	if len(story.Events) != 1 {
		t.Errorf("events = %+v, want the comment", story.Events)
	}
}

// TestCommitCheckpointsOfItsRoot ties a checkpoint to the root the same
// commit adds.
func TestCommitCheckpointsOfItsRoot(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	root := taskRoot(7)
	c := Checkpoint{Source: model.SourceVikunja, Instance: testVikunja, Scope: "loop/root", Value: "{}", Root: &root}
	if err := s.Commit(ctx, Commit{Batch: &Batch{Root: root}, Checkpoints: []Checkpoint{c}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Checkpoint(ctx, c.Source, c.Instance, c.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != c.Value || got.Root == nil || *got.Root.Task != taskKey(7) {
		t.Errorf("checkpoint = %+v, want %q tied to task 7", got, c.Value)
	}
}

// TestCommitIsAtomic fails a commit on its last checkpoint, tied to a
// root never stored: neither its batch nor its other checkpoints are
// stored.
func TestCommitIsAtomic(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	root, unknown := taskRoot(7), taskRoot(8)
	c := Commit{
		Batch: &Batch{Root: root, Records: model.Records{Events: []model.Event{comment(7, "comments/1", 0)}}},
		Checkpoints: []Checkpoint{
			{Source: model.SourceKelos, Instance: testCluster, Scope: "loop/a", Value: "1"},
			{Source: model.SourceVikunja, Instance: testVikunja, Scope: "loop/b", Value: "2", Root: &unknown},
		},
	}
	if err := s.Commit(ctx, c); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Commit = %v, want ErrNotFound for the unknown root", err)
	}
	if _, err := s.Story(ctx, root); !errors.Is(err, ErrNotFound) {
		t.Errorf("story after the failed commit: %v, want ErrNotFound", err)
	}
	if cps, err := s.Checkpoints(ctx, "loop/"); err != nil || len(cps) != 0 {
		t.Errorf("checkpoints after the failed commit = %+v, %v; want none", cps, err)
	}
}

func TestCommitRejectsInvalid(t *testing.T) {
	s := newTestStore(t, Retention{})
	tests := []struct {
		name string
		c    Commit
	}{
		{"invalid batch", Commit{Batch: &Batch{Root: model.Root{}}}},
		{"invalid checkpoint", Commit{Checkpoints: []Checkpoint{{Source: model.SourceKelos, Scope: "loop/a", Value: "1"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.Commit(t.Context(), tt.c); err == nil {
				t.Error("Commit accepted it")
			}
		})
	}
}

// TestCheckpointsByPrefix lists the checkpoints of a scope prefix, of
// every source instance, and reads the prefix literally: "_" and "%" are
// not wildcards.
func TestCheckpointsByPrefix(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	put := []Checkpoint{
		{Source: model.SourceVikunja, Instance: testVikunja, Scope: "loop/b", Value: "2"},
		{Source: model.SourceKelos, Instance: testCluster, Scope: "loop/a", Value: "1"},
		{Source: model.SourceKelos, Instance: testCluster, Scope: "loopx", Value: "3"},
		{Source: model.SourceKelos, Instance: testCluster, Scope: "tasks", Value: "4"},
	}
	if err := s.Commit(ctx, Commit{Checkpoints: put}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Checkpoints(ctx, "loop/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Scope != "loop/a" || got[0].Source != model.SourceKelos ||
		got[1].Scope != "loop/b" || got[1].Instance != testVikunja || got[1].Value != "2" {
		t.Errorf("checkpoints = %+v, want loop/a on Kelos and loop/b on Vikunja", got)
	}
	for _, prefix := range []string{"loop_", "%"} {
		if got, err := s.Checkpoints(ctx, prefix); err != nil || len(got) != 0 {
			t.Errorf("Checkpoints(%q) = %+v, %v; want none", prefix, got, err)
		}
	}
}
