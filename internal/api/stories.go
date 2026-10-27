package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sperano/sillage/internal/model"
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

// instancePattern matches the characters of a host, with its port or as a
// bracketed IPv6 address, in ASCII: no space, slash or control character,
// nothing the store would refuse, and nothing that lower case would turn
// into another spelling of a host.
var instancePattern = regexp.MustCompile(`^[a-zA-Z0-9.:\[\]-]+$`)

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

func (a *api) taskCoverage(r *http.Request) (any, error) {
	root, err := taskRoot(r)
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

// instanceParam is the path's instance, a host in lower case as the
// model's keys name it.
func instanceParam(r *http.Request) (string, error) {
	instance := r.PathValue("instance")
	if len(instance) > maxInstance || !instancePattern.MatchString(instance) {
		return "", badRequest("the instance must be a host, with or without its port, of at most %d characters",
			maxInstance)
	}
	return strings.ToLower(instance), nil
}

// idParam is the path's id, a positive number.
func idParam(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, ok := positive(raw)
	if !ok {
		return 0, badRequest("%s is not a positive id", echo(raw))
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
