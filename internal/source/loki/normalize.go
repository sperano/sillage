package loki

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// The services (stream label service_name) and events of the usage
// telemetry, as Alloy stores them.
const (
	serviceClaudeCode = "claude-code"
	serviceCodex      = "codex_cli_rs"

	eventClaudeRequest = "api_request"
	eventClaudeError   = "api_error"
	// claudeEventPrefix is how Claude Code's own event names start; Alloy
	// may keep it.
	claudeEventPrefix = "claude_code."

	eventCodexSSE      = "codex.sse_event"
	eventCodexRequest  = "codex.api_request"
	eventCodexTurnCost = "codex.turn_cost"
	codexKindCompleted = "response.completed"

	// httpErrorStatus is the lowest HTTP status of a failed request.
	httpErrorStatus = 400
)

// normalize turns one entry into a usage event: nil, nil when the entry is
// of no interest, and an error when it should have been an event and could
// not be one.
func normalize(cfg Config, e entry) (*model.UsageEvent, error) {
	a := attrs(e.labels)
	var ev *model.UsageEvent
	var err error
	switch a["service_name"] {
	case serviceClaudeCode:
		ev, err = normalizeClaude(a)
	case serviceCodex:
		ev, err = normalizeCodex(a)
	}
	if err != nil || ev == nil {
		if err != nil {
			err = fmt.Errorf("%s %s: %w", a["service_name"], a["event_name"], err)
		}
		return nil, err
	}
	if err := identify(cfg, a, e, ev); err != nil {
		return nil, fmt.Errorf("%s %s: %w", a["service_name"], a["event_name"], err)
	}
	if err := ev.Validate(); err != nil {
		return nil, err
	}
	return ev, nil
}

// identify sets what every event shares: where it came from, its time, and
// the identity its runner gave it. It never invents one.
func identify(cfg Config, a attrs, e entry, ev *model.UsageEvent) error {
	ev.Cluster, ev.Namespace = cfg.Cluster, a["k8s_namespace_name"]
	ev.Pod, ev.PodUID, ev.TaskName = a["k8s_pod_name"], a["k8s_pod_uid"], a["kelos_task_name"]
	ev.Model = a["model"]
	ev.At = e.at.UTC()
	if t, err := time.Parse(time.RFC3339Nano, a["event_timestamp"]); err == nil {
		ev.At = t.UTC()
	}
	for _, id := range []struct{ name, value string }{
		{"k8s_pod_uid", ev.PodUID}, {"k8s_pod_name", ev.Pod}, {"k8s_namespace_name", ev.Namespace},
		{"stream", ev.Stream}, {"sequence", ev.Seq},
	} {
		if id.value == "" {
			return fmt.Errorf("no %s", id.name)
		}
	}
	return nil
}

func normalizeClaude(a attrs) (*model.UsageEvent, error) {
	name := trimPrefix(a["event_name"], claudeEventPrefix)
	ev := &model.UsageEvent{
		Runner: model.RunnerClaudeCode, Event: a["event_name"], Stream: a["session_id"], Seq: a["event_sequence"],
		Agent: a["agent_id"], ParentAgent: a["parent_agent_id"], Speed: a["speed"],
	}
	switch name {
	case eventClaudeError:
		ev.Kind, ev.Status = model.UsageFailedRequest, a["status_code"]
		return ev, nil
	case eventClaudeRequest:
		ev.Kind = model.UsageResponse
	default:
		return nil, nil
	}
	var err error
	if ev.Tokens, err = claudeTokens(a); err != nil {
		return nil, err
	}
	if ev.ReportedCostUSD, err = a.decimal("cost_usd"); err != nil {
		return nil, err
	}
	return ev, nil
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// claudeTokens reads an api_request's counts: input_tokens is uncached,
// and cache_creation_tokens is the total of the 5-minute and 1-hour
// writes, when it says which.
func claudeTokens(a attrs) (*model.TokenCounts, error) {
	var t model.TokenCounts
	var total *int64
	for _, f := range []struct {
		name string
		to   **int64
	}{
		{"input_tokens", &t.UncachedInput}, {"cache_read_tokens", &t.CacheRead}, {"output_tokens", &t.Output},
		{"cache_creation_tokens", &total}, {"cache_creation_5m_tokens", &t.CacheWrite5m},
		{"cache_creation_1h_tokens", &t.CacheWrite1h},
	} {
		n, err := a.count(f.name)
		if err != nil {
			return nil, err
		}
		*f.to = n
	}
	if t.CacheWrite5m == nil && t.CacheWrite1h == nil {
		t.CacheWriteUnknownTTL = total
		return &t, nil
	}
	if total == nil {
		return &t, nil
	}
	rest := *total - deref(t.CacheWrite5m) - deref(t.CacheWrite1h)
	if rest < 0 {
		return nil, fmt.Errorf("the cache writes by duration (%d) exceed the total (%d)",
			*total-rest, *total)
	}
	if rest > 0 {
		t.CacheWriteUnknownTTL = &rest
	}
	return &t, nil
}

func deref(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

func normalizeCodex(a attrs) (*model.UsageEvent, error) {
	ev := &model.UsageEvent{
		Runner: model.RunnerCodex, Event: a["event_name"], Stream: a["conversation_id"],
		Seq: a["event_timestamp"], ServiceTier: a["service_tier"],
	}
	switch a["event_name"] {
	case eventCodexSSE:
		if a["event_kind"] != codexKindCompleted {
			return nil, nil
		}
		ev.Kind = model.UsageResponse
		tokens, err := codexTokens(a)
		ev.Tokens = tokens
		return ev, err
	case eventCodexRequest:
		return codexFailure(a, ev)
	case eventCodexTurnCost:
		ev.Kind = model.UsageReportedCost
		cost, err := codexCost(a)
		ev.ReportedCostUSD = cost
		return ev, err
	}
	return nil, nil
}

// codexFailure is a failed codex.api_request: one whose HTTP status is an
// error. Successful attempts carry no usage, and are skipped.
func codexFailure(a attrs, ev *model.UsageEvent) (*model.UsageEvent, error) {
	status := a["http_response_status_code"]
	if status == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(status)
	if err != nil {
		return nil, fmt.Errorf("http_response_status_code %q is not a number", status)
	}
	if n < httpErrorStatus {
		return nil, nil
	}
	ev.Kind, ev.Status = model.UsageFailedRequest, status
	return ev, nil
}

// codexTokens reads a response.completed's counts: input_token_count
// includes the cached and the cache-written tokens, output_token_count the
// reasoning ones. tool_token_count is the response's total, and ignored.
func codexTokens(a attrs) (*model.TokenCounts, error) {
	var input, cached, written, output, reasoning *int64
	for _, f := range []struct {
		name string
		to   **int64
	}{
		{"input_token_count", &input}, {"cached_token_count", &cached}, {"cache_write_token_count", &written},
		{"output_token_count", &output}, {"reasoning_token_count", &reasoning},
	} {
		n, err := a.count(f.name)
		if err != nil {
			return nil, err
		}
		*f.to = n
	}
	t := &model.TokenCounts{CacheRead: cached, CacheWriteUnknownTTL: written, Output: output, Reasoning: reasoning}
	if input != nil {
		uncached := *input - deref(cached) - deref(written)
		if uncached < 0 {
			return nil, fmt.Errorf("input_token_count %d is less than its cached (%d) and cache-written (%d) tokens",
				*input, deref(cached), deref(written))
		}
		t.UncachedInput = &uncached
	}
	return t, nil
}

// codexCost reads a turn_cost's estimate: usage_estimated_usd, else
// cost_usd_micros.
func codexCost(a attrs) (string, error) {
	cost, err := a.decimal("usage_estimated_usd")
	if err != nil || cost != "" {
		return cost, err
	}
	if cost, err = a.micros("cost_usd_micros"); err != nil || cost != "" {
		return cost, err
	}
	return "", fmt.Errorf("no usage_estimated_usd or cost_usd_micros")
}
