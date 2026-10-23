package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// saveObservation stores a state seen by polling once per unchanged value:
// a value the task's previous read also saw extends that record's last
// observed time. The task's reads are known only from the observations
// they stored, so a read that saw no bucket, label or assignee at all
// leaves no trace, and a value seen on both sides of it stays one record.
func (w *saver) saveObservation(e model.Event) (int64, error) {
	if id, found, err := w.eventID(e.Source, e.Instance, e.ID); err != nil || found {
		return id, err
	}
	subject, series, err := observationSeries(e)
	if err != nil {
		return 0, err
	}
	at := e.ObservedAt
	// Within a stored record's range: the same read saved again, or an
	// older read arriving late.
	s, found, err := w.seriesRow(`SELECT id, source, instance, event_id FROM events
		WHERE series = $1 AND observed_at <= $2 AND last_observed_at >= $2 LIMIT 1`, series, at)
	if err == nil && !found {
		s, found, err = w.continueSeries(subject, series, at)
	}
	if err != nil {
		return 0, err
	}
	if found {
		w.moved[e.Ref()] = s.ref
		return s.id, nil
	}
	return w.upsertEvent(e, eventRow{lastObserved: at, subject: &subject, series: &series})
}

// continueSeries extends to at the series' record that the task's
// previous read, before at, saw.
func (w *saver) continueSeries(subject, series string, at time.Time) (seriesRow, bool, error) {
	var prev *time.Time
	if err := w.tx.QueryRow(w.ctx, `SELECT max(last_observed_at) FROM events
		WHERE observed_subject = $1 AND last_observed_at < $2`, subject, at).Scan(&prev); err != nil || prev == nil {
		return seriesRow{}, false, err
	}
	s, found, err := w.seriesRow(`SELECT id, source, instance, event_id FROM events
		WHERE series = $1 AND last_observed_at = $2 LIMIT 1`, series, *prev)
	if err != nil || !found {
		return s, false, err
	}
	return s, true, w.exec("UPDATE events SET last_observed_at = $2 WHERE id = $1", s.id, at)
}

// seriesRow is an observation stored under another id.
type seriesRow struct {
	id  int64
	ref model.Ref
}

func (w *saver) seriesRow(sql string, args ...any) (seriesRow, bool, error) {
	var s seriesRow
	var source, instance, id string
	err := w.tx.QueryRow(w.ctx, sql, args...).Scan(&s.id, &source, &instance, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	s.ref = eventRef(model.Source(source), instance, id)
	return s, true, nil
}

// observationSeries returns the task an observation is on and the key of
// its series: that task, the kind and the observed value (the details).
func observationSeries(e model.Event) (subject, series string, err error) {
	value, err := json.Marshal(e.Details) // map keys marshal sorted
	if err != nil {
		return "", "", err
	}
	subject = e.Related.Task.Ref().String()
	return subject, subject + seriesSep + string(e.Kind) + seriesSep + string(value), nil
}
