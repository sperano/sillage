package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// testHook is a fabricated hook of testRepo.
var testHook = Hook{Repository: testRepo, ID: 7001}

// testHookPath is testHook's delivery log.
const testHookPath = testRepoPath + "/hooks/7001/deliveries"

func TestParseHook(t *testing.T) {
	got, err := ParseHook(" example-org/widgets:7001 ")
	if err != nil || got != testHook {
		t.Fatalf("ParseHook = %+v, %v; want %+v", got, err, testHook)
	}
	if got.String() != "example-org/widgets:7001" {
		t.Errorf("String = %q", got.String())
	}
	for _, bad := range []string{"", "example-org/widgets", "widgets:1", "example-org/widgets:0",
		"example-org/widgets:-3", "example-org/widgets:x", "a/b/c:1"} {
		if _, err := ParseHook(bad); err == nil {
			t.Errorf("ParseHook(%q) accepted", bad)
		}
	}
}

// fakeHookLog serves a delivery log of attempts, newest first, two per
// page with a cursor, and counts the pages asked for.
type fakeHookLog struct {
	mu       sync.Mutex
	attempts []HookDelivery
	pages    int
}

func (f *fakeHookLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != testHookPath {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages++
	start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	end := min(start+testPerPage, len(f.attempts))
	if end < len(f.attempts) {
		q := r.URL.Query()
		q.Set("cursor", strconv.Itoa(end))
		u := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}
		w.Header().Set(headerLink, "<"+u.String()+`>; rel="next"`)
	}
	_ = json.NewEncoder(w).Encode(f.attempts[start:end])
}

func (f *fakeHookLog) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pages
}

// testAttempts are attempts 105 down to 101, a minute apart, the newest
// at testObservedAt.
func testAttempts() []HookDelivery {
	var out []HookDelivery
	for i := range 5 {
		id := int64(105 - i)
		out = append(out, HookDelivery{
			ID: id, GUID: fmt.Sprintf("guid-%d", id), StatusCode: http.StatusAccepted, Event: "pull_request",
			DeliveredAt: testObservedAt.Add(-time.Duration(i) * time.Minute),
		})
	}
	return out
}

func ids(ds []HookDelivery) []int64 {
	var out []int64
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}

func TestHookDeliveriesStopsAtTheLastChecked(t *testing.T) {
	tests := []struct {
		name  string
		after int64
		since time.Time
		want  []int64
		pages int
	}{
		{"nothing new costs one request", 105, time.Time{}, nil, 1},
		{"stops on the page that reaches it", 103, time.Time{}, []int64{105, 104}, 2},
		{"every one", 0, time.Time{}, []int64{105, 104, 103, 102, 101}, 3},
		{"since a time", 0, testObservedAt.Add(-90 * time.Second), []int64{105, 104}, 2},
	}
	for _, tt := range tests {
		log := &fakeHookLog{attempts: testAttempts()}
		got, err := newTestClient(t, log).HookDeliveries(t.Context(), testHook, tt.after, tt.since)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if fmt.Sprint(ids(got)) != fmt.Sprint(tt.want) {
			t.Errorf("%s: attempts %v, want %v", tt.name, ids(got), tt.want)
		}
		if log.asked() != tt.pages {
			t.Errorf("%s: %d pages asked for, want %d", tt.name, log.asked(), tt.pages)
		}
	}
}

func TestHookDeliveryReceived(t *testing.T) {
	for code, want := range map[int]bool{0: false, 200: true, 202: true, 299: true, 302: false, 404: false, 503: false} {
		if got := (HookDelivery{StatusCode: code}).Received(); got != want {
			t.Errorf("status %d: Received = %v, want %v", code, got, want)
		}
	}
}

func TestHookPayload(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testHookPath+"/104" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":104,"event":"pull_request","request":{"headers":{},` +
			`"payload":{"action":"closed","number":3}},"response":{"payload":"busy"}}`))
	}))
	event, payload, err := c.HookPayload(t.Context(), testHook, 104)
	if err != nil {
		t.Fatal(err)
	}
	if event != "pull_request" || string(payload) != `{"action":"closed","number":3}` {
		t.Errorf("HookPayload = %q, %s", event, payload)
	}
	_, _, err = c.HookPayload(t.Context(), testHook, 1)
	if !Refused(err) {
		t.Errorf("a missing attempt: error %v, want a refusal", err)
	}
}

func TestRedeliver(t *testing.T) {
	tests := []struct {
		name    string
		respond func(w http.ResponseWriter)
		refused bool
		limited bool
	}{
		{"accepted", func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }, false, false},
		{"forbidden", func(w http.ResponseWriter) {
			http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
		}, true, false},
		{"unprocessable", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnprocessableEntity) }, true, false},
		{"server error", func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }, false, false},
		{"rate limited", func(w http.ResponseWriter) {
			w.Header().Set(headerRateRemaining, "0")
			w.Header().Set(headerRateReset, strconv.FormatInt(testObservedAt.Add(time.Minute).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
		}, false, true},
	}
	for _, tt := range tests {
		var method, path string
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			tt.respond(w)
		}))
		err := c.Redeliver(t.Context(), testHook, 104)
		if method != http.MethodPost || path != testHookPath+"/104/attempts" {
			t.Errorf("%s: sent %s %s", tt.name, method, path)
		}
		_, limited := errors.AsType[*RateLimitError](err)
		if Refused(err) != tt.refused || limited != tt.limited || (err == nil) != (tt.name == "accepted") {
			t.Errorf("%s: error %v: refused %v, limited %v", tt.name, err, Refused(err), limited)
		}
	}
}
