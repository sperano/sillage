package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/store"
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

func TestNewCollection(t *testing.T) {
	withDB := func(env map[string]string) map[string]string {
		env[store.EnvDatabaseURL] = testDatabaseURL
		return env
	}
	noGitHubToken := withDB(ingestEnv())
	delete(noGitHubToken, github.EnvToken)
	tests := []struct {
		name string
		env  map[string]string
		// loop is whether a loop is made; off the sources off, each
		// reason naming offIn.
		loop  bool
		off   []model.Source
		offIn string
	}{
		{name: "off without a store"},
		{
			name: "bad interval", env: withDB(map[string]string{reconcile.EnvInterval: "often"}),
			off: []model.Source{model.SourceGitHub, model.SourceKelos, model.SourceVikunja}, offIn: reconcile.EnvInterval,
		},
		{
			name: "no source configured", env: withDB(map[string]string{}),
			off: []model.Source{model.SourceGitHub, model.SourceKelos, model.SourceVikunja}, offIn: vikunja.EnvURL,
		},
		{name: "no GitHub token", env: noGitHubToken, loop: true, off: []model.Source{model.SourceGitHub},
			offIn: github.EnvToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBERNETES_SERVICE_HOST", "")
			t.Setenv("KUBECONFIG", writeKubeconfig(t))
			_, m, _ := newMetrics()
			var logs bytes.Buffer
			// No test reaches the store: the loop is made, never started.
			var st *store.Store
			if tt.env != nil {
				st = new(store.Store)
			}
			c, err := newCollection(func(k string) string { return tt.env[k] }, st, m,
				slog.New(slog.NewTextHandler(&logs, nil)))
			if err != nil {
				t.Fatalf("newCollection() error = %v, want the collector or its sources off", err)
			}
			if (c.loop != nil) != tt.loop {
				t.Errorf("loop made = %v, want %v", c.loop != nil, tt.loop)
			}
			var off []model.Source
			for _, o := range c.off {
				off = append(off, o.Source)
				if !strings.Contains(o.Reason, tt.offIn) {
					t.Errorf("%s off for %q, want a reason naming %s", o.Source, o.Reason, tt.offIn)
				}
			}
			if !slices.Equal(off, tt.off) {
				t.Errorf("off = %v, want %v", off, tt.off)
			}
			if !strings.Contains(logs.String(), "off") {
				t.Errorf("logs = %q, want them to say what is off", logs.String())
			}
		})
	}
}

func TestNewCollectionNeedsTheDatabaseURL(t *testing.T) {
	_, m, _ := newMetrics()
	if _, err := newCollection(emptyEnv, new(store.Store), m, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("newCollection() without the database URL succeeded, want the lock refused")
	}
}

// writeKubeconfig writes a kubeconfig of a fabricated cluster no test
// connects to, and returns its path: Kelos has a client.
func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := "apiVersion: v1\nkind: Config\nclusters:\n- name: example\n  cluster: {server: 'https://kube.example.com:6443'}\n" +
		"users:\n- name: reader\n  user: {token: fabricated}\ncontexts:\n- name: example\n  context: {cluster: example, user: reader}\n" +
		"current-context: example\n"
	if err := os.WriteFile(path, []byte(kubeconfig), kubeconfigPerm); err != nil {
		t.Fatal(err)
	}
	return path
}

// kubeconfigPerm is the mode of a written kubeconfig.
const kubeconfigPerm = 0o600
