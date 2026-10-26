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

// rootOrder lists roots most recently active first, then by key, as
// SearchRoots does.
const rootOrder = "ORDER BY o.last_activity DESC NULLS LAST, o.ref"

// rootColumns are the columns collectSummaries reads, from roots as o.
const rootColumns = "o.body, COALESCE(o.title, ''), o.last_activity"

// RunQuery names the runs of a Kelos Task: one attempt by its UID, or
// every attempt by its name, which a retry reuses. Namespace, when set,
// narrows it to one namespace. Exactly one of Name and UID is set. It
// matches runs in every cluster: a Task reference ([<namespace>/]<name>,
// a UID) names none, so the same name in two clusters matches both.
type RunQuery struct {
	Namespace string
	Name      string
	UID       string
}

func (q RunQuery) validate() error {
	if (q.Name == "") == (q.UID == "") {
		return errors.New("store: a run query needs exactly one of a name and a UID")
	}
	return nil
}

// SourceFreshness is how recently one source instance was read, across
// the stored roots that involve it. Coverage is per root: a source read
// for one root says nothing of another's records, so this says how fresh
// the reads are, not that any story is complete.
type SourceFreshness struct {
	Source   model.Source `json:"source"`
	Instance string       `json:"instance"`
	// Roots is how many stored roots have coverage of the instance.
	Roots int `json:"roots"`
	// NeverRead is how many of them record no successful read.
	NeverRead int `json:"neverRead"`
	// NewestRead is the latest successful read of any of them, and
	// OldestRead the stalest one's latest successful read; nil when no
	// root was read.
	NewestRead *time.Time `json:"newestRead,omitempty"`
	OldestRead *time.Time `json:"oldestRead,omitempty"`
}

// Root returns the summary of a stored root, or ErrNotFound.
func (s *Store) Root(ctx context.Context, root model.Root) (RootSummary, error) {
	if err := root.Validate(); err != nil {
		return RootSummary{}, fmt.Errorf("store: %w", err)
	}
	found, err := s.summaries(ctx, "SELECT "+rootColumns+" FROM roots o WHERE o.ref = $1", rootRef(root).String())
	if err != nil {
		return RootSummary{}, fmt.Errorf("store: root %s: %w", rootRef(root), err)
	}
	if len(found) == 0 {
		return RootSummary{}, fmt.Errorf("store: root %s: %w", rootRef(root), ErrNotFound)
	}
	return found[0], nil
}

// RootsOfPullRequest returns the roots whose stories hold the pull
// request, most recently active first, at most MaxSearchLimit. The
// repository matches in any case, as GitHub's do.
func (s *Store) RootsOfPullRequest(ctx context.Context, key model.PRKey) ([]RootSummary, error) {
	found, err := s.summaries(ctx, "SELECT "+rootColumns+` FROM roots o
		WHERE EXISTS (SELECT 1 FROM root_pull_requests m JOIN pull_requests p ON p.id = m.pull_request_id
			WHERE m.root_id = o.id AND p.instance = $1 AND lower(p.repository) = lower($2) AND p.number = $3)
		`+rootOrder+" LIMIT $4", key.Instance, key.Repository, key.Number, MaxSearchLimit)
	if err != nil {
		return nil, fmt.Errorf("store: roots of %s: %w", key, err)
	}
	return found, nil
}

// RootsOfRuns returns the roots whose stories hold a run q names, most
// recently active first, at most MaxSearchLimit. A name is matched in the
// runs' stored bodies, which no index covers: resolve is rare and runs
// few next to events.
func (s *Store) RootsOfRuns(ctx context.Context, q RunQuery) ([]RootSummary, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}
	found, err := s.summaries(ctx, "SELECT "+rootColumns+` FROM roots o
		WHERE EXISTS (SELECT 1 FROM root_runs m JOIN runs r ON r.id = m.run_id
			WHERE m.root_id = o.id
				AND (($1 <> '' AND r.uid = $1) OR ($2 <> '' AND r.body->>'taskName' = $2))
				AND ($3 = '' OR r.namespace = $3))
		`+rootOrder+" LIMIT $4", q.UID, q.Name, q.Namespace, MaxSearchLimit)
	if err != nil {
		return nil, fmt.Errorf("store: roots of runs: %w", err)
	}
	return found, nil
}

// Freshness returns, for each source instance any stored root's coverage
// names, how recently it was read, in source and instance order.
func (s *Store) Freshness(ctx context.Context) ([]SourceFreshness, error) {
	rows, err := s.pool.Query(ctx, `SELECT source, instance, count(*),
			count(*) FILTER (WHERE last_successful_read IS NULL),
			max(last_successful_read), min(last_successful_read)
		FROM coverage GROUP BY source, instance ORDER BY source, instance`)
	if err != nil {
		return nil, fmt.Errorf("store: freshness: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SourceFreshness, error) {
		var f SourceFreshness
		err := row.Scan(&f.Source, &f.Instance, &f.Roots, &f.NeverRead, &f.NewestRead, &f.OldestRead)
		f.NewestRead, f.OldestRead = utcPointer(f.NewestRead), utcPointer(f.OldestRead)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: freshness: %w", err)
	}
	return out, nil
}

// summaries runs a query that selects rootColumns.
func (s *Store) summaries(ctx context.Context, sql string, args ...any) ([]RootSummary, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return collectSummaries(rows)
}

// collectSummaries reads rows of rootColumns.
func collectSummaries(rows pgx.Rows) ([]RootSummary, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RootSummary, error) {
		var r RootSummary
		var body []byte
		if err := row.Scan(&body, &r.Title, &r.LastActivity); err != nil {
			return r, err
		}
		r.LastActivity = utcPointer(r.LastActivity)
		return r, json.Unmarshal(body, &r.Root)
	})
}

// utcPointer returns t in UTC, or nil.
func utcPointer(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
