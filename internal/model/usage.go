package model

import (
	"errors"
	"fmt"
	"time"
)

// Runner is the agent CLI that exported a usage event.
type Runner string

// The runners whose usage events sillage reads.
const (
	RunnerClaudeCode Runner = "claude-code"
	RunnerCodex      Runner = "codex"
)

func (r Runner) valid() bool { return r == RunnerClaudeCode || r == RunnerCodex }

// UsageKind is what a usage event records.
type UsageKind string

// The kinds of usage event.
const (
	// UsageResponse: one completed model response and the tokens it used,
	// its own and not a running total.
	UsageResponse UsageKind = "response"
	// UsageFailedRequest: one model request that failed, with no tokens.
	UsageFailedRequest UsageKind = "failed-request"
	// UsageReportedCost: the runner's own cost estimate of a turn, apart
	// from any response.
	UsageReportedCost UsageKind = "reported-cost"
)

func (k UsageKind) valid() bool {
	return k == UsageResponse || k == UsageFailedRequest || k == UsageReportedCost
}

// UsageEvent is one usage event a runner in a Kelos run's pod exported,
// as the telemetry store kept it. The pod is the one the collector
// stamped from the connection, never one the runner claimed. A usage
// event is evidence apart from every story: it is joined to a run by its
// pod (or, failing that, by its Task name and time) only when costs are
// computed.
type UsageEvent struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	PodUID    string `json:"podUID"`
	// TaskName is the kelos.dev/task label of the pod, a hint for the
	// join when the pod is not one a run was seen with.
	TaskName string    `json:"taskName,omitempty"`
	Runner   Runner    `json:"runner"`
	Kind     UsageKind `json:"kind"`
	// Event is the runner's own event name (api_request, codex.sse_event,
	// ...), part of the identity: two events of one stream can share Seq.
	Event string `json:"event"`
	// Stream is the runner's session (Claude Code's session.id) or
	// conversation (Codex's conversation.id).
	Stream string `json:"stream"`
	// Seq orders the event in its stream: Claude Code's event.sequence,
	// Codex's event.timestamp. With PodUID, Event and Stream it is the
	// identity that makes an event delivered twice count once.
	Seq string    `json:"seq"`
	At  time.Time `json:"at"`
	// Model is the model the runner reported for the request, when it did.
	Model string `json:"model,omitempty"`
	// Agent is the Claude Code subagent that made the request (agent_id),
	// and ParentAgent the one that started it; both empty for the main
	// loop.
	Agent       string `json:"agent,omitempty"`
	ParentAgent string `json:"parentAgent,omitempty"`
	// Speed is Claude Code's request speed ("fast"), ServiceTier Codex's
	// service tier, when reported.
	Speed       string `json:"speed,omitempty"`
	ServiceTier string `json:"serviceTier,omitempty"`
	// Tokens are a response's token counts; nil on other kinds.
	Tokens *TokenCounts `json:"tokens,omitempty"`
	// ReportedCostUSD is the runner's own cost estimate in US dollars, as
	// a decimal string, when it reported one. It is kept apart from
	// sillage's estimate and never added to it.
	ReportedCostUSD string `json:"reportedCostUSD,omitempty"`
	// Status is a failed request's HTTP status, when reported.
	Status string `json:"status,omitempty"`
}

// TokenCounts are the tokens of one response by how they are priced. A
// nil count was not reported; it is unknown, never zero.
type TokenCounts struct {
	// UncachedInput is input neither read from nor written to a cache.
	UncachedInput *int64 `json:"uncachedInput,omitempty"`
	CacheRead     *int64 `json:"cacheRead,omitempty"`
	CacheWrite5m  *int64 `json:"cacheWrite5m,omitempty"`
	CacheWrite1h  *int64 `json:"cacheWrite1h,omitempty"`
	// CacheWriteUnknownTTL is cache-write input whose cache duration the
	// runner did not report, so that its price is unknown.
	CacheWriteUnknownTTL *int64 `json:"cacheWriteUnknownTTL,omitempty"`
	// Output includes Reasoning.
	Output    *int64 `json:"output,omitempty"`
	Reasoning *int64 `json:"reasoning,omitempty"`
}

// UsageKey is a usage event's identity.
type UsageKey struct {
	PodUID string `json:"podUID"`
	Event  string `json:"event"`
	Stream string `json:"stream"`
	Seq    string `json:"seq"`
}

// Key returns e's identity.
func (e UsageEvent) Key() UsageKey {
	return UsageKey{PodUID: e.PodUID, Event: e.Event, Stream: e.Stream, Seq: e.Seq}
}

// Validate reports whether e is complete and self-consistent.
func (e UsageEvent) Validate() error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("usage event %s/%s %s %s/%s: %w", e.Namespace, e.Pod, e.Event, e.Stream, e.Seq, err)
	}
	return nil
}

func (e UsageEvent) validate() error {
	if e.Cluster == "" || e.Namespace == "" || e.Pod == "" || e.PodUID == "" {
		return errors.New("cluster, namespace, pod and pod UID are required")
	}
	if e.Event == "" || e.Stream == "" || e.Seq == "" {
		return errors.New("event, stream and sequence are required")
	}
	if !e.Runner.valid() {
		return fmt.Errorf("unknown runner %q", e.Runner)
	}
	if !e.Kind.valid() {
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	if e.At.IsZero() {
		return errors.New("a time is required")
	}
	if (e.Kind == UsageResponse) != (e.Tokens != nil) {
		return errors.New("a response, and only a response, has token counts")
	}
	if e.ReportedCostUSD != "" && !decimalPattern.MatchString(e.ReportedCostUSD) {
		return fmt.Errorf("reported cost %q is not a non-negative decimal", e.ReportedCostUSD)
	}
	if e.Tokens != nil {
		return e.Tokens.validate()
	}
	return nil
}

// Counts returns the counts by name, in a fixed order.
func (t TokenCounts) Counts() []NamedCount {
	return []NamedCount{
		{"uncachedInput", t.UncachedInput}, {"cacheRead", t.CacheRead},
		{"cacheWrite5m", t.CacheWrite5m}, {"cacheWrite1h", t.CacheWrite1h},
		{"cacheWriteUnknownTTL", t.CacheWriteUnknownTTL},
		{"output", t.Output}, {"reasoning", t.Reasoning},
	}
}

// NamedCount is one count of TokenCounts.
type NamedCount struct {
	Name  string
	Count *int64
}

func (t TokenCounts) validate() error {
	for _, c := range t.Counts() {
		if c.Count != nil && *c.Count < 0 {
			return fmt.Errorf("token count %s is negative", c.Name)
		}
	}
	if t.Reasoning != nil && (t.Output == nil || *t.Reasoning > *t.Output) {
		return errors.New("reasoning tokens are part of the output and cannot exceed it")
	}
	return nil
}

// Normalize returns e with its time in UTC.
func (e UsageEvent) Normalize() UsageEvent {
	e.At = e.At.UTC()
	return e
}
