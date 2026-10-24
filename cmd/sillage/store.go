package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/store"
)

// The store keeps the collector's Kelos Task origins: the collector loop
// passes it with collector.WithOriginMemory.
var _ collector.OriginMemory = (*store.Store)(nil)

// storeOpenTimeout bounds connecting, migrating and pruning at startup, so
// that an unreachable database fails the start instead of hanging it.
const storeOpenTimeout = 5 * time.Minute

// openStore opens the store, applying its migrations and its retention
// policy, when store.EnvDatabaseURL is set. Without it, it returns nil:
// sillage still serves, and the API answers 503 for what it would read.
func openStore(ctx context.Context, getenv func(string) string) (*store.Store, error) {
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
	st, err := store.Open(ctx, cfg)
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
