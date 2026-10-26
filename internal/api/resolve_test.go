package api

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func resolvePath(link string) string { return "/api/v1/resolve?url=" + url.QueryEscape(link) }

func resolveAPI(t *testing.T) http.Handler {
	t.Helper()
	return New(Config{Store: newFakeStore(t)})
}

func TestResolve(t *testing.T) {
	h := resolveAPI(t)
	tests := []struct {
		name string
		link string
		kind LinkKind
		want []string
	}{
		{"vikunja task", "https://vikunja.example.com/tasks/101", LinkVikunjaTask, []string{"discuss-no-pr"}},
		{"vikunja comment anchor", "https://Vikunja.example.com/tasks/101#comment-3", LinkVikunjaTask,
			[]string{"discuss-no-pr"}},
		{"vikunja on a port", "https://vikunja.example.com:8443/tasks/101/", LinkVikunjaTask,
			[]string{"discuss-no-pr"}},
		{"pull request", "https://github.example.com/example-org/widgets/pull/7", LinkPullRequest,
			[]string{"implementation-merged"}},
		{"pull request's files", "https://github.example.com/Example-Org/widgets/pull/7/files", LinkPullRequest,
			[]string{"implementation-merged"}},
		{"kelos task name, every attempt", "vikunja-widgets-agent-opus-104", LinkKelosTask,
			[]string{"failed-run-retry"}},
		{"kelos task in its namespace", "agents/vikunja-widgets-agent-opus-104", LinkKelosTask,
			[]string{"failed-run-retry"}},
		{"kelos task uid", "00000000-0000-4000-8000-00000000104b", LinkKelosTask, []string{"failed-run-retry"}},
		{"kelos placeholder run", "vikunja-widgets-agent-opus-105", LinkKelosTask, []string{"announced-run-only"}},
		{"kelos console URL", "https://kelos.example.com/agents/tasks/vikunja-widgets-agent-opus-102", LinkKelosTask,
			[]string{"implementation-merged"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ResolveResponse
			decode(t, get(t, h, resolvePath(tt.link)), &got)
			var titles []string
			for _, r := range got.Roots {
				titles = append(titles, r.Title)
			}
			if got.Kind != tt.kind || !slices.Equal(titles, tt.want) {
				t.Errorf("resolve(%q) = %s %q, want %s %q", tt.link, got.Kind, titles, tt.kind, tt.want)
			}
		})
	}
}

func TestResolveNotFound(t *testing.T) {
	h := resolveAPI(t)
	for _, link := range []string{
		"https://vikunja.example.com/tasks/999",
		"https://vikunja.example.org/tasks/101",
		"https://github.example.com/example-org/widgets/pull/8",
		"https://github.example.com/example-org/other/pull/7",
		"vikunja-widgets-agent-opus-1",
		"other/vikunja-widgets-agent-opus-104",
		"https://example.com/",
	} {
		wantProblem(t, get(t, h, resolvePath(link)), http.StatusNotFound)
	}
}

func TestResolveBadRequest(t *testing.T) {
	h := resolveAPI(t)
	for _, path := range []string{
		"/api/v1/resolve",
		resolvePath("   "),
		resolvePath("https://vikunja.example.com/tasks/" + strings.Repeat("1", MaxResolveURL)),
		resolvePath("Not A Task"),
		resolvePath("vikunja.example.com/tasks/101"),
		resolvePath("ftp://vikunja.example.com/tasks/101"),
		resolvePath("a/b/c"),
		resolvePath("Agents/task"),
		resolvePath(strings.Repeat("a", maxTaskName+1)),
		resolvePath("agents/task..name"),
		resolvePath("agents/task.-name"),
		resolvePath("agents/" + strings.Repeat("a", 64) + ".name"),
	} {
		wantProblem(t, get(t, h, path), http.StatusBadRequest)
	}
}
