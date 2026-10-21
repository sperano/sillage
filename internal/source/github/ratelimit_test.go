package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// recordSleeps replaces c's sleep with one that records each wait and
// returns at once.
func recordSleeps(c *Client) func() []time.Duration {
	var mu sync.Mutex
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		return nil
	}
	return func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(waits)
	}
}

// scripted answers the n-th request (from 0) with respond(n, w).
func scripted(respond func(n int, w http.ResponseWriter)) (http.Handler, func() int) {
	var mu sync.Mutex
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n := calls
		calls++
		mu.Unlock()
		respond(n, w)
	})
	return h, func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func okPR(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(PullRequest{Number: 1})
}

func TestRateLimited(t *testing.T) {
	header := func(k, v string) http.Header { h := http.Header{}; h.Set(k, v); return h }
	tests := []struct {
		name string
		code int
		h    http.Header
		body string
		want bool
	}{
		{"429", http.StatusTooManyRequests, http.Header{}, "", true},
		{"403 exhausted", http.StatusForbidden, header(headerRateRemaining, "0"), "", true},
		{"403 retry-after", http.StatusForbidden, header(headerRetryAfter, "30"), "", true},
		{"403 secondary message", http.StatusForbidden, http.Header{},
			`{"message":"You have exceeded a secondary rate limit."}`, true},
		{"403 permission", http.StatusForbidden, header(headerRateRemaining, "4999"),
			`{"message":"Resource not accessible by personal access token"}`, false},
		{"404", http.StatusNotFound, header(headerRateRemaining, "0"), "", false},
	}
	for _, tt := range tests {
		if got := rateLimited(tt.code, tt.h, []byte(tt.body)); got != tt.want {
			t.Errorf("%s: rateLimited = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRetryWait(t *testing.T) {
	now := testObservedAt
	reset := strconv.FormatInt(now.Add(2*time.Minute).Unix(), 10)
	tests := []struct {
		name    string
		headers map[string]string
		attempt int
		want    time.Duration
	}{
		{"retry-after wins", map[string]string{headerRetryAfter: "7", headerRateRemaining: "0", headerRateReset: reset}, 0, 7 * time.Second},
		{"until reset", map[string]string{headerRateRemaining: "0", headerRateReset: reset}, 0, 2*time.Minute + resetMargin},
		{"reset passed", map[string]string{headerRateRemaining: "0", headerRateReset: strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)}, 0, 0},
		{"no hint", nil, 0, secondaryLimitWait},
		{"no hint, third try", nil, 2, 4 * secondaryLimitWait},
		{"malformed retry-after", map[string]string{headerRetryAfter: "soon"}, 1, 2 * secondaryLimitWait},
	}
	for _, tt := range tests {
		h := http.Header{}
		for k, v := range tt.headers {
			h.Set(k, v)
		}
		if got := retryWait(h, now, tt.attempt); got != tt.want {
			t.Errorf("%s: retryWait = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// A secondary rate limit is waited out and the request retried.
func TestGetRetriesAfterRateLimit(t *testing.T) {
	h, calls := scripted(func(n int, w http.ResponseWriter) {
		if n == 0 {
			w.Header().Set(headerRetryAfter, "3")
			http.Error(w, `{"message":"secondary rate limit"}`, http.StatusForbidden)
			return
		}
		okPR(w)
	})
	c := newTestClient(t, h)
	waits := recordSleeps(c)
	var pr PullRequest
	if err := c.getOne(t.Context(), "/x", &pr); err != nil {
		t.Fatalf("getOne: %v", err)
	}
	if got, want := waits(), []time.Duration{3 * time.Second}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if calls() != 2 {
		t.Errorf("%d requests, want 2", calls())
	}
}

// After the response that used the window's last request, the next
// request waits for the reset instead of being refused.
func TestGetWaitsForExhaustedWindow(t *testing.T) {
	reset := testObservedAt.Add(time.Minute)
	h, calls := scripted(func(_ int, w http.ResponseWriter) {
		w.Header().Set(headerRateRemaining, "0")
		w.Header().Set(headerRateReset, strconv.FormatInt(reset.Unix(), 10))
		okPR(w)
	})
	c := newTestClient(t, h)
	waits := recordSleeps(c)
	var pr PullRequest
	for range 2 {
		if err := c.getOne(t.Context(), "/x", &pr); err != nil {
			t.Fatalf("getOne: %v", err)
		}
	}
	if got, want := waits(), []time.Duration{time.Minute + resetMargin}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if calls() != 2 {
		t.Errorf("%d requests, want 2", calls())
	}
}

func TestGetGivesUpOnRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantCalls  int
		wantWaits  int
	}{
		{"wait longer than the limit", 2 * DefaultMaxRateLimitWait, 1, 0},
		{"retries run out", time.Second, defaultMaxRateLimitRetries + 1, defaultMaxRateLimitRetries},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, calls := scripted(func(_ int, w http.ResponseWriter) {
				w.Header().Set(headerRetryAfter, strconv.Itoa(int(tt.retryAfter/time.Second)))
				w.WriteHeader(http.StatusTooManyRequests)
			})
			c := newTestClient(t, h)
			waits := recordSleeps(c)
			var pr PullRequest
			err := c.getOne(t.Context(), "/x", &pr)
			rle, ok := errors.AsType[*RateLimitError](err)
			if !ok || rle.Path != "/x" || rle.Wait != tt.retryAfter {
				t.Fatalf("getOne error = %v, want a RateLimitError for /x after %s", err, tt.retryAfter)
			}
			if calls() != tt.wantCalls || len(waits()) != tt.wantWaits {
				t.Errorf("%d requests and %d waits, want %d and %d", calls(), len(waits()), tt.wantCalls, tt.wantWaits)
			}
		})
	}
}

// A window that resets later than the client may wait is refused before
// any request is sent.
func TestGetRefusesLongBudgetWait(t *testing.T) {
	h, calls := scripted(func(int, http.ResponseWriter) {})
	c := newTestClient(t, h)
	c.resumeAt = testObservedAt.Add(2 * DefaultMaxRateLimitWait)
	var pr PullRequest
	if _, ok := errors.AsType[*RateLimitError](c.getOne(t.Context(), "/x", &pr)); !ok {
		t.Error("getOne did not return a RateLimitError")
	}
	if calls() != 0 {
		t.Errorf("%d requests sent, want none", calls())
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(t.Context(), time.Nanosecond); err != nil {
		t.Errorf("sleepContext = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepContext after cancel = %v, want context.Canceled", err)
	}
}

// A wait cut short by the context returns its error, path named.
func TestGetRateLimitWaitCancelled(t *testing.T) {
	h, _ := scripted(func(_ int, w http.ResponseWriter) {
		w.Header().Set(headerRetryAfter, "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := newTestClient(t, h)
	c.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	var pr PullRequest
	err := c.getOne(t.Context(), "/x", &pr)
	if !errors.Is(err, context.Canceled) || err.Error() != "github: GET /x: context canceled" {
		t.Errorf("getOne error = %v, want context.Canceled naming /x", err)
	}
}
