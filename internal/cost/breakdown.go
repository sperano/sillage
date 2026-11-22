package cost

import (
	"math/big"
	"slices"
)

// Category is one way tokens are priced.
type Category string

// The categories, in the order a breakdown lists them.
const (
	CategoryUncachedInput        Category = "uncachedInput"
	CategoryCacheRead            Category = "cacheRead"
	CategoryCacheWrite5m         Category = "cacheWrite5m"
	CategoryCacheWrite1h         Category = "cacheWrite1h"
	CategoryCacheWriteUnknownTTL Category = "cacheWriteUnknownTTL"
	CategoryOutput               Category = "output"
)

// categories lists every category in order.
var categories = []Category{
	CategoryUncachedInput, CategoryCacheRead, CategoryCacheWrite5m,
	CategoryCacheWrite1h, CategoryCacheWriteUnknownTTL, CategoryOutput,
}

func (c Category) valid() bool { return slices.Contains(categories, c) }

// Status is how much of a breakdown's usage is known and priced.
type Status string

// The statuses of a breakdown.
const (
	// StatusComplete: every expected usage event was read and priced.
	StatusComplete Status = "complete"
	// StatusPartial: some usage was priced, and some is missing or has
	// no known price: EstimatedUSD is a lower bound.
	StatusPartial Status = "partial"
	// StatusUnknown: nothing could be priced: there is no estimate, not a
	// zero one.
	StatusUnknown Status = "unknown"
	// StatusNone: the run used no model, and none was expected (a
	// model-free automation Task).
	StatusNone Status = "none"
)

// usdDecimals is how many decimals a dollar amount is written with. Sums
// are exact; only the written amount is rounded.
const usdDecimals = 6

// Breakdown is the estimated API-equivalent cost of some usage, by
// category, with the runners' own reported cost apart.
type Breakdown struct {
	Status Status `json:"status"`
	// EstimatedUSD is the priced part, in US dollars; absent when nothing
	// was priced. With StatusPartial it is a lower bound.
	EstimatedUSD string `json:"estimatedUSD,omitempty"`
	// ReportedUSD is the sum of the runners' own cost estimates, when any
	// reported one. It is never added to EstimatedUSD.
	ReportedUSD    string         `json:"reportedUSD,omitempty"`
	Requests       int            `json:"requests"`
	FailedRequests int            `json:"failedRequests"`
	Categories     []CategoryCost `json:"categories"`
	// Notes are the reasons for a status other than complete, and the
	// remarks on how usage was attributed, sorted.
	Notes []string `json:"notes,omitempty"`
}

// CategoryCost is one category of a breakdown.
type CategoryCost struct {
	Category Category `json:"category"`
	// Tokens is the sum of the reported tokens of the category.
	Tokens int64 `json:"tokens"`
	// EstimatedUSD is their priced part; absent when none was priced.
	EstimatedUSD string `json:"estimatedUSD,omitempty"`
	// Unpriced says that some of the category's tokens have no known
	// price, or were not reported.
	Unpriced bool `json:"unpriced,omitempty"`
}

// The notes of a breakdown. Those in lowering lower its status.
const (
	NoteNoUsageEvents     = "no-usage-events"
	NoteBeforeTelemetry   = "before-telemetry"
	NoteFeedOff           = "usage-feed-off"
	NoteInProgress        = "in-progress"
	NotePriceUnknown      = "price-unknown"
	NoteTierUnknown       = "tier-unknown"
	NoteModelNotReported  = "model-not-reported"
	NoteCacheWriteTTL     = "cache-write-ttl-unknown"
	NoteTokensNotReported = "tokens-not-reported"
	NoteMayLoseUnsent     = "failed-run-may-lose-unsent-events"
	NoteUnattributed      = "unattributed-usage"
	NoteAttributedByName  = "attributed-by-task-name"
)

// lowering are the notes that make a breakdown partial or unknown.
var lowering = map[string]bool{
	NoteNoUsageEvents: true, NoteBeforeTelemetry: true, NoteFeedOff: true,
	NoteInProgress: true, NotePriceUnknown: true, NoteTierUnknown: true,
	NoteModelNotReported: true, NoteCacheWriteTTL: true, NoteTokensNotReported: true,
	NoteMayLoseUnsent: true,
}

// acc accumulates usage into a breakdown.
type acc struct {
	usd      big.Rat
	priced   bool
	reported big.Rat
	anyRep   bool
	requests int
	failed   int
	tokens   map[Category]int64
	catUSD   map[Category]*big.Rat
	unpriced map[Category]bool
	notes    map[string]bool
	// none: no usage was expected of anything added; empty: nothing was
	// added yet.
	none, empty bool
}

func newAcc() *acc {
	return &acc{tokens: map[Category]int64{}, catUSD: map[Category]*big.Rat{},
		unpriced: map[Category]bool{}, notes: map[string]bool{}, empty: true}
}

func (a *acc) note(n string) { a.notes[n] = true }

// add adds b's usage into a.
func (a *acc) add(b *acc) {
	if b.priced {
		a.usd.Add(&a.usd, &b.usd)
		a.priced = true
	}
	if b.anyRep {
		a.reported.Add(&a.reported, &b.reported)
		a.anyRep = true
	}
	a.requests += b.requests
	a.failed += b.failed
	for c, n := range b.tokens {
		a.tokens[c] += n
	}
	for c, u := range b.catUSD {
		a.addCategoryUSD(c, u)
	}
	for c := range b.unpriced {
		a.unpriced[c] = true
	}
	for n := range b.notes {
		a.notes[n] = true
	}
	a.none = (a.empty || a.none) && b.none
	a.empty = false
}

func (a *acc) addCategoryUSD(c Category, u *big.Rat) {
	if a.catUSD[c] == nil {
		a.catUSD[c] = new(big.Rat)
	}
	a.catUSD[c].Add(a.catUSD[c], u)
}

// status returns a's status.
func (a *acc) status() Status {
	if a.none && !a.priced && a.requests == 0 && a.failed == 0 {
		return StatusNone
	}
	lowered := len(a.unpriced) > 0
	for n := range a.notes {
		lowered = lowered || lowering[n]
	}
	switch {
	case !a.priced && (lowered || a.requests == 0):
		return StatusUnknown
	case lowered:
		return StatusPartial
	}
	return StatusComplete
}

// breakdown returns a written as a Breakdown.
func (a *acc) breakdown() Breakdown {
	b := Breakdown{Status: a.status(), Requests: a.requests, FailedRequests: a.failed,
		Categories: make([]CategoryCost, 0, len(categories))}
	if a.priced {
		b.EstimatedUSD = usd(&a.usd)
	}
	if a.anyRep {
		b.ReportedUSD = usd(&a.reported)
	}
	for _, c := range categories {
		cc := CategoryCost{Category: c, Tokens: a.tokens[c], Unpriced: a.unpriced[c]}
		if u := a.catUSD[c]; u != nil {
			cc.EstimatedUSD = usd(u)
		}
		b.Categories = append(b.Categories, cc)
	}
	for n := range a.notes {
		b.Notes = append(b.Notes, n)
	}
	slices.Sort(b.Notes)
	return b
}

// usd writes a dollar amount with usdDecimals decimals.
func usd(r *big.Rat) string { return r.FloatString(usdDecimals) }
