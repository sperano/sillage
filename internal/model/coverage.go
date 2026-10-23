package model

import (
	"fmt"
	"slices"
	"time"
)

// Gap is a span of time for which a source's records are known to be
// missing (the collector was down, a watch expired, objects were deleted
// before they were read). Whatever happened inside it is unknown: never
// idle time, never work.
type Gap struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Reason string    `json:"reason"`
}

// Coverage says how far one source instance has been read.
type Coverage struct {
	Source   Source `json:"source"`
	Instance string `json:"instance"`
	// LastSuccessfulRead is when the source was last read without error;
	// nil if it never was. Anything after it is unknown.
	LastSuccessfulRead *time.Time `json:"lastSuccessfulRead,omitempty"`
	Gaps               []Gap      `json:"gaps,omitempty"`
	// Notes say what the reads left unknown that no time range bounds: a
	// pull request's commits whose checks were not read, a source not read
	// because nothing named what to read in it.
	Notes []string `json:"notes,omitempty"`
}

// Key returns the coverage's identity: source and instance.
func (c Coverage) Key() string { return string(c.Source) + "/" + c.Instance }

// Known reports whether the source's records at t are known: t is not
// after the last successful read and inside no gap.
func (c Coverage) Known(t time.Time) bool {
	if c.LastSuccessfulRead == nil || t.After(*c.LastSuccessfulRead) {
		return false
	}
	for _, g := range c.Gaps {
		if !t.Before(g.Start) && t.Before(g.End) {
			return false
		}
	}
	return true
}

// Validate reports whether the coverage is complete, each gap has a
// reason and a positive length, and no note is empty.
func (c Coverage) Validate() error {
	if !c.Source.valid() || c.Instance == "" {
		return fmt.Errorf("coverage %s: source and instance are required", c.Key())
	}
	for _, g := range c.Gaps {
		if g.Reason == "" || !g.End.After(g.Start) {
			return fmt.Errorf("coverage %s: a gap needs a reason and an end after its start", c.Key())
		}
	}
	if slices.Contains(c.Notes, "") {
		return fmt.Errorf("coverage %s: a note is empty", c.Key())
	}
	return nil
}

func (c *Coverage) normalize() {
	if c.LastSuccessfulRead != nil {
		t := c.LastSuccessfulRead.UTC()
		c.LastSuccessfulRead = &t
	}
	for i := range c.Gaps {
		c.Gaps[i].Start = c.Gaps[i].Start.UTC()
		c.Gaps[i].End = c.Gaps[i].End.UTC()
	}
	sortGaps(c.Gaps)
	slices.Sort(c.Notes)
}
