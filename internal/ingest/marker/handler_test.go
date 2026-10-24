package marker

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

// A marker is stored with what the review verified of its pod, never
// what its body says of it.
func TestStoresTheMarkerWithItsVerifiedSender(t *testing.T) {
	hs := newHarness(t)
	expectStatus(t, "a start", hs.post(hs.token, body()), http.StatusAccepted)
	if len(hs.store.markers) != 1 {
		t.Fatalf("stored %d markers, want 1", len(hs.store.markers))
	}
	m := hs.store.markers[0]
	want := model.MarkerSender{Cluster: testCluster, Namespace: testNamespace, ServiceAccount: "default",
		Pod: testPod, PodUID: testPodUID}
	if m.Sender != want || !m.ReceivedAt.Equal(testNow) {
		t.Errorf("sender %+v received %s, want %+v at %s", m.Sender, m.ReceivedAt, want, testNow)
	}
	if m.Report.Kind != model.MarkerBlockerWait || m.Report.At != testNow.Add(123*time.Millisecond) {
		t.Errorf("report = %+v", m.Report)
	}
}

func TestAnswersWhatTheStoreDid(t *testing.T) {
	for _, c := range []struct {
		result store.MarkerResult
		err    error
		want   int
	}{
		{store.MarkerDuplicate, nil, http.StatusOK},
		{store.MarkerConflict, nil, http.StatusConflict},
		{store.MarkerLimited, nil, http.StatusTooManyRequests},
		{"", errors.New("database down"), http.StatusServiceUnavailable},
	} {
		hs := newHarness(t)
		hs.store.result, hs.store.err = c.result, c.err
		expectStatus(t, string(c.result), hs.post(hs.token, body()), c.want)
	}
}

func TestRefusesTokens(t *testing.T) {
	other := podStatus("elsewhere")
	notSA := podStatus(testNamespace)
	notSA.User.Username = "alice"
	podless := podStatus(testNamespace)
	delete(podless.User.Extra, extraPodUID)
	twoPods := podStatus(testNamespace)
	twoPods.User.Extra[extraPodName] = []string{testPod, "another"}
	apiServer := podStatus(testNamespace)
	apiServer.Audiences = []string{"https://kubernetes.default.svc"}
	refused := podStatus(testNamespace)
	refused.Authenticated = false
	for name, st := range map[string]ReviewStatus{
		"another namespace's ServiceAccount": other, "no ServiceAccount": notSA, "a token bound to no pod": podless,
		"two pods": twoPods, "another audience": apiServer, "not authenticated": refused,
	} {
		hs := newHarness(t)
		token := fabricatedToken(name, testNow.Add(time.Hour))
		hs.reviewer.statuses[token] = st
		expectStatus(t, name, hs.post(token, body()), http.StatusUnauthorized)
		if len(hs.store.markers) != 0 {
			t.Errorf("%s: a marker was stored", name)
		}
	}
	hs := newHarness(t)
	expectStatus(t, "no token", hs.post("", body()), http.StatusUnauthorized)
	expectStatus(t, "an unknown token", hs.post(fabricatedToken("forged", testNow.Add(time.Hour)), body()),
		http.StatusUnauthorized)
	expectStatus(t, "a huge token", hs.post(strings.Repeat("a", maxTokenBytes+1), body()), http.StatusUnauthorized)
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body()))
	req.Header.Set("Authorization", "Basic "+hs.token)
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	expectStatus(t, "basic authentication", rec, http.StatusUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); got != bearerScheme {
		t.Errorf("a 401 challenges with %q, want %q", got, bearerScheme)
	}
	req = httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body()))
	req.Header.Set("Authorization", "bearer   "+hs.token)
	rec = httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	expectStatus(t, "spaces after the scheme", rec, http.StatusAccepted)
	expectStatus(t, "a space inside the token", hs.post(hs.token+" x", body()), http.StatusUnauthorized)
	hs.reviewer.err = errors.New("API server unreachable")
	rec = hs.post(fabricatedToken("new", testNow.Add(time.Hour)), body())
	expectStatus(t, "a review failing", rec, http.StatusServiceUnavailable)
	if strings.Contains(rec.Body.String(), "unreachable") {
		t.Errorf("a failed review answers what the API server said: %q", rec.Body.String())
	}
}

// A token's verdict is reviewed once, then cached until the token expires;
// a refused token is reviewed every time.
func TestCachesVerdictsUntilTheTokenExpires(t *testing.T) {
	hs := newHarness(t)
	for range 3 {
		expectStatus(t, "a cached token", hs.post(hs.token, body()), http.StatusAccepted)
	}
	if hs.reviewer.reviews != 1 {
		t.Errorf("%d reviews of one token, want 1", hs.reviewer.reviews)
	}
	hs.clock = testNow.Add(time.Hour)
	hs.post(hs.token, strings.ReplaceAll(body(), "12:00:00", "13:00:00"))
	if hs.reviewer.reviews != 2 {
		t.Errorf("%d reviews once the token expired, want 2", hs.reviewer.reviews)
	}
	forged := fabricatedToken("forged", testNow.Add(24*time.Hour))
	for range 2 {
		hs.post(forged, body())
	}
	if hs.reviewer.reviews != 4 {
		t.Errorf("%d reviews with a refused token twice, want 4", hs.reviewer.reviews)
	}
}

func TestRefusesBodies(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"content", strings.Replace(body(), `"version":1`, `"version":1,"prompt":"hello"`, 1), http.StatusBadRequest},
		{"an op field", strings.Replace(body(), `"version":1`, `"version":1,"op":"read"`, 1), http.StatusBadRequest},
		{"a wiki request", strings.Replace(body(), `"blocker-wait"`, `"wiki-request"`, 1), http.StatusBadRequest},
		{"version 2", strings.Replace(body(), `"version":1`, `"version":2`, 1), http.StatusBadRequest},
		{"two objects", body() + body(), http.StatusBadRequest},
		{"no JSON", "start blocker-wait", http.StatusBadRequest},
		{"an empty body", "", http.StatusBadRequest},
		{"a time too early", strings.Replace(body(), "12:00:00", "11:54:59", 1), http.StatusBadRequest},
		{"a time too late", strings.Replace(body(), "12:00:00", "12:05:01", 1), http.StatusBadRequest},
		{"a time not in UTC", strings.Replace(body(), "12:00:00.123Z", "13:00:00.123+01:00", 1),
			http.StatusBadRequest},
		{"a body too large", strings.Replace(body(), `"version":1`,
			`"version":1,"pad":"`+strings.Repeat("x", MaxBodyBytes)+`"`, 1), http.StatusRequestEntityTooLarge},
	} {
		hs := newHarness(t)
		expectStatus(t, c.name, hs.post(hs.token, c.body), c.want)
		if len(hs.store.markers) != 0 {
			t.Errorf("%s: a marker was stored", c.name)
		}
	}
	// A body declared too large is refused before its token is reviewed.
	hs := newHarness(t)
	expectStatus(t, "a body declared too large", hs.post(fabricatedToken("unreviewed", testNow.Add(time.Hour)),
		strings.Repeat("x", MaxBodyBytes+1)), http.StatusRequestEntityTooLarge)
	if hs.reviewer.reviews != 0 {
		t.Errorf("%d reviews for a body declared too large, want 0", hs.reviewer.reviews)
	}
	// A body of unknown length is cut at the limit.
	hs = newHarness(t)
	long := strings.Replace(body(), `"version":1`, `"version":1,"pad":"`+strings.Repeat("x", MaxBodyBytes)+`"`, 1)
	req := httptest.NewRequest(http.MethodPost, Path, struct{ io.Reader }{strings.NewReader(long)})
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+hs.token)
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	expectStatus(t, "a body of unknown length too large", rec, http.StatusRequestEntityTooLarge)
	expectStatus(t, "a time within the skew", hs.post(hs.token, strings.Replace(body(), "12:00:00", "12:04:59", 1)),
		http.StatusAccepted)
}

// With every body slot held, a request waits for one, then is answered
// 503 without its body being read.
func TestBoundsTheBodiesInFlight(t *testing.T) {
	hs := newHarness(t)
	for range maxBodiesInFlight {
		hs.h.bodies <- struct{}{}
	}
	expectStatus(t, "every slot held", hs.post(hs.token, body()), http.StatusServiceUnavailable)
	if len(hs.store.markers) != 0 {
		t.Error("a marker was stored without a body slot")
	}
	<-hs.h.bodies
	expectStatus(t, "a slot free", hs.post(hs.token, body()), http.StatusAccepted)
	if len(hs.h.bodies) != maxBodiesInFlight-1 {
		t.Errorf("%d slots held after the request, want %d", len(hs.h.bodies), maxBodiesInFlight-1)
	}
}

func TestOnlyPostsToThePath(t *testing.T) {
	hs := newHarness(t)
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, Path, nil))
	expectStatus(t, "a GET", rec, http.StatusMethodNotAllowed)
	rec = httptest.NewRecorder()
	hs.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, Path+"s", strings.NewReader(body())))
	expectStatus(t, "another path", rec, http.StatusNotFound)
}

func TestNewNeedsItsParts(t *testing.T) {
	hs := newHarness(t)
	if _, err := New(Config{Cluster: testCluster}, hs.reviewer, hs.store, hs.h.metrics, hs.h.log); err == nil {
		t.Error("a handler without a namespace was built")
	}
	if _, err := New(Config{Cluster: testCluster, Namespace: testNamespace}, nil, hs.store, hs.h.metrics,
		hs.h.log); err == nil {
		t.Error("a handler without a reviewer was built")
	}
}
