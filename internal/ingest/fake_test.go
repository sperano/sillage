package ingest

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const (
	testVikunja = "vikunja.example.com"
	testGitHub  = "github.example.com"
	testWiki    = "wiki.example.com"
	testRepo    = "example-org/widgets"
	// testGitHubSecret and testVikunjaSecret sign the tests' deliveries.
	testGitHubSecret  = "github-test-secret"
	testVikunjaSecret = "vikunja-test-secret"
)

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func testCollectorConfig() collector.Config {
	return collector.Config{VikunjaInstance: testVikunja, GitHubInstance: testGitHub, WikiInstance: testWiki}
}

func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunja, ID: id}}
}

func pageRoot(id int64) model.Root {
	return model.Root{Page: &model.PageKey{Instance: testWiki, ID: id, Locale: "en"}}
}

func refs(roots []model.Root) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		ref, _ := refOf(r)
		out = append(out, ref.String())
	}
	return out
}

// fakeStore records attempts in memory, as *store.Store does.
type fakeStore struct {
	mu       sync.Mutex
	attempts []model.Delivery
	// prRoots are the stored roots of each pull request, by key string.
	prRoots map[string][]model.Root
	// readErr fails DeliveryAccepted and RootsOfPullRequest, recordErr
	// RecordDelivery.
	readErr, recordErr error
	// acceptRace makes the next accepted RecordDelivery find an attempt
	// accepted meanwhile.
	acceptRace bool
}

func (s *fakeStore) DeliveryAccepted(_ context.Context, source model.Source, instance, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return false, s.readErr
	}
	return s.acceptedLocked(source, instance, id), nil
}

func (s *fakeStore) acceptedLocked(source model.Source, instance, id string) bool {
	for _, a := range s.attempts {
		if a.Source == source && a.Instance == instance && a.ID == id && a.Outcome == model.DeliveryAccepted {
			return true
		}
	}
	return false
}

func (s *fakeStore) RecordDelivery(_ context.Context, d model.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordErr != nil {
		return s.recordErr
	}
	if err := d.Validate(); err != nil {
		return err
	}
	if d.Outcome == model.DeliveryAccepted && (s.acceptRace || s.acceptedLocked(d.Source, d.Instance, d.ID)) {
		s.acceptRace = false
		return store.ErrAlreadyAccepted
	}
	s.attempts = append(s.attempts, d)
	return nil
}

func (s *fakeStore) RootsOfPullRequest(_ context.Context, key model.PRKey) ([]store.RootSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return nil, s.readErr
	}
	var out []store.RootSummary
	for _, r := range s.prRoots[key.String()] {
		out = append(out, store.RootSummary{Root: r})
	}
	return out, nil
}

func (s *fakeStore) recorded() []model.Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Delivery(nil), s.attempts...)
}

// fakeScheduler records the roots scheduled; full refuses them.
type fakeScheduler struct {
	mu        sync.Mutex
	scheduled []model.Root
	full      bool
}

func (s *fakeScheduler) Schedule(root model.Root) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.full {
		return false
	}
	s.scheduled = append(s.scheduled, root)
	return true
}

func (s *fakeScheduler) roots() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return refs(s.scheduled)
}

// harness is a handler over fakes, with its log and metrics.
type harness struct {
	h       *Handler
	store   *fakeStore
	sched   *fakeScheduler
	metrics *Metrics
	log     *bytes.Buffer
}

func newHarness(t *testing.T, secrets Secrets) *harness {
	t.Helper()
	m, err := NewMetrics(prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	hs := &harness{store: &fakeStore{}, sched: &fakeScheduler{}, metrics: m, log: &bytes.Buffer{}}
	log := slog.New(slog.NewTextHandler(hs.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hs.h, err = New(Config{Collector: testCollectorConfig(), Secrets: secrets}, hs.store, hs.sched, m, log)
	if err != nil {
		t.Fatal(err)
	}
	hs.h.now = func() time.Time { return testNow }
	return hs
}

func testSecrets() Secrets { return Secrets{GitHub: testGitHubSecret, Vikunja: testVikunjaSecret} }

func hexHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// githubRequest is a GitHub delivery of body, signed with secret.
func githubRequest(id, event, secret string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, PathGitHub, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(githubDeliveryHeader, id)
	r.Header.Set(githubEventHeader, event)
	r.Header.Set(githubSignatureHeader, githubSignaturePrefix+hexHMAC(secret, body))
	return r
}

// vikunjaRequest is a Vikunja delivery of body, signed with secret.
func vikunjaRequest(secret string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, PathVikunja, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(vikunjaSignatureHeader, hexHMAC(secret, body))
	return r
}

func (hs *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

// checkLog fails if the log holds any of secrets.
func (hs *harness) checkLog(t *testing.T, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(hs.log.String(), s) {
			t.Errorf("the log holds %q:\n%s", s, hs.log)
		}
	}
}

var errFakeStore = errors.New("fake store unavailable")

const (
	githubPRBody = `{"action":"opened","number":12,"pull_request":{"number":12,"head":{"ref":"kelos/vikunja-42"},` +
		`"body":"PAYLOAD-MARKER"},"repository":{"full_name":"` + testRepo + `"}}`
	vikunjaCommentBody = `{"event_name":"task.comment.created","time":"2026-03-01T12:00:00Z",` +
		`"data":{"task":{"id":42,"title":"PAYLOAD-MARKER"},"comment":{"id":7}}}`
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
