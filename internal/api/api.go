package api

import (
	"context"
	"net/http"
	"time"

	"github.com/sperano/sillage/internal/source/wiki"
	"github.com/sperano/sillage/internal/store"
)

// Prefix starts every API path. Every path under it is the API's: one no
// route serves is a JSON 404, never the frontend.
const Prefix = "/api/"

// The routes, under /api/v1/.
const (
	RouteTaskStory    = "GET /api/v1/stories/task/{instance}/{id}"
	RoutePageStory    = "GET /api/v1/stories/page/{instance}/{id}/{locale}"
	RouteTaskCoverage = "GET /api/v1/coverage/task/{instance}/{id}"
	RoutePageCoverage = "GET /api/v1/coverage/page/{instance}/{id}/{locale}"
	RouteCoverage     = "GET /api/v1/coverage"
	RouteResolve      = "GET /api/v1/resolve"
	RouteSearch       = "GET /api/v1/search"
	RouteVersion      = "GET /api/v1/version"
)

const (
	// DefaultTimeout bounds a request when Config.Timeout is zero.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxResponseBytes bounds a response's body when
	// Config.MaxResponseBytes is zero.
	DefaultMaxResponseBytes = 16 * mebibyte
	// MinResponseBytes is the lowest bound Config.MaxResponseBytes takes:
	// every problem body fits in it, the one saying a response exceeds
	// the bound included.
	MinResponseBytes = kibibyte
	kibibyte         = 1 << 10
	mebibyte         = 1 << 20
)

// PageLister lists a wiki's pages with their locale and path: the wiki
// source's client is one.
type PageLister interface {
	Pages(ctx context.Context) ([]wiki.Page, error)
}

// Config is what the API serves from.
type Config struct {
	// Store is what the API reads. Without one, every endpoint but the
	// version answers 503.
	Store store.Reader
	// Version is the binary's version, as the version endpoint reports it.
	Version string
	// Wiki lists the wiki's pages, so that resolve finds a page by the
	// path a URL names; WikiInstance is its host. Without them, a wiki
	// URL resolves only by page id (/i/<id>).
	Wiki         PageLister
	WikiInstance string
	// Timeout bounds each request; DefaultTimeout when zero.
	Timeout time.Duration
	// MaxResponseBytes bounds each response's body;
	// DefaultMaxResponseBytes when zero or less, and raised to
	// MinResponseBytes when positive but lower.
	MaxResponseBytes int
}

// api serves the routes of one Config.
type api struct {
	cfg Config
}

// New returns the API's handler, which serves every path under Prefix.
func New(cfg Config) http.Handler {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	switch {
	case cfg.MaxResponseBytes <= 0:
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	case cfg.MaxResponseBytes < MinResponseBytes:
		cfg.MaxResponseBytes = MinResponseBytes
	}
	a := &api{cfg: cfg}
	mux := http.NewServeMux()
	a.handle(mux, RouteTaskStory, a.needStore(a.taskStory))
	a.handle(mux, RoutePageStory, a.needStore(a.pageStory))
	a.handle(mux, RouteTaskCoverage, a.needStore(a.taskCoverage))
	a.handle(mux, RoutePageCoverage, a.needStore(a.pageCoverage))
	a.handle(mux, RouteCoverage, a.needStore(a.freshness))
	a.handle(mux, RouteResolve, a.needStore(a.resolve))
	a.handle(mux, RouteSearch, a.needStore(a.search))
	a.handle(mux, RouteVersion, a.version)
	a.handle(mux, Prefix, unrouted)
	return noStore(mux)
}

// endpoint answers a request with the value to send as JSON, or an error
// that problemOf turns into a status.
type endpoint func(r *http.Request) (any, error)

// handle serves pattern by e, within the request timeout.
func (a *api) handle(mux *http.ServeMux, pattern string, e endpoint) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), a.cfg.Timeout)
		defer cancel()
		body, err := e(r.WithContext(ctx))
		if err != nil {
			writeProblem(w, r, err)
			return
		}
		a.writeJSON(w, r, body)
	})
}

// needStore answers 503 when no store is configured, else as e.
func (a *api) needStore(e endpoint) endpoint {
	return func(r *http.Request) (any, error) {
		if a.cfg.Store == nil {
			return nil, problem(http.StatusServiceUnavailable, "no store is configured: nothing is stored")
		}
		return e(r)
	}
}

// unrouted answers a path under Prefix that no route serves: 405 for a
// method the API never serves, else 404.
func unrouted(r *http.Request) (any, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, errMethod
	}
	return nil, problem(http.StatusNotFound, "no API route serves "+echo(r.URL.Path))
}

// noStore keeps every API response out of caches: a story changes as the
// collector reads.
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}
