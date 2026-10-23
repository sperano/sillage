package store

import (
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// savePullRequests upserts each pull request and its checks. A check run
// is replaced by a later read of the same id, and never removed: an
// earlier commit's checks stay when a read lists only the latest's.
func (w *saver) savePullRequests(prs []model.PullRequest) error {
	for _, p := range prs {
		var id int64
		err := w.tx.QueryRow(w.ctx, `
			INSERT INTO pull_requests (instance, repository, number, ref, branch) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (ref) DO UPDATE SET branch = EXCLUDED.branch
			RETURNING id`,
			p.Key.Instance, p.Key.Repository, p.Key.Number, p.Key.Ref().String(), p.Branch).Scan(&id)
		if err != nil {
			return err
		}
		for _, c := range p.Checks {
			body, err := json.Marshal(c)
			if err != nil {
				return err
			}
			if err := w.exec(`
				INSERT INTO check_runs (pull_request_id, check_id, sha, name, conclusion, body)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (pull_request_id, check_id) DO UPDATE SET
					sha = EXCLUDED.sha, name = EXCLUDED.name, conclusion = EXCLUDED.conclusion, body = EXCLUDED.body`,
				id, c.ID, c.SHA, c.Name, string(c.Conclusion), body); err != nil {
				return err
			}
		}
		if err := w.exec(`INSERT INTO root_pull_requests (root_id, pull_request_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, w.root, id); err != nil {
			return err
		}
	}
	return nil
}

// saveCorrelations upserts each correlation by the pair it links, after
// following the records the batch's ends were stored as. One whose end was
// dropped is dropped.
func (w *saver) saveCorrelations(correlations []model.Correlation) error {
	for _, c := range correlations {
		if w.dropped[c.From] || w.dropped[c.To] {
			continue
		}
		c.From, c.To = w.ref(c.From), w.ref(c.To)
		if c.From == c.To {
			continue
		}
		id, err := w.upsertCorrelation(c)
		if err != nil {
			return err
		}
		if err := w.exec(`INSERT INTO root_correlations (root_id, correlation_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, w.root, id); err != nil {
			return err
		}
	}
	return nil
}

func (w *saver) upsertCorrelation(c model.Correlation) (int64, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	var id int64
	err = w.tx.QueryRow(w.ctx, `
		INSERT INTO correlations (pair_key, from_ref, to_ref, body) VALUES ($1, $2, $3, $4)
		ON CONFLICT (pair_key) DO UPDATE SET from_ref = EXCLUDED.from_ref, to_ref = EXCLUDED.to_ref, body = EXCLUDED.body
		RETURNING id`,
		pairKey(c), c.From.String(), c.To.String(), body).Scan(&id)
	return id, err
}

// repointCorrelations makes the stored correlations with an end at from
// end at to instead. One that now duplicates a stored correlation is
// folded into it: its stories keep the stored one.
func (w *saver) repointCorrelations(from, to model.Ref) error {
	rows, err := w.tx.Query(w.ctx, "SELECT id, body FROM correlations WHERE from_ref = $1 OR to_ref = $1",
		from.String())
	if err != nil {
		return err
	}
	type stored struct {
		id int64
		c  model.Correlation
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (stored, error) {
		var s stored
		var body []byte
		if err := row.Scan(&s.id, &body); err != nil {
			return s, err
		}
		return s, json.Unmarshal(body, &s.c)
	})
	if err != nil {
		return err
	}
	for _, s := range found {
		if err := w.repointCorrelation(s.id, s.c, from, to); err != nil {
			return err
		}
	}
	return nil
}

func (w *saver) repointCorrelation(id int64, c model.Correlation, from, to model.Ref) error {
	if c.From == from {
		c.From = to
	}
	if c.To == from {
		c.To = to
	}
	if c.From == c.To {
		return w.exec("DELETE FROM correlations WHERE id = $1", id)
	}
	var existing int64
	err := w.tx.QueryRow(w.ctx, "SELECT id FROM correlations WHERE pair_key = $1 AND id <> $2",
		pairKey(c), id).Scan(&existing)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		body, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return w.exec("UPDATE correlations SET pair_key = $2, from_ref = $3, to_ref = $4, body = $5 WHERE id = $1",
			id, pairKey(c), c.From.String(), c.To.String(), body)
	case err != nil:
		return err
	}
	if err := w.exec(`INSERT INTO root_correlations (root_id, correlation_id)
		SELECT root_id, $2 FROM root_correlations WHERE correlation_id = $1 ON CONFLICT DO NOTHING`,
		id, existing); err != nil {
		return err
	}
	return w.exec("DELETE FROM correlations WHERE id = $1", id)
}

// pairKey identifies the pair of records c links, either way round, as
// model.BuildStory does.
func pairKey(c model.Correlation) string {
	from, to := c.From.String(), c.To.String()
	if to < from {
		from, to = to, from
	}
	return from + " <-> " + to
}

// saveCoverage merges each coverage record with the root's stored one.
func (w *saver) saveCoverage(coverage []model.Coverage) error {
	for _, c := range coverage {
		var body []byte
		err := w.tx.QueryRow(w.ctx, "SELECT body FROM coverage WHERE root_id = $1 AND source = $2 AND instance = $3",
			w.root, string(c.Source), c.Instance).Scan(&body)
		switch {
		case err == nil:
			var stored model.Coverage
			if err := json.Unmarshal(body, &stored); err != nil {
				return err
			}
			c = mergeCoverage(stored, c)
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if body, err = json.Marshal(c); err != nil {
			return err
		}
		if err := w.exec(`
			INSERT INTO coverage (root_id, source, instance, last_successful_read, body) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (root_id, source, instance) DO UPDATE SET
				last_successful_read = EXCLUDED.last_successful_read, body = EXCLUDED.body`,
			w.root, string(c.Source), c.Instance, c.LastSuccessfulRead, body); err != nil {
			return err
		}
	}
	return nil
}
