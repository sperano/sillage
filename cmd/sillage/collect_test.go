package main

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// testDatabaseURL is a fabricated database no test connects to.
const testDatabaseURL = "postgres://sillage@db.example.com:5432/sillage"

func TestMetricsServed(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want %d", metricsPath, rec.Code, http.StatusOK)
	}
	for _, want := range []string{"go_goroutines", "sillage_collector_lock_held"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s does not serve %s", metricsPath, want)
		}
	}
}

func TestOpenStoreNotBuilt(t *testing.T) {
	if _, _, err := openStore(t.Context(), testDatabaseURL); !errors.Is(err, errNoStore) {
		t.Errorf("openStore() error = %v, want %v", err, errNoStore)
	}
}

func TestStartCollector(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		inErr string
	}{
		{name: "off without a database", env: nil},
		{
			name:  "bad interval",
			env:   map[string]string{databaseURLEnv: testDatabaseURL, reconcile.EnvInterval: "often"},
			inErr: reconcile.EnvInterval,
		},
		{name: "no source configured", env: map[string]string{databaseURLEnv: testDatabaseURL}, inErr: vikunja.EnvURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, m := newMetrics()
			var logs bytes.Buffer
			wait, err := startCollector(t.Context(), func(k string) string { return tt.env[k] }, m,
				slog.New(slog.NewTextHandler(&logs, nil)))
			switch {
			case tt.inErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.inErr) {
					t.Errorf("startCollector() error = %v, want one naming %s", err, tt.inErr)
				}
			case err != nil:
				t.Fatalf("startCollector() error = %v", err)
			default:
				wait()
				if !strings.Contains(logs.String(), "collector off") {
					t.Errorf("logs = %q, want them to say the collector is off", logs.String())
				}
			}
		})
	}
}
