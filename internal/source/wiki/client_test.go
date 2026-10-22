package wiki

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

func TestQueryStatusError(t *testing.T) {
	fake := storyFake(t, "live-edit-not-in-trail")
	c := newTestClient(t, fake)
	cfg := testConfig("https://wiki.example.com")
	cfg.Token = "wrong-" + testToken
	wrong, err := NewClient(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.authorize = wrong.authorize
	_, err = c.Fetch(t.Context(), fake.pageID)
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.Code != http.StatusUnauthorized || se.Operation != opPage {
		t.Fatalf("Fetch error = %v, want a 401 StatusError for %s", err, opPage)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error %q leaks the token", err)
	}
}

func TestQueryErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"GraphQL error", `{"errors":[{"message":"This page does not exist.","extensions":{"exception":{"code":6003}}}],` +
			`"data":{"pages":{"single":null}}}`, "SillagePage: This page does not exist."},
		{"malformed JSON", `{"data": `, "decoding"},
		{"malformed data", `{"data": {"pages": 3}}`, "decoding data"},
		{"oversize", `{"data": {"pages": "` + strings.Repeat("x", 2<<10) + `"}}`, "response larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, graphQLHandler(func(_ string, w http.ResponseWriter) {
				_, _ = w.Write([]byte(tt.body))
			}))
			c.maxBody = 1 << 10
			_, err := c.Fetch(t.Context(), 57)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Fetch error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestGraphQLErrorForbidden(t *testing.T) {
	forbidden := ResponseError{Message: messageForbidden}
	viewForbidden := ResponseError{Message: "You are not authorized to view this page."}
	viewForbidden.Extensions.Exception.Code = codePageViewForbidden
	notFound := ResponseError{Message: "This page does not exist."}
	notFound.Extensions.Exception.Code = 6003
	tests := []struct {
		name string
		errs []ResponseError
		want bool
	}{
		{"directive", []ResponseError{forbidden}, true},
		{"page view", []ResponseError{viewForbidden, forbidden}, true},
		{"not found", []ResponseError{notFound}, false},
		{"mixed", []ResponseError{forbidden, notFound}, false},
		{"none", nil, false},
	}
	for _, tt := range tests {
		if got := (&GraphQLError{Errors: tt.errs}).forbidden(); got != tt.want {
			t.Errorf("%s: forbidden() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestQueryTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	c, err := NewClient(testConfig(base), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Fetch(t.Context(), 57)
	if err == nil || !strings.Contains(err.Error(), opPage) || strings.Contains(err.Error(), base) {
		t.Errorf("Fetch error = %v, want the operation and not the URL", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = newTestClient(t, http.NotFoundHandler()).Fetch(ctx, 57)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch error = %v, want context.Canceled", err)
	}
}

func TestNewClientRejects(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no instance":         {BaseURL: "https://wiki.example.com", Token: testToken, AgentUsers: testAgents},
		"no token":            {BaseURL: "https://wiki.example.com", Instance: testInstance, AgentUsers: testAgents},
		"bad URL":             {BaseURL: "wiki.example.com", Instance: testInstance, Token: testToken, AgentUsers: testAgents},
		"no agent users":      {BaseURL: "https://wiki.example.com", Instance: testInstance, Token: testToken},
		"zero agent user":     {BaseURL: "https://wiki.example.com", Instance: testInstance, Token: testToken, AgentUsers: []int64{21, 0}},
		"negative agent user": {BaseURL: "https://wiki.example.com", Instance: testInstance, Token: testToken, AgentUsers: []int64{-3}},
	} {
		if _, err := NewClient(cfg, nil); err == nil {
			t.Errorf("%s: NewClient succeeded", name)
		}
	}
}

func TestClientNeverPrintsToken(t *testing.T) {
	c, err := NewClient(testConfig("https://wiki.example.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var logged strings.Builder
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("client", "c", c)
	for _, out := range []string{fmt.Sprintf("%v %+v", c, c), logged.String()} {
		if strings.Contains(out, testToken) {
			t.Errorf("token printed: %s", out)
		}
	}
}

// A base URL with a trailing slash still reaches GraphQL at /graphql: the
// fake server fails any other path.
func TestNewClientTrailingSlash(t *testing.T) {
	fake := storyFake(t, "live-edit-not-in-trail")
	if _, err := newTestClientAt(t, fake, "/").Fetch(t.Context(), fake.pageID); err != nil {
		t.Errorf("Fetch: %v", err)
	}
}
