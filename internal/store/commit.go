package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// Commit is what one step of the collection loop writes: a read of one
// root and the checkpoints it advanced, or checkpoints alone.
type Commit struct {
	// Batch is the read of one root; nil for checkpoints alone.
	Batch *Batch
	// Checkpoints are put as PutCheckpoint puts each. Their roots must be
	// stored, or be the batch's.
	Checkpoints []Checkpoint
}

// Commit stores c in one transaction: all of it, or nothing. The batch is
// stored by Save's rules but one: its coverage of each source instance
// replaces the root's stored coverage of that instance instead of merging
// with it, as the caller computed the whole of it. The root's coverage of
// the instances the batch does not carry is kept.
func (s *Store) Commit(ctx context.Context, c Commit) error {
	var story model.Story
	if c.Batch != nil {
		var err error
		if story, err = c.Batch.story(); err != nil {
			return err
		}
	}
	for _, cp := range c.Checkpoints {
		if err := cp.validate(); err != nil {
			return err
		}
	}
	err := s.write(ctx, func(tx pgx.Tx) error {
		if c.Batch != nil {
			if err := newSaver(ctx, tx, coverageReplace).save(c.Batch.Title, story); err != nil {
				return err
			}
		}
		for _, cp := range c.Checkpoints {
			if err := putCheckpoint(ctx, tx, cp); err != nil {
				return fmt.Errorf("checkpoint %s/%s %s: %w", cp.Source, cp.Instance, cp.Scope, err)
			}
		}
		return nil
	})
	switch {
	case err != nil && c.Batch != nil:
		return fmt.Errorf("store: commit %s: %w", rootRef(c.Batch.Root), err)
	case err != nil:
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
