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
	"github.com/sperano/sillage/internal/store"
)

// databaseURLEnv names the environment variable holding the PostgreSQL
// URL of the store. Unset, the collector does not run; the server still
// does.
const databaseURLEnv = "SILLAGE_DATABASE_URL"

// errNoStore is the start of a collector this build cannot store for.
var errNoStore = errors.New(databaseURLEnv + " is set, but this build has no PostgreSQL store yet")

// newMetrics returns the handler of /metrics, serving the Go runtime's
// and the process's metrics and the collector's, and the collector's.
func newMetrics() (http.Handler, *reconcile.Metrics) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := reconcile.NewMetrics(reg)
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), m
}

// openStore opens the PostgreSQL store at url. The store lands with its
// own change; until then a configured database fails the start rather
// than collect into nothing.
func openStore(context.Context, string) (reconcile.Store, error) { return nil, errNoStore }

// startCollector starts the collection loop the environment configures,
// until ctx is done, and returns a function that waits for it to stop.
// Without databaseURLEnv it starts nothing.
func startCollector(ctx context.Context, getenv func(string) string, m *reconcile.Metrics) (func(), error) {
	url := getenv(databaseURLEnv)
	if url == "" {
		slog.Info("collector off: " + databaseURLEnv + " is unset")
		return func() {}, nil
	}
	cfg, err := reconcile.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	st, err := openStore(ctx, url)
	if err != nil {
		return nil, err
	}
	lock, err := store.NewAdvisoryLock(url)
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
	loop, err := reconcile.New(cfg, env.Collector, reconcile.SourcesOf(clients), st, m, slog.Default())
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx, lock)
	}()
	return func() { <-done }, nil
}
