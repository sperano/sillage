package overview

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// touches reports whether the interval shows anything in the window: a
// known bound inside it, or a start before it and an end after it or still
// open. An interval with an unknown end touches the window only through a
// known time inside it: nothing says it went on.
func touches(w Window, i model.Interval) bool {
	start, hasStart := i.Start.Time()
	end, hasEnd := i.End.Time()
	if (hasStart && w.Contains(start)) || (hasEnd && w.Contains(end)) {
		return true
	}
	if !hasStart || !start.Before(w.From) {
		return false
	}
	return i.End.State == model.BoundOpen || (hasEnd && !end.Before(w.To))
}

// latestIn returns the latest time in the window at which the interval
// shows anything: its end when inside, the window's end when it runs past
// it or is still open, else the known time inside it.
func latestIn(w Window, i model.Interval) (time.Time, bool) {
	if !touches(w, i) {
		return time.Time{}, false
	}
	if end, ok := i.End.Time(); ok {
		if w.Contains(end) {
			return end, true
		}
		if !end.Before(w.To) {
			return w.To, true
		}
	}
	if i.End.State == model.BoundOpen {
		return w.To, true
	}
	start, _ := i.Start.Time()
	return start, true
}

// runExtent is the span a run shows: from its creation, when recorded (it
// may wait before it starts), else its start, to its end.
func runExtent(r model.Run) model.Interval {
	if r.Created != nil {
		return model.Interval{Start: model.KnownAt(*r.Created), End: r.Interval.End}
	}
	return r.Interval
}

// runTouches reports whether the run shows anything in the window: its
// extent, or its own interval, whose known start in the window an extent
// from an earlier creation to an unknown end does not show.
func runTouches(w Window, r model.Run) bool {
	return touches(w, runExtent(r)) || touches(w, r.Interval)
}

// runAnchor is the first recorded time of a run: its creation, start or
// end.
func runAnchor(r model.Run) (time.Time, bool) {
	ext := runExtent(r)
	if t, ok := ext.Start.Time(); ok {
		return t, true
	}
	return ext.End.Time()
}
