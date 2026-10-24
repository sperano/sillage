package reconcile

import (
	"context"
	"errors"

	"github.com/sperano/sillage/internal/model"
)

// Store keeps what the loop reads, so that a story outlives its sources'
// cleanup and sillage's restarts. Each method returns, with an error,
// once its context is done: the loop bounds every call with a deadline.
type Store interface {
	// Records returns everything committed for root: its events, runs,
	// pull requests and correlations, and its coverage. A root never
	// committed has none, and no error.
	Records(ctx context.Context, root model.Root) (model.Records, error)
	// Checkpoints returns the values of the checkpoints whose key starts
	// with prefix, by key, of every source instance.
	Checkpoints(ctx context.Context, prefix string) (map[string][]byte, error)
	// Commit writes c in one transaction: all of it, or nothing.
	// internal/reconcile/storetest holds the contract every Store keeps.
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
	// assignee seen) has a new key at every read that commits records; the
	// store keeps one per value (the event but its id and ObservedAt) for
	// as long as the task's committed reads see it unchanged, with when it
	// was first seen, which Records returns as the event's id and
	// ObservedAt, and when it was last seen, the latest ObservedAt
	// committed for it, which the store keeps for the read API (the loop's
	// tests' fake keeps every observation instead: the loop reads none
	// back). A read whose story did not change commits no records, so
	// last seen is the last read that did; that the value still held at
	// the reads after it is what the root's coverage says, its
	// LastSuccessfulRead advancing while the story is unchanged.
	// Records' coverage replaces the root's coverage of the same source
	// instance; the root's coverage of the instances it does not carry is
	// kept.
	Records model.Records
	// Checkpoints are upserted by key.
	Checkpoints map[string]Checkpoint
}

// Checkpoint is one of the loop's checkpoints, with what it is about.
type Checkpoint struct {
	// Source and Instance are the source instance it belongs to. No two
	// checkpoints of one key belong to different instances: each key
	// names its instance.
	Source   model.Source
	Instance string
	// Root, when set, ties it to a root the store holds, or that the same
	// commit adds: dropping the root (retention) drops it too.
	Root *model.Root
	// Value is JSON.
	Value []byte
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
