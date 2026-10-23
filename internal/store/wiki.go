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
	if err := w.moveEventStories(old, id); err != nil {
		return err
	}
	if err := w.repointCorrelations(live, e.Ref()); err != nil {
		return err
	}
	return w.exec("DELETE FROM events WHERE id = $1", old)
}

// saveLive stores the live record e and returns the row the root's story
// holds for it: the stored version that supersedes it, else the page's
// newer stored live record, else its own row, which then replaces the
// page's older live records.
func (w *saver) saveLive(e model.Event) (int64, error) {
	var version int64
	var versionID string
	err := w.tx.QueryRow(w.ctx, `SELECT id, event_id FROM events
		WHERE source = $1 AND instance = $2 AND supersedes = $3 ORDER BY id LIMIT 1`,
		string(e.Source), e.Instance, e.ID).Scan(&version, &versionID)
	switch {
	case err == nil:
		w.moved[e.Ref()] = eventRef(e.Source, e.Instance, versionID)
		return version, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, err
	}
	others, err := w.livesOf(e)
	if err != nil {
		return 0, err
	}
	for _, o := range others {
		if o.event.Time().After(e.Time()) {
			w.replacedBy(e, o.event)
			return o.id, nil
		}
	}
	id, err := w.upsertEvent(e, eventRow{lastObserved: e.ObservedAt})
	if err != nil {
		return 0, err
	}
	for _, o := range others {
		if err := w.replaceLive(o, id, e); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// sameEdit reports whether two live records of a page may be one edit,
// re-rendered, or the same account's later save: then the newer keeps the
// older's correlations. Otherwise they are dropped rather than given to
// someone else's edit.
func sameEdit(older, newer model.Event) bool { return older.Actor == newer.Actor }

// replacedBy records that the batch's live record e is stored as newer.
func (w *saver) replacedBy(e, newer model.Event) {
	if sameEdit(e, newer) {
		w.moved[e.Ref()] = newer.Ref()
		return
	}
	w.dropped[e.Ref()] = true
}

// replaceLive deletes the older live record old, which e (row id)
// replaces, after giving e its stories and, for the same edit, its
// correlations.
func (w *saver) replaceLive(old storedEvent, id int64, e model.Event) error {
	if err := w.moveEventStories(old.id, id); err != nil {
		return err
	}
	if sameEdit(old.event, e) {
		if err := w.repointCorrelations(old.event.Ref(), e.Ref()); err != nil {
			return err
		}
	}
	return w.deleteEvent(old.id, old.event.Ref())
}

// moveEventStories adds event row to every story holding row from.
func (w *saver) moveEventStories(from, to int64) error {
	return w.exec(`INSERT INTO root_events (root_id, event_id)
		SELECT root_id, $2 FROM root_events WHERE event_id = $1 ON CONFLICT DO NOTHING`, from, to)
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
