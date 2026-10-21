package model

import (
	"errors"
	"fmt"
	"time"
)

// BoundState says what is known about one end of an interval.
type BoundState string

// The states of an interval's end.
const (
	// BoundKnown: a source recorded the time.
	BoundKnown BoundState = "known"
	// BoundUnknown: no source recorded it. Nothing may be inferred, not
	// even from a neighbouring event.
	BoundUnknown BoundState = "unknown"
	// BoundOpen: the interval had not ended at the last successful read.
	// Only an end can be open.
	BoundOpen BoundState = "open"
)

// Bound is one end of an interval. At is set exactly when State is
// BoundKnown.
type Bound struct {
	State BoundState `json:"state"`
	At    *time.Time `json:"at,omitempty"`
}

// KnownAt returns a known bound at t.
func KnownAt(t time.Time) Bound { return Bound{State: BoundKnown, At: &t} }

// UnknownBound returns an unknown bound.
func UnknownBound() Bound { return Bound{State: BoundUnknown} }

// OpenEnd returns an open end.
func OpenEnd() Bound { return Bound{State: BoundOpen} }

// Time returns the bound's time and whether it is known.
func (b Bound) Time() (time.Time, bool) {
	if b.State != BoundKnown || b.At == nil {
		return time.Time{}, false
	}
	return *b.At, true
}

func (b Bound) validate(end bool) error {
	switch b.State {
	case BoundKnown:
		if b.At == nil {
			return errors.New("a known bound needs a time")
		}
		return nil
	case BoundUnknown:
	case BoundOpen:
		if !end {
			return errors.New("only an end can be open")
		}
	default:
		return fmt.Errorf("unknown bound state %q", b.State)
	}
	if b.At != nil {
		return fmt.Errorf("a %s bound has no time", b.State)
	}
	return nil
}

func (b *Bound) normalize() {
	if b.At != nil {
		t := b.At.UTC()
		b.At = &t
	}
}

// Interval is a span of time whose ends are each known, unknown or (the
// end only) open.
type Interval struct {
	Start Bound `json:"start"`
	End   Bound `json:"end"`
}

// Duration returns the interval's length, and false unless both ends are
// known: an interval with an unknown or open end has no duration, and a
// caller must not substitute one.
func (i Interval) Duration() (time.Duration, bool) {
	start, ok := i.Start.Time()
	if !ok {
		return 0, false
	}
	end, ok := i.End.Time()
	if !ok {
		return 0, false
	}
	return end.Sub(start), true
}

// Validate reports whether both ends are consistent and, when both are
// known, the end is not before the start.
func (i Interval) Validate() error {
	if err := i.Start.validate(false); err != nil {
		return fmt.Errorf("interval start: %w", err)
	}
	if err := i.End.validate(true); err != nil {
		return fmt.Errorf("interval end: %w", err)
	}
	if d, ok := i.Duration(); ok && d < 0 {
		return errors.New("interval ends before it starts")
	}
	return nil
}

// anchor is the interval's first known bound: its start, else its end.
func (i Interval) anchor() (time.Time, bool) {
	if t, ok := i.Start.Time(); ok {
		return t, true
	}
	return i.End.Time()
}

func (i *Interval) normalize() {
	i.Start.normalize()
	i.End.normalize()
}
