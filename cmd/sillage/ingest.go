package main

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/store"
)

// notCollecting is why the ingest paths refuse a delivery while the
// collection loop of this process does not collect.
const notCollecting = "this process does not collect (another holds the collector lock, or the store's " +
	"state is loading): its periodic reads catch up"

// newIngest returns the handler of the webhook ingest paths over st,
// asking sched, which c's loop reads, to read the roots deliveries name,
// and counting them in reg. It serves only while c collects. Without a
// webhook secret, a store or a collection loop, it returns a handler that
// answers 503 to every request and records nothing, and says why.
func newIngest(getenv func(string) string, st *store.Store, sched ingest.Scheduler, c *collection,
	reg prometheus.Registerer) (http.Handler, error) {
	secrets := ingest.SecretsFromEnv(getenv)
	var off string
	switch {
	case !secrets.Any():
		off = "no webhook secret is configured ($" + ingest.EnvGitHubSecret + ", $" + ingest.EnvVikunjaSecret + ")"
	case st == nil:
		off = "no database is configured ($" + store.EnvDatabaseURL + ")"
	case c == nil:
		off = "no collection loop reads the roots deliveries name"
	default:
		h, err := buildIngest(getenv, secrets, st, sched, reg)
		if err != nil {
			return nil, err
		}
		return whileCollecting(c.collecting, h), nil
	}
	slog.Warn("webhook ingestion off: every delivery is refused", "reason", off)
	return ingestUnavailable(off), nil
}

// whileCollecting serves h while collecting reports true, and otherwise
// answers 503 and records nothing: only the process that collects takes
// deliveries, so that no read they ask for waits in the queue of a
// process that reads none. Vikunja retries a delivery refused so; GitHub
// does not, and the periodic reads find what it named.
func whileCollecting(collecting func() bool, h http.Handler) http.Handler {
	off := ingestUnavailable(notCollecting)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !collecting() {
			off.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// buildIngest returns the ingest handler over st, for the sources the
// environment configures.
func buildIngest(getenv func(string) string, secrets ingest.Secrets, st ingest.Store, sched ingest.Scheduler,
	reg prometheus.Registerer) (http.Handler, error) {
	env, err := collector.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	m, err := ingest.NewMetrics(reg)
	if err != nil {
		return nil, err
	}
	h, err := ingest.New(ingest.Config{Collector: env.Collector, Secrets: secrets}, st, sched, m, slog.Default())
	if err != nil {
		return nil, err
	}
	slog.Info("webhook ingestion on", "github", secrets.GitHub != "", "vikunja", secrets.Vikunja != "")
	return h, nil
}

// ingestUnavailable answers every webhook delivery 503, saying why, and
// records nothing.
func ingestUnavailable(reason string) http.Handler {
	return unavailable("webhook ingestion is off: " + reason)
}

// unavailable answers every request 503 with msg, and records nothing.
func unavailable(msg string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, msg, http.StatusServiceUnavailable)
	})
}
