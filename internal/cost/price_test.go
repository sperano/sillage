package cost

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func count(n int64) *int64 { return &n }

func response(t *testing.T, runner model.Runner, modelName string, tokens *model.TokenCounts) model.UsageEvent {
	t.Helper()
	return model.UsageEvent{Runner: runner, Kind: model.UsageResponse, Model: modelName,
		At: at(t, "2026-11-23T10:00:00Z"), Tokens: tokens}
}

func TestPriceEventExactAmounts(t *testing.T) {
	e := response(t, model.RunnerClaudeCode, "claude-opus-5-5", &model.TokenCounts{
		UncachedInput: count(10), Output: count(20), CacheRead: count(30), CacheWrite5m: count(40), CacheWrite1h: count(50)})
	b := priceEvent(mustTable(t), e).acc.breakdown()
	// 10×4 + 20×20 + 30×0.20 + 40×5 + 50×8 dollars per million tokens.
	if b.Status != StatusComplete || b.EstimatedUSD != "0.001046" {
		t.Fatalf("got %s %q, want complete 0.001046", b.Status, b.EstimatedUSD)
	}
	want := map[Category]string{CategoryUncachedInput: "0.000040", CategoryOutput: "0.000400",
		CategoryCacheRead: "0.000006", CategoryCacheWrite5m: "0.000200", CategoryCacheWrite1h: "0.000400"}
	for _, c := range b.Categories {
		if c.EstimatedUSD != want[c.Category] {
			t.Errorf("%s: got %q, want %q", c.Category, c.EstimatedUSD, want[c.Category])
		}
	}
}

func TestPriceEventStatuses(t *testing.T) {
	tab := mustTable(t)
	full := func() *model.TokenCounts {
		return &model.TokenCounts{UncachedInput: count(1000000), Output: count(0), CacheRead: count(0), CacheWrite5m: count(0)}
	}
	noOutput := full()
	noOutput.Output = nil
	noCacheWrite := full()
	noCacheWrite.CacheWrite5m = nil
	fast := response(t, model.RunnerClaudeCode, "claude-opus-5-5", full())
	fast.Speed = "turbo"
	codexWrite := response(t, model.RunnerCodex, "gpt-5.6-sol", &model.TokenCounts{
		UncachedInput: count(1000000), Output: count(0), CacheRead: count(0), CacheWriteUnknownTTL: count(10)})
	cases := []struct {
		name      string
		e         model.UsageEvent
		status    Status
		estimated string
		notes     []string
	}{
		{"complete", response(t, model.RunnerClaudeCode, "claude-opus-5-5", full()), StatusComplete, "4.000000", nil},
		{"zero tokens are a zero price", response(t, model.RunnerClaudeCode, "claude-opus-5-5", &model.TokenCounts{
			UncachedInput: count(0), Output: count(0), CacheRead: count(0), CacheWrite5m: count(0)}), StatusComplete, "0.000000", nil},
		{"output not reported", response(t, model.RunnerClaudeCode, "claude-opus-5-5", noOutput), StatusPartial, "4.000000", []string{NoteTokensNotReported}},
		{"cache writes not reported", response(t, model.RunnerClaudeCode, "claude-opus-5-5", noCacheWrite), StatusPartial, "4.000000", []string{NoteTokensNotReported}},
		{"unknown model", response(t, model.RunnerClaudeCode, "claude-haiku-5", full()), StatusUnknown, "", []string{NotePriceUnknown}},
		{"no model", response(t, model.RunnerClaudeCode, "", full()), StatusUnknown, "", []string{NoteModelNotReported}},
		{"unknown speed", fast, StatusUnknown, "", []string{NoteTierUnknown}},
		// Over 272K prompt tokens: the long-context rate.
		{"cache write of unknown duration", codexWrite, StatusPartial, "8.000000", []string{NoteCacheWriteTTL}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := priceEvent(tab, c.e).acc.breakdown()
			if b.Status != c.status || b.EstimatedUSD != c.estimated || !slices.Equal(b.Notes, c.notes) {
				t.Errorf("got %s %q %v, want %s %q %v", b.Status, b.EstimatedUSD, b.Notes, c.status, c.estimated, c.notes)
			}
		})
	}
}

func TestPriceEventFailedAndReported(t *testing.T) {
	tab := mustTable(t)
	failed := priceEvent(tab, model.UsageEvent{Runner: model.RunnerClaudeCode, Kind: model.UsageFailedRequest,
		Model: "claude-opus-5-5", At: at(t, "2026-11-23T10:00:00Z")}).acc.breakdown()
	if failed.Status != StatusComplete || failed.EstimatedUSD != "0.000000" || failed.FailedRequests != 1 || failed.Requests != 0 {
		t.Errorf("failed request: got %+v, want complete, 0.000000, one failed request", failed)
	}
	e := response(t, model.RunnerClaudeCode, "claude-opus-5-5", &model.TokenCounts{
		UncachedInput: count(1000000), Output: count(0), CacheRead: count(0), CacheWrite5m: count(0)})
	e.ReportedCostUSD = "9.5"
	b := priceEvent(tab, e).acc.breakdown()
	if b.EstimatedUSD != "4.000000" || b.ReportedUSD != "9.500000" {
		t.Errorf("got estimated %q reported %q, want 4.000000 and 9.500000 apart", b.EstimatedUSD, b.ReportedUSD)
	}
	turn := priceEvent(tab, model.UsageEvent{Runner: model.RunnerCodex, Kind: model.UsageReportedCost,
		Model: "gpt-5.6-sol", At: at(t, "2026-11-23T10:00:00Z"), ReportedCostUSD: "0.25"})
	if b := turn.acc.breakdown(); b.EstimatedUSD != "" || b.ReportedUSD != "0.250000" || b.Requests != 0 || turn.tier != TierStandard {
		t.Errorf("reported cost: got %+v tier %q, want only 0.250000 reported, at the standard tier", b, turn.tier)
	}
}
