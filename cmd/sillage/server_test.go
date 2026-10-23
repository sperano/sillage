package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sperano/sillage/internal/api"
)

const (
	testIndex  = "<!doctype html><title>test</title>"
	testScript = "console.log('test')"
)

func testHandler() http.Handler {
	metrics, _ := newMetrics()
	return newHandler(fstest.MapFS{
		indexFile:         {Data: []byte(testIndex)},
		"assets/app-1.js": {Data: []byte(testScript)},
	}, api.New(api.Config{Version: "test"}), metrics)
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		method   string
		wantCode int
		wantBody string
	}{
		{http.MethodGet, http.StatusOK, "ok\n"},
		// The recorder keeps what the handler wrote; net/http drops a HEAD
		// response's body on the wire.
		{http.MethodHead, http.StatusOK, "ok\n"},
		{http.MethodPost, http.StatusMethodNotAllowed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testHandler().ServeHTTP(rec, httptest.NewRequest(tt.method, healthzPath, nil))
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			// Only a 200's body is ours; an error body is net/http's.
			if rec.Code == http.StatusOK && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestFrontendIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), testIndex) {
		t.Errorf("body = %q, want it to contain %q", rec.Body.String(), testIndex)
	}
}

// TestFrontendDeepLinks serves the entry page for every path that is no
// file, so that the app routes a deep link itself, and the files as they
// are.
func TestFrontendDeepLinks(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/task/vikunja.example.com/101", testIndex},
		{"/page/wiki.example.com/57/en", testIndex},
		{"/open?url=https%3A%2F%2Fvikunja.example.com%2Ftasks%2F101", testIndex},
		{"/assets/", testIndex},
		{"/assets/app-1.js", testScript},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tt.want {
			t.Errorf("GET %s = %d %q, want 200 %q", tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}
}

// TestFrontendMissingFile answers 404 for a missing file, which HTML
// would replace with the wrong type.
func TestFrontendMissingFile(t *testing.T) {
	for _, path := range []string{"/assets/app-0.js", "/favicon.ico"} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}

// TestAPIMounted routes every path under /api/ to the API, unknown ones
// included: they answer JSON, never the entry page.
func TestAPIMounted(t *testing.T) {
	tests := []struct {
		path string
		code int
	}{
		{"/api/v1/version", http.StatusOK},
		{"/api/v1/stories/task/vikunja.example.com/101", http.StatusServiceUnavailable},
		{"/api/v1/unknown", http.StatusNotFound},
		{"/api/", http.StatusNotFound},
		// The mux redirects a subtree root named without its slash.
		{"/api", http.StatusTemporaryRedirect},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != tt.code {
			t.Errorf("GET %s: status = %d, want %d", tt.path, rec.Code, tt.code)
		}
		if strings.Contains(rec.Body.String(), testIndex) {
			t.Errorf("GET %s served the entry page", tt.path)
		}
	}
}

func TestFrontendRefusesOtherMethods(t *testing.T) {
	for _, path := range []string{"/", "/task/vikunja.example.com/101", "/assets/app-1.js"} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}
