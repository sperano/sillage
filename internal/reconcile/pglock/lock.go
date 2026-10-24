// Package pglock is the collector lock as a PostgreSQL session advisory
// lock: one sillage process collects at a time, on whatever database the
// store is in.
package pglock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/reconcile"
)

const (
	// collectorKey is the collector's advisory lock key: "sillage" in
	// ASCII, unlikely to be another application's key.
	collectorKey int64 = 0x73696c6c616765
	// checkInterval is how often a held lock's connection is checked, and
	// how long one check may take: a process whose connection is gone no
	// longer holds the lock, and another may take it. Until the check
	// fails, the process that lost the lock goes on collecting, for up to
	// twice this long.
	checkInterval = 15 * time.Second
	// releaseTimeout bounds releasing the lock, once the work is done or
	// canceled.
	releaseTimeout = 5 * time.Second
)

// Lock is the collector lock, held on a connection of its own: a session
// advisory lock lasts as long as the session, so a process that dies
// releases it with its connection.
type Lock struct {
	config *pgx.ConnConfig
	key    int64
	check  time.Duration
}

var _ reconcile.Locker = (*Lock)(nil)

// New returns the collector lock on the database databaseURL names. Its
// errors never repeat the URL, which holds the password.
func New(databaseURL string) (*Lock, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("pglock: the database URL does not parse")
	}
	return &Lock{config: cfg, key: collectorKey, check: checkInterval}, nil
}

// WithLock implements reconcile.Locker.
func (l *Lock) WithLock(ctx context.Context, f func(context.Context) error) error {
	conn, err := pgx.ConnectConfig(ctx, l.config)
	if err != nil {
		return fmt.Errorf("pglock: connecting: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", l.key).Scan(&locked); err != nil {
		return fmt.Errorf("pglock: taking the lock: %w", err)
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
	cancel(nil)
	<-watched
	// The watcher has stopped: the cause is final. A loss can race f
	// returning for its own reason; the loss wins, the lock being gone.
	if errors.Is(context.Cause(lockCtx), reconcile.ErrLockLost) {
		return reconcile.ErrLockLost
	}
	releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancelRelease()
	// Closing the connection releases the lock too, so a failed unlock
	// changes nothing; unlocking first releases it at once, even if the
	// close is slow.
	_, _ = conn.Exec(releaseCtx, "SELECT pg_advisory_unlock($1)", l.key)
	return err
}

// watch checks conn until ctx is done, and cancels ctx with
// reconcile.ErrLockLost when the connection, and so the lock, is gone or
// does not answer within a check interval.
func (l *Lock) watch(ctx context.Context, conn *pgx.Conn, cancel context.CancelCauseFunc) {
	t := time.NewTicker(l.check)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, cancelPing := context.WithTimeout(ctx, l.check)
			err := conn.Ping(pingCtx)
			cancelPing()
			if err != nil && ctx.Err() == nil {
				cancel(reconcile.ErrLockLost)
				return
			}
		}
	}
}
