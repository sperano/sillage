package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sperano/sillage/internal/model"
)

// saveLockID is the transaction advisory lock every write takes, so that
// the rules that read before they write (re-keying, observation ranges,
// live wiki records) see one save at a time.
const saveLockID int64 = 0x5111a6e0002

// ErrNotFound is returned for a root the store has no story of.
var ErrNotFound = errors.New("store: not found")

// Reader is what the read API needs from the store.
type Reader interface {
	// Story returns everything stored about root, as model.BuildStory
	// assembles it, or ErrNotFound.
	Story(ctx context.Context, root model.Root) (model.Story, error)
	// SearchRoots lists the stored roots that match q, most recently
	// active first.
	SearchRoots(ctx context.Context, q RootQuery) ([]RootSummary, error)
	// Coverage returns how far each source was read for root, or
	// ErrNotFound.
	Coverage(ctx context.Context, root model.Root) ([]model.Coverage, error)
}

var _ Reader = (*Store)(nil)

// Store persists model records in PostgreSQL.
type Store struct {
	pool      *pgxpool.Pool
	retention Retention
}

// Open connects to the database, applies the embedded migrations it lacks
// and returns the store. Its errors never quote the connection string.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	pc, err := cfg.poolConfig()
	if err != nil {
		return nil, err
	}
	return open(ctx, pc, cfg.Retention)
}

func open(ctx context.Context, pc *pgxpool.Config, retention Retention) (*Store, error) {
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: connecting: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, retention: retention}, nil
}

// Close closes the connections.
func (s *Store) Close() { s.pool.Close() }

// write runs fn in a transaction holding the save lock.
func (s *Store) write(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", saveLockID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// rootID returns the id of a stored root, or ErrNotFound.
func rootID(ctx context.Context, tx pgx.Tx, root model.Root) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, "SELECT id FROM roots WHERE ref = $1", rootRef(root).String()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// rootRef is the reference of a valid root's key.
func rootRef(root model.Root) model.Ref {
	if root.Task != nil {
		return root.Task.Ref()
	}
	return root.Page.Ref()
}
