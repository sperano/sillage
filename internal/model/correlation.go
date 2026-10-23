package model

import (
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

// PairKey identifies the pair of records whatever the direction, so that a
// link recorded from each end counts as one. Each ref is quoted, so that
// no ref, whatever its id holds, can make two pairs share a key.
func (c Correlation) PairKey() string {
	from, to := strconv.Quote(c.From.String()), strconv.Quote(c.To.String())
	if to < from {
		from, to = to, from
	}
	return from + " <-> " + to
}

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
