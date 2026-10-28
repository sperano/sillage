package main

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/store"
)

// newIngest returns the handler of the webhook ingest paths over st,
// asking sched to read the roots deliveries name and counting them in reg.
// Without a webhook secret, a store, a scheduler or the sources'
// configuration, it returns a handler that answers 503 to every request
// and records nothing, and says why.
func newIngest(getenv func(string) string, st *store.Store, sched ingest.Scheduler,
	reg prometheus.Registerer) http.Handler {
	secrets := ingest.SecretsFromEnv(getenv)
	var off string
	switch {
	case !secrets.Any():
		off = "no webhook secret is configured ($" + ingest.EnvGitHubSecret + ", $" + ingest.EnvVikunjaSecret + ")"
	case st == nil:
		off = "no database is configured ($" + store.EnvDatabaseURL + ")"
	case sched == nil:
		off = "the collection loop does not read the roots deliveries name yet"
	default:
		h, err := buildIngest(getenv, secrets, st, sched, reg)
		if err == nil {
			return h
		}
		off = err.Error()
	}
	slog.Warn("webhook ingestion off: every delivery is refused", "reason", off)
	return ingestUnavailable(off)
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
