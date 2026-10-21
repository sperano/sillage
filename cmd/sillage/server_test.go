package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndex = "<!doctype html><title>test</title>"

func testHandler() http.Handler {
	return newHandler(fstest.MapFS{"index.html": {Data: []byte(testIndex)}})
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
