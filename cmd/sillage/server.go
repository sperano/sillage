package main

import (
	"io"
	"io/fs"
	"net/http"
)

// healthzPath answers 200 while the process can serve HTTP. It checks no
// dependency: a source being down must not make sillage look dead.
const healthzPath = "/healthz"

// newHandler routes sillage's endpoints; every other GET path is served
// from frontend.
func newHandler(frontend fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthzPath, handleHealthz)
	mux.Handle("GET /", http.FileServerFS(frontend))
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}
