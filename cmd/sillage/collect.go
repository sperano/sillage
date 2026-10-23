package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/reconcile/pglock"
	"github.com/sperano/sillage/internal/store"
)

// errNoStore is the start of a collector this build cannot write to the
// store for: the loop's store adapter lands with its own change.
var errNoStore = errors.New(store.EnvDatabaseURL + " is set, but the collector cannot write to this store yet")

// newMetrics returns the handler of /metrics, which serves the Go
// runtime's, the process's and the collector's metrics, and the
// collector's metrics.
func newMetrics() (http.Handler, *reconcile.Metrics) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := reconcile.NewMetrics(reg)
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), m
}

// startCollector starts the collection loop the environment configures
// into st, until ctx is done, and returns a function that waits for it to
// stop. Without a store it starts nothing. The loop's lock is on st's
// database.
func startCollector(ctx context.Context, getenv func(string) string, st *store.Store, m *reconcile.Metrics,
	log *slog.Logger) (func(), error) {
	if st == nil {
		log.Info("collector off: no store", "env", store.EnvDatabaseURL)
		return func() {}, nil
	}
	cfg, err := reconcile.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	scfg, err := store.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	lock, err := pglock.New(string(scfg.DatabaseURL))
	if err != nil {
		return nil, err
	}
	env, err := collector.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	clients, err := env.OpenClients()
	if err != nil {
		return nil, err
	}
	_, _, _ = cfg, lock, clients
	return nil, errNoStore
}
