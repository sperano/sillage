package ingest

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestGitHubIdentify(t *testing.T) {
	g := githubParser{cfg: testCollectorConfig()}
	h := http.Header{}
	h.Set(githubDeliveryHeader, "d-1")
	h.Set(githubEventHeader, "check_run")
	if id, event, err := g.identify(h, nil); err != nil || id != "d-1" || event != "check_run" {
		t.Errorf("identify() = %q, %q, %v", id, event, err)
	}
	h.Set(githubDeliveryHeader, strings.Repeat("x", maxDeliveryIDLength))
	h.Set(githubEventHeader, strings.Repeat("x", maxEventLength))
	if _, _, err := g.identify(h, nil); err != nil {
		t.Errorf("identify() at the bounds = %v, want no error", err)
	}
	tests := []struct {
		change func(http.Header)
		want   error
	}{
		{func(h http.Header) { h.Del(githubDeliveryHeader) }, errNoDeliveryID},
		{func(h http.Header) { h.Del(githubEventHeader) }, errNoDeliveryID},
		{func(h http.Header) { h.Set(githubDeliveryHeader, strings.Repeat("x", maxDeliveryIDLength+1)) }, errLongDeliveryID},
		{func(h http.Header) { h.Set(githubEventHeader, strings.Repeat("x", maxEventLength+1)) }, errLongEvent},
	}
	for _, tt := range tests {
		h := h.Clone()
		tt.change(h)
		if _, _, err := g.identify(h, nil); !errors.Is(err, tt.want) {
			t.Errorf("identify(%v) = %v, want %v", h, err, tt.want)
		}
	}
}

func TestGitHubRoots(t *testing.T) {
	stored := map[string][]model.Root{
		model.PRKey{Instance: testGitHub, Repository: testRepo, Number: 12}.String(): {taskRoot(42), taskRoot(50)},
	}
	tests := []struct {
		name  string
		event string
		body  string
		want  []model.Root
		err   error
	}{
		{"pull request by branch and story", "pull_request", githubPRBody, []model.Root{taskRoot(42), taskRoot(50)}, nil},
		{"review on a page branch", "pull_request_review",
			`{"pull_request":{"number":3,"head":{"ref":"kelos/wiki-7"}},"repository":{"full_name":"` + testRepo + `"}}`,
			[]model.Root{pageRoot(7)}, nil},
		{"comment on a pull request", "issue_comment",
			`{"issue":{"number":12,"pull_request":{"url":"x"}},"repository":{"full_name":"` + testRepo + `"}}`,
			[]model.Root{taskRoot(42), taskRoot(50)}, nil},
		{"comment on an issue", "issue_comment",
			`{"issue":{"number":12},"repository":{"full_name":"` + testRepo + `"}}`, nil, nil},
		{"check run from a fork", "check_run",
			`{"check_run":{"check_suite":{"head_branch":"kelos/vikunja-8"},"pull_requests":[]},` +
				`"repository":{"full_name":"` + testRepo + `"}}`, []model.Root{taskRoot(8)}, nil},
		{"check run on a stored pull request", "check_run",
			`{"check_run":{"check_suite":{"head_branch":"feature"},"pull_requests":[{"number":12,"head":{"ref":"feature"}}]},` +
				`"repository":{"full_name":"` + testRepo + `"}}`, []model.Root{taskRoot(42), taskRoot(50)}, nil},
		{"check suite", "check_suite",
			`{"check_suite":{"head_branch":"kelos/vikunja-8","pull_requests":[]},"repository":{"full_name":"` + testRepo + `"}}`,
			[]model.Root{taskRoot(8)}, nil},
		{"another branch", "pull_request",
			`{"pull_request":{"number":99,"head":{"ref":"main"}},"repository":{"full_name":"` + testRepo + `"}}`, nil, nil},
		{"ping", githubPing, `{"zen":"x"}`, nil, nil},
		{"not json", "pull_request", `payload=x`, nil, errNotJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := githubParser{cfg: testCollectorConfig(), store: &fakeStore{prRoots: stored}}
			got, why, err := g.roots(t.Context(), tt.event, []byte(tt.body))
			if !errors.Is(err, tt.err) {
				t.Fatalf("roots() error = %v, want %v", err, tt.err)
			}
			if !slices.Equal(refs(got), refs(tt.want)) {
				t.Errorf("roots() = %q, want %q", refs(got), refs(tt.want))
			}
			if err == nil && len(got) == 0 && why == "" {
				t.Error("no root and no reason")
			}
		})
	}
}

func TestGitHubRootsStoreError(t *testing.T) {
	g := githubParser{cfg: testCollectorConfig(), store: &fakeStore{readErr: errFakeStore}}
	_, _, err := g.roots(t.Context(), "pull_request", []byte(githubPRBody))
	if se := (*storeError)(nil); !errors.As(err, &se) || !errors.Is(err, errFakeStore) {
		t.Errorf("roots() error = %v, want a storeError", err)
	}
}
