package main

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
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

// collection is the collection loop the environment configures, not yet
// started, and the sources it does not read.
type collection struct {
	// loop and lock are nil when the collector is off as a whole.
	loop *reconcile.Loop
	lock reconcile.Locker
	// off are the sources not read, and why, as the API reports them.
	off []api.OffSource
}

// newCollection reads the collection loop's configuration into st, a
// loop which reads the roots deliveries name too. A source whose
// configuration is incomplete is off, and logged once; a configuration
// that names no source instance, or no loop setting, turns the collector
// off as a whole, logged too. Without a store it reads nothing. It fails
// only when the lock cannot be made on st's database.
func newCollection(getenv func(string) string, st *store.Store, deliveries reconcile.Deliveries,
	m *reconcile.Metrics, log *slog.Logger) (collection, error) {
	if st == nil {
		log.Info("collector off: no store", "env", store.EnvDatabaseURL)
		return collection{}, nil
	}
	scfg, err := store.ConfigFromEnv(getenv)
	if err != nil {
		return collection{}, err
	}
	lock, err := pglock.New(string(scfg.DatabaseURL))
	if err != nil {
		return collection{}, err
	}
	cfg, err := reconcile.ConfigFromEnv(getenv)
	if err != nil {
		return collectorOff(log, err), nil
	}
	env, err := collector.ConfigFromEnv(getenv)
	if err != nil {
		return collectorOff(log, err), nil
	}
	clients := env.OpenClients()
	off := offSources(env.Collector)
	for _, o := range off {
		log.Warn("source off: its configuration is incomplete, so it is not read", "source", o.Source,
			"instance", o.Instance, "reason", o.Reason)
	}
	src := reconcile.SourcesOf(clients, receipts{st: st, cfg: env.Collector})
	loop, err := reconcile.New(cfg, env.Collector, src, loopStore{st}, deliveries, m, log)
	if err != nil {
		return collectorOff(log, err), nil
	}
	return collection{loop: loop, lock: lock, off: off}, nil
}

// collectorOff logs that the collector is off for err, and returns the
// collection that reads nothing, every source off for that reason.
func collectorOff(log *slog.Logger, err error) collection {
	log.Error("collector off: its configuration is incomplete, so no source is read", "error", err)
	reason := "the collector is off: " + strings.ReplaceAll(err.Error(), "\n", "; ")
	var c collection
	for _, s := range []model.Source{model.SourceGitHub, model.SourceKelos, model.SourceVikunja} {
		c.off = append(c.off, api.OffSource{Source: s, Reason: reason})
	}
	return c
}

// offSources are the sources cfg leaves off, in source order.
func offSources(cfg collector.Config) []api.OffSource {
	var out []api.OffSource
	for _, s := range slices.Sorted(maps.Keys(cfg.Off)) {
		out = append(out, api.OffSource{Source: s, Instance: cfg.Instance(s), Reason: cfg.Off[s]})
	}
	return out
}

// start runs the loop until ctx is done, and returns a function that
// waits for it to stop. With the collector off, it starts nothing.
func (c collection) start(ctx context.Context) func() {
	if c.loop == nil {
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
// has loaded the store's state. With the collector off, it never does.
func (c collection) collecting() bool { return c.loop != nil && c.loop.Collecting() }
