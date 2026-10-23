package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/wiki"
	"github.com/sperano/sillage/internal/store"
)

const (
	// MaxSearchQuery bounds a search query, in characters.
	MaxSearchQuery = 200
	// maxInstance bounds an instance path segment: a DNS name and a port.
	maxInstance = 260
	// apiVersion is the version of the routes under /api/v1/.
	apiVersion = "v1"
)

// The query parameters.
const (
	paramQuery = "q"
	paramLimit = "limit"
	paramURL   = "url"
)

// CoverageResponse is a root's coverage: how far each source was read
// for it, and where its records are missing.
type CoverageResponse struct {
	Root     model.Root       `json:"root"`
	Coverage []model.Coverage `json:"coverage"`
}

// FreshnessResponse is how recently each source instance was read, across
// the stored roots. Gaps are per root: see a root's coverage.
type FreshnessResponse struct {
	Sources []store.SourceFreshness `json:"sources"`
}

// SearchResponse lists the roots a query matches, most recently active
// first.
type SearchResponse struct {
	Query string              `json:"query"`
	Roots []store.RootSummary `json:"roots"`
}

// VersionResponse names the binary's and the API's versions.
type VersionResponse struct {
	Version string `json:"version"`
	API     string `json:"api"`
}

func (a *api) taskStory(r *http.Request) (any, error) {
	root, err := taskRoot(r)
	if err != nil {
		return nil, err
	}
	return a.cfg.Store.Story(r.Context(), root)
}

func (a *api) pageStory(r *http.Request) (any, error) {
	root, err := pageRoot(r)
	if err != nil {
		return nil, err
	}
	return a.cfg.Store.Story(r.Context(), root)
}

func (a *api) taskCoverage(r *http.Request) (any, error) {
	root, err := taskRoot(r)
	if err != nil {
		return nil, err
	}
	return a.coverage(r, root)
}

func (a *api) pageCoverage(r *http.Request) (any, error) {
	root, err := pageRoot(r)
	if err != nil {
		return nil, err
	}
	return a.coverage(r, root)
}

func (a *api) coverage(r *http.Request, root model.Root) (any, error) {
	cov, err := a.cfg.Store.Coverage(r.Context(), root)
	if err != nil {
		return nil, err
	}
	return CoverageResponse{Root: root, Coverage: nonNil(cov)}, nil
}

func (a *api) freshness(r *http.Request) (any, error) {
	sources, err := a.cfg.Store.Freshness(r.Context())
	if err != nil {
		return nil, err
	}
	return FreshnessResponse{Sources: nonNil(sources)}, nil
}

// search matches q against the stored roots' titles and keys; an empty
// q lists the most recently active roots.
func (a *api) search(r *http.Request) (any, error) {
	q := strings.TrimSpace(r.URL.Query().Get(paramQuery))
	if n := utf8.RuneCountInString(q); n > MaxSearchQuery {
		return nil, badRequest("the query has %d characters, more than the %d allowed", n, MaxSearchQuery)
	}
	limit, err := searchLimit(r.URL.Query().Get(paramLimit))
	if err != nil {
		return nil, err
	}
	roots, err := a.cfg.Store.SearchRoots(r.Context(), store.RootQuery{Text: q, Limit: limit})
	if err != nil {
		return nil, err
	}
	return SearchResponse{Query: q, Roots: nonNil(roots)}, nil
}

// searchLimit parses the limit parameter: empty for the store's default,
// else 1 to store.MaxSearchLimit.
func searchLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > store.MaxSearchLimit {
		return 0, badRequest("%s must be a number from 1 to %d", paramLimit, store.MaxSearchLimit)
	}
	return n, nil
}

func (a *api) version(*http.Request) (any, error) {
	return VersionResponse{Version: a.cfg.Version, API: apiVersion}, nil
}

// taskRoot reads the task a path names.
func taskRoot(r *http.Request) (model.Root, error) {
	instance, err := instanceParam(r)
	if err != nil {
		return model.Root{}, err
	}
	id, err := idParam(r)
	if err != nil {
		return model.Root{}, err
	}
	return model.Root{Task: &model.TaskKey{Instance: instance, ID: id}}, nil
}

// pageRoot reads the wiki page a path names.
func pageRoot(r *http.Request) (model.Root, error) {
	instance, err := instanceParam(r)
	if err != nil {
		return model.Root{}, err
	}
	id, err := idParam(r)
	if err != nil {
		return model.Root{}, err
	}
	locale := r.PathValue("locale")
	if !wiki.IsLocale(locale) {
		return model.Root{}, badRequest("%q is not a wiki locale", locale)
	}
	return model.Root{Page: &model.PageKey{Instance: instance, ID: id, Locale: locale}}, nil
}

// instanceParam is the path's instance, a host in lower case as the
// model's keys name it.
func instanceParam(r *http.Request) (string, error) {
	instance := strings.ToLower(r.PathValue("instance"))
	if instance == "" || len(instance) > maxInstance {
		return "", badRequest("the instance must be a host of at most %d characters", maxInstance)
	}
	return instance, nil
}

// idParam is the path's id, a positive number.
func idParam(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("%q is not a positive id", raw)
	}
	return id, nil
}

// nonNil keeps an empty list a JSON array rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
