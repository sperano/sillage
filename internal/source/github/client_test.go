package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinkNext(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"none", nil, ""},
		{"next and last", []string{`<https://a.example.com/x?page=2>; rel="next", <https://a.example.com/x?page=5>; rel="last"`},
			"https://a.example.com/x?page=2"},
		{"last page", []string{`<https://a.example.com/x?page=1>; rel="prev", <https://a.example.com/x?page=1>; rel="first"`}, ""},
		{"second header value", []string{`<https://a.example.com/x?page=1>; rel="prev"`, `<https://a.example.com/x?page=3>; rel="next"`},
			"https://a.example.com/x?page=3"},
		{"malformed", []string{`garbage, <x>`}, ""},
	}
	for _, tt := range tests {
		if got := linkNext(tt.values); got != tt.want {
			t.Errorf("%s: linkNext = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A next link to another host would carry the token there: refused.
func TestPaginateRefusesForeignNextPage(t *testing.T) {
	for _, next := range []string{"https://elsewhere.example.com/repos/x", "http://%zz", "/relative"} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(headerLink, "<"+next+`>; rel="next"`)
			_, _ = w.Write([]byte(`[]`))
		}))
		_, err := listAll[Review](t.Context(), c, "/x", nil)
		if err == nil || !strings.Contains(err.Error(), "next page outside the API") {
			t.Errorf("next %q: error = %v, want a refusal", next, err)
		}
	}
}

// A list whose Link header never ends is cut off.
func TestPaginateStopsAtMaxPages(t *testing.T) {
	const maxPages = 3
	var srvURL string
	h, calls := scripted(func(_ int, w http.ResponseWriter) {
		w.Header().Set(headerLink, "<"+srvURL+`/x?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	c, err := NewClient(Config{APIURL: srv.URL, Instance: testInstance, Token: testToken}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.maxPages = maxPages
	_, err = listAll[Review](t.Context(), c, "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "more than 3 pages") {
		t.Errorf("error = %v, want the page limit", err)
	}
	if calls() != maxPages {
		t.Errorf("read %d pages, want %d", calls(), maxPages)
	}
}

func TestFetchStatusError(t *testing.T) {
	fake := newFakeGitHub(t, "closed-unmerged")
	c := newTestClient(t, fake)
	wrong, err := NewClient(Config{APIURL: DefaultAPIURL, Instance: testInstance, Token: "wrong-" + testToken}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.authorize = wrong.authorize
	_, err = c.FetchPR(t.Context(), testRepo, 31)
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.Code != http.StatusUnauthorized || se.Path != testRepoPath+"/pulls/31" {
		t.Fatalf("FetchPR error = %v, want a 401 StatusError for the pull request", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error %q leaks the token", err)
	}
}

func TestGetRejectsBadResponses(t *testing.T) {
	const maxBody = 1 << 10
	tests := []struct {
		name string
		body string
		want string
	}{
		{"malformed JSON", `{"number": `, "decoding"},
		{"oversize", `{"title":"` + strings.Repeat("x", 2*maxBody) + `"}`, "response larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			c.maxBody = maxBody
			_, err := c.FetchPR(t.Context(), testRepo, 1)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("FetchPR error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFetchTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	apiURL := srv.URL
	srv.Close()
	c, err := NewClient(Config{APIURL: apiURL, Instance: testInstance, Token: testToken}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchBranch(t.Context(), testRepo, Branch{Repository: testRepo, Name: "b"})
	if err == nil || !strings.Contains(err.Error(), testRepoPath+"/pulls") || strings.Contains(err.Error(), "per_page") {
		t.Errorf("FetchBranch error = %v, want the path and not the full URL", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = newTestClient(t, newFakeGitHub(t, "no-pr")).FetchPR(ctx, testRepo, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FetchPR error = %v, want context.Canceled", err)
	}
}

func TestClientNeverPrintsToken(t *testing.T) {
	c, err := NewClient(Config{APIURL: DefaultAPIURL, Instance: dotComInstance, Token: testToken}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{APIURL: DefaultAPIURL, Token: testToken}
	var logged strings.Builder
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("client", "c", c, "cfg", cfg)
	for _, out := range []string{
		fmt.Sprintf("%v %+v %#v %v %#v", c, c, c, cfg, cfg),
		logged.String(),
	} {
		if strings.Contains(out, testToken) {
			t.Errorf("token printed: %s", out)
		}
	}
}

func TestNewClientRejectsBadConfig(t *testing.T) {
	for _, cfg := range []Config{
		{APIURL: "ftp://api.example.com", Instance: testInstance, Token: testToken},
		{APIURL: DefaultAPIURL, Instance: testInstance},
		{APIURL: DefaultAPIURL, Token: testToken},
	} {
		if _, err := NewClient(cfg, nil); err == nil {
			t.Errorf("NewClient(%+v) accepted it", cfg)
		}
	}
}
