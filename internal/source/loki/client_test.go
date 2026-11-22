package loki

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	fixtureDir  = "../../../testdata/fixtures/usage/loki"
	testCluster = "cluster-a"
	testNS      = "kelos-agents"
	claudePod   = "widgets-agent-7-abcde"
	claudeUID   = "3f1c2d4e-0000-4000-8000-000000000001"
	codexPod    = "widgets-agent-8-fghij"
	codexUID    = "3f1c2d4e-0000-4000-8000-000000000002"
)

var windowFrom = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// lokiStub answers each request with the next of its bodies, then with an
// empty result, and records the requests.
type lokiStub struct {
	mu       sync.Mutex
	bodies   [][]byte
	requests []*http.Request
}

func (l *lokiStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, r)
	body := []byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`)
	if len(l.bodies) > 0 {
		body, l.bodies = l.bodies[0], l.bodies[1:]
	}
	_, _ = w.Write(body)
}

func newClient(t *testing.T, h http.Handler, mod func(*Config)) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{BaseURL: srv.URL, Cluster: testCluster, Namespace: testNS}
	if mod != nil {
		mod(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func i64(n int64) *int64 { return &n }

func wantEvents() []model.UsageEvent {
	base := model.UsageEvent{Cluster: testCluster, Namespace: testNS, Pod: claudePod, PodUID: claudeUID,
		TaskName: "widgets-agent-7", Runner: model.RunnerClaudeCode, Stream: "session-aaa"}
	main := base
	main.Kind, main.Event, main.Seq = model.UsageResponse, "api_request", "1"
	main.At = time.Date(2026, 3, 1, 12, 0, 0, 500_000_000, time.UTC)
	main.Model, main.Speed, main.ReportedCostUSD = "claude-example-1", "fast", "0.12"
	main.Tokens = &model.TokenCounts{UncachedInput: i64(100), CacheRead: i64(2000), CacheWrite5m: i64(200),
		CacheWrite1h: i64(50), CacheWriteUnknownTTL: i64(50), Output: i64(50)}
	sub := base
	sub.Kind, sub.Event, sub.Seq = model.UsageResponse, "api_request", "2"
	sub.At = time.Date(2026, 3, 1, 12, 0, 1, 500_000_000, time.UTC)
	sub.Model, sub.Agent, sub.ParentAgent, sub.ReportedCostUSD = "claude-example-1", "agent-sub-1", "agent-main", "0.000015"
	sub.Tokens = &model.TokenCounts{UncachedInput: i64(7), CacheRead: i64(0), CacheWriteUnknownTTL: i64(40), Output: i64(9)}
	failed := base
	failed.Kind, failed.Event, failed.Seq, failed.Status = model.UsageFailedRequest, "api_error", "3", "529"
	failed.At = time.Date(2026, 3, 1, 12, 0, 2, 500_000_000, time.UTC)
	failed.Model = "claude-example-1"

	cx := model.UsageEvent{Cluster: testCluster, Namespace: testNS, Pod: codexPod, PodUID: codexUID,
		TaskName: "widgets-agent-8", Runner: model.RunnerCodex, Stream: "conv-bbb", Model: "gpt-example-1",
		Seq: "2026-03-01T12:00:04.250Z", At: time.Date(2026, 3, 1, 12, 0, 4, 250_000_000, time.UTC)}
	resp := cx
	resp.Kind, resp.Event, resp.ServiceTier = model.UsageResponse, "codex.sse_event", "priority"
	resp.Tokens = &model.TokenCounts{UncachedInput: i64(300), CacheRead: i64(600), CacheWriteUnknownTTL: i64(100),
		Output: i64(80), Reasoning: i64(30)}
	cost := cx
	cost.Kind, cost.Event, cost.ReportedCostUSD = model.UsageReportedCost, "codex.turn_cost", "0.0345"
	fail := cx
	fail.Kind, fail.Event, fail.Status, fail.Model = model.UsageFailedRequest, "codex.api_request", "429", ""
	fail.Seq, fail.At = "2026-03-01T12:00:05.250Z", time.Date(2026, 3, 1, 12, 0, 5, 250_000_000, time.UTC)
	return []model.UsageEvent{main, sub, failed, main, resp, cost, fail}
}

func TestQueryNormalizesBothEncodings(t *testing.T) {
	for _, name := range []string{"merged.json", "categorized.json"} {
		t.Run(name, func(t *testing.T) {
			stub := &lokiStub{bodies: [][]byte{fixture(t, name)}}
			c := newClient(t, stub, nil)
			res, err := c.Query(t.Context(), windowFrom, windowFrom.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			want := wantEvents()
			if len(res.Events) != len(want) {
				t.Fatalf("%d events, want %d: %+v", len(res.Events), len(want), res.Events)
			}
			for i := range want {
				if !eventsEqual(res.Events[i], want[i]) {
					t.Errorf("event %d = %s, want %s", i, dump(res.Events[i]), dump(want[i]))
				}
			}
			if res.Entries != 11 || res.Skipped != 3 || res.Malformed != 1 || res.Pages != 1 || res.Stalled != 0 {
				t.Errorf("counts = %+v, want 11 entries, 3 skipped, 1 malformed, 1 page", res)
			}
			if len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "no stream") {
				t.Errorf("problems = %v, want the missing stream", res.Problems)
			}
		})
	}
}

func TestQueryRequest(t *testing.T) {
	stub := &lokiStub{}
	c := newClient(t, stub, func(cfg *Config) { cfg.OrgID = "tenant-1" })
	to := windowFrom.Add(time.Hour)
	if _, err := c.Query(t.Context(), windowFrom, to); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 1 {
		t.Fatalf("%d requests, want 1", len(stub.requests))
	}
	r := stub.requests[0]
	q := r.URL.Query()
	want := map[string]string{
		"query":     `{k8s_namespace_name="kelos-agents", service_name=~"claude-code|codex_cli_rs"}`,
		"start":     strconv.FormatInt(windowFrom.UnixNano(), 10),
		"end":       strconv.FormatInt(to.UnixNano(), 10),
		"limit":     strconv.Itoa(DefaultPageLimit),
		"direction": "forward",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if r.Method != http.MethodGet || r.URL.Path != "/loki/api/v1/query_range" || r.Header.Get("X-Scope-OrgID") != "tenant-1" {
		t.Errorf("request = %s %s org %q", r.Method, r.URL.Path, r.Header.Get("X-Scope-OrgID"))
	}
}

func TestQueryNoOrgHeaderByDefault(t *testing.T) {
	stub := &lokiStub{}
	c := newClient(t, stub, nil)
	if _, err := c.Query(t.Context(), windowFrom, windowFrom.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := stub.requests[0].Header["X-Scope-Orgid"]; ok {
		t.Error("X-Scope-OrgID sent without a configured tenant")
	}
}

func TestQueryFollowsPages(t *testing.T) {
	stub := &lokiStub{bodies: [][]byte{fixture(t, "page1.json"), fixture(t, "page2.json")}}
	c := newClient(t, stub, func(cfg *Config) { cfg.PageLimit = 2 })
	res, err := c.Query(t.Context(), windowFrom, windowFrom.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var seqs []string
	for _, e := range res.Events {
		seqs = append(seqs, e.Seq)
	}
	if want := []string{"1", "2", "2", "3"}; !slices.Equal(seqs, want) {
		t.Errorf("sequences = %v, want %v (the page boundary repeats its last entry)", seqs, want)
	}
	if res.Pages != 3 || res.Stalled != 0 {
		t.Errorf("pages = %d, stalled = %d; want 3, 0", res.Pages, res.Stalled)
	}
	starts := starts(stub)
	second := time.Date(2026, 3, 1, 12, 0, 2, 0, time.UTC).UnixNano()
	third := time.Date(2026, 3, 1, 12, 0, 3, 0, time.UTC).UnixNano()
	if want := []int64{windowFrom.UnixNano(), second, third}; !slices.Equal(starts, want) {
		t.Errorf("starts = %v, want %v", starts, want)
	}
}

func TestQueryAdvancesPastAPageOfOneTimestamp(t *testing.T) {
	stub := &lokiStub{bodies: [][]byte{fixture(t, "stalled1.json"), fixture(t, "stalled2.json")}}
	c := newClient(t, stub, func(cfg *Config) { cfg.PageLimit = 2 })
	res, err := c.Query(t.Context(), windowFrom.Add(time.Second), windowFrom.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.Stalled != 1 || res.Pages != 3 {
		t.Errorf("stalled = %d, pages = %d; want 1, 3", res.Stalled, res.Pages)
	}
	first := windowFrom.Add(time.Second).UnixNano()
	if got := starts(stub); got[1] != first+1 {
		t.Errorf("second start = %d, want %d (1 ns after the stalled page)", got[1], first+1)
	}
}

func starts(stub *lokiStub) []int64 {
	var out []int64
	for _, r := range stub.requests {
		n, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		out = append(out, n)
	}
	return out
}

func TestQueryEmptyWindowSendsNothing(t *testing.T) {
	stub := &lokiStub{}
	c := newClient(t, stub, nil)
	res, err := c.Query(t.Context(), windowFrom, windowFrom)
	if err != nil || res.Pages != 0 || len(stub.requests) != 0 {
		t.Errorf("Query = %+v, %v with %d requests; want none", res, err, len(stub.requests))
	}
}

func TestQueryErrors(t *testing.T) {
	tests := []struct {
		name    string
		h       http.HandlerFunc
		mod     func(*Config)
		wantErr string
	}{
		{"server error", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "too many outstanding requests", http.StatusTooManyRequests)
		}, nil, "429 Too Many Requests: too many outstanding requests"},
		{"not json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, nil,
			"decoding the answer"},
		{"failed status", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"error","data":{}}`))
		}, nil, `status "error"`},
		{"matrix", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		}, nil, `result type "matrix"`},
		{"bad timestamp", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[["x","l"]]}]}}`))
		}, nil, "timestamp"},
		{"oversize", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}` + strings.Repeat(" ", 100)))
		}, func(c *Config) { c.MaxResponseBytes = 50 }, "over 50 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(t, tt.h, tt.mod)
			_, err := c.Query(t.Context(), windowFrom, windowFrom.Add(time.Hour))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestQueryHonoursContext(t *testing.T) {
	c := newClient(t, &lokiStub{}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Query(ctx, windowFrom, windowFrom.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error = %v, want a cancellation", err)
	}
}
