package store

import (
	"cmp"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// mergeRun returns overlay with what it does not know taken from base: two
// reads of one attempt, overlay the more authoritative (the later read, or
// the one keyed by UID over a pod-keyed placeholder). A read after the
// Task's cleanup knows only what its TaskRecord copied, so the pod, job,
// creation time and message read earlier survive it. The key is overlay's.
func mergeRun(base, overlay model.Run) model.Run {
	out := overlay
	out.TaskName = cmp.Or(overlay.TaskName, base.TaskName)
	out.Pod = cmp.Or(overlay.Pod, base.Pod)
	out.Job = cmp.Or(overlay.Job, base.Job)
	out.Agent = cmp.Or(overlay.Agent, base.Agent)
	out.Project = cmp.Or(overlay.Project, base.Project)
	out.Message = cmp.Or(overlay.Message, base.Message)
	out.Model = cmp.Or(overlay.Model, base.Model)
	if overlay.Role == model.RoleUnknown {
		out.Role = base.Role
	}
	if overlay.Created == nil {
		out.Created = base.Created
	}
	if overlay.Usage == nil {
		out.Usage = base.Usage
	}
	// Interval and outcome go together: an open end means waiting or
	// running. An overlay that knows neither (an announcement) keeps
	// base's.
	if overlay.Outcome == model.OutcomeUnknown && base.Outcome != model.OutcomeUnknown {
		out.Interval, out.Outcome = base.Interval, base.Outcome
	}
	out.Evidence = mergeEvidence(base.Evidence, overlay.Evidence)
	return out
}

// mergeEvidence returns overlay's evidence and base's for the objects
// overlay does not cite (by source, instance and ref).
func mergeEvidence(base, overlay []model.Evidence) []model.Evidence {
	type ref struct {
		source   model.Source
		instance string
		ref      string
	}
	out := slices.Clone(overlay)
	cited := make(map[ref]bool, len(overlay))
	for _, ev := range overlay {
		cited[ref{ev.Source, ev.Instance, ev.Ref}] = true
	}
	for _, ev := range base {
		if !cited[ref{ev.Source, ev.Instance, ev.Ref}] {
			out = append(out, ev)
		}
	}
	return out
}

// mergeCoverage combines two reads' coverage of one source instance for
// one root. What either read knew stays known: the store holds its
// records. A time is a gap only where the later read has a gap and the
// earlier one either had a gap too or had not reached it yet; the later
// read's reasons, notes and other fields are kept. A coverage that was
// never read successfully adds nothing.
func mergeCoverage(stored, incoming model.Coverage) model.Coverage {
	switch {
	case incoming.LastSuccessfulRead == nil && stored.LastSuccessfulRead != nil:
		return stored
	case stored.LastSuccessfulRead == nil:
		return incoming
	}
	older, newer := stored, incoming
	if incoming.LastSuccessfulRead.Before(*stored.LastSuccessfulRead) {
		older, newer = incoming, stored
	}
	// The older read did not know (L_older, L_newer]: as a half-open span
	// of nanosecond times, [L_older+1ns, L_newer+1ns).
	unknown := append(spans(older.Gaps), span{
		start: older.LastSuccessfulRead.Add(time.Nanosecond),
		end:   newer.LastSuccessfulRead.Add(time.Nanosecond),
	})
	out := newer
	out.Gaps = intersectGaps(newer.Gaps, unionSpans(unknown))
	return out
}

// span is a half-open time range [start, end), as a Gap is.
type span struct{ start, end time.Time }

func spans(gaps []model.Gap) []span {
	out := make([]span, 0, len(gaps)+1)
	for _, g := range gaps {
		out = append(out, span{g.Start, g.End})
	}
	return out
}

// unionSpans returns the union of ss as sorted, disjoint spans.
func unionSpans(ss []span) []span {
	ss = slices.DeleteFunc(slices.Clone(ss), func(s span) bool { return !s.end.After(s.start) })
	slices.SortFunc(ss, func(a, b span) int { return a.start.Compare(b.start) })
	var out []span
	for _, s := range ss {
		if n := len(out); n > 0 && !s.start.After(out[n-1].end) {
			if s.end.After(out[n-1].end) {
				out[n-1].end = s.end
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// intersectGaps returns the parts of gaps inside the spans, each with its
// gap's reason; nil when there are none.
func intersectGaps(gaps []model.Gap, within []span) []model.Gap {
	var out []model.Gap
	for _, g := range gaps {
		for _, s := range within {
			start, end := latest(g.Start, s.start), earliest(g.End, s.end)
			if end.After(start) {
				out = append(out, model.Gap{Start: start, End: end, Reason: g.Reason})
			}
		}
	}
	return out
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// activity returns the latest time the records show anything happening,
// and false when they show no time: an event's time (its last
// observation, for a state seen again), a run's known bounds and
// creation. Retention counts from it.
func activity(recs model.Records) (time.Time, bool) {
	var last time.Time
	see := func(t time.Time) {
		if t.After(last) {
			last = t
		}
	}
	for _, e := range recs.Events {
		see(e.Time())
		if e.LastObservedAt != nil {
			see(*e.LastObservedAt)
		}
	}
	for _, r := range recs.Runs {
		if r.Created != nil {
			see(*r.Created)
		}
		if t, ok := r.Interval.Start.Time(); ok {
			see(t)
		}
		if t, ok := r.Interval.End.Time(); ok {
			see(t)
		}
	}
	return last, !last.IsZero()
}
