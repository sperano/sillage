package main

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/dynamic"

	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/ingest/marker"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/store"
)

// newMarkerIngest returns the handler of marker.Path over st, reviewing
// the pods' tokens through the client newClient returns, for the Kelos
// cluster and namespace the environment configures, and counting markers
// in reg. Without a store it returns a handler that answers 503 to every
// request and records nothing, and says why. Unlike the webhook paths, it
// needs no collector: a marker asks for no read.
func newMarkerIngest(getenv func(string) string, st *store.Store, reg prometheus.Registerer,
	newClient func() (dynamic.Interface, error)) (http.Handler, error) {
	if st == nil {
		off := "no database is configured ($" + store.EnvDatabaseURL + ")"
		slog.Warn("phase marker ingestion off: every marker is refused", "reason", off)
		return unavailable("phase marker ingestion is off: " + off), nil
	}
	return buildMarkerIngest(getenv, st, reg, newClient)
}

// buildMarkerIngest returns the marker handler over st.
func buildMarkerIngest(getenv func(string) string, st marker.Store, reg prometheus.Registerer,
	newClient func() (dynamic.Interface, error)) (http.Handler, error) {
	kcfg, err := kelos.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	client, err := newClient()
	if err != nil {
		return nil, err
	}
	rev, err := marker.NewKubeReviewer(client)
	if err != nil {
		return nil, err
	}
	m, err := marker.NewMetrics(reg)
	if err != nil {
		return nil, err
	}
	h, err := marker.New(marker.Config{Cluster: kcfg.Cluster, Namespace: kcfg.Namespace}, rev, st, m, slog.Default())
	if err != nil {
		return nil, err
	}
	slog.Info("phase marker ingestion on", "cluster", kcfg.Cluster, "namespace", kcfg.Namespace)
	return h, nil
}

// ingestRoutes serves marker.Path with markers, and every other ingest
// path with webhooks.
func ingestRoutes(webhooks, markers http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(ingest.Prefix, webhooks)
	mux.Handle(marker.Path, markers)
	return mux
}
