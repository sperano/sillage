package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// The wiki source's details on page edits.
const (
	detailVersion    = "version"
	detailSupersedes = "supersedes"
	detailAuthorID   = "authorId"
	versionLive      = "live"
)

// seriesSep separates the parts of an observation's series key: the unit
// separator, which neither a ref nor a kind contains and JSON escapes.
// (PostgreSQL text cannot hold NUL.)
const seriesSep = "\x1f"

// eventRow is the columns an event is stored with besides its body.
type eventRow struct {
	lastObserved time.Time
	subject      *string
	series       *string
}

func (w *saver) saveEvents(events []model.Event) error {
	// Archived versions go before live records, so that a live record they
	// supersede is recognised whatever the batch's order; observations go
	// in read order, so that each extends the previous read's.
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b model.Event) int {
		if la, lb := isLive(a), isLive(b); la != lb {
			if la {
				return 1
			}
			return -1
		}
		return a.ObservedAt.Compare(b.ObservedAt)
	})
	for _, e := range events {
		if e.Related.Run != nil {
			if to, ok := w.runKeys[*e.Related.Run]; ok {
				e.Related.Run = &to
			}
		}
		if err := w.saveEvent(e); err != nil {
			return fmt.Errorf("event %s: %w", e.Key(), err)
		}
	}
	return nil
}

func (w *saver) saveEvent(e model.Event) error {
	var id int64
	var err error
	switch {
	case e.Kind.Observed(): // without a source time, on a task: model.Event.Validate
		id, err = w.saveObservation(e)
		if err == nil && e.LastObservedAt != nil {
			// A range already read (a story saved again): it ends there.
			err = w.exec("UPDATE events SET last_observed_at = GREATEST(last_observed_at, $2) WHERE id = $1",
				id, *e.LastObservedAt)
		}
	case isLive(e):
		id, err = w.saveLive(e)
	default:
		id, err = w.upsertEvent(e, eventRow{lastObserved: e.ObservedAt})
		if err == nil && e.Source == model.SourceWiki && e.Details[detailSupersedes] != "" {
			err = w.supersede(id, e)
		}
	}
	if err != nil {
		return err
	}
	return w.exec("INSERT INTO root_events (root_id, event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", w.root, id)
}

// upsertEvent stores e under its key and returns its row. A stored read
// newer than e's is kept. The last observed time is the column's, never
// the body's.
func (w *saver) upsertEvent(e model.Event, row eventRow) (int64, error) {
	e.LastObservedAt = nil
	body, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	var runRef, livePage *string
	if e.Related.Run != nil {
		runRef = nullable(e.Related.Run.Ref().String())
	}
	if isLive(e) {
		livePage = nullable(e.Related.Page.Ref().String())
	}
	var supersedes *string
	if e.Source == model.SourceWiki {
		supersedes = nullable(e.Details[detailSupersedes])
	}
	var id int64
	err = w.tx.QueryRow(w.ctx, `
		INSERT INTO events (source, instance, event_id, kind, at, observed_at, last_observed_at,
			observed_subject, series, run_ref, supersedes, live_page, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (source, instance, event_id) DO UPDATE SET
			kind = EXCLUDED.kind, at = EXCLUDED.at, observed_at = EXCLUDED.observed_at,
			last_observed_at = GREATEST(EXCLUDED.last_observed_at, events.last_observed_at),
			run_ref = EXCLUDED.run_ref, supersedes = EXCLUDED.supersedes, live_page = EXCLUDED.live_page,
			body = EXCLUDED.body
		WHERE EXCLUDED.observed_at >= events.observed_at
		RETURNING id`,
		string(e.Source), e.Instance, e.ID, string(e.Kind), e.Time(), e.ObservedAt, row.lastObserved,
		row.subject, row.series, runRef, supersedes, livePage, body).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id, _, err = w.eventID(e.Source, e.Instance, e.ID)
	}
	return id, err
}

// eventID returns the row of a stored event, and false when there is none.
func (w *saver) eventID(source model.Source, instance, id string) (int64, bool, error) {
	var row int64
	err := w.tx.QueryRow(w.ctx, "SELECT id FROM events WHERE source = $1 AND instance = $2 AND event_id = $3",
		string(source), instance, id).Scan(&row)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return row, err == nil, err
}

// deleteEvent deletes a stored event and the correlations naming it.
func (w *saver) deleteEvent(id int64, ref model.Ref) error {
	if err := w.exec("DELETE FROM correlations WHERE from_ref = $1 OR to_ref = $1", ref.String()); err != nil {
		return err
	}
	return w.exec("DELETE FROM events WHERE id = $1", id)
}

// eventRef is the reference of the event with key parts.
func eventRef(source model.Source, instance, id string) model.Ref {
	return model.Event{Source: source, Instance: instance, ID: id}.Ref()
}

func isLive(e model.Event) bool {
	return e.Source == model.SourceWiki && e.Related.Page != nil && e.Details[detailVersion] == versionLive
}
