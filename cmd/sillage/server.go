package main

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/sperano/sillage/internal/api"
)

// healthzPath answers 200 while the process can serve HTTP. It checks no
// dependency: a source being down must not make sillage look dead.
const healthzPath = "/healthz"

// indexFile is the frontend's entry page, served for every path that is
// none of its files.
const indexFile = "index.html"

// newHandler routes sillage's endpoints: the health check, the read API
// under api.Prefix (all of it, its unknown paths too), and frontend for
// every other GET path. The frontend's pattern takes every method, as a
// method-specific "GET /" would conflict with the API's subtree: it
// refuses the others itself.
func newHandler(frontend fs.FS, apiHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthzPath, handleHealthz)
	mux.Handle(api.Prefix, apiHandler)
	mux.Handle("/", frontendHandler(frontend))
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

// frontendHandler serves frontend's files, and its index.html for any
// other path, so that a deep link to a client-side route loads the app,
// which then routes it (and says when nothing matches).
func frontendHandler(frontend fs.FS) http.Handler {
	files := http.FileServerFS(frontend)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if name := strings.TrimPrefix(path.Clean(r.URL.Path), "/"); name != "" && isFile(frontend, name) {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFileFS(w, r, frontend, indexFile)
	})
}

// isFile reports whether name is a regular file of fsys: a directory is
// no file to serve, so it gets the entry page rather than a listing.
func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}
