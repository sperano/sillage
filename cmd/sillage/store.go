package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/store"
)

// The store keeps the collector's Kelos Task origins: the collector loop
// passes it with collector.WithOriginMemory.
var _ collector.OriginMemory = (*store.Store)(nil)

const (
	// storeOpenTimeout bounds connecting, migrating and pruning at startup,
	// so that an unreachable database fails the start instead of hanging it.
	storeOpenTimeout = 5 * time.Minute
	// storeConnectAttempts is the maximum number of initial connection
	// attempts before startup fails.
	storeConnectAttempts = 30
	// storeConnectAttemptTimeout bounds each attempt to reach PostgreSQL.
	storeConnectAttemptTimeout = 5 * time.Second
	// storeConnectRetryInterval separates initial connection attempts.
	storeConnectRetryInterval = 2 * time.Second
)

type storeOpenFunc func(context.Context, store.Config) (*store.Store, error)
type storeRetryWait func(context.Context, time.Duration) error

// openStore opens the store, applying its migrations and its retention
// policy, when store.EnvDatabaseURL is set. Without it, it returns nil:
// sillage still serves, and the API answers 503 for what it would read.
func openStore(ctx context.Context, getenv func(string) string) (*store.Store, error) {
	return openStoreWith(ctx, getenv, store.Open, waitForStoreRetry, slog.Default())
}

func openStoreWith(ctx context.Context, getenv func(string) string, open storeOpenFunc,
	wait storeRetryWait, log *slog.Logger,
) (*store.Store, error) {
	cfg, err := store.ConfigFromEnv(getenv)
	if errors.Is(err, store.ErrNotConfigured) {
		slog.Warn("no database configured: nothing is stored", "env", store.EnvDatabaseURL)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, storeOpenTimeout)
	defer cancel()
	st, err := openStoreRetry(ctx, cfg, open, wait, log)
	if err != nil {
		return nil, err
	}
	pruned, err := st.Prune(ctx, time.Now())
	if err != nil {
		st.Close()
		return nil, err
	}
	slog.Info("store open", "retention", cfg.Retention.String(), "pruned", pruned)
	return st, nil
}

func openStoreRetry(ctx context.Context, cfg store.Config, open storeOpenFunc, wait storeRetryWait,
	log *slog.Logger,
) (*store.Store, error) {
	cfg.MaxConnectTimeout = storeConnectAttemptTimeout
	for attempt := 1; attempt <= storeConnectAttempts; attempt++ {
		st, err := open(ctx, cfg)
		if err == nil || !errors.Is(err, store.ErrConnecting) || attempt == storeConnectAttempts {
			return st, err
		}
		if stopErr := ctx.Err(); stopErr != nil {
			return nil, storeRetryStopped(err, stopErr)
		}
		log.Warn("store connection failed; retrying",
			"attempt", attempt, "maxAttempts", storeConnectAttempts,
			"retryIn", storeConnectRetryInterval, "error", err)
		if waitErr := wait(ctx, storeConnectRetryInterval); waitErr != nil {
			return nil, storeRetryStopped(err, waitErr)
		}
	}
	panic("unreachable")
}

func storeRetryStopped(connectErr, stopErr error) error {
	return fmt.Errorf("%w: retry stopped: %w", connectErr, stopErr)
}

func waitForStoreRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
