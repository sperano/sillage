package ingest

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
)

func TestNewRequiresDependencies(t *testing.T) {
	m, err := NewMetrics(prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Collector: testCollectorConfig()}, nil, &fakeScheduler{}, m, discardLog()); err == nil {
		t.Error("New without a store succeeded")
	}
	if _, err := New(Config{}, &fakeStore{}, &fakeScheduler{}, m, discardLog()); err == nil {
		t.Error("New without instances succeeded")
	}
}

func TestAcceptedGitHubDelivery(t *testing.T) {
	hs := newHarness(t, testSecrets())
	w := hs.serve(githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", w.Code, w.Body)
	}
	want := model.Delivery{
		Source: model.SourceGitHub, Instance: testGitHub, ID: "d-1", Event: "pull_request",
		ReceivedAt: testNow, Outcome: model.DeliveryAccepted,
	}
	if got := hs.store.recorded(); len(got) != 1 || got[0] != want {
		t.Errorf("recorded %+v, want [%+v]", got, want)
	}
	if got := hs.sched.roots(); !slices.Equal(got, refs([]model.Root{taskRoot(42)})) {
		t.Errorf("scheduled %q, want task 42", got)
	}
	if got := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues("github", "accepted")); got != 1 {
		t.Errorf("accepted deliveries = %v, want 1", got)
	}
	hs.checkLog(t, testGitHubSecret, "PAYLOAD-MARKER")
}

// TestReplayedDeliveryIsIgnored replays a body under its own delivery id.
// GitHub does not sign the id, so a body replayed under a fresh one is
// accepted again (see the package documentation): it only asks for a read.
func TestReplayedDeliveryIsIgnored(t *testing.T) {
	tests := []struct {
		name    string
		request func() *http.Request
	}{
		{"github", func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
		}},
		{"vikunja", func() *http.Request { return vikunjaRequest(testVikunjaSecret, []byte(vikunjaCommentBody)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, testSecrets())
			if w := hs.serve(tt.request()); w.Code != http.StatusAccepted {
				t.Fatalf("first attempt: status = %d (%s), want 202", w.Code, w.Body)
			}
			w := hs.serve(tt.request())
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "duplicate") {
				t.Errorf("replay: status = %d (%s), want 200 duplicate", w.Code, w.Body)
			}
			if got := hs.sched.roots(); len(got) != 1 {
				t.Errorf("scheduled %q, want task 42 once", got)
			}
			got := hs.store.recorded()
			if len(got) != 2 || got[1].Outcome != model.DeliveryIgnored || got[1].Detail != duplicateDetail ||
				got[1].ID != got[0].ID {
				t.Errorf("recorded %+v, want the replay recorded as an ignored duplicate", got)
			}
		})
	}
}

func TestRetryAfterFailureIsAccepted(t *testing.T) {
	hs := newHarness(t, testSecrets())
	hs.sched.full = true
	if w := hs.serve(vikunjaRequest(testVikunjaSecret, []byte(vikunjaCommentBody))); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("full queue: status = %d, want 503", w.Code)
	}
	hs.sched.full = false
	if w := hs.serve(vikunjaRequest(testVikunjaSecret, []byte(vikunjaCommentBody))); w.Code != http.StatusAccepted {
		t.Fatalf("retry: status = %d (%s), want 202", w.Code, w.Body)
	}
	got := hs.store.recorded()
	if len(got) != 2 || got[0].Outcome != model.DeliveryFailed || got[0].Detail != queueFullDetail ||
		got[1].Outcome != model.DeliveryAccepted {
		t.Errorf("recorded %+v, want failed then accepted", got)
	}
}

func TestRefusedRequestsLeaveNoTrace(t *testing.T) {
	body := []byte(githubPRBody)
	tampered := []byte(strings.Replace(githubPRBody, "kelos/vikunja-42", "kelos/vikunja-43", 1))
	tests := []struct {
		name    string
		secrets Secrets
		request func() *http.Request
		status  int
	}{
		{"tampered github body", testSecrets(), func() *http.Request {
			r := githubRequest("d-1", "pull_request", testGitHubSecret, tampered)
			r.Header.Set(githubSignatureHeader, githubSignaturePrefix+hexHMAC(testGitHubSecret, body))
			return r
		}, http.StatusUnauthorized},
		{"tampered vikunja body", testSecrets(), func() *http.Request {
			r := vikunjaRequest(testVikunjaSecret, []byte(strings.Replace(vikunjaCommentBody, `"id":42`, `"id":43`, 1)))
			r.Header.Set(vikunjaSignatureHeader, hexHMAC(testVikunjaSecret, []byte(vikunjaCommentBody)))
			return r
		}, http.StatusUnauthorized},
		{"another secret", testSecrets(), func() *http.Request {
			return githubRequest("d-1", "pull_request", "not-the-secret", body)
		}, http.StatusUnauthorized},
		{"unsigned", testSecrets(), func() *http.Request {
			r := githubRequest("d-1", "pull_request", testGitHubSecret, body)
			r.Header.Del(githubSignatureHeader)
			return r
		}, http.StatusUnauthorized},
		{"vikunja signature format on github", testSecrets(), func() *http.Request {
			r := githubRequest("d-1", "pull_request", testGitHubSecret, body)
			r.Header.Set(githubSignatureHeader, hexHMAC(testGitHubSecret, body))
			return r
		}, http.StatusUnauthorized},
		{"no secret configured, signed with the empty key", Secrets{Vikunja: testVikunjaSecret}, func() *http.Request {
			return githubRequest("d-1", "pull_request", "", body)
		}, http.StatusForbidden},
		{"no delivery id", testSecrets(), func() *http.Request {
			r := githubRequest("d-1", "pull_request", testGitHubSecret, body)
			r.Header.Del(githubDeliveryHeader)
			return r
		}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, tt.secrets)
			w := hs.serve(tt.request())
			if w.Code != tt.status {
				t.Errorf("status = %d (%s), want %d", w.Code, w.Body, tt.status)
			}
			if got := hs.store.recorded(); len(got) != 0 {
				t.Errorf("recorded %+v, want nothing", got)
			}
			if got := hs.sched.roots(); len(got) != 0 {
				t.Errorf("scheduled %q, want nothing", got)
			}
			source := "github"
			if strings.Contains(tt.name, "vikunja body") {
				source = "vikunja"
			}
			if got := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues(source, string(outcomeRefused))); got != 1 {
				t.Errorf("refused deliveries = %v, want 1", got)
			}
			hs.checkLog(t, testGitHubSecret, testVikunjaSecret, "PAYLOAD-MARKER")
		})
	}
}

func TestBodyIsBounded(t *testing.T) {
	tooLarge := func(limit int) []byte { return []byte(`{"pad":"` + strings.Repeat("x", limit) + `"}`) }
	github, vikunja := tooLarge(MaxGitHubBodyBytes), tooLarge(MaxVikunjaBodyBytes)
	tests := []struct {
		name          string
		request       func() *http.Request
		contentLength int64
	}{
		{"github declared", func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, github)
		}, int64(len(github))},
		{"github undeclared", func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, github)
		}, -1},
		{"vikunja undeclared", func() *http.Request { return vikunjaRequest(testVikunjaSecret, vikunja) }, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, testSecrets())
			if tt.contentLength > 0 {
				// A declared size is refused without waiting for a slot.
				for range maxBodiesInFlight {
					hs.h.bodies <- struct{}{}
				}
			}
			r := tt.request()
			r.ContentLength = tt.contentLength
			if w := hs.serve(r); w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status = %d, want 413", w.Code)
			}
			if got := hs.store.recorded(); len(got) != 0 {
				t.Errorf("recorded %+v, want nothing", got)
			}
		})
	}
}

func TestMethodsAndPaths(t *testing.T) {
	hs := newHarness(t, testSecrets())
	r := githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
	r.Method = http.MethodGet
	if w := hs.serve(r); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET: status = %d, Allow %q; want 405, POST", w.Code, w.Header().Get("Allow"))
	}
	r = githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
	r.URL.Path = Prefix + "gitlab"
	if w := hs.serve(r); w.Code != http.StatusNotFound {
		t.Errorf("unknown source: status = %d, want 404", w.Code)
	}
	if got := hs.store.recorded(); len(got) != 0 {
		t.Errorf("recorded %+v, want nothing", got)
	}
}

func TestOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*harness)
		request func() *http.Request
		status  int
		outcome model.DeliveryOutcome
		detail  string
	}{
		{"ping", nil, func() *http.Request {
			return githubRequest("d-1", githubPing, testGitHubSecret, []byte(`{"zen":"x","hook_id":1}`))
		}, http.StatusOK, model.DeliveryIgnored, "ping"},
		{"no root", nil, func() *http.Request {
			return vikunjaRequest(testVikunjaSecret, []byte(`{"event_name":"project.updated","data":{"project":{"id":3}}}`))
		}, http.StatusOK, model.DeliveryIgnored, "names no task"},
		{"not json", nil, func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, []byte(`payload=%7B%7D`))
		}, http.StatusBadRequest, model.DeliveryRejected, "not a JSON payload"},
		{"store unreadable", func(hs *harness) { hs.store.readErr = errFakeStore }, func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
		}, http.StatusServiceUnavailable, model.DeliveryFailed, "the store failed while reading the earlier attempts"},
		{"queue full", func(hs *harness) { hs.sched.full = true }, func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
		}, http.StatusServiceUnavailable, model.DeliveryFailed, queueFullDetail},
		{"accepted meanwhile", func(hs *harness) { hs.store.acceptRace = true }, func() *http.Request {
			return githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody))
		}, http.StatusOK, model.DeliveryIgnored, duplicateDetail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := newHarness(t, testSecrets())
			if tt.setup != nil {
				tt.setup(hs)
			}
			w := hs.serve(tt.request())
			if w.Code != tt.status {
				t.Errorf("status = %d (%s), want %d", w.Code, w.Body, tt.status)
			}
			got := hs.store.recorded()
			if len(got) != 1 || got[0].Outcome != tt.outcome || !strings.Contains(got[0].Detail, tt.detail) {
				t.Fatalf("recorded %+v, want one %s attempt with %q", got, tt.outcome, tt.detail)
			}
			counted := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues(string(got[0].Source), string(tt.outcome)))
			if counted != 1 {
				t.Errorf("%s deliveries = %v, want 1", tt.outcome, counted)
			}
		})
	}
}

func TestRecordFailure(t *testing.T) {
	hs := newHarness(t, testSecrets())
	hs.store.recordErr = errFakeStore
	w := hs.serve(vikunjaRequest(testVikunjaSecret, []byte(vikunjaCommentBody)))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "could not be recorded") {
		t.Errorf("status = %d (%s), want 503 saying it was not recorded", w.Code, w.Body)
	}
	if got := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues("vikunja", "failed")); got != 1 {
		t.Errorf("failed deliveries = %v, want 1", got)
	}
}

func TestBodiesInFlightAreBounded(t *testing.T) {
	hs := newHarness(t, testSecrets())
	for range maxBodiesInFlight {
		hs.h.bodies <- struct{}{}
	}
	w := hs.serve(githubRequest("d-1", "pull_request", testGitHubSecret, []byte(githubPRBody)))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d (%s), want 503 while every slot is taken", w.Code, w.Body)
	}
	if got := hs.store.recorded(); len(got) != 0 {
		t.Errorf("recorded %+v, want nothing", got)
	}
	if got := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues("github", string(outcomeBusy))); got != 1 {
		t.Errorf("busy deliveries = %v, want 1", got)
	}
	if got := testutil.ToFloat64(hs.metrics.deliveries.WithLabelValues("github", string(outcomeRefused))); got != 0 {
		t.Errorf("refused deliveries = %v, want 0: busy says nothing of authenticity", got)
	}
	// A request waits for a slot freed meanwhile.
	go func() {
		time.Sleep(slotWait / 4)
		<-hs.h.bodies
	}()
	if w := hs.serve(githubRequest("d-2", "pull_request", testGitHubSecret, []byte(githubPRBody))); w.Code != http.StatusAccepted {
		t.Errorf("status = %d (%s) once a slot frees, want 202", w.Code, w.Body)
	}
	if len(hs.h.bodies) != maxBodiesInFlight-1 {
		t.Errorf("%d slots held after the request, want %d", len(hs.h.bodies), maxBodiesInFlight-1)
	}
}

func TestNoSecretSaysNothingOfTheConfiguration(t *testing.T) {
	hs := newHarness(t, Secrets{Vikunja: testVikunjaSecret})
	w := hs.serve(githubRequest("d-1", "pull_request", "", []byte(githubPRBody)))
	if w.Code != http.StatusForbidden || strings.Contains(strings.ToLower(w.Body.String()), "secret") {
		t.Errorf("status = %d (%s), want a 403 that does not mention the secret", w.Code, w.Body)
	}
}
