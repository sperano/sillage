package cost

import (
	"math/big"

	"github.com/sperano/sillage/internal/model"
)

// priced is one usage event's contribution: its breakdown, and the price
// entry that priced it, if any.
type priced struct {
	acc   *acc
	entry string
	tier  Tier
}

// priceEvent returns e's contribution under t.
func priceEvent(t *Table, e model.UsageEvent) priced {
	a := newAcc()
	a.empty = false
	if e.ReportedCostUSD != "" {
		if r, ok := new(big.Rat).SetString(e.ReportedCostUSD); ok {
			a.reported.Set(r)
			a.anyRep = true
		}
	}
	tier, tierKnown := tierOf(e)
	switch e.Kind {
	case model.UsageFailedRequest:
		// A failed request reports no tokens and is not billed: its known
		// price is zero.
		a.failed = 1
		a.priced = true
		return priced{acc: a, tier: tier}
	case model.UsageReportedCost:
		return priced{acc: a, tier: tier}
	}
	a.requests = 1
	tokens := reportedTokens(a, e.Tokens)
	if !tierKnown {
		a.note(NoteTierUnknown)
		unpriceAll(a, tokens)
		return priced{acc: a}
	}
	if e.Model == "" {
		a.note(NoteModelNotReported)
		unpriceAll(a, tokens)
		return priced{acc: a, tier: tier}
	}
	entry := t.Lookup(providerOf(e.Runner), modelID(e.Model), tier, e.At, promptOf(tokens))
	if entry == nil {
		a.note(NotePriceUnknown)
		unpriceAll(a, tokens)
		return priced{acc: a, tier: tier}
	}
	if !a.notes[NoteTokensNotReported] {
		// Every count was reported: the estimate is known, zero included.
		// Otherwise it is only what the categories priced below add up to,
		// and absent, never zero, when none was.
		a.priced = true
	}
	for c, n := range tokens {
		priceCategory(a, entry, c, n)
	}
	return priced{acc: a, entry: entry.ID, tier: tier}
}

// reportedTokens records the event's token counts in a, by category, and
// notes the counts it lacks. It returns the counts reported.
func reportedTokens(a *acc, tc *model.TokenCounts) map[Category]int64 {
	out := map[Category]int64{}
	if tc == nil {
		a.note(NoteTokensNotReported)
		return out
	}
	byCat := map[Category]*int64{
		CategoryUncachedInput: tc.UncachedInput, CategoryCacheRead: tc.CacheRead,
		CategoryCacheWrite5m: tc.CacheWrite5m, CategoryCacheWrite1h: tc.CacheWrite1h,
		CategoryCacheWriteUnknownTTL: tc.CacheWriteUnknownTTL, CategoryOutput: tc.Output,
	}
	for _, c := range []Category{CategoryUncachedInput, CategoryCacheRead, CategoryOutput} {
		if byCat[c] == nil {
			a.note(NoteTokensNotReported)
			a.unpriced[c] = true
		}
	}
	if tc.CacheWrite5m == nil && tc.CacheWrite1h == nil && tc.CacheWriteUnknownTTL == nil {
		a.note(NoteTokensNotReported)
		a.unpriced[CategoryCacheWriteUnknownTTL] = true
	}
	for c, p := range byCat {
		if p != nil && *p > 0 {
			out[c] = *p
			a.tokens[c] += *p
		}
	}
	return out
}

// promptOf returns a request's prompt size: every input token, cached or
// not.
func promptOf(tokens map[Category]int64) int64 {
	var n int64
	for c, v := range tokens {
		if c != CategoryOutput {
			n += v
		}
	}
	return n
}

// priceCategory prices n tokens of c at entry.
func priceCategory(a *acc, entry *Entry, c Category, n int64) {
	rate, ok := entry.Rate(c)
	if !ok {
		a.unpriced[c] = true
		if c == CategoryCacheWriteUnknownTTL {
			a.note(NoteCacheWriteTTL)
		} else {
			a.note(NotePriceUnknown)
		}
		return
	}
	u := new(big.Rat).Mul(rate, new(big.Rat).SetInt64(n))
	a.usd.Add(&a.usd, u)
	a.priced = true
	a.addCategoryUSD(c, u)
}

// unpriceAll marks every reported category unpriced.
func unpriceAll(a *acc, tokens map[Category]int64) {
	for c := range tokens {
		a.unpriced[c] = true
	}
}
