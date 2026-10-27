package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

// LinkKind names what a resolved link points to.
type LinkKind string

// The kinds of link resolve reads.
const (
	LinkVikunjaTask LinkKind = "vikunja-task"
	LinkPullRequest LinkKind = "github-pull-request"
	LinkKelosTask   LinkKind = "kelos-task"
)

const (
	// MaxResolveURL bounds the link resolve reads, in bytes.
	MaxResolveURL = 2048
	// vikunjaTaskRoute starts a Vikunja task's URL: /tasks/<id>.
	vikunjaTaskRoute = "tasks"
	// pullRoute is a GitHub pull request URL's third segment:
	// /<owner>/<repo>/pull/<number>.
	pullRoute = "pull"
	// maxTaskName bounds a Kubernetes object name (a DNS subdomain).
	maxTaskName = 253
)

var (
	// dnsLabelPattern matches a DNS label: a Kubernetes namespace, and
	// each dot-separated label of an object name (a DNS subdomain).
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
	// uidPattern matches a Kubernetes UID.
	uidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ResolveResponse names the roots whose stories hold what a link points
// to, most recently active first, and what kind of link it was read as.
type ResolveResponse struct {
	Kind  LinkKind            `json:"kind"`
	Roots []store.RootSummary `json:"roots"`
}

// lookup is one reading of a link: what it would point to, and how to
// find the stored stories that hold it.
type lookup struct {
	kind LinkKind
	find func(ctx context.Context) ([]store.RootSummary, error)
}

// resolve answers with the first reading of the link that a stored story
// holds, or 404.
func (a *api) resolve(r *http.Request) (any, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(paramURL))
	switch {
	case raw == "":
		return nil, badRequest("%s is required: a Vikunja task or GitHub pull request URL, "+
			"or a Kelos Task reference", paramURL)
	case len(raw) > MaxResolveURL:
		return nil, badRequest("%s has %d bytes, more than the %d allowed", paramURL, len(raw), MaxResolveURL)
	}
	lookups, link := a.lookups(raw)
	if len(lookups) == 0 && !link {
		return nil, badRequest("%s is neither a link nor a Kelos Task reference ([<namespace>/]<name> or a UID)",
			paramURL)
	}
	for _, l := range lookups {
		roots, err := l.find(r.Context())
		if err != nil {
			return nil, err
		}
		if len(roots) > 0 {
			return ResolveResponse{Kind: l.kind, Roots: roots}, nil
		}
	}
	return nil, problem(http.StatusNotFound, "no stored story holds what the link points to")
}

// lookups are the readings of raw, in the order they are tried, and
// whether raw is a link: an http or https URL by its host and path, or,
// when its path is none of a Vikunja task or pull request,
// by its last segment as a Kelos Task (the console has no documented deep
// link); anything else as a Kelos Task reference.
func (a *api) lookups(raw string) ([]lookup, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return a.kelosLookups(raw), false
	}
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	var out []lookup
	for _, host := range hostsOf(u) {
		out = append(out, a.urlLookups(host, raw, parts)...)
	}
	if len(out) == 0 && len(parts) > 0 {
		out = a.kelosLookups(parts[len(parts)-1])
	}
	return out, true
}

// hostsOf are the instance names u's host may be stored under: the host
// in lower case, with its port as GitHub names an instance, and without,
// as the collector names Vikunja's.
func hostsOf(u *url.URL) []string {
	hosts := []string{strings.ToLower(u.Host)}
	if name := strings.ToLower(u.Hostname()); name != hosts[0] {
		hosts = append(hosts, name)
	}
	return hosts
}

// urlLookups are the readings of a URL at host with path parts: a Vikunja
// task or a GitHub pull request.
func (a *api) urlLookups(host, raw string, parts []string) []lookup {
	var out []lookup
	if len(parts) == 2 && parts[0] == vikunjaTaskRoute {
		if id, ok := positive(parts[1]); ok {
			out = append(out, a.rootLookup(LinkVikunjaTask, model.Root{Task: &model.TaskKey{Instance: host, ID: id}}))
		}
	}
	if len(parts) >= 4 && parts[2] == pullRoute {
		if n, ok := positive(parts[3]); ok {
			key := model.PRKey{Instance: host, Repository: parts[0] + "/" + parts[1], Number: int(n)}
			out = append(out, lookup{LinkPullRequest, func(ctx context.Context) ([]store.RootSummary, error) {
				return a.cfg.Store.RootsOfPullRequest(ctx, key)
			}})
		}
	}
	return out
}

// rootLookup finds root itself.
func (a *api) rootLookup(kind LinkKind, root model.Root) lookup {
	return lookup{kind, func(ctx context.Context) ([]store.RootSummary, error) {
		found, err := a.cfg.Store.Root(ctx, root)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []store.RootSummary{found}, nil
	}}
}

// kelosLookups are the readings of a Kelos Task reference:
// [<namespace>/]<name>, or a UID, which is also a valid name.
func (a *api) kelosLookups(ref string) []lookup {
	namespace, name, found := strings.Cut(ref, "/")
	if !found {
		namespace, name = "", ref
	}
	if !isTaskName(name) || (found && !dnsLabelPattern.MatchString(namespace)) {
		return nil
	}
	queries := []store.RunQuery{{Namespace: namespace, Name: name}}
	if uidPattern.MatchString(name) {
		queries = slices.Insert(queries, 0, store.RunQuery{Namespace: namespace, UID: name})
	}
	out := make([]lookup, 0, len(queries))
	for _, q := range queries {
		out = append(out, lookup{LinkKelosTask, func(ctx context.Context) ([]store.RootSummary, error) {
			return a.cfg.Store.RootsOfRuns(ctx, q)
		}})
	}
	return out
}

// isTaskName reports whether name is a Kubernetes object name: a DNS
// subdomain, of DNS labels joined by dots.
func isTaskName(name string) bool {
	if len(name) > maxTaskName {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

// positive parses a positive id written in decimal digits only, without
// leading zeroes, so that one resource has one spelling ("+5" and "05"
// are none).
func positive(s string) (int64, bool) {
	if s == "" || s[0] == '0' || strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}
