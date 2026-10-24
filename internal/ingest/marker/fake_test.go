package marker

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const (
	testCluster   = "cluster-a"
	testNamespace = "agents"
	testPod       = "vikunja-example-agent-42-abcde"
	testPodUID    = "00000000-0000-4000-8000-000000000042"
	testSpan      = "5f0c2b3e9a7d4c1e8b6a2f4d3c1e0b9a"
)

// testNow is when the tests' requests are received.
var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// fabricatedToken is a JWT-shaped token expiring at exp: only its payload
// is ever read, once a review authenticated it.
func fabricatedToken(name string, exp time.Time) string {
	enc := base64.RawURLEncoding
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "sub": name})
	return enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString(payload) + ".c2lnbmF0dXJl"
}

// podStatus is a review authenticating the default ServiceAccount of ns,
// bound to the test pod.
func podStatus(ns string) ReviewStatus {
	return ReviewStatus{
		Authenticated: true, Audiences: []string{Audience},
		User: UserInfo{Username: serviceAccountPrefix + ns + ":default", Extra: map[string][]string{
			extraPodName: {testPod}, extraPodUID: {testPodUID},
		}},
	}
}

// fakeReviewer answers the reviews of the tokens it knows, and refuses
// the others.
type fakeReviewer struct {
	mu       sync.Mutex
	statuses map[string]ReviewStatus
	err      error
	reviews  int
}

func (f *fakeReviewer) Review(_ context.Context, token string) (ReviewStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews++
	if f.err != nil {
		return ReviewStatus{}, f.err
	}
	if st, ok := f.statuses[token]; ok {
		return st, nil
	}
	return ReviewStatus{Error: "invalid bearer token"}, nil
}

// fakeStore records markers, answering result, or err.
type fakeStore struct {
	result  store.MarkerResult
	err     error
	markers []model.Marker
}

func (f *fakeStore) RecordMarker(_ context.Context, m model.Marker, perPod int) (store.MarkerResult, error) {
	if perPod != PerPodLimit {
		return "", errors.New("the limit per pod is not PerPodLimit")
	}
	if f.err != nil {
		return "", f.err
	}
	f.markers = append(f.markers, m)
	if f.result == "" {
		return store.MarkerStored, nil
	}
	return f.result, nil
}

// harness is a handler over a fake reviewer and store, receiving at its
// clock.
type harness struct {
	h        *Handler
	reviewer *fakeReviewer
	store    *fakeStore
	clock    time.Time
	token    string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	m, err := NewMetrics(prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	token := fabricatedToken("pod-token", testNow.Add(time.Hour))
	rev := &fakeReviewer{statuses: map[string]ReviewStatus{token: podStatus(testNamespace)}}
	st := &fakeStore{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, err := New(Config{Cluster: testCluster, Namespace: testNamespace}, rev, st, m, log)
	if err != nil {
		t.Fatal(err)
	}
	hs := &harness{h: h, reviewer: rev, store: st, clock: testNow, token: token}
	h.now = func() time.Time { return hs.clock }
	return hs
}

// body is a blocker wait's start at the test time, as an emitter sends it.
func body() string {
	return `{"version":1,"emitter":"wait_for_blockers","kind":"blocker-wait","edge":"start",` +
		`"span":"` + testSpan + `","at":"2026-03-01T12:00:00.123Z","taskName":"vikunja-example-agent-42"}`
}

// post sends body to Path with token, and returns the answer.
func (hs *harness) post(token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	return rec
}

func expectStatus(t *testing.T, name string, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("%s: status %d (%s), want %d", name, rec.Code, strings.TrimSpace(rec.Body.String()), want)
	}
}
