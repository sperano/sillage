package cost

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// Provider is who sells a model's API.
type Provider string

// The providers the price table covers.
const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
)

// Tier is the speed or service tier a request was served at.
type Tier string

// The tiers the price table covers.
const (
	TierStandard Tier = "standard"
	TierFast     Tier = "fast"
	TierFlex     Tier = "flex"
)

// tokensPerRate is the unit of every rate: a price per million tokens.
const tokensPerRate = 1_000_000

//go:embed prices.json
var embeddedPrices []byte

// Table is a dated, versioned price list, verified against the
// providers' published prices on VerifiedAt.
type Table struct {
	Version     string       `json:"version"`
	VerifiedAt  string       `json:"verifiedAt"`
	Currency    string       `json:"currency"`
	Unit        string       `json:"unit"`
	Sources     []Source     `json:"sources"`
	Assumptions []Assumption `json:"assumptions"`
	Entries     []Entry      `json:"entries"`
}

// Source is a page the table was read from.
type Source struct {
	Provider Provider `json:"provider"`
	URL      string   `json:"url"`
	ReadAt   string   `json:"readAt"`
}

// Assumption is one stated assumption of every estimate.
type Assumption struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Entry is the price of one model at one tier over a time range and a
// range of prompt sizes.
type Entry struct {
	ID       string   `json:"id"`
	Provider Provider `json:"provider"`
	Model    string   `json:"model"`
	Tier     Tier     `json:"tier"`
	From     time.Time
	// Until is exclusive; nil for no known end.
	Until *time.Time
	// PromptAbove and PromptUpTo bound the request's prompt size (all of
	// its input tokens, cached or not): above PromptAbove, at most
	// PromptUpTo (nil: no bound).
	PromptAbove int64  `json:"promptAbove,omitempty"`
	PromptUpTo  *int64 `json:"promptUpTo,omitempty"`
	Note        string `json:"note,omitempty"`
	// Rates are US dollars per million tokens by category; a category
	// without a rate is unknown at this entry.
	Rates map[Category]string `json:"rates"`
	rates map[Category]*big.Rat
}

// entryJSON is Entry as the table writes it, with its times as text.
type entryJSON struct {
	ID          string              `json:"id"`
	Provider    Provider            `json:"provider"`
	Model       string              `json:"model"`
	Tier        Tier                `json:"tier"`
	From        time.Time           `json:"from"`
	Until       *time.Time          `json:"until,omitempty"`
	PromptAbove int64               `json:"promptAbove,omitempty"`
	PromptUpTo  *int64              `json:"promptUpTo,omitempty"`
	Note        string              `json:"note,omitempty"`
	Rates       map[Category]string `json:"rates"`
}

// UnmarshalJSON reads an entry and parses its rates.
func (e *Entry) UnmarshalJSON(b []byte) error {
	var j entryJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*e = Entry{ID: j.ID, Provider: j.Provider, Model: j.Model, Tier: j.Tier, From: j.From.UTC(),
		Until: j.Until, PromptAbove: j.PromptAbove, PromptUpTo: j.PromptUpTo, Note: j.Note, Rates: j.Rates}
	if e.Until != nil {
		u := e.Until.UTC()
		e.Until = &u
	}
	return e.parseRates()
}

// rateDecimal is a rate as the table writes it.
var rateDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func (e *Entry) parseRates() error {
	e.rates = make(map[Category]*big.Rat, len(e.Rates))
	for c, s := range e.Rates {
		if !c.valid() {
			return fmt.Errorf("price %s: unknown category %q", e.ID, c)
		}
		r, ok := new(big.Rat).SetString(s)
		if !rateDecimal.MatchString(s) || !ok {
			return fmt.Errorf("price %s: rate %q of %s is not a decimal", e.ID, s, c)
		}
		e.rates[c] = r
	}
	return nil
}

// Rate returns the entry's rate of c, per token, or false without one.
func (e *Entry) Rate(c Category) (*big.Rat, bool) {
	r, ok := e.rates[c]
	if !ok {
		return nil, false
	}
	return new(big.Rat).Quo(r, big.NewRat(tokensPerRate, 1)), true
}

// DefaultTable returns the embedded price table.
func DefaultTable() (*Table, error) { return ParseTable(embeddedPrices) }

// ParseTable reads and validates a price table.
func ParseTable(b []byte) (*Table, error) {
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("cost: price table: %w", err)
	}
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("cost: price table %s: %w", t.Version, err)
	}
	return &t, nil
}

func (t *Table) validate() error {
	if t.Version == "" || t.VerifiedAt == "" || len(t.Sources) == 0 {
		return errors.New("a version, a verification date and sources are required")
	}
	ids := make(map[string]bool, len(t.Entries))
	for i := range t.Entries {
		e := &t.Entries[i]
		if e.ID == "" || ids[e.ID] {
			return fmt.Errorf("entry %d: missing or repeated id %q", i, e.ID)
		}
		ids[e.ID] = true
		if e.Model == "" || e.From.IsZero() || (e.Until != nil && !e.Until.After(e.From)) {
			return fmt.Errorf("price %s: a model and a non-empty time range are required", e.ID)
		}
		if e.PromptUpTo != nil && *e.PromptUpTo <= e.PromptAbove {
			return fmt.Errorf("price %s: an empty prompt range", e.ID)
		}
	}
	for i := range t.Entries {
		for j := i + 1; j < len(t.Entries); j++ {
			if overlap(&t.Entries[i], &t.Entries[j]) {
				return fmt.Errorf("prices %s and %s overlap", t.Entries[i].ID, t.Entries[j].ID)
			}
		}
	}
	return nil
}

// overlap reports whether two entries could both price one request.
func overlap(a, b *Entry) bool {
	if a.Provider != b.Provider || a.Model != b.Model || a.Tier != b.Tier {
		return false
	}
	before := func(x, y *Entry) bool { return x.Until != nil && !x.Until.After(y.From) }
	if before(a, b) || before(b, a) {
		return false
	}
	below := func(x, y *Entry) bool { return x.PromptUpTo != nil && *x.PromptUpTo <= y.PromptAbove }
	return !below(a, b) && !below(b, a)
}

// Lookup returns the entry that prices a request, or nil when none does:
// its price is unknown.
func (t *Table) Lookup(p Provider, modelName string, tier Tier, at time.Time, prompt int64) *Entry {
	for i := range t.Entries {
		e := &t.Entries[i]
		if e.Provider != p || e.Model != modelName || e.Tier != tier {
			continue
		}
		if at.Before(e.From) || (e.Until != nil && !at.Before(*e.Until)) {
			continue
		}
		if prompt < 0 || (prompt <= e.PromptAbove && e.PromptAbove > 0) {
			continue
		}
		if e.PromptUpTo != nil && prompt > *e.PromptUpTo {
			continue
		}
		return e
	}
	return nil
}

// providerOf returns who sells the models a runner uses.
func providerOf(r model.Runner) Provider {
	if r == model.RunnerCodex {
		return ProviderOpenAI
	}
	return ProviderAnthropic
}

// contextSuffix is a runner's context-window suffix on a model name
// ("claude-opus-5-5[1m]"): not part of the model's id.
var contextSuffix = regexp.MustCompile(`\[[^\]]*\]$`)

// modelID returns the model's id as the price table names it.
func modelID(name string) string {
	return strings.ToLower(strings.TrimSpace(contextSuffix.ReplaceAllString(strings.TrimSpace(name), "")))
}

// tierOf returns the tier an event reports, or false for a speed or
// service tier the table does not know.
func tierOf(e model.UsageEvent) (Tier, bool) {
	if e.Runner == model.RunnerCodex {
		switch strings.ToLower(e.ServiceTier) {
		case "", "default", "auto", "standard":
			return TierStandard, true
		case "flex":
			return TierFlex, true
		case "priority", "fast":
			return TierFast, true
		}
		return "", false
	}
	switch strings.ToLower(e.Speed) {
	case "", "normal", "standard":
		return TierStandard, true
	case "fast":
		return TierFast, true
	}
	return "", false
}

// MustDefaultTable returns the embedded price table, and panics if it is
// invalid: it is embedded, and its tests parse it.
func MustDefaultTable() *Table {
	t, err := DefaultTable()
	if err != nil {
		panic(err)
	}
	return t
}
