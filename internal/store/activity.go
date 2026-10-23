package store

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// touchEvent notes that a save stored event row id, or gave it another
// record's stories.
func (w *saver) touchEvent(id int64) { w.events = append(w.events, id) }

// touchRun notes that a save stored r as run row id.
func (w *saver) touchRun(id int64, r model.Run) {
	if at, ok := runActivity(r); ok {
		if prev, seen := w.runs[id]; !seen || at.After(prev) {
			w.runs[id] = at
		}
	}
}

// touchRoots raises the last activity of every root holding a record the
// batch stored to that record's latest time. Records are shared, so a save
// can change other roots' stories too: a run's end read, a live edit's
// re-render taking the older one's place. Retention counts from it.
func (w *saver) touchRoots() error {
	// An event's time (its last observation, for a state seen again).
	if err := w.exec(`UPDATE roots r SET last_activity = GREATEST(r.last_activity, a.at)
		FROM (SELECT re.root_id, max(CASE WHEN e.observed_subject IS NULL THEN e.at
				ELSE GREATEST(e.at, e.last_observed_at) END) AS at
			FROM root_events re JOIN events e ON e.id = re.event_id
			WHERE re.event_id = ANY($1) GROUP BY re.root_id) a
		WHERE r.id = a.root_id`, w.events); err != nil {
		return err
	}
	ids := make([]int64, 0, len(w.runs))
	ats := make([]time.Time, 0, len(w.runs))
	for id, at := range w.runs {
		ids, ats = append(ids, id), append(ats, at)
	}
	return w.exec(`UPDATE roots r SET last_activity = GREATEST(r.last_activity, a.at)
		FROM (SELECT rr.root_id, max(x.at) AS at
			FROM root_runs rr JOIN unnest($1::bigint[], $2::timestamptz[]) AS x(run_id, at) USING (run_id)
			GROUP BY rr.root_id) a
		WHERE r.id = a.root_id`, ids, ats)
}

// runActivity returns the latest time the run shows anything happening,
// its known bounds and creation, and false when it shows none.
func runActivity(r model.Run) (time.Time, bool) {
	var last time.Time
	see := func(t time.Time) {
		if t.After(last) {
			last = t
		}
	}
	if r.Created != nil {
		see(*r.Created)
	}
	if t, ok := r.Interval.Start.Time(); ok {
		see(t)
	}
	if t, ok := r.Interval.End.Time(); ok {
		see(t)
	}
	return last, !last.IsZero()
}
