package store

import (
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// supersede drops the stored live record that the archived version e
// (row id) names in its "supersedes" detail: the same save, now with a
// version id. The live record's stories and correlations move to the
// version.
func (w *saver) supersede(id int64, e model.Event) error {
	liveID := e.Details[detailSupersedes]
	live := eventRef(e.Source, e.Instance, liveID)
	w.moved[live] = e.Ref()
	old, found, err := w.eventID(e.Source, e.Instance, liveID)
	if err != nil || !found {
		return err
	}
	if err := w.exec(`INSERT INTO root_events (root_id, event_id)
		SELECT root_id, $2 FROM root_events WHERE event_id = $1 ON CONFLICT DO NOTHING`, old, id); err != nil {
		return err
	}
	if err := w.repointCorrelations(live, e.Ref()); err != nil {
		return err
	}
	return w.exec("DELETE FROM events WHERE id = $1", old)
}

// keepLive reports whether the live record e is stored: not when a stored
// version supersedes it, nor when a newer live record of its page is
// stored. Older live records of the page are deleted.
func (w *saver) keepLive(e model.Event) (bool, error) {
	var version string
	err := w.tx.QueryRow(w.ctx, `SELECT event_id FROM events
		WHERE source = $1 AND instance = $2 AND supersedes = $3 ORDER BY id LIMIT 1`,
		string(e.Source), e.Instance, e.ID).Scan(&version)
	switch {
	case err == nil:
		w.moved[e.Ref()] = eventRef(e.Source, e.Instance, version)
		return false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}
	others, err := w.livesOf(e)
	if err != nil {
		return false, err
	}
	for _, o := range others {
		if o.event.Time().After(e.Time()) {
			w.dropped[e.Ref()] = true
			return false, nil
		}
	}
	for _, o := range others {
		if err := w.deleteEvent(o.id, o.event.Ref()); err != nil {
			return false, err
		}
	}
	return true, nil
}

// storedEvent is an event row.
type storedEvent struct {
	id    int64
	event model.Event
}

// livesOf returns the stored live records of e's page other than e.
func (w *saver) livesOf(e model.Event) ([]storedEvent, error) {
	rows, err := w.tx.Query(w.ctx, `SELECT id, body FROM events
		WHERE live_page = $1 AND NOT (source = $2 AND instance = $3 AND event_id = $4)`,
		e.Related.Page.Ref().String(), string(e.Source), e.Instance, e.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedEvent, error) {
		var s storedEvent
		var body []byte
		if err := row.Scan(&s.id, &body); err != nil {
			return s, err
		}
		return s, json.Unmarshal(body, &s.event)
	})
}
