package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// Checkpoint is where one read of a source left off, so that the next
// resumes from it.
type Checkpoint struct {
	Source   model.Source
	Instance string
	// Scope names what was read, within the source instance: the listed
	// resource for a Kelos resourceVersion ("tasks"), the request path for
	// a GitHub ETag, the root's ref for the last read of one root.
	Scope string
	// Value is the resourceVersion, ETag or RFC 3339 time.
	Value string
	// Root, when set, ties the checkpoint to a stored root: pruning the
	// root deletes it.
	Root *model.Root
	// UpdatedAt is when the checkpoint was last put; set by the store.
	UpdatedAt time.Time
}

func (c Checkpoint) validate() error {
	if c.Instance == "" || c.Scope == "" {
		return errors.New("store: a checkpoint needs an instance and a scope")
	}
	if c.Root != nil {
		return c.Root.Validate()
	}
	return nil
}

// PutCheckpoint stores c, replacing the checkpoint of its scope. Its root,
// when set, must be stored (ErrNotFound otherwise).
func (s *Store) PutCheckpoint(ctx context.Context, c Checkpoint) error {
	if err := c.validate(); err != nil {
		return err
	}
	err := s.write(ctx, func(tx pgx.Tx) error {
		var root *int64
		if c.Root != nil {
			id, err := rootID(ctx, tx, *c.Root)
			if err != nil {
				return err
			}
			root = &id
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO checkpoints (source, instance, scope, value, root_id, updated_at)
			VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (source, instance, scope) DO UPDATE SET
				value = EXCLUDED.value, root_id = EXCLUDED.root_id, updated_at = EXCLUDED.updated_at`,
			string(c.Source), c.Instance, c.Scope, c.Value, root)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: put checkpoint %s/%s %s: %w", c.Source, c.Instance, c.Scope, err)
	}
	return nil
}

// Checkpoint returns the checkpoint of a scope, or ErrNotFound.
func (s *Store) Checkpoint(ctx context.Context, source model.Source, instance, scope string) (Checkpoint, error) {
	c := Checkpoint{Source: source, Instance: instance, Scope: scope}
	var root []byte
	err := s.pool.QueryRow(ctx, `SELECT c.value, c.updated_at, r.body FROM checkpoints c
		LEFT JOIN roots r ON r.id = c.root_id
		WHERE c.source = $1 AND c.instance = $2 AND c.scope = $3`,
		string(source), instance, scope).Scan(&c.Value, &c.UpdatedAt, &root)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil && root != nil {
		c.Root = new(model.Root)
		err = json.Unmarshal(root, c.Root)
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("store: checkpoint %s/%s %s: %w", source, instance, scope, err)
	}
	c.UpdatedAt = c.UpdatedAt.UTC()
	return c, nil
}
