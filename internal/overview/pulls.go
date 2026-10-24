package overview

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// prEvents groups the events that name a pull request by its key, each in
// the story's (time) order.
func prEvents(events []model.Event) map[model.PRKey][]model.Event {
	by := make(map[model.PRKey][]model.Event)
	for _, e := range events {
		if e.Related.PR != nil {
			by[*e.Related.PR] = append(by[*e.Related.PR], e)
		}
	}
	return by
}

// pullRequest returns the pull request's periods and state, and whether
// its periods touch the window.
func pullRequest(w Window, pr model.PullRequest, events []model.Event) (PullRequest, bool) {
	out := PullRequest{Key: pr.Key, Branch: pr.Branch, Periods: periods(events), State: stateAt(w, events)}
	for _, p := range out.Periods {
		if touches(w, p) {
			return out, true
		}
	}
	return out, false
}

// periods returns the spans a pull request was open: each opening or
// reopening to the next merge or close. A merge or close with no opening
// before it ends a period whose start is unknown; an opening with neither
// after it is open at the last read.
func periods(events []model.Event) []model.Interval {
	out := []model.Interval{}
	var current *model.Interval
	for _, e := range events {
		switch e.Kind {
		case model.KindPROpened, model.KindPRReopened:
			if current == nil {
				current = &model.Interval{Start: model.KnownAt(e.Time()), End: model.OpenEnd()}
			}
		case model.KindPRMerged, model.KindPRClosed:
			if current == nil {
				current = &model.Interval{Start: model.UnknownBound()}
			}
			current.End = model.KnownAt(e.Time())
			out = append(out, *current)
			current = nil
		}
	}
	if current != nil {
		out = append(out, *current)
	}
	return out
}

// stateAt returns the pull request's state as its last opening, reopening,
// merge or close before the window's end left it.
func stateAt(w Window, events []model.Event) PRState {
	state := PRUnknown
	for _, e := range events {
		if !e.Time().Before(w.To) {
			break
		}
		switch e.Kind {
		case model.KindPROpened, model.KindPRReopened:
			state = PROpen
		case model.KindPRMerged:
			state = PRMerged
		case model.KindPRClosed:
			state = PRClosed
		}
	}
	return state
}

// reviewWaits returns the review waits of one pull request's events that
// touch the window, as the single-task timeline draws them: from each
// request to the requested reviewer's review or the request's removal.
// With no answer, a wait is open at the last read, unless the pull request
// was merged, or closed and not reopened, since: then its end is unknown,
// and Closed says when that happened.
func reviewWaits(w Window, pr model.PRKey, events []model.Event) []ReviewWait {
	var out []ReviewWait
	for i, request := range events {
		if request.Kind != model.KindReviewRequested {
			continue
		}
		interval, closed := waitInterval(request, events[i+1:])
		wait := ReviewWait{
			PR:       pr,
			Reviewer: request.Details["reviewer"],
			Team:     request.Details["team"],
			Request:  request.Key(),
			Interval: interval,
			Closed:   closed,
		}
		if touches(w, waitExtent(wait)) {
			out = append(out, wait)
		}
	}
	return out
}

// waitExtent is the span a review wait is known to show: its interval, or,
// when the pull request closed unanswered, the interval ended at the close,
// until which the request was still waiting. The wait keeps its unknown
// end as served: nothing says it was answered then.
func waitExtent(rw ReviewWait) model.Interval {
	if rw.Closed == nil {
		return rw.Interval
	}
	return model.Interval{Start: rw.Interval.Start, End: model.KnownAt(*rw.Closed)}
}

// waitInterval returns the wait from request to its answer among the
// events after it and, when the pull request closed before any answer,
// the time it did.
func waitInterval(request model.Event, after []model.Event) (model.Interval, *time.Time) {
	start := model.KnownAt(request.Time())
	for _, e := range after {
		if answers(request, e) {
			return model.Interval{Start: start, End: model.KnownAt(e.Time())}, nil
		}
	}
	if closed, ok := closedAt(after); ok {
		return model.Interval{Start: start, End: model.UnknownBound()}, &closed
	}
	return model.Interval{Start: start, End: model.OpenEnd()}, nil
}

// answers reports whether e answers request: the requested reviewer's own
// review, or the removal of the same request. A team's request ends only
// at its removal.
func answers(request, e model.Event) bool {
	switch e.Kind {
	case model.KindReviewRequestRemoved:
		return requestee(e) == requestee(request)
	case model.KindReviewSubmitted:
		reviewer := request.Details["reviewer"]
		return reviewer != "" && e.Actor.Login == reviewer
	}
	return false
}

// requestee is who a request or removal names: a reviewer, or a team.
func requestee(e model.Event) string {
	if r := e.Details["reviewer"]; r != "" {
		return r
	}
	if t := e.Details["team"]; t != "" {
		return "team " + t
	}
	return ""
}

// closedAt returns when the events left the pull request merged, or
// closed and not reopened: its merge, or its last close.
func closedAt(events []model.Event) (time.Time, bool) {
	var when time.Time
	closed := false
	for _, e := range events {
		switch e.Kind {
		case model.KindPRMerged:
			return e.Time(), true
		case model.KindPRClosed:
			when, closed = e.Time(), true
		case model.KindPRReopened:
			when, closed = time.Time{}, false
		}
	}
	return when, closed
}
