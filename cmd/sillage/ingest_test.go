package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/collector/collectortest"
	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
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

// signedDelivery is a Vikunja delivery of a comment on task id, signed
// with testVikunjaSecret.
func signedDelivery(id int64) *http.Request {
	body := []byte(`{"event_name":"task.comment.created","data":{"task":{"id":` + strconv.FormatInt(id, 10) + `}}}`)
	mac := hmac.New(sha256.New, []byte(testVikunjaSecret))
	mac.Write(body)
	r := httptest.NewRequest(http.MethodPost, ingest.PathVikunja, bytes.NewReader(body))
	r.Header.Set("X-Vikunja-Signature", hex.EncodeToString(mac.Sum(nil)))
	return r
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

func (s *acceptingStore) AcceptDelivery(_ context.Context, d model.Delivery, _ []model.Root, act func() error) error {
	if err := act(); err != nil {
		return err
	}
	s.recorded = append(s.recorded, d)
	return nil
}

func (s *acceptingStore) RecordDelivery(_ context.Context, d model.Delivery, _ []model.Root) error {
	s.recorded = append(s.recorded, d)
	return nil
}

func (*acceptingStore) RootsOfPullRequest(context.Context, model.PRKey) ([]store.RootSummary, error) {
	return nil, nil
}

func TestNewIngestUnavailable(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		st   *store.Store
		c    collection
		want string
	}{
		{"no secret", map[string]string{}, &store.Store{}, collection{}, ingest.EnvGitHubSecret},
		{"no store", ingestEnv(), nil, collection{}, store.EnvDatabaseURL},
		// The store is never used: newIngest stops at the missing loop, or
		// at the sources' configuration.
		{"no collection loop", ingestEnv(), &store.Store{}, collection{}, "no collection loop"},
		{"no source configured", map[string]string{ingest.EnvVikunjaSecret: testVikunjaSecret}, &store.Store{},
			collection{loop: new(reconcile.Loop)}, vikunja.EnvURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newIngest(func(k string) string { return tt.env[k] }, tt.st, &recordingScheduler{}, tt.c,
				prometheus.NewRegistry())
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
	rec := httptest.NewRecorder()
	newHandler(nil, http.NotFoundHandler(), h, http.NotFoundHandler()).ServeHTTP(rec, signedDelivery(42))
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

func TestWhileCollecting(t *testing.T) {
	var collecting bool
	h := whileCollecting(func() bool { return collecting }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	for _, tt := range []struct {
		collecting bool
		want       int
	}{{false, http.StatusServiceUnavailable}, {true, http.StatusAccepted}} {
		collecting = tt.collecting
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, ingest.PathVikunja, nil))
		if rec.Code != tt.want {
			t.Errorf("collecting %v: status = %d (%s), want %d", tt.collecting, rec.Code, rec.Body, tt.want)
		}
	}
}

// TestSignedDeliveryEndsInARead serves the ingest paths and runs the
// collection loop over PostgreSQL, the real clients answered from the
// corpus story of a task no Kelos run names and no project listing
// lists, the interval an hour: a signed Vikunja delivery naming the task
// is refused while the loop does not collect, then accepted once it
// does, and the next cycle, which the delivery starts, stores the root as
// the collector assembles it.
func TestSignedDeliveryEndsInARead(t *testing.T) {
	st := newTestStore(t)
	story := collectortest.Stories["reopened-task"]
	cl := collectortest.New(t, corpusFixtures, story.Dirs, collectortest.Report{Elsewhere: t.Errorf, Broken: t.Errorf})
	cfg := reconcile.DefaultConfig()
	cfg.Interval, cfg.DeliveryDelay = time.Hour, time.Millisecond
	reg := prometheus.NewRegistry()
	queue := ingest.NewQueue(ingest.DefaultQueueCapacity)
	loop, err := reconcile.New(cfg, corpusCollectorConfig(), reconcile.Sources{
		Sources:  collector.Sources{Vikunja: cl.Vikunja, Kelos: cl.Kelos, GitHub: cl.GitHub},
		Projects: corpusProjects{},
	}, loopStore{st}, queue, reconcile.NewMetrics(reg), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	c := collection{loop: loop, lock: noLock{}}
	env := ingestEnv()
	h := newIngest(func(k string) string { return env[k] }, st, queue, c, reg)
	id := story.Root.Task.ID
	if rec := serveRecorded(h, signedDelivery(id)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before the loop collects: status = %d (%s), want 503", rec.Code, rec.Body)
	}
	ctx, cancel := context.WithCancel(t.Context())
	wait := c.start(ctx)
	defer func() {
		cancel()
		wait()
	}()
	// The first cycle reads nothing: nothing but a delivery names the task.
	pollUntil(t, "the first cycle", func() bool { return cycles(t, reg) > 0 })
	if rec := serveRecorded(h, signedDelivery(id)); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", rec.Code, rec.Body)
	}
	pollUntil(t, "the delivered task's story", func() bool {
		_, err := st.Story(t.Context(), story.Root)
		return err == nil
	})
	checkRootCollected(t, st, story)
	if got := counterValue(t, reg, "sillage_webhook_deliveries_total",
		map[string]string{"source": "vikunja", "outcome": "accepted"}); got != 1 {
		t.Errorf("accepted deliveries = %v, want 1", got)
	}
	if got := counterValue(t, reg, "sillage_collector_delivery_reads_total", map[string]string{"kind": "caused"}); got != 1 {
		t.Errorf("reads deliveries caused = %v, want 1", got)
	}
}

func serveRecorded(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// pollUntil polls cond every cyclePoll until it holds, failing the test
// after cycleTimeout.
func pollUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(cycleTimeout); !cond(); time.Sleep(cyclePoll) {
		if time.Now().After(deadline) {
			t.Fatalf("no %s within %s", what, cycleTimeout)
		}
	}
}

// counterValue returns the value of reg's counter name with labels; 0
// without one.
func counterValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			got := make(map[string]string)
			for _, l := range m.GetLabel() {
				got[l.GetName()] = l.GetValue()
			}
			if maps.Equal(got, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}
