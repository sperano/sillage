package cost

import (
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func mustTable(t *testing.T) *Table {
	t.Helper()
	tab, err := DefaultTable()
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLookup(t *testing.T) {
	tab := mustTable(t)
	cases := []struct {
		name     string
		provider Provider
		model    string
		tier     Tier
		at       string
		prompt   int64
		want     string
	}{
		{"haiku at its threshold", ProviderAnthropic, "claude-haiku-5-5", TierStandard, "2026-11-23T00:00:00Z", 100000, "anthropic/claude-haiku-5-5/standard"},
		{"haiku over its threshold", ProviderAnthropic, "claude-haiku-5-5", TierStandard, "2026-11-23T00:00:00Z", 100001, "anthropic/claude-haiku-5-5/standard/long"},
		{"empty prompt", ProviderAnthropic, "claude-haiku-5-5", TierStandard, "2026-11-23T00:00:00Z", 0, "anthropic/claude-haiku-5-5/standard"},
		{"opus fast at any size", ProviderAnthropic, "claude-opus-5-5", TierFast, "2026-11-23T00:00:00Z", 900000, "anthropic/claude-opus-5-5/fast"},
		{"before the table", ProviderAnthropic, "claude-opus-5-5", TierStandard, "2026-11-21T23:59:59Z", 10, ""},
		{"sol before its promotion ends", ProviderOpenAI, "gpt-5.6-sol", TierStandard, "2026-11-30T23:59:59Z", 10, "openai/gpt-5.6-sol/standard"},
		{"sol after its promotion ends", ProviderOpenAI, "gpt-5.6-sol", TierStandard, "2026-12-01T00:00:00Z", 10, ""},
		{"sol long context", ProviderOpenAI, "gpt-5.6-sol", TierStandard, "2026-11-23T00:00:00Z", 272001, "openai/gpt-5.6-sol/standard/long"},
		{"sol fast long context has no price", ProviderOpenAI, "gpt-5.6-sol", TierFast, "2026-11-23T00:00:00Z", 272001, ""},
		{"a model the table lacks", ProviderAnthropic, "claude-haiku-5", TierStandard, "2026-11-23T00:00:00Z", 10, ""},
		{"a provider's model under the other", ProviderOpenAI, "claude-opus-5-5", TierStandard, "2026-11-23T00:00:00Z", 10, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ""
			if e := tab.Lookup(c.provider, c.model, c.tier, at(t, c.at), c.prompt); e != nil {
				got = e.ID
			}
			if got != c.want {
				t.Errorf("Lookup = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseTableRefuses(t *testing.T) {
	const head = `{"version":"v","verifiedAt":"2026-11-22","sources":[{"provider":"anthropic","url":"https://example.com","readAt":"2026-11-22"}],"entries":[`
	entry := func(id, extra, rates string) string {
		return `{"id":"` + id + `","provider":"anthropic","model":"m","tier":"standard","from":"2026-11-22T00:00:00Z"` + extra + `,"rates":` + rates + `}`
	}
	ok := `{"output":"1"}`
	cases := []struct{ name, body, want string }{
		{"no version", `{"verifiedAt":"x","sources":[],"entries":[]}`, "a version, a verification date and sources are required"},
		{"repeated id", head + entry("a", "", ok) + "," + entry("a", `,"promptAbove":10`, ok) + "]}", `missing or repeated id "a"`},
		{"overlap", head + entry("a", "", ok) + "," + entry("b", "", ok) + "]}", "prices a and b overlap"},
		{"bad rate", head + entry("a", "", `{"output":"1e3"}`) + "]}", `rate "1e3" of output is not a decimal`},
		{"unknown category", head + entry("a", "", `{"tools":"1"}`) + "]}", `unknown category "tools"`},
		{"empty prompt range", head + entry("a", `,"promptAbove":10,"promptUpTo":10`, ok) + "]}", "an empty prompt range"},
		{"empty time range", head + entry("a", `,"until":"2026-11-22T00:00:00Z"`, ok) + "]}", "a non-empty time range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseTable([]byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("ParseTable error = %v, want one saying %q", err, c.want)
			}
		})
	}
}

func TestParseTableAcceptsBands(t *testing.T) {
	const body = `{"version":"v","verifiedAt":"2026-11-22","sources":[{"provider":"anthropic","url":"https://example.com","readAt":"2026-11-22"}],"entries":[
		{"id":"a","provider":"anthropic","model":"m","tier":"standard","from":"2026-11-22T00:00:00Z","promptUpTo":10,"rates":{"output":"1"}},
		{"id":"b","provider":"anthropic","model":"m","tier":"standard","from":"2026-11-22T00:00:00Z","promptAbove":10,"rates":{"output":"2"}},
		{"id":"c","provider":"anthropic","model":"m","tier":"standard","from":"2026-10-01T00:00:00Z","until":"2026-11-22T00:00:00Z","rates":{"output":"3"}}]}`
	if _, err := ParseTable([]byte(body)); err != nil {
		t.Fatalf("ParseTable: %v", err)
	}
}

func TestModelID(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5[1m]": "claude-opus-5-5",
		" GPT-5.6-Sol ":       "gpt-5.6-sol",
		"claude-sonnet-5":     "claude-sonnet-5",
	} {
		if got := modelID(in); got != want {
			t.Errorf("modelID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTierOf(t *testing.T) {
	cases := []struct {
		runner      model.Runner
		speed, tier string
		want        Tier
		known       bool
	}{
		{model.RunnerClaudeCode, "", "", TierStandard, true},
		{model.RunnerClaudeCode, "normal", "", TierStandard, true},
		{model.RunnerClaudeCode, "fast", "", TierFast, true},
		{model.RunnerClaudeCode, "turbo", "", "", false},
		{model.RunnerCodex, "", "", TierStandard, true},
		{model.RunnerCodex, "", "auto", TierStandard, true},
		{model.RunnerCodex, "", "flex", TierFlex, true},
		{model.RunnerCodex, "", "priority", TierFast, true},
		{model.RunnerCodex, "", "scale", "", false},
	}
	for _, c := range cases {
		got, known := tierOf(model.UsageEvent{Runner: c.runner, Speed: c.speed, ServiceTier: c.tier})
		if got != c.want || known != c.known {
			t.Errorf("tierOf(%s, speed %q, tier %q) = %q, %v; want %q, %v", c.runner, c.speed, c.tier, got, known, c.want, c.known)
		}
	}
}
