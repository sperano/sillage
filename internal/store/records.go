package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// savePullRequests upserts each pull request and its checks. A check run
// is replaced by a later read of the same id, and never removed: an
// earlier commit's checks stay when a read lists only the latest's.
func (w *saver) savePullRequests(prs []model.PullRequest) error {
	for _, p := range prs {
		if err := w.savePullRequest(p); err != nil {
			return fmt.Errorf("pull request %s: %w", p.Key, err)
		}
	}
	return nil
}

func (w *saver) savePullRequest(p model.PullRequest) error {
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
			return fmt.Errorf("check %d: %w", c.ID, err)
		}
	}
	return w.exec(`INSERT INTO root_pull_requests (root_id, pull_request_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, w.root, id)
}

// correlationBody is a stored correlation as a root's story holds it (c
// the correlation, m its root_correlations row): the ends that c keeps,
// and the confidence, evidence and ambiguity that story's read found,
// whatever another story's found.
const correlationBody = `c.body || m.body`

// correlationEnds is what a stored correlation keeps for every story: the
// records it links. The rest of it is each root's (see saveCorrelations).
type correlationEnds struct {
	From model.Ref `json:"from"`
	To   model.Ref `json:"to"`
}

func (e correlationEnds) pair() model.RefPair { return model.PairOf(e.From, e.To) }

// saveCorrelations upserts each correlation by the pair it links, after
// following the records the batch's ends were stored as. One whose end was
// dropped is dropped. Its confidence, evidence and ambiguity are kept for
// the root: the root's latest read decides them, another root's read
// never does.
func (w *saver) saveCorrelations(correlations []model.Correlation) error {
	for _, c := range correlations {
		c.From, c.To = w.ref(c.From), w.ref(c.To)
		if w.dropped[c.From] || w.dropped[c.To] || c.From == c.To {
			continue
		}
		if err := w.saveCorrelation(c); err != nil {
			return fmt.Errorf("correlation %s: %w", c, err)
		}
	}
	return nil
}

func (w *saver) saveCorrelation(c model.Correlation) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	id, err := w.upsertCorrelation(correlationEnds{From: c.From, To: c.To})
	if err != nil {
		return err
	}
	return w.exec(`INSERT INTO root_correlations (root_id, correlation_id, body) VALUES ($1, $2, $3::jsonb - 'from' - 'to')
		ON CONFLICT (root_id, correlation_id) DO UPDATE SET body = EXCLUDED.body`,
		w.root, id, body)
}

// upsertCorrelation stores the ends of a correlation by their pair.
func (w *saver) upsertCorrelation(e correlationEnds) (int64, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	var id int64
	err = w.tx.QueryRow(w.ctx, `
		INSERT INTO correlations (pair_key, from_ref, to_ref, body) VALUES ($1, $2, $3, $4)
		ON CONFLICT (pair_key) DO UPDATE SET from_ref = EXCLUDED.from_ref, to_ref = EXCLUDED.to_ref, body = EXCLUDED.body
		RETURNING id`,
		e.pair().Key(), e.From.String(), e.To.String(), body).Scan(&id)
	return id, err
}

// repointCorrelations makes the stored correlations with an end at from
// end at to instead. One that now duplicates a stored correlation is
// folded into it: its stories keep the stored one, and a story that held
// only the folded one takes its confidence, evidence and ambiguity.
func (w *saver) repointCorrelations(from, to model.Ref) error {
	rows, err := w.tx.Query(w.ctx, "SELECT id, body FROM correlations WHERE from_ref = $1 OR to_ref = $1",
		from.String())
	if err != nil {
		return err
	}
	type stored struct {
		id int64
		e  correlationEnds
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (stored, error) {
		var s stored
		var body []byte
		if err := row.Scan(&s.id, &body); err != nil {
			return s, err
		}
		return s, json.Unmarshal(body, &s.e)
	})
	if err != nil {
		return err
	}
	for _, s := range found {
		if err := w.repointCorrelation(s.id, s.e, from, to); err != nil {
			return err
		}
	}
	return nil
}

func (w *saver) repointCorrelation(id int64, e correlationEnds, from, to model.Ref) error {
	if e.From == from {
		e.From = to
	}
	if e.To == from {
		e.To = to
	}
	if e.From == e.To {
		return w.exec("DELETE FROM correlations WHERE id = $1", id)
	}
	var existing int64
	err := w.tx.QueryRow(w.ctx, "SELECT id FROM correlations WHERE pair_key = $1 AND id <> $2",
		e.pair().Key(), id).Scan(&existing)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		body, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return w.exec("UPDATE correlations SET pair_key = $2, from_ref = $3, to_ref = $4, body = $5 WHERE id = $1",
			id, e.pair().Key(), e.From.String(), e.To.String(), body)
	case err != nil:
		return err
	}
	if err := w.exec(`INSERT INTO root_correlations (root_id, correlation_id, body)
		SELECT root_id, $2, body FROM root_correlations WHERE correlation_id = $1 ON CONFLICT DO NOTHING`,
		id, existing); err != nil {
		return err
	}
	return w.exec("DELETE FROM correlations WHERE id = $1", id)
}

// saveCoverage stores each coverage record by the saver's rule (merged
// with the root's stored one, or in its place), without PhaseMarkersNote,
// which a story read adds while its runs have markers.
func (w *saver) saveCoverage(coverage []model.Coverage) error {
	for _, c := range coverage {
		c.Notes = slices.DeleteFunc(slices.Clone(c.Notes), func(n string) bool { return n == model.PhaseMarkersNote })
		put := w.putCoverage
		if w.coverage == coverageMerge {
			put = w.mergeCoverage
		}
		if err := put(c); err != nil {
			return fmt.Errorf("coverage %s: %w", c.Key(), err)
		}
	}
	return nil
}

func (w *saver) mergeCoverage(c model.Coverage) error {
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
	return w.putCoverage(c)
}

// putCoverage stores c as the root's coverage of its source instance.
func (w *saver) putCoverage(c model.Coverage) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return w.exec(`
		INSERT INTO coverage (root_id, source, instance, last_successful_read, body) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (root_id, source, instance) DO UPDATE SET
			last_successful_read = EXCLUDED.last_successful_read, body = EXCLUDED.body`,
		w.root, string(c.Source), c.Instance, c.LastSuccessfulRead, body)
}
