package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/store"
)

// loopStore is the store as the collection loop uses it.
type loopStore struct{ st *store.Store }

var _ reconcile.Store = loopStore{}

// Records returns the root's stored story's records; none for a root the
// store has no story of.
func (s loopStore) Records(ctx context.Context, root model.Root) (model.Records, error) {
	story, err := s.st.Story(ctx, root)
	if errors.Is(err, store.ErrNotFound) {
		return model.Records{}, nil
	}
	if err != nil {
		return model.Records{}, err
	}
	return story.Records, nil
}

// Checkpoints returns the values of the checkpoints whose scope starts
// with prefix, by scope. The loop's keys name their source instance, so a
// scope stored under two instances is an error, not one to choose.
func (s loopStore) Checkpoints(ctx context.Context, prefix string) (map[string][]byte, error) {
	cps, err := s.st.Checkpoints(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(cps))
	for _, c := range cps {
		if _, dup := out[c.Scope]; dup {
			return nil, fmt.Errorf("checkpoint %s is stored under more than one source instance", c.Scope)
		}
		out[c.Scope] = []byte(c.Value)
	}
	return out, nil
}

// Commit writes c as one store commit: the root's read, whose coverage
// replaces the stored coverage of its instances, and the checkpoints,
// each scoped by its key.
func (s loopStore) Commit(ctx context.Context, c reconcile.Commit) error {
	var sc store.Commit
	if c.Root != nil {
		sc.Batch = &store.Batch{Root: *c.Root, Records: c.Records}
	}
	for _, key := range slices.Sorted(maps.Keys(c.Checkpoints)) {
		cp := c.Checkpoints[key]
		sc.Checkpoints = append(sc.Checkpoints, store.Checkpoint{
			Source: cp.Source, Instance: cp.Instance, Scope: key, Value: string(cp.Value), Root: cp.Root,
		})
	}
	return s.st.Commit(ctx, sc)
}
