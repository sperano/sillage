package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// MaxActivityRoots caps the roots one Activity call reads.
const MaxActivityRoots = 1000

// ErrTooManyRoots is returned when more roots than a query's limit may be
// active in its window.
var ErrTooManyRoots = errors.New("store: too many roots in the window")

// ActivityQuery selects the roots that may have been active in a window.
type ActivityQuery struct {
	// From and To bound the window: from inclusive, to exclusive.
	From, To time.Time
	// Project, when set, keeps the roots holding a run with that project
	// label.
	Project string
	// Kinds are the event kinds read; an event of one of them in the
	// window makes its root active.
	Kinds []model.EventKind
	// Limit caps the roots read: MaxActivityRoots when zero or negative,
	// at most MaxActivityRoots. More is ErrTooManyRoots, never a cut list.
	// The API leaves it at the default; tests lower it.
	Limit int
}

// RootActivity is one root and the records an overview reads of it: its
// runs and the correlations that link them, its pull requests without
// their checks, the events of the query's kinds, and its coverage.
type RootActivity struct {
	RootSummary
	Records model.Records
}

// runBounds names a run's bounds, from its stored body, as b: first (its
// creation, else its start), start, last (its end) and end_state.
const runBounds = `CROSS JOIN LATERAL (SELECT
		COALESCE((r.body->>'created')::timestamptz, (r.body->'interval'->'start'->>'at')::timestamptz) AS first,
		(r.body->'interval'->'start'->>'at')::timestamptz AS start,
		(r.body->'interval'->'end'->>'at')::timestamptz AS last,
		r.body->'interval'->'end'->>'state' AS end_state) b`

// activeRootsSQL selects the roots that may have been active in [$1, $2),
// with the project $3 when set, at most $5: a run with a recorded time in
// the window or an interval across it; an event of the kinds $4 in it, a
// completion observation only when it saw the task not done after a
// completion (every read records one, so any other would make every task
// read in the window a candidate); or a pull request opened, reopened or
// asked for a review before its end and not merged or closed before its
// start. The overview decides what each shows; this only leaves out what
// cannot show anything.
const activeRootsSQL = "SELECT o.id, " + rootColumns + ` FROM roots o
	WHERE ($3 = '' OR EXISTS (SELECT 1 FROM root_runs m JOIN runs r ON r.id = m.run_id
			WHERE m.root_id = o.id AND r.body->>'project' = $3))
		AND (EXISTS (SELECT 1 FROM root_runs m JOIN runs r ON r.id = m.run_id ` + runBounds + `
				WHERE m.root_id = o.id AND ((b.first >= $1 AND b.first < $2) OR (b.start >= $1 AND b.start < $2)
					OR (b.last >= $1 AND b.last < $2) OR (b.first < $1 AND (b.end_state = 'open' OR b.last >= $2))))
			OR EXISTS (SELECT 1 FROM root_events m JOIN events e ON e.id = m.event_id
				WHERE m.root_id = o.id AND e.kind = ANY($4) AND e.at >= $1 AND e.at < $2
					AND (e.kind <> 'task.completion_observed' OR (e.body->'details'->>'done' = 'false'
						AND EXISTS (SELECT 1 FROM root_events dm JOIN events d ON d.id = dm.event_id
							WHERE dm.root_id = o.id AND d.kind = 'task.done' AND d.at < e.at))))
			OR EXISTS (SELECT 1 FROM root_events m JOIN events e ON e.id = m.event_id
				WHERE m.root_id = o.id AND e.kind IN ('pr.opened', 'pr.reopened', 'pr.review_requested') AND e.at < $2
					AND COALESCE((SELECT s.kind FROM root_events sm JOIN events s ON s.id = sm.event_id
						WHERE sm.root_id = o.id AND s.kind IN ('pr.opened', 'pr.reopened', 'pr.merged', 'pr.closed')
							AND s.at < $1 AND s.body->'related'->'pr' = e.body->'related'->'pr'
						-- Equal timestamps do not establish causal order. Descending kind order
						-- prefers reopened/opened to merged/closed, keeping this query conservative.
						ORDER BY s.at DESC, s.kind DESC LIMIT 1), '') NOT IN ('pr.merged', 'pr.closed')))
	ORDER BY o.ref LIMIT $5`

// Activity returns the roots that may have been active in the query's
// window, in key order, with the records an overview reads of them. It
// keeps every root that can show something in the window, and may keep
// some that show nothing: the overview decides.
func (s *Store) Activity(ctx context.Context, q ActivityQuery) ([]RootActivity, error) {
	if !q.From.Before(q.To) {
		return nil, errors.New("store: an activity window must end after it starts")
	}
	limit := q.Limit
	if limit <= 0 || limit > MaxActivityRoots {
		limit = MaxActivityRoots
	}
	kinds := make([]string, 0, len(q.Kinds))
	for _, k := range q.Kinds {
		kinds = append(kinds, string(k))
	}
	var out []RootActivity
	err := s.read(ctx, func(tx pgx.Tx) error {
		ids, roots, err := activeRoots(ctx, tx, q, kinds, limit)
		if err != nil {
			return err
		}
		out = roots
		return loadActivity(ctx, tx, ids, kinds, out)
	})
	if err != nil {
		return nil, fmt.Errorf("store: activity: %w", err)
	}
	return out, nil
}

// activeRoots returns the ids and summaries of the roots activeRootsSQL
// selects, or ErrTooManyRoots when there are more than limit.
func activeRoots(ctx context.Context, tx pgx.Tx, q ActivityQuery, kinds []string, limit int) ([]int64, []RootActivity, error) {
	rows, err := tx.Query(ctx, activeRootsSQL, q.From, q.To, q.Project, kinds, limit+1)
	if err != nil {
		return nil, nil, err
	}
	var ids []int64
	var out []RootActivity
	var id int64
	var body []byte
	var title string
	var last *time.Time
	_, err = pgx.ForEachRow(rows, []any{&id, &body, &title, &last}, func() error {
		r := RootActivity{RootSummary: RootSummary{Title: title, LastActivity: utcPointer(last)}}
		if err := json.Unmarshal(body, &r.Root); err != nil {
			return err
		}
		ids = append(ids, id)
		out = append(out, r)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(out) > limit {
		return nil, nil, fmt.Errorf("more than %d: %w", limit, ErrTooManyRoots)
	}
	return ids, out, nil
}

// activityCorrelationsSQL selects the correlations of the roots $1 with a
// run at an end: the overview tells by them a root's own runs from the
// candidates of its records.
const activityCorrelationsSQL = "SELECT m.root_id, " + correlationBody + ` FROM root_correlations m
	JOIN correlations c ON c.id = m.correlation_id
	WHERE m.root_id = ANY($1) AND (c.body->'from'->>'kind' = 'run' OR c.body->'to'->>'kind' = 'run')`

// loadActivity reads the records of the roots ids name into out, which
// holds them in the same order.
func loadActivity(ctx context.Context, tx pgx.Tx, ids []int64, kinds []string, out []RootActivity) error {
	at := make(map[int64]*model.Records, len(ids))
	for i, id := range ids {
		out[i].Records = model.Records{}
		at[id] = &out[i].Records
	}
	if err := forEachBody(ctx, tx, `SELECT m.root_id, r.body FROM root_runs m JOIN runs r ON r.id = m.run_id
		WHERE m.root_id = ANY($1)`, func(root int64, body []byte) error {
		return appendJSON(&at[root].Runs, body)
	}, ids); err != nil {
		return err
	}
	if err := loadActivityEvents(ctx, tx, ids, kinds, at); err != nil {
		return err
	}
	if err := forEachBody(ctx, tx, activityCorrelationsSQL, func(root int64, body []byte) error {
		return appendJSON(&at[root].Correlations, body)
	}, ids); err != nil {
		return err
	}
	if err := forEachBody(ctx, tx, `SELECT root_id, body FROM coverage WHERE root_id = ANY($1)`,
		func(root int64, body []byte) error { return appendJSON(&at[root].Coverage, body) }, ids); err != nil {
		return err
	}
	return loadActivityPullRequests(ctx, tx, ids, at)
}

// loadActivityEvents reads the events of the kinds of the roots, each
// observation with its last observed time, as loadEvents does: a task's
// completion at an instant needs the last read that still saw it done.
func loadActivityEvents(ctx context.Context, tx pgx.Tx, ids []int64, kinds []string, at map[int64]*model.Records) error {
	rows, err := tx.Query(ctx, `SELECT m.root_id, e.body, e.observed_at, e.last_observed_at
		FROM root_events m JOIN events e ON e.id = m.event_id
		WHERE m.root_id = ANY($1) AND e.kind = ANY($2)`, ids, kinds)
	if err != nil {
		return err
	}
	var root int64
	var body []byte
	var first, last time.Time
	_, err = pgx.ForEachRow(rows, []any{&root, &body, &first, &last}, func() error {
		e, err := decodeStoredEvent(body, first, last)
		if err != nil {
			return err
		}
		at[root].Events = append(at[root].Events, e)
		return nil
	})
	return err
}

// loadActivityPullRequests reads the pull requests of the roots, without
// their checks.
func loadActivityPullRequests(ctx context.Context, tx pgx.Tx, ids []int64, at map[int64]*model.Records) error {
	rows, err := tx.Query(ctx, `SELECT m.root_id, p.instance, p.repository, p.number, p.branch
		FROM root_pull_requests m JOIN pull_requests p ON p.id = m.pull_request_id WHERE m.root_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	var root int64
	var p model.PullRequest
	_, err = pgx.ForEachRow(rows, []any{&root, &p.Key.Instance, &p.Key.Repository, &p.Key.Number, &p.Branch}, func() error {
		at[root].PullRequests = append(at[root].PullRequests, model.PullRequest{Key: p.Key, Branch: p.Branch})
		return nil
	})
	return err
}

// forEachBody calls fn with each row of a query selecting a root id and a
// JSON body.
func forEachBody(ctx context.Context, tx pgx.Tx, sql string, fn func(root int64, body []byte) error, args ...any) error {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	var root int64
	var body []byte
	_, err = pgx.ForEachRow(rows, []any{&root, &body}, func() error { return fn(root, body) })
	return err
}

// appendJSON decodes body and appends it to list.
func appendJSON[T any](list *[]T, body []byte) error {
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}
	*list = append(*list, v)
	return nil
}
