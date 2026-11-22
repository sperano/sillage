package model

import (
	"strings"
	"testing"
	"time"
)

func validUsage() UsageEvent {
	n := int64(1)
	return UsageEvent{Cluster: "cluster-a", Namespace: "agents", Pod: "pod-a", PodUID: "uid-a",
		Runner: RunnerClaudeCode, Kind: UsageResponse, Event: "api_request", Stream: "s", Seq: "1",
		At:     time.Date(2026, time.November, 23, 10, 0, 0, 0, time.UTC),
		Tokens: &TokenCounts{UncachedInput: &n, Output: &n}}
}

func TestUsageEventValidate(t *testing.T) {
	neg, two, one := int64(-1), int64(2), int64(1)
	cases := []struct {
		name string
		edit func(*UsageEvent)
		want string
	}{
		{"valid", func(*UsageEvent) {}, ""},
		{"no pod UID", func(e *UsageEvent) { e.PodUID = "" }, "pod UID are required"},
		{"no sequence", func(e *UsageEvent) { e.Seq = "" }, "sequence are required"},
		{"unknown runner", func(e *UsageEvent) { e.Runner = "opencode" }, `unknown runner "opencode"`},
		{"unknown kind", func(e *UsageEvent) { e.Kind = "span" }, `unknown kind "span"`},
		{"no time", func(e *UsageEvent) { e.At = time.Time{} }, "a time is required"},
		{"a response without tokens", func(e *UsageEvent) { e.Tokens = nil }, "only a response, has token counts"},
		{"a failure with tokens", func(e *UsageEvent) { e.Kind = UsageFailedRequest }, "only a response, has token counts"},
		{"a negative count", func(e *UsageEvent) { e.Tokens.CacheRead = &neg }, "token count cacheRead is negative"},
		{"reasoning over output", func(e *UsageEvent) { e.Tokens.Output, e.Tokens.Reasoning = &one, &two }, "cannot exceed it"},
		{"a cost that is not a decimal", func(e *UsageEvent) { e.ReportedCostUSD = "-1" }, `reported cost "-1"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validUsage()
			c.edit(&e)
			err := e.Validate()
			if c.want == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Validate = %v, want an error saying %q", err, c.want)
			}
		})
	}
}

func TestUsageEventKeyAndNormalize(t *testing.T) {
	e := validUsage()
	e.At = time.Date(2026, time.November, 23, 3, 0, 0, 0, time.FixedZone("UTC-7", -7*60*60))
	if got := e.Normalize().At; got.Location() != time.UTC || !got.Equal(e.At) {
		t.Errorf("Normalize().At = %v, want %v in UTC", got, e.At)
	}
	if got, want := e.Key(), (UsageKey{PodUID: "uid-a", Event: "api_request", Stream: "s", Seq: "1"}); got != want {
		t.Errorf("Key = %+v, want %+v", got, want)
	}
}
