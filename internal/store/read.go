package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

const (
	// DefaultSearchLimit is how many roots SearchRoots returns when the
	// query sets no limit.
	DefaultSearchLimit = 50
	// MaxSearchLimit caps a query's limit.
	MaxSearchLimit = 500
)

// RootQuery selects roots.
type RootQuery struct {
	// Text matches, case-insensitively, part of a root's title or of its
	// key ("tasks/12", "wiki.example.com"); empty matches every root.
	Text string
	// Limit caps the results: DefaultSearchLimit when zero or negative, at
	// most MaxSearchLimit.
	Limit int
}

// RootSummary is one stored root.
type RootSummary struct {
	Root  model.Root `json:"root"`
	Title string     `json:"title,omitempty"`
	// LastActivity is the latest time of the root's records; nil when none
	// has a time.
	LastActivity *time.Time `json:"lastActivity,omitempty"`
}

// read runs fn in a read-only transaction that sees one snapshot.
func (s *Store) read(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

// Story returns root's stored story, or ErrNotFound.
func (s *Store) Story(ctx context.Context, root model.Root) (model.Story, error) {
	if err := root.Validate(); err != nil {
		return model.Story{}, fmt.Errorf("store: %w", err)
	}
	var recs model.Records
	err := s.read(ctx, func(tx pgx.Tx) error {
		id, err := rootID(ctx, tx, root)
		if err != nil {
			return err
		}
		recs, err = loadRecords(ctx, tx, id)
		return err
	})
	if err != nil {
		return model.Story{}, fmt.Errorf("store: story %s: %w", rootRef(root), err)
	}
	story, err := model.BuildStory(root, recs)
	if err != nil {
		return model.Story{}, fmt.Errorf("store: story %s: %w", rootRef(root), err)
	}
	return story, nil
}

func loadRecords(ctx context.Context, tx pgx.Tx, root int64) (model.Records, error) {
	var recs model.Records
	var err error
	if recs.Events, err = loadEvents(ctx, tx, root); err != nil {
		return recs, err
	}
	if recs.Runs, err = queryJSON[model.Run](ctx, tx, `SELECT r.body FROM runs r
		JOIN root_runs m ON m.run_id = r.id WHERE m.root_id = $1`, root); err != nil {
		return recs, err
	}
	if recs.PullRequests, err = loadPullRequests(ctx, tx, root); err != nil {
		return recs, err
	}
	if recs.Correlations, err = queryJSON[model.Correlation](ctx, tx, `SELECT c.body FROM correlations c
		JOIN root_correlations m ON m.correlation_id = c.id WHERE m.root_id = $1`, root); err != nil {
		return recs, err
	}
	recs.Coverage, err = queryJSON[model.Coverage](ctx, tx,
		"SELECT body FROM coverage WHERE root_id = $1 ORDER BY source, instance", root)
	return recs, err
}

// loadEvents reads the root's events, each observation with its last
// observed time when a later read saw it again.
func loadEvents(ctx context.Context, tx pgx.Tx, root int64) ([]model.Event, error) {
	rows, err := tx.Query(ctx, `SELECT e.body, e.observed_at, e.last_observed_at FROM events e
		JOIN root_events m ON m.event_id = e.id WHERE m.root_id = $1`, root)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Event, error) {
		var e model.Event
		var body []byte
		var first, last time.Time
		if err := row.Scan(&body, &first, &last); err != nil {
			return e, err
		}
		if err := json.Unmarshal(body, &e); err != nil {
			return e, err
		}
		if last.After(first) {
			last = last.UTC()
			e.LastObservedAt = &last
		}
		return e, nil
	})
}

func loadPullRequests(ctx context.Context, tx pgx.Tx, root int64) ([]model.PullRequest, error) {
	rows, err := tx.Query(ctx, `SELECT p.id, p.instance, p.repository, p.number, p.branch FROM pull_requests p
		JOIN root_pull_requests m ON m.pull_request_id = p.id WHERE m.root_id = $1`, root)
	if err != nil {
		return nil, err
	}
	var ids []int64
	prs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.PullRequest, error) {
		var id int64
		var p model.PullRequest
		err := row.Scan(&id, &p.Key.Instance, &p.Key.Repository, &p.Key.Number, &p.Branch)
		ids = append(ids, id)
		return p, err
	})
	if err != nil {
		return nil, err
	}
	for i, id := range ids {
		if prs[i].Checks, err = queryJSON[model.CheckRun](ctx, tx,
			"SELECT body FROM check_runs WHERE pull_request_id = $1", id); err != nil {
			return nil, err
		}
	}
	return prs, nil
}

// queryJSON reads rows of one JSON column into T.
func queryJSON[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) {
		var v T
		var body []byte
		if err := row.Scan(&body); err != nil {
			return v, err
		}
		return v, json.Unmarshal(body, &v)
	})
}

// Coverage returns how far each source was read for root, in source and
// instance order, or ErrNotFound.
func (s *Store) Coverage(ctx context.Context, root model.Root) ([]model.Coverage, error) {
	if err := root.Validate(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var out []model.Coverage
	err := s.read(ctx, func(tx pgx.Tx) error {
		id, err := rootID(ctx, tx, root)
		if err != nil {
			return err
		}
		out, err = queryJSON[model.Coverage](ctx, tx,
			"SELECT body FROM coverage WHERE root_id = $1 ORDER BY source, instance", id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store: coverage %s: %w", rootRef(root), err)
	}
	return out, nil
}

// SearchRoots lists the roots matching q, most recently active first.
func (s *Store) SearchRoots(ctx context.Context, q RootQuery) ([]RootSummary, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, MaxSearchLimit)
	pattern := "%" + likeEscaper.Replace(strings.TrimSpace(q.Text)) + "%"
	rows, err := s.pool.Query(ctx, `SELECT body, COALESCE(title, ''), last_activity FROM roots
		WHERE title ILIKE $1 OR ref ILIKE $1
		ORDER BY last_activity DESC NULLS LAST, ref
		LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search roots: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RootSummary, error) {
		var r RootSummary
		var body []byte
		if err := row.Scan(&body, &r.Title, &r.LastActivity); err != nil {
			return r, err
		}
		if r.LastActivity != nil {
			t := r.LastActivity.UTC()
			r.LastActivity = &t
		}
		return r, json.Unmarshal(body, &r.Root)
	})
	if err != nil {
		return nil, fmt.Errorf("store: search roots: %w", err)
	}
	return out, nil
}

// likeEscaper escapes LIKE's wildcards and its default escape character,
// so that search text matches literally.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
