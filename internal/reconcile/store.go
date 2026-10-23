package reconcile

import (
	"context"
	"errors"

	"github.com/sperano/sillage/internal/model"
)

// Store keeps what the loop reads, so that a story outlives its sources'
// cleanup and sillage's restarts.
type Store interface {
	// Records returns everything committed for root: its events, runs,
	// pull requests and correlations, and its coverage. A root never
	// committed has none, and no error.
	Records(ctx context.Context, root model.Root) (model.Records, error)
	// Checkpoints returns the checkpoints whose key starts with prefix, by
	// key.
	Checkpoints(ctx context.Context, prefix string) (map[string][]byte, error)
	// Commit writes c in one transaction: all of it, or nothing.
	Commit(ctx context.Context, c Commit) error
}

// Commit is what one read of a root adds to a store, or checkpoints alone.
type Commit struct {
	// Root is the story Records belong to; nil for checkpoints alone.
	Root *model.Root
	// Records' events, runs and pull requests are upserted by key, and
	// its correlations by the pair they join: nothing is deleted, except a
	// run keyed by pod once a run keyed by UID names the pod, which the
	// store re-keys, its correlations with it. An observation (an event
	// with a fingerprint id and no source time: a bucket, label or
	// assignee seen) has a new key at every read that commits; the store
	// keeps one per unchanged value, with when it was first and last seen.
	// Records' coverage replaces the root's coverage of the same source
	// instance.
	Records model.Records
	// Checkpoints are upserted by key.
	Checkpoints map[string][]byte
}

// The errors of a Locker.
var (
	// ErrLockHeld: another process holds the collector lock.
	ErrLockHeld = errors.New("another sillage process holds the collector lock")
	// ErrLockLost: the lock was lost while held, its connection gone.
	ErrLockLost = errors.New("the collector lock was lost")
)

// Locker lets one process collect at a time.
type Locker interface {
	// WithLock runs f while holding the lock, with a context canceled if
	// the lock is lost, and releases it when f returns. It returns
	// ErrLockHeld without running f when another process holds the lock,
	// and ErrLockLost when f stopped because the lock was lost.
	WithLock(ctx context.Context, f func(context.Context) error) error
}
