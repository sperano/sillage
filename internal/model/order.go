package model

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// sort puts every list in its documented order. Each order ends on the
// record's identity, so no two records compare equal and the result never
// depends on the input order.
func (r *Records) sort() {
	slices.SortFunc(r.Events, compareEvents)
	slices.SortFunc(r.Runs, compareRuns)
	slices.SortFunc(r.PullRequests, func(a, b PullRequest) int {
		return cmp.Or(
			strings.Compare(a.Key.Instance, b.Key.Instance),
			strings.Compare(a.Key.Repository, b.Key.Repository),
			cmp.Compare(a.Key.Number, b.Key.Number),
		)
	})
	slices.SortFunc(r.Correlations, func(a, b Correlation) int {
		return cmp.Or(
			strings.Compare(a.From.String(), b.From.String()),
			strings.Compare(a.To.String(), b.To.String()),
		)
	})
	slices.SortFunc(r.Coverage, func(a, b Coverage) int {
		return strings.Compare(a.Key(), b.Key())
	})
}

// compareEvents orders events by Time, then identity.
func compareEvents(a, b Event) int {
	return cmp.Or(
		a.Time().Compare(b.Time()),
		strings.Compare(string(a.Source), string(b.Source)),
		strings.Compare(a.Instance, b.Instance),
		strings.Compare(a.ID, b.ID),
	)
}

// compareRuns orders runs by their first known bound (start, else end);
// runs with neither come last. Ties fall back to the key.
func compareRuns(a, b Run) int {
	return cmp.Or(
		compareKnownFirst(a.Interval.anchor, b.Interval.anchor),
		strings.Compare(a.Key.String(), b.Key.String()),
	)
}

// sortChecks orders checks by start (unknown starts last), then commit,
// name and id.
func sortChecks(checks []CheckRun) {
	slices.SortFunc(checks, func(a, b CheckRun) int {
		return cmp.Or(
			compareKnownFirst(a.Interval.Start.Time, b.Interval.Start.Time),
			strings.Compare(a.SHA, b.SHA),
			strings.Compare(a.Name, b.Name),
			cmp.Compare(a.ID, b.ID),
		)
	})
}

func sortEvidence(evidence []Evidence) {
	slices.SortFunc(evidence, func(a, b Evidence) int {
		return cmp.Or(
			strings.Compare(string(a.Source), string(b.Source)),
			strings.Compare(a.Instance, b.Instance),
			strings.Compare(a.Ref, b.Ref),
			strings.Compare(a.Note, b.Note),
		)
	})
}

func sortGaps(gaps []Gap) {
	slices.SortFunc(gaps, func(a, b Gap) int {
		return cmp.Or(a.Start.Compare(b.Start), a.End.Compare(b.End), strings.Compare(a.Reason, b.Reason))
	})
}

// anchor is the interval's first known bound: its start, else its end.
func (i Interval) anchor() (time.Time, bool) {
	if t, ok := i.Start.Time(); ok {
		return t, true
	}
	return i.End.Time()
}

// compareKnownFirst orders two possibly unknown times: known before
// unknown, known ones by time.
func compareKnownFirst(a, b func() (time.Time, bool)) int {
	at, aok := a()
	bt, bok := b()
	switch {
	case aok && bok:
		return at.Compare(bt)
	case aok:
		return -1
	case bok:
		return 1
	}
	return 0
}
