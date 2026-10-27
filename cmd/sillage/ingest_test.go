package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/store"
)

const testVikunjaSecret = "vikunja-test-secret"

// ingestEnv configures every source, and the Vikunja webhook secret.
func ingestEnv() map[string]string {
	return map[string]string{
		vikunja.EnvURL:           "https://vikunja.example.com",
		vikunja.EnvToken:         "token",
		kelos.EnvCluster:         "cluster-a",
		kelos.EnvNamespace:       "agents",
		kelos.EnvLabelDomain:     "vikunja.example.com",
		kelos.EnvVikunjaInstance: "vikunja.example.com",
		github.EnvAPIURL:         "https://api.github.example.com",
		github.EnvToken:          "token",
		ingest.EnvVikunjaSecret:  testVikunjaSecret,
	}
}

type recordingScheduler struct{ roots []model.Root }

func (s *recordingScheduler) Schedule(root model.Root) bool {
	s.roots = append(s.roots, root)
	return true
}

type acceptingStore struct{ recorded []model.Delivery }

func (*acceptingStore) DeliveryAccepted(context.Context, model.Source, string, string) (bool, error) {
	return false, nil
}

func (s *acceptingStore) AcceptDelivery(_ context.Context, d model.Delivery, act func() error) error {
	if err := act(); err != nil {
		return err
	}
	s.recorded = append(s.recorded, d)
	return nil
}

func (s *acceptingStore) RecordDelivery(_ context.Context, d model.Delivery) error {
	s.recorded = append(s.recorded, d)
	return nil
}

func (*acceptingStore) RootsOfPullRequest(context.Context, model.PRKey) ([]store.RootSummary, error) {
	return nil, nil
}

func TestNewIngestUnavailable(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		st    *store.Store
		sched ingest.Scheduler
		want  string
	}{
		{"no secret", map[string]string{}, &store.Store{}, &recordingScheduler{}, ingest.EnvGitHubSecret},
		{"no store", ingestEnv(), nil, &recordingScheduler{}, store.EnvDatabaseURL},
		// The store is never used: newIngest stops at the missing scheduler.
		{"no scheduler", ingestEnv(), &store.Store{}, nil, "collection loop does not read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := newIngest(func(k string) string { return tt.env[k] }, tt.st, tt.sched, prometheus.NewRegistry())
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, ingest.PathVikunja, strings.NewReader("{}")))
			if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("status = %d (%s), want 503 naming %s", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

func TestBuildIngestServesDeliveries(t *testing.T) {
	env := ingestEnv()
	getenv := func(k string) string { return env[k] }
	st, sched := &acceptingStore{}, &recordingScheduler{}
	h, err := buildIngest(getenv, ingest.SecretsFromEnv(getenv), st, sched, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"event_name":"task.updated","data":{"task":{"id":42}}}`)
	mac := hmac.New(sha256.New, []byte(testVikunjaSecret))
	mac.Write(body)
	r := httptest.NewRequest(http.MethodPost, ingest.PathVikunja, bytes.NewReader(body))
	r.Header.Set("X-Vikunja-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	newHandler(nil, http.NotFoundHandler(), h, http.NotFoundHandler()).ServeHTTP(rec, r)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", rec.Code, rec.Body)
	}
	if len(sched.roots) != 1 || sched.roots[0].Task == nil || *sched.roots[0].Task != (model.TaskKey{
		Instance: "vikunja.example.com", ID: 42,
	}) {
		t.Errorf("scheduled %+v, want task 42", sched.roots)
	}
	if len(st.recorded) != 1 || st.recorded[0].Outcome != model.DeliveryAccepted {
		t.Errorf("recorded %+v, want one accepted attempt", st.recorded)
	}
}

func TestBuildIngestNeedsTheCollectorConfig(t *testing.T) {
	getenv := func(k string) string { return map[string]string{ingest.EnvVikunjaSecret: "x"}[k] }
	_, err := buildIngest(getenv, ingest.SecretsFromEnv(getenv), &acceptingStore{}, &recordingScheduler{},
		prometheus.NewRegistry())
	if err == nil {
		t.Error("buildIngest without the sources' configuration succeeded")
	}
}

func TestIngestPathsAreRouted(t *testing.T) {
	for _, path := range []string{ingest.PathGitHub, ingest.PathVikunja, ingest.Prefix + "other"} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s: status = %d, want the ingest handler's 503", path, rec.Code)
		}
	}
}
