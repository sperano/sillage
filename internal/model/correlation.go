package model

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
)

// Confidence is how strongly the evidence establishes a correlation.
type Confidence string

// The confidences of a correlation.
const (
	// ConfidenceIdentifier: an identifier both records carry establishes
	// it (a Task label naming the task, a pod name, a branch).
	ConfidenceIdentifier Confidence = "identifier"
	// ConfidenceHeuristic: only a coincidence suggests it (same account
	// within a run's window). Show it as uncertain, never as a causal link.
	ConfidenceHeuristic Confidence = "heuristic"
)

// Correlation links two records the sources do not link themselves.
//
// When the evidence fits several candidates (two concurrent runs of one
// agent profile share its account), each candidate gets its own
// correlation flagged Ambiguous: all are kept, none is chosen, and the
// records are never merged.
type Correlation struct {
	From       Ref        `json:"from"`
	To         Ref        `json:"to"`
	Confidence Confidence `json:"confidence"`
	Ambiguous  bool       `json:"ambiguous,omitempty"`
	Evidence   []Evidence `json:"evidence"`
}

func (c Correlation) String() string { return c.From.String() + " -> " + c.To.String() }

// RefPair is two records whatever the direction, so that a link recorded
// from each end counts as one. It compares the refs themselves: no id can
// make two pairs equal.
type RefPair struct{ first, second Ref }

// PairOf returns the pair of a and b, in either order.
func PairOf(a, b Ref) RefPair {
	if cmp.Or(cmp.Compare(b.Kind, a.Kind), cmp.Compare(b.ID, a.ID)) < 0 {
		a, b = b, a
	}
	return RefPair{first: a, second: b}
}

func (p RefPair) String() string { return p.first.String() + " <-> " + p.second.String() }

// Key is the pair as text, for where a comparable struct cannot go (a
// database column). Each ref is quoted, so that no ref, whatever its id
// holds, can make two pairs share a key.
func (p RefPair) Key() string {
	return strconv.Quote(p.first.String()) + " <-> " + strconv.Quote(p.second.String())
}

// Pair returns the pair of records the correlation joins.
func (c Correlation) Pair() RefPair { return PairOf(c.From, c.To) }

// Validate reports whether the correlation is complete.
func (c Correlation) Validate() error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("correlation %s: %w", c, err)
	}
	return nil
}

func (c Correlation) validate() error {
	if err := errors.Join(c.From.Validate(), c.To.Validate()); err != nil {
		return err
	}
	if c.From == c.To {
		return errors.New("a record cannot correlate with itself")
	}
	switch c.Confidence {
	case ConfidenceIdentifier, ConfidenceHeuristic:
	default:
		return fmt.Errorf("unknown confidence %q", c.Confidence)
	}
	return validateEvidence(c.Evidence)
}
