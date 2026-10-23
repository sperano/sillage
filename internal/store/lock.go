package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/reconcile"
)

const (
	// collectorLockKey is the collector's PostgreSQL advisory lock key:
	// "sillage" in ASCII, unlikely to be another application's key.
	collectorLockKey int64 = 0x73696c6c616765
	// lockCheckInterval is how often a held lock's connection is checked:
	// a process whose connection is gone no longer holds the lock, and
	// another may take it.
	lockCheckInterval = 15 * time.Second
	// lockReleaseTimeout bounds releasing the lock, after the work is done
	// or canceled.
	lockReleaseTimeout = 5 * time.Second
)

// AdvisoryLock is the collector lock as a PostgreSQL session advisory
// lock, held on a connection of its own: the lock lasts as long as the
// session, so a process that dies releases it with its connection.
type AdvisoryLock struct {
	config *pgx.ConnConfig
	key    int64
	check  time.Duration
}

// NewAdvisoryLock returns the collector lock on the database databaseURL
// names. Its errors never repeat the URL, which holds the password.
func NewAdvisoryLock(databaseURL string) (*AdvisoryLock, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("store: the database URL does not parse")
	}
	return &AdvisoryLock{config: cfg, key: collectorLockKey, check: lockCheckInterval}, nil
}

// WithLock implements reconcile.Locker.
func (l *AdvisoryLock) WithLock(ctx context.Context, f func(context.Context) error) error {
	conn, err := pgx.ConnectConfig(ctx, l.config)
	if err != nil {
		return fmt.Errorf("store: connecting for the collector lock: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lockReleaseTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", l.key).Scan(&locked); err != nil {
		return fmt.Errorf("store: taking the collector lock: %w", err)
	}
	if !locked {
		return reconcile.ErrLockHeld
	}
	lockCtx, cancel := context.WithCancelCause(ctx)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		l.watch(lockCtx, conn, cancel)
	}()
	err = f(lockCtx)
	lost := errors.Is(context.Cause(lockCtx), reconcile.ErrLockLost)
	cancel(nil)
	<-watched
	if lost {
		return reconcile.ErrLockLost
	}
	releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(ctx), lockReleaseTimeout)
	defer cancelRelease()
	// Closing the connection releases the lock too; unlocking first says
	// so at once, even if the close is slow.
	_, _ = conn.Exec(releaseCtx, "SELECT pg_advisory_unlock($1)", l.key)
	return err
}

// watch checks conn until ctx is done, and cancels it with
// reconcile.ErrLockLost when the connection, and so the lock, is gone.
func (l *AdvisoryLock) watch(ctx context.Context, conn *pgx.Conn, cancel context.CancelCauseFunc) {
	t := time.NewTicker(l.check)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := conn.Ping(ctx); err != nil && ctx.Err() == nil {
				cancel(reconcile.ErrLockLost)
				return
			}
		}
	}
}
