package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const testVersion = "1.2.3"

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodGet, path)
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("%s %s: Cache-Control = %q, want no-store", method, path, got)
	}
	return rec
}

// decode checks a 200 JSON response and decodes its body into v.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
}

// wantProblem checks that rec is a problem body with status.
func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int) Problem {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeProblem {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeProblem)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	if p.Status != status || p.Title != http.StatusText(status) || p.Type != problemType || p.Detail == "" {
		t.Errorf("problem = %+v, want status %d with its title and a detail", p, status)
	}
	return p
}

func TestVersion(t *testing.T) {
	var got VersionResponse
	decode(t, get(t, New(Config{Version: testVersion}), "/api/v1/version"), &got)
	if got != (VersionResponse{Version: testVersion, API: apiVersion}) {
		t.Errorf("version = %+v", got)
	}
}

func TestNoStore(t *testing.T) {
	h := New(Config{Version: testVersion})
	for _, path := range []string{
		"/api/v1/stories/task/vikunja.example.com/101",
		"/api/v1/stories/page/wiki.example.com/57/en",
		"/api/v1/coverage/task/vikunja.example.com/101",
		"/api/v1/coverage",
		"/api/v1/resolve?url=https://vikunja.example.com/tasks/101",
		"/api/v1/search?q=widgets",
	} {
		wantProblem(t, get(t, h, path), http.StatusServiceUnavailable)
	}
}

func TestUnrouted(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, path := range []string{"/api/", "/api/v1/", "/api/v1/stories", "/api/v2/version",
		"/api/v1/stories/task/vikunja.example.com/101/extra"} {
		wantProblem(t, get(t, h, path), http.StatusNotFound)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, path := range []string{"/api/v1/version", "/api/v1/stories/task/vikunja.example.com/101", "/api/x"} {
			rec := do(t, h, method, path)
			wantProblem(t, rec, http.StatusMethodNotAllowed)
			if got := rec.Header().Get("Allow"); got != allowedMethods {
				t.Errorf("%s %s: Allow = %q, want %q", method, path, got, allowedMethods)
			}
		}
	}
}

// failingStore fails every read, as a store whose database is down.
type failingStore struct {
	store.Reader
	err error
}

func (f failingStore) Story(context.Context, model.Root) (model.Story, error) {
	return model.Story{}, f.err
}

func (f failingStore) SearchRoots(context.Context, store.RootQuery) ([]store.RootSummary, error) {
	return nil, f.err
}

func TestStoreFailureHidesItsError(t *testing.T) {
	secret := "connecting to db.example.com as sillage"
	h := New(Config{Store: failingStore{err: errors.New(secret)}})
	for _, path := range []string{"/api/v1/stories/task/vikunja.example.com/101", "/api/v1/search?q=a"} {
		rec := get(t, h, path)
		wantProblem(t, rec, http.StatusInternalServerError)
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("%s: body %s quotes the store's error", path, rec.Body)
		}
	}
}

// slowStore answers once its context is done, as a store that hangs.
type slowStore struct{ store.Reader }

func (slowStore) Story(ctx context.Context, _ model.Root) (model.Story, error) {
	<-ctx.Done()
	return model.Story{}, ctx.Err()
}

func TestTimeout(t *testing.T) {
	const timeout = 10 * time.Millisecond
	h := New(Config{Store: slowStore{}, Timeout: timeout})
	wantProblem(t, get(t, h, "/api/v1/stories/task/vikunja.example.com/101"), http.StatusServiceUnavailable)
}

func TestResponseBound(t *testing.T) {
	fixtures := newFakeStore(t)
	path := "/api/v1/stories/task/vikunja.example.com/102"
	full := get(t, New(Config{Store: fixtures}), path)
	if full.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", full.Code)
	}
	exact := New(Config{Store: fixtures, MaxResponseBytes: full.Body.Len()})
	if rec := get(t, exact, path); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), full.Body.Bytes()) {
		t.Errorf("a story of exactly the bound: status %d, want the whole story", rec.Code)
	}
	short := New(Config{Store: fixtures, MaxResponseBytes: full.Body.Len() - 1})
	wantProblem(t, get(t, short, path), http.StatusInternalServerError)
}

// TestProblemsFitTheLowestBound answers problems that quote long input,
// and the one for a story past the bound, under the lowest bound.
func TestProblemsFitTheLowestBound(t *testing.T) {
	h := New(Config{Store: newFakeStore(t), MaxResponseBytes: 1})
	long := strings.Repeat("a", 2*MinResponseBytes)
	// strconv.Quote writes a NUL as \x00, whose backslash JSON escapes.
	escaped := strings.Repeat("%00", 2*MinResponseBytes)
	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"story past the bound", "/api/v1/stories/task/vikunja.example.com/101", http.StatusInternalServerError},
		{"long unknown path", "/api/v1/" + long, http.StatusNotFound},
		{"long id", "/api/v1/stories/task/vikunja.example.com/" + long, http.StatusBadRequest},
		{"long locale", "/api/v1/stories/page/wiki.example.com/57/" + long, http.StatusBadRequest},
		{"escaped id", "/api/v1/stories/task/vikunja.example.com/" + escaped, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.path)
			wantProblem(t, rec, tt.status)
			if rec.Body.Len() > MinResponseBytes {
				t.Errorf("problem of %d bytes, more than %d", rec.Body.Len(), MinResponseBytes)
			}
		})
	}
}

func TestClientGoneWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/stories/task/vikunja.example.com/101", nil)
	New(Config{Store: slowStore{}}).ServeHTTP(rec, req)
	if rec.Body.Len() != 0 {
		t.Errorf("body = %s, want none for a client that is gone", rec.Body)
	}
}
