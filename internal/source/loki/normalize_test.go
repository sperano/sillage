package loki

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

var entryTime = time.Date(2026, 3, 1, 12, 0, 9, 0, time.UTC)

func claudeLabels(extra map[string]string) map[string]string {
	l := map[string]string{
		"service_name": "claude-code", "k8s_namespace_name": testNS, "k8s_pod_name": claudePod,
		"k8s_pod_uid": claudeUID, "session_id": "s1", "event_sequence": "5", "event_name": "api_request",
	}
	maps.Copy(l, extra)
	return l
}

func codexLabels(extra map[string]string) map[string]string {
	l := map[string]string{
		"service_name": "codex_cli_rs", "k8s_namespace_name": testNS, "k8s_pod_name": codexPod,
		"k8s_pod_uid": codexUID, "conversation_id": "c1", "event_timestamp": "2026-03-01T12:00:04.250+02:00",
		"event_name": "codex.turn_cost", "usage_estimated_usd": "1",
	}
	maps.Copy(l, extra)
	return l
}

func normalizeLabels(t *testing.T, l map[string]string) (*model.UsageEvent, error) {
	t.Helper()
	return normalize(Config{Cluster: testCluster}, entry{at: entryTime, labels: l})
}

func TestNormalizeClaudeTokens(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]string
		want    model.TokenCounts
		wantErr string
	}{
		{"absent counters stay nil", nil, model.TokenCounts{}, ""},
		{"total without a split is of unknown duration",
			map[string]string{"cache_creation_tokens": "90"}, model.TokenCounts{CacheWriteUnknownTTL: i64(90)}, ""},
		{"split covering the total leaves no remainder",
			map[string]string{"cache_creation_tokens": "90", "cache_creation_5m_tokens": "60", "cache_creation_1h_tokens": "30"},
			model.TokenCounts{CacheWrite5m: i64(60), CacheWrite1h: i64(30)}, ""},
		{"a half split leaves the rest unknown",
			map[string]string{"cache_creation_tokens": "90", "cache_creation_5m_tokens": "60"},
			model.TokenCounts{CacheWrite5m: i64(60), CacheWriteUnknownTTL: i64(30)}, ""},
		{"split without a total",
			map[string]string{"cache_creation_1h_tokens": "30"}, model.TokenCounts{CacheWrite1h: i64(30)}, ""},
		{"not a number", map[string]string{"input_tokens": "1.5"}, model.TokenCounts{}, "input_tokens"},
		{"negative", map[string]string{"output_tokens": "-1"}, model.TokenCounts{}, "output_tokens"},
		{"explicit zero stays zero", map[string]string{"cache_read_tokens": "0"},
			model.TokenCounts{CacheRead: i64(0)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := normalizeLabels(t, claudeLabels(tt.extra))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := dump(model.UsageEvent{Tokens: ev.Tokens}); got != dump(model.UsageEvent{Tokens: &tt.want}) {
				t.Errorf("tokens = %s, want %s", got, dump(model.UsageEvent{Tokens: &tt.want}))
			}
			if ev.Kind != model.UsageResponse || ev.Runner != model.RunnerClaudeCode || ev.Stream != "s1" || ev.Seq != "5" {
				t.Errorf("event = %s", dump(*ev))
			}
		})
	}
}

func TestNormalizeClaudeCostAndTime(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]string
		want    string
		wantErr string
	}{
		{"decimal text is kept", map[string]string{"cost_usd": "0.12"}, "0.12", ""},
		{"trailing zeros are kept", map[string]string{"cost_usd": "0.1200"}, "0.1200", ""},
		{"exponent is converted exactly", map[string]string{"cost_usd": "1.5e-7"}, "0.00000015", ""},
		{"large exponent", map[string]string{"cost_usd": "2e3"}, "2000", ""},
		{"negative", map[string]string{"cost_usd": "-0.1"}, "", "cost_usd"},
		{"not a number", map[string]string{"cost_usd": "free"}, "", "cost_usd"},
		{"too many places", map[string]string{"cost_usd": "1e-40"}, "", "decimal places"},
		{"absent", nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := normalizeLabels(t, claudeLabels(tt.extra))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || ev.ReportedCostUSD != tt.want {
				t.Errorf("cost = %q, %v; want %q", ev.ReportedCostUSD, err, tt.want)
			}
		})
	}
}

func TestNormalizeTimes(t *testing.T) {
	tests := []struct {
		name string
		ts   string
		want time.Time
	}{
		{"event timestamp in UTC", "2026-03-01T14:00:00.5+02:00", time.Date(2026, 3, 1, 12, 0, 0, 500_000_000, time.UTC)},
		{"absent falls back to the entry", "", entryTime},
		{"unparsable falls back to the entry", "yesterday", entryTime},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := normalizeLabels(t, claudeLabels(map[string]string{"event_timestamp": tt.ts}))
			if err != nil {
				t.Fatal(err)
			}
			if !ev.At.Equal(tt.want) || ev.At.Location() != time.UTC {
				t.Errorf("At = %v, want %v in UTC", ev.At, tt.want)
			}
		})
	}
}

func TestNormalizeClaudeKinds(t *testing.T) {
	ev, err := normalizeLabels(t, claudeLabels(map[string]string{"event_name": "claude_code.api_error", "status_code": "500"}))
	if err != nil || ev.Kind != model.UsageFailedRequest || ev.Status != "500" || ev.Tokens != nil ||
		ev.Event != "claude_code.api_error" {
		t.Errorf("api_error = %+v, %v", ev, err)
	}
	for _, name := range []string{"user_prompt", "tool_result", ""} {
		if ev, err := normalizeLabels(t, claudeLabels(map[string]string{"event_name": name})); ev != nil || err != nil {
			t.Errorf("event %q = %+v, %v; want skipped", name, ev, err)
		}
	}
	if ev, err := normalizeLabels(t, claudeLabels(map[string]string{"service_name": "other"})); ev != nil || err != nil {
		t.Errorf("other service = %+v, %v; want skipped", ev, err)
	}
}

func TestNormalizeClaudeSplitOverTheTotal(t *testing.T) {
	ev, err := normalizeLabels(t, claudeLabels(map[string]string{"cache_creation_tokens": "50",
		"cache_creation_5m_tokens": "60", "cache_creation_1h_tokens": "5", "input_tokens": "11", "output_tokens": "12",
		"cost_usd": "0.5"}))
	var bad *inconsistentError
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "cache writes by duration (65) exceed the total (50)") {
		t.Fatalf("error = %v, want an inconsistentError naming the counts", err)
	}
	want := model.TokenCounts{UncachedInput: i64(11), Output: i64(12)}
	if ev == nil || dump(model.UsageEvent{Tokens: ev.Tokens}) != dump(model.UsageEvent{Tokens: &want}) ||
		ev.ReportedCostUSD != "0.5" || ev.Stream != "s1" {
		t.Errorf("event = %+v, want it kept with every cache write unknown", ev)
	}
}

func TestResultKeepsInconsistentEntriesAsMalformedEvents(t *testing.T) {
	var r Result
	r.add(Config{Cluster: testCluster}, entry{at: entryTime, labels: claudeLabels(map[string]string{
		"cache_creation_tokens": "1", "cache_creation_5m_tokens": "2"})})
	r.add(Config{Cluster: testCluster}, entry{at: entryTime, labels: claudeLabels(map[string]string{"event_name": "other"})})
	if r.Entries != 2 || r.Malformed != 1 || r.Skipped != 1 || len(r.Events) != 1 || len(r.Problems) != 1 ||
		!strings.Contains(r.Problems[0], "exceed the total (1)") {
		t.Errorf("result = %+v, want the inconsistent entry kept and counted malformed, the other skipped", r)
	}
}

func TestNormalizeMissingIdentity(t *testing.T) {
	for label, want := range map[string]string{
		"k8s_pod_uid": "no k8s_pod_uid", "k8s_pod_name": "no k8s_pod_name",
		"k8s_namespace_name": "no k8s_namespace_name", "session_id": "no stream", "event_sequence": "no sequence",
	} {
		t.Run(label, func(t *testing.T) {
			l := claudeLabels(nil)
			delete(l, label)
			ev, err := normalizeLabels(t, l)
			if ev != nil || err == nil || !strings.HasSuffix(err.Error(), ": "+want) {
				t.Errorf("= %+v, %v; want a malformed entry ending %q", ev, err, want)
			}
		})
	}
	l := codexLabels(nil)
	delete(l, "event_timestamp")
	if ev, err := normalizeLabels(t, l); ev != nil || err == nil || !strings.Contains(err.Error(), "no sequence") {
		t.Errorf("codex without a timestamp = %+v, %v; want malformed", ev, err)
	}
}

func TestNormalizeCodexResponse(t *testing.T) {
	resp := func(extra map[string]string) map[string]string {
		l := codexLabels(map[string]string{"event_name": "codex.sse_event", "event_kind": "response.completed"})
		maps.Copy(l, extra)
		return l
	}
	t.Run("counts", func(t *testing.T) {
		ev, err := normalizeLabels(t, resp(map[string]string{"input_token_count": "1000", "cached_token_count": "600",
			"output_token_count": "80", "tool_token_count": "5"}))
		if err != nil {
			t.Fatal(err)
		}
		want := model.TokenCounts{UncachedInput: i64(400), CacheRead: i64(600), Output: i64(80)}
		if dump(model.UsageEvent{Tokens: ev.Tokens}) != dump(model.UsageEvent{Tokens: &want}) {
			t.Errorf("tokens = %s", dump(*ev))
		}
		if ev.Seq != "2026-03-01T12:00:04.250+02:00" || !ev.At.Equal(time.Date(2026, 3, 1, 10, 0, 4, 250_000_000, time.UTC)) {
			t.Errorf("seq = %q, at = %v", ev.Seq, ev.At)
		}
	})
	t.Run("input absent leaves uncached unknown", func(t *testing.T) {
		ev, err := normalizeLabels(t, resp(map[string]string{"cached_token_count": "6"}))
		if err != nil || ev.Tokens.UncachedInput != nil || *ev.Tokens.CacheRead != 6 {
			t.Errorf("= %s, %v", dump(*ev), err)
		}
	})
	t.Run("cached and written over the input leaves uncached unknown", func(t *testing.T) {
		ev, err := normalizeLabels(t, resp(map[string]string{"input_token_count": "10", "cached_token_count": "8",
			"cache_write_token_count": "3", "output_token_count": "7"}))
		var bad *inconsistentError
		if !errors.As(err, &bad) || !strings.Contains(err.Error(), "less than its cached (8) and cache-written (3)") {
			t.Fatalf("error = %v, want an inconsistentError naming the counts", err)
		}
		want := model.TokenCounts{CacheRead: i64(8), CacheWriteUnknownTTL: i64(3), Output: i64(7)}
		if ev == nil || dump(model.UsageEvent{Tokens: ev.Tokens}) != dump(model.UsageEvent{Tokens: &want}) {
			t.Errorf("event = %+v, want it kept with tokens %s", ev, dump(model.UsageEvent{Tokens: &want}))
		}
	})
	t.Run("reasoning over the output", func(t *testing.T) {
		_, err := normalizeLabels(t, resp(map[string]string{"output_token_count": "1", "reasoning_token_count": "2"}))
		if err == nil || !strings.Contains(err.Error(), "reasoning") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("other sse kinds are skipped", func(t *testing.T) {
		if ev, err := normalizeLabels(t, resp(map[string]string{"event_kind": "response.created"})); ev != nil || err != nil {
			t.Errorf("= %+v, %v", ev, err)
		}
	})
}

func TestNormalizeCodexRequestAndCost(t *testing.T) {
	req := func(status string) map[string]string {
		return codexLabels(map[string]string{"event_name": "codex.api_request", "http_response_status_code": status})
	}
	for status, failed := range map[string]bool{"200": false, "399": false, "400": true, "503": true, "": false} {
		ev, err := normalizeLabels(t, req(status))
		if err != nil || (ev != nil) != failed {
			t.Errorf("status %q = %+v, %v; failed request: %v", status, ev, err, failed)
		}
		if ev != nil && (ev.Kind != model.UsageFailedRequest || ev.Status != status || ev.Tokens != nil) {
			t.Errorf("status %q = %s", status, dump(*ev))
		}
	}
	if _, err := normalizeLabels(t, req("abc")); err == nil || !strings.Contains(err.Error(), "http_response_status_code") {
		t.Errorf("error = %v, want a bad status", err)
	}
	costs := []struct {
		name  string
		extra map[string]string
		want  string
	}{
		{"estimated", map[string]string{"usage_estimated_usd": "0.0345"}, "0.0345"},
		{"estimate wins over micros", map[string]string{"usage_estimated_usd": "0.5", "cost_usd_micros": "7"}, "0.5"},
		{"micros", map[string]string{"usage_estimated_usd": "", "cost_usd_micros": "1500000"}, "1.5"},
		{"small micros", map[string]string{"usage_estimated_usd": "", "cost_usd_micros": "12"}, "0.000012"},
		{"zero micros", map[string]string{"usage_estimated_usd": "", "cost_usd_micros": "0"}, "0"},
		{"whole micros", map[string]string{"usage_estimated_usd": "", "cost_usd_micros": "3000000"}, "3"},
	}
	for _, tt := range costs {
		ev, err := normalizeLabels(t, codexLabels(tt.extra))
		if err != nil || ev.ReportedCostUSD != tt.want || ev.Kind != model.UsageReportedCost {
			t.Errorf("%s = %+v, %v; want cost %q", tt.name, ev, err, tt.want)
		}
	}
	if _, err := normalizeLabels(t, codexLabels(map[string]string{"usage_estimated_usd": ""})); err == nil ||
		!strings.Contains(err.Error(), "no usage_estimated_usd or cost_usd_micros") {
		t.Errorf("turn_cost without a cost: %v", err)
	}
	if _, err := normalizeLabels(t, codexLabels(map[string]string{"usage_estimated_usd": "", "cost_usd_micros": "1.5"})); err == nil {
		t.Error("fractional micros accepted")
	}
}
