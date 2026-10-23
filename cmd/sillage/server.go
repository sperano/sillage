package main

import (
	"io"
	"io/fs"
	"net/http"
)

const (
	// healthzPath answers 200 while the process can serve HTTP. It checks
	// no dependency: a source being down must not make sillage look dead.
	healthzPath = "/healthz"
	// metricsPath serves the Prometheus metrics. Who may reach it is the
	// deployment's decision.
	metricsPath = "/metrics"
)

// newHandler routes sillage's endpoints; every other GET path is served
// from frontend.
func newHandler(frontend fs.FS, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthzPath, handleHealthz)
	mux.Handle("GET "+metricsPath, metrics)
	mux.Handle("GET /", http.FileServerFS(frontend))
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}
