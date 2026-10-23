package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sperano/sillage/internal/store"
)

// openStore opens the store, applying its migrations and its retention
// policy, when store.EnvDatabaseURL is set. Without it, it returns nil:
// nothing reads or writes the store yet, so sillage still serves.
func openStore(ctx context.Context, getenv func(string) string) (*store.Store, error) {
	cfg, err := store.ConfigFromEnv(getenv)
	if errors.Is(err, store.ErrNotConfigured) {
		slog.Warn("no database configured: nothing is stored", "env", store.EnvDatabaseURL)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
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
