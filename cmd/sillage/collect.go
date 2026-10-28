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

// collection is the collection loop the environment configures, and the
// lock it collects under.
type collection struct {
	loop *reconcile.Loop
	lock reconcile.Locker
}

// newCollector returns the collection loop the environment configures
// into st, which reads the roots deliveries name too; nil, and nothing to
// start, without a store. The loop's lock is on st's database.
func newCollector(getenv func(string) string, st *store.Store, deliveries reconcile.Deliveries,
	m *reconcile.Metrics, log *slog.Logger) (*collection, error) {
	if st == nil {
		log.Info("collector off: no store", "env", store.EnvDatabaseURL)
		return nil, nil
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
	loop, err := reconcile.New(cfg, env.Collector, reconcile.SourcesOf(clients), loopStore{st}, deliveries, m, log)
	if err != nil {
		return nil, err
	}
	return &collection{loop: loop, lock: lock}, nil
}

// start runs the loop until ctx is done, and returns a function that
// waits for it to stop. A nil c starts nothing.
func (c *collection) start(ctx context.Context) func() {
	if c == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.loop.Run(ctx, c.lock)
	}()
	return func() { <-done }
}

// collecting reports whether the loop collects: it holds the lock and
// has loaded the store's state.
func (c *collection) collecting() bool { return c.loop.Collecting() }
