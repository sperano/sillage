package model

import (
	"slices"
	"testing"
	"time"
)

// knownAt and unknownTime build the time getters compareKnownFirst takes.
func knownAt(t time.Time) func() (time.Time, bool) {
	return func() (time.Time, bool) { return t, true }
}

func unknownTime() (time.Time, bool) { return time.Time{}, false }

func TestCompareKnownFirst(t *testing.T) {
	tests := []struct {
		name string
		a, b func() (time.Time, bool)
		want int
	}{
		{"earlier known first", knownAt(at(0)), knownAt(at(1)), -1},
		{"later known last", knownAt(at(1)), knownAt(at(0)), 1},
		{"equal known", knownAt(at(1)), knownAt(at(1)), 0},
		{"same instant in another zone", knownAt(at(1)), knownAt(atEST(1)), 0},
		{"known before unknown", knownAt(at(1000)), unknownTime, -1},
		{"unknown after known", unknownTime, knownAt(at(-1000)), 1},
		{"both unknown", unknownTime, unknownTime, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareKnownFirst(tt.a, tt.b); got != tt.want {
				t.Errorf("compareKnownFirst() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIntervalAnchor(t *testing.T) {
	tests := []struct {
		name   string
		iv     Interval
		want   time.Time
		wantOK bool
	}{
		{"start when known", Interval{Start: KnownAt(at(1)), End: KnownAt(at(2))}, at(1), true},
		{"start over an open end", Interval{Start: KnownAt(at(1)), End: Open()}, at(1), true},
		{"end when the start is unknown", Interval{Start: Unknown(), End: KnownAt(at(2))}, at(2), true},
		{"nothing known", Interval{Start: Unknown(), End: Unknown()}, time.Time{}, false},
		{"unknown start, open end", Interval{Start: Unknown(), End: Open()}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.iv.anchor()
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Errorf("anchor() = %v, %v, want %v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCompareRuns(t *testing.T) {
	startAt := func(uid string, minutes int) Run {
		r := testRun(uid)
		r.Interval = Interval{Start: KnownAt(at(minutes)), End: Unknown()}
		return r
	}
	endOnly := func(uid string, minutes int) Run {
		r := testRun(uid)
		r.Interval = Interval{Start: Unknown(), End: KnownAt(at(minutes))}
		return r
	}
	neither := func(uid string) Run {
		r := testRun(uid)
		r.Interval = Interval{Start: Unknown(), End: Unknown()}
		return r
	}
	tests := []struct {
		name string
		a, b Run
		want int
	}{
		{"earlier start first", startAt("a", 1), startAt("b", 2), -1},
		{"later start last", startAt("a", 2), startAt("b", 1), 1},
		{"known start before nothing known", startAt("z", 99), neither("a"), -1},
		{"nothing known after a known start", neither("a"), startAt("z", 99), 1},
		{"known end before nothing known", endOnly("z", 99), neither("a"), -1},
		{"end stands in for a missing start", endOnly("a", 5), startAt("b", 3), 1},
		{"equal anchors fall back to the key", startAt("a", 1), startAt("b", 1), -1},
		{"nothing known on both falls back to the key", neither("b"), neither("a"), 1},
		{"identical", startAt("a", 1), startAt("a", 1), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareRuns(tt.a, tt.b); got != tt.want {
				t.Errorf("compareRuns() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRecordsSortPutsUnknownRunsLast(t *testing.T) {
	unknownFirstKey := testRun("a-unknown")
	unknownFirstKey.Interval = Interval{Start: Unknown(), End: Unknown()}
	unknownFirstKey.Outcome = OutcomeUnknown
	late, early := testRun("z-late"), testRun("m-early")
	late.Interval = Interval{Start: KnownAt(at(50)), End: Unknown()}
	early.Interval = Interval{Start: KnownAt(at(5)), End: Unknown()}
	recs := Records{Runs: []Run{unknownFirstKey, late, early}}
	recs.sort()

	var uids []string
	for _, r := range recs.Runs {
		uids = append(uids, r.Key.UID)
	}
	if want := []string{"m-early", "z-late", "a-unknown"}; !slices.Equal(uids, want) {
		t.Errorf("run order = %v, want %v", uids, want)
	}
}

func TestSortChecks(t *testing.T) {
	check := func(id int64, sha, name string, start Bound) CheckRun {
		c := testCheck(id, sha)
		c.Name = name
		c.Interval = Interval{Start: start, End: Unknown()}
		return c
	}
	checks := []CheckRun{
		check(1, "aaa", "build", Unknown()),
		check(2, "aaa", "lint", KnownAt(at(5))),
		check(4, "bbb", "build", KnownAt(at(5))),
		check(3, "aaa", "build", KnownAt(at(5))),
		check(6, "aaa", "build", Unknown()),
		check(5, "aaa", "build", KnownAt(at(1))),
	}
	sortChecks(checks)

	var ids []int64
	for _, c := range checks {
		ids = append(ids, c.ID)
	}
	// Known starts by time, then commit, name and id; unknown starts last.
	if want := []int64{5, 3, 2, 4, 1, 6}; !slices.Equal(ids, want) {
		t.Errorf("check order = %v, want %v", ids, want)
	}
}

func TestCompareEvents(t *testing.T) {
	event := func(source Source, instance, id string, minutes int) Event {
		e := testEvent(id)
		e.Source, e.Instance = source, instance
		e.SourceTime = timePtr(at(minutes))
		return e
	}
	polled := testEvent("polled")
	polled.SourceTime, polled.ObservedAt = nil, at(7)

	tests := []struct {
		name string
		a, b Event
		want int
	}{
		{"earlier first", event(SourceWiki, "z", "z", 1), event(SourceGitHub, "a", "a", 2), -1},
		{"same time, source", event(SourceGitHub, "z", "z", 1), event(SourceVikunja, "a", "a", 1), -1},
		{"same time and source, instance", event(SourceGitHub, "a", "z", 1), event(SourceGitHub, "b", "a", 1), -1},
		{"same time, source and instance, id", event(SourceGitHub, "a", "b", 1), event(SourceGitHub, "a", "a", 1), 1},
		{"identical", event(SourceGitHub, "a", "a", 1), event(SourceGitHub, "a", "a", 1), 0},
		{"observed time stands in", polled, event(SourceVikunja, testVikunjaHost, "x", 8), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareEvents(tt.a, tt.b); got != tt.want {
				t.Errorf("compareEvents() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSortEvidence(t *testing.T) {
	ev := func(source Source, instance, ref, note string) Evidence {
		return Evidence{Source: source, Instance: instance, Ref: ref, Note: note}
	}
	got := []Evidence{
		ev(SourceWiki, "a", "a", "a"),
		ev(SourceKelos, "b", "a", "a"),
		ev(SourceKelos, "a", "b", "a"),
		ev(SourceKelos, "a", "a", "b"),
		ev(SourceKelos, "a", "a", "a"),
	}
	sortEvidence(got)
	want := []Evidence{
		ev(SourceKelos, "a", "a", "a"),
		ev(SourceKelos, "a", "a", "b"),
		ev(SourceKelos, "a", "b", "a"),
		ev(SourceKelos, "b", "a", "a"),
		ev(SourceWiki, "a", "a", "a"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("evidence order = %+v, want %+v", got, want)
	}
}

func TestSortGaps(t *testing.T) {
	got := []Gap{
		{Start: at(5), End: at(9), Reason: "b"},
		{Start: at(1), End: at(9), Reason: "z"},
		{Start: at(1), End: at(3), Reason: "z"},
		{Start: at(1), End: at(3), Reason: "a"},
	}
	sortGaps(got)
	want := []Gap{
		{Start: at(1), End: at(3), Reason: "a"},
		{Start: at(1), End: at(3), Reason: "z"},
		{Start: at(1), End: at(9), Reason: "z"},
		{Start: at(5), End: at(9), Reason: "b"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("gap order = %+v, want %+v", got, want)
	}
}
