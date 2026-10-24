package main

import (
	"context"
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

// newMetrics returns the handler of /metrics, which serves the Go
// runtime's, the process's, the collector's and the endpoints' metrics,
// the collector's metrics to record into, and the registry the endpoints
// register theirs with.
func newMetrics() (http.Handler, *reconcile.Metrics, prometheus.Registerer) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := reconcile.NewMetrics(reg)
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), m, reg
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
	loop, err := reconcile.New(cfg, env.Collector, reconcile.SourcesOf(clients), loopStore{st}, m, log)
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
