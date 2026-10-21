package model

import (
	"testing"
	"time"
)

func TestCoverageKnown(t *testing.T) {
	gap := Gap{Start: at(20), End: at(30), Reason: "collector down"}
	withGap := testCoverage()
	withGap.Gaps = []Gap{gap, {Start: at(40), End: at(45), Reason: "watch expired"}}
	neverRead := testCoverage()
	neverRead.LastSuccessfulRead = nil
	neverReadWithGap := withGap
	neverReadWithGap.LastSuccessfulRead = nil

	tests := []struct {
		name string
		cov  Coverage
		at   time.Time
		want bool
	}{
		{"before the last read", testCoverage(), at(10), true},
		{"at the last read", testCoverage(), at(60), true},
		{"after the last read", testCoverage(), at(60).Add(time.Nanosecond), false},
		{"long after the last read", testCoverage(), at(600), false},
		{"never read", neverRead, at(10), false},
		{"never read, before everything", neverRead, time.Time{}, false},
		{"never read, with gaps", neverReadWithGap, at(10), false},
		{"before a gap", withGap, at(19), true},
		{"just before a gap", withGap, gap.Start.Add(-time.Nanosecond), true},
		{"at a gap's start (inclusive)", withGap, gap.Start, false},
		{"inside a gap", withGap, at(25), false},
		{"just before a gap's end", withGap, gap.End.Add(-time.Nanosecond), false},
		{"at a gap's end (exclusive)", withGap, gap.End, true},
		{"between gaps", withGap, at(35), true},
		{"inside the second gap", withGap, at(42), false},
		{"after the gaps, before the last read", withGap, at(50), true},
		{"a gap time in another zone", withGap, at(25).In(testEST), false},
		{"after the last read, in another zone", withGap, at(61).In(testEST), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cov.Known(tt.at); got != tt.want {
				t.Errorf("Known(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestCoverageValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Coverage)
		want   string
	}{
		{"valid", func(*Coverage) {}, ""},
		{"never read", func(c *Coverage) { c.LastSuccessfulRead = nil }, ""},
		{"unknown source", func(c *Coverage) { c.Source = "gitlab" }, "source and instance"},
		{"missing instance", func(c *Coverage) { c.Instance = "" }, "source and instance"},
		{
			"valid gap",
			func(c *Coverage) { c.Gaps = []Gap{{Start: at(1), End: at(2), Reason: "collector down"}} },
			"",
		},
		{
			"gap without reason",
			func(c *Coverage) { c.Gaps = []Gap{{Start: at(1), End: at(2)}} },
			"a gap needs a reason",
		},
		{
			"gap ending at its start",
			func(c *Coverage) { c.Gaps = []Gap{{Start: at(1), End: at(1), Reason: "r"}} },
			"end after its start",
		},
		{
			"gap ending before its start",
			func(c *Coverage) { c.Gaps = []Gap{{Start: at(2), End: at(1), Reason: "r"}} },
			"end after its start",
		},
		{
			"second gap invalid",
			func(c *Coverage) {
				c.Gaps = []Gap{{Start: at(1), End: at(2), Reason: "r"}, {Start: at(5), End: at(3), Reason: "r"}}
			},
			"end after its start",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testCoverage()
			tt.mutate(&c)
			checkErr(t, c.Validate(), tt.want)
		})
	}
}

func TestCoverageKey(t *testing.T) {
	const want = "vikunja/vikunja.example.com"
	if got := testCoverage().Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestCoverageValidateNamesCoverage(t *testing.T) {
	c := testCoverage()
	c.Gaps = []Gap{{Start: at(1), End: at(2)}}
	checkErr(t, c.Validate(), "coverage vikunja/vikunja.example.com:")
}

func TestCoverageNormalize(t *testing.T) {
	c := testCoverage()
	c.LastSuccessfulRead = timePtr(atEST(60))
	c.Gaps = []Gap{
		{Start: atEST(40), End: atEST(45), Reason: "b"},
		{Start: atEST(20), End: atEST(30), Reason: "a"},
	}
	c.normalize()
	if got := c.LastSuccessfulRead.Location(); got != time.UTC {
		t.Errorf("LastSuccessfulRead location = %v, want UTC", got)
	}
	if c.Gaps[0].Reason != "a" || c.Gaps[1].Reason != "b" {
		t.Errorf("gap reasons = %q, %q, want a, b", c.Gaps[0].Reason, c.Gaps[1].Reason)
	}
	for _, g := range c.Gaps {
		if g.Start.Location() != time.UTC || g.End.Location() != time.UTC {
			t.Errorf("gap %q is not in UTC: %v - %v", g.Reason, g.Start, g.End)
		}
	}

	c.LastSuccessfulRead = nil
	c.normalize()
	if c.LastSuccessfulRead != nil {
		t.Errorf("normalize set a nil LastSuccessfulRead to %v", c.LastSuccessfulRead)
	}
}
