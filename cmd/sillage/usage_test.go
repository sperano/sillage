package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/loki"
	"github.com/sperano/sillage/internal/store"
)

func usageEnv(overrides map[string]string) func(string) string {
	env := map[string]string{
		loki.EnvURL: "http://loki.example.com", usageSinceEnv: "2026-03-01T00:00:00+02:00",
		kelos.EnvCluster: "cluster-a", kelos.EnvNamespace: "kelos-agents",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestUsageSettings(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		unset   bool
	}{
		{"both set", nil, "", false},
		{"neither set", map[string]string{loki.EnvURL: "", usageSinceEnv: ""}, "neither", true},
		{"url without since", map[string]string{usageSinceEnv: ""}, usageSinceEnv, false},
		{"since without url", map[string]string{loki.EnvURL: ""}, loki.EnvURL, false},
		{"bad since", map[string]string{usageSinceEnv: "last week"}, "RFC 3339", false},
		{"bad url", map[string]string{loki.EnvURL: "not a url"}, loki.EnvURL, false},
		{"no cluster", map[string]string{kelos.EnvCluster: ""}, kelos.EnvCluster, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, since, err := usageSettings(usageEnv(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || errors.Is(err, errUsageUnset) != tt.unset {
					t.Fatalf("error = %v, want one containing %q (unset: %v)", err, tt.wantErr, tt.unset)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !since.Equal(time.Date(2026, 2, 28, 22, 0, 0, 0, time.UTC)) || cfg.BaseURL != "http://loki.example.com" ||
				cfg.Cluster != "cluster-a" || cfg.Namespace != "kelos-agents" {
				t.Errorf("settings = %+v, %v", cfg, since)
			}
		})
	}
}

func TestNewUsageFeed(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		noStore   bool
		wantOn    bool
		wantLog   string
		wantLevel string
	}{
		{"on", nil, false, true, "", ""},
		{"no store", nil, true, false, "no store", "INFO"},
		{"unset", map[string]string{loki.EnvURL: "", usageSinceEnv: ""}, false, false, "neither", "INFO"},
		{"half set", map[string]string{usageSinceEnv: ""}, false, false, "without", "WARN"},
		{"unparsable", map[string]string{usageSinceEnv: "x"}, false, false, "RFC 3339", "WARN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))
			var st *store.Store
			if !tt.noStore {
				st = new(store.Store) // never used: the feed is not started
			}
			u := newUsageFeed(usageEnv(tt.env), st, prometheus.NewRegistry(), log)
			if (u.feed != nil) != tt.wantOn {
				t.Fatalf("feed on = %v, want %v", u.feed != nil, tt.wantOn)
			}
			want := api.UsageConfig{}
			if tt.wantOn {
				want = api.UsageConfig{On: true, Since: time.Date(2026, 2, 28, 22, 0, 0, 0, time.UTC)}
			}
			if got := u.config(); got != want {
				t.Errorf("config = %+v, want %+v", got, want)
			}
			if tt.wantLog == "" && buf.Len() != 0 {
				t.Errorf("logged %q for a feed that is on", buf.String())
			}
			if tt.wantLog != "" && (!strings.Contains(buf.String(), tt.wantLog) || !strings.Contains(buf.String(), "level="+tt.wantLevel)) {
				t.Errorf("log = %q, want a %s mentioning %q", buf.String(), tt.wantLevel, tt.wantLog)
			}
		})
	}
}

func TestUsageFeedOffStartsNothing(t *testing.T) {
	wait := usageFeed{}.start(context.Background())
	wait() // returns at once
}
