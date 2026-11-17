package reconcile

import (
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// settled reports whether s, read at at, is settled: its task done and
// not reopened, every pull request closed or merged, and every run
// finished. Nothing in it then changes on GitHub unless a delivery, a
// task change or a new run says so, so the backstop leaves it unread.
func settled(s model.Story, at time.Time) bool {
	c, ok := s.CompletionAt(at)
	if !ok || c.State != model.CompletionDone {
		return false
	}
	for _, r := range s.Runs {
		if !finished(r) {
			return false
		}
	}
	for _, p := range s.PullRequests {
		if prOpen(s.Events, p.Key) {
			return false
		}
	}
	return true
}

// finished reports whether r ended: its end is known, or how it ended.
func finished(r model.Run) bool {
	if _, ok := r.Interval.End.Time(); ok {
		return true
	}
	return r.Outcome == model.OutcomeSucceeded || r.Outcome == model.OutcomeFailed
}

// prOpen reports whether pull request key is open as its latest opening,
// closing, merge or reopening says; open without any of them, since what
// it is is unknown.
func prOpen(events []model.Event, key model.PRKey) bool {
	var states []model.Event
	for _, e := range events {
		if e.Related.PR == nil || *e.Related.PR != key {
			continue
		}
		switch e.Kind {
		case model.KindPROpened, model.KindPRReopened, model.KindPRClosed, model.KindPRMerged:
			states = append(states, e)
		}
	}
	if len(states) == 0 {
		return true
	}
	last := slices.MaxFunc(states, func(a, b model.Event) int { return a.Time().Compare(b.Time()) })
	return last.Kind == model.KindPROpened || last.Kind == model.KindPRReopened
}
