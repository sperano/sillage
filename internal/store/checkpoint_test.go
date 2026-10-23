package store

import (
	"errors"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestCheckpointRoundTrip(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	if _, err := s.Checkpoint(ctx, model.SourceKelos, testCluster, "tasks"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Checkpoint before any put = %v, want ErrNotFound", err)
	}
	for _, v := range []string{"100", "250"} {
		c := Checkpoint{Source: model.SourceKelos, Instance: testCluster, Scope: "tasks", Value: v}
		if err := s.PutCheckpoint(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Checkpoint(ctx, model.SourceKelos, testCluster, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "250" || got.Root != nil || got.UpdatedAt.IsZero() {
		t.Errorf("checkpoint = %+v, want value 250, no root and a time", got)
	}
}

func TestCheckpointOfRoot(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	root := taskRoot(7)
	c := Checkpoint{Source: model.SourceVikunja, Instance: testVikunja, Scope: rootRef(root).String(),
		Value: "2026-03-01T12:00:00Z", Root: &root}
	if err := s.PutCheckpoint(ctx, c); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PutCheckpoint for an unknown root = %v, want ErrNotFound", err)
	}
	mustSave(t, s, root, model.Records{})
	if err := s.PutCheckpoint(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Checkpoint(ctx, c.Source, c.Instance, c.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root == nil || got.Root.Task == nil || *got.Root.Task != taskKey(7) {
		t.Errorf("checkpoint root = %+v, want task 7", got.Root)
	}
}

func TestCheckpointValidate(t *testing.T) {
	tests := []struct {
		name string
		c    Checkpoint
	}{
		{"no source", Checkpoint{Instance: testCluster, Scope: "tasks"}},
		{"no instance", Checkpoint{Source: model.SourceKelos, Scope: "tasks"}},
		{"no scope", Checkpoint{Source: model.SourceKelos, Instance: testCluster}},
		{"invalid root", Checkpoint{Source: model.SourceKelos, Instance: testCluster, Scope: "x", Root: &model.Root{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.validate(); err == nil {
				t.Error("validate accepted it")
			}
		})
	}
}
