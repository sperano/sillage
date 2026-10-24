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

// Story returns root's stored story, or ErrNotFound. Its Kelos coverage
// notes what the phase markers leave unknown (see addMarkerNotes).
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
		if recs, err = loadRecords(ctx, tx, id); err != nil {
			return err
		}
		return addMarkerNotes(ctx, tx, id, recs.Coverage)
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
	type row struct {
		id int64
		pr model.PullRequest
	}
	rows, err := tx.Query(ctx, `SELECT p.id, p.instance, p.repository, p.number, p.branch FROM pull_requests p
		JOIN root_pull_requests m ON m.pull_request_id = p.id WHERE m.root_id = $1`, root)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var p row
		err := r.Scan(&p.id, &p.pr.Key.Instance, &p.pr.Key.Repository, &p.pr.Key.Number, &p.pr.Branch)
		return p, err
	})
	if err != nil {
		return nil, err
	}
	checks, err := loadChecks(ctx, tx, root)
	if err != nil {
		return nil, err
	}
	prs := make([]model.PullRequest, 0, len(found))
	for _, p := range found {
		p.pr.Checks = checks[p.id]
		prs = append(prs, p.pr)
	}
	return prs, nil
}

// loadChecks reads the check runs of the root's pull requests, by pull
// request row.
func loadChecks(ctx context.Context, tx pgx.Tx, root int64) (map[int64][]model.CheckRun, error) {
	rows, err := tx.Query(ctx, `SELECT c.pull_request_id, c.body FROM check_runs c
		JOIN root_pull_requests m ON m.pull_request_id = c.pull_request_id WHERE m.root_id = $1`, root)
	if err != nil {
		return nil, err
	}
	checks := make(map[int64][]model.CheckRun)
	var pr int64
	var body []byte
	_, err = pgx.ForEachRow(rows, []any{&pr, &body}, func() error {
		var c model.CheckRun
		if err := json.Unmarshal(body, &c); err != nil {
			return err
		}
		checks[pr] = append(checks[pr], c)
		return nil
	})
	return checks, err
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
// instance order, or ErrNotFound. Its Kelos coverage notes what the phase
// markers leave unknown, as Story's does.
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
		if out, err = queryJSON[model.Coverage](ctx, tx,
			"SELECT body FROM coverage WHERE root_id = $1 ORDER BY source, instance", id); err != nil {
			return err
		}
		return addMarkerNotes(ctx, tx, id, out)
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
	out, err := s.summaries(ctx, "SELECT "+rootColumns+` FROM roots o
		WHERE o.title ILIKE $1 OR o.ref ILIKE $1 `+rootOrder+" LIMIT $2", pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search roots: %w", err)
	}
	return out, nil
}

// likeEscaper escapes LIKE's wildcards and its default escape character,
// so that search text matches literally.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
