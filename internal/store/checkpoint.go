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
	// Value is opaque text: a resourceVersion, an ETag, an RFC 3339 time,
	// or the collection loop's JSON state.
	Value string
	// Root, when set, ties the checkpoint to a stored root: pruning the
	// root deletes it.
	Root *model.Root
	// UpdatedAt is when the checkpoint was last put; set by the store.
	UpdatedAt time.Time
}

func (c Checkpoint) validate() error {
	if !c.Source.Valid() {
		return fmt.Errorf("store: checkpoint: unknown source %q", c.Source)
	}
	if c.Instance == "" || c.Scope == "" || c.Value == "" {
		return errors.New("store: checkpoint: an instance, a scope and a value are required")
	}
	if c.Root != nil {
		if err := c.Root.Validate(); err != nil {
			return fmt.Errorf("store: checkpoint: %w", err)
		}
	}
	return nil
}

// PutCheckpoint stores c, replacing the checkpoint of its scope. Its root,
// when set, must be stored (ErrNotFound otherwise).
func (s *Store) PutCheckpoint(ctx context.Context, c Checkpoint) error {
	if err := c.validate(); err != nil {
		return err
	}
	if err := s.write(ctx, func(tx pgx.Tx) error { return putCheckpoint(ctx, tx, c) }); err != nil {
		return fmt.Errorf("store: put checkpoint %s/%s %s: %w", c.Source, c.Instance, c.Scope, err)
	}
	return nil
}

// putCheckpoint stores a valid checkpoint in tx.
func putCheckpoint(ctx context.Context, tx pgx.Tx, c Checkpoint) error {
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
}

// checkpointColumns are what scanCheckpoint reads, from checkpoints c
// joined to roots r.
const checkpointColumns = "c.source, c.instance, c.scope, c.value, c.updated_at, r.body"

// Checkpoint returns the checkpoint of a scope, or ErrNotFound.
func (s *Store) Checkpoint(ctx context.Context, source model.Source, instance, scope string) (Checkpoint, error) {
	c, err := s.checkpoint(ctx, source, instance, scope)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("store: checkpoint %s/%s %s: %w", source, instance, scope, err)
	}
	return c, nil
}

func (s *Store) checkpoint(ctx context.Context, source model.Source, instance, scope string) (Checkpoint, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+checkpointColumns+` FROM checkpoints c
		LEFT JOIN roots r ON r.id = c.root_id
		WHERE c.source = $1 AND c.instance = $2 AND c.scope = $3`,
		string(source), instance, scope)
	if err != nil {
		return Checkpoint{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanCheckpoint)
}

// Checkpoints returns the checkpoints whose scope starts with prefix, of
// every source instance, in scope order.
func (s *Store) Checkpoints(ctx context.Context, prefix string) ([]Checkpoint, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+checkpointColumns+` FROM checkpoints c
		LEFT JOIN roots r ON r.id = c.root_id
		WHERE starts_with(c.scope, $1) ORDER BY c.scope, c.source, c.instance`, prefix)
	if err != nil {
		return nil, fmt.Errorf("store: checkpoints %q: %w", prefix, err)
	}
	out, err := pgx.CollectRows(rows, scanCheckpoint)
	if err != nil {
		return nil, fmt.Errorf("store: checkpoints %q: %w", prefix, err)
	}
	return out, nil
}

// scanCheckpoint reads a row of checkpointColumns.
func scanCheckpoint(row pgx.CollectableRow) (Checkpoint, error) {
	var c Checkpoint
	var source string
	var root []byte
	if err := row.Scan(&source, &c.Instance, &c.Scope, &c.Value, &c.UpdatedAt, &root); err != nil {
		return c, err
	}
	c.Source, c.UpdatedAt = model.Source(source), c.UpdatedAt.UTC()
	if root != nil {
		c.Root = new(model.Root)
		if err := json.Unmarshal(root, c.Root); err != nil {
			return c, err
		}
	}
	return c, nil
}
