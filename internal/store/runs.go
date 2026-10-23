package store

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// storedRun is a run row.
type storedRun struct {
	id  int64
	run model.Run
}

func (w *saver) saveRuns(runs []model.Run) error {
	for _, r := range runs {
		id, key, err := w.saveRun(r)
		if err != nil {
			return fmt.Errorf("run %s: %w", r.Key, err)
		}
		if key != r.Key {
			w.runKeys[r.Key] = key
			w.moved[r.Key.Ref()] = key.Ref()
		}
		if err := w.exec("INSERT INTO root_runs (root_id, run_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			w.root, id); err != nil {
			return err
		}
	}
	return nil
}

// saveRun stores r and returns its row and the key it is stored under.
func (w *saver) saveRun(r model.Run) (int64, model.RunKey, error) {
	if r.Key.UID == "" {
		return w.savePlaceholder(r)
	}
	stored, found, err := w.runByRef(r.Key.Ref())
	if err != nil {
		return 0, model.RunKey{}, err
	}
	var placeholder storedRun
	hasPlaceholder := false
	if r.Pod != "" {
		placeholder, hasPlaceholder, err = w.runByRef(podKey(r.Key, r.Pod).Ref())
		if err != nil {
			return 0, model.RunKey{}, err
		}
	}
	switch {
	case hasPlaceholder && found:
		// Both rows exist: fold the placeholder into the UID's.
		merged := mergeRun(mergeRun(placeholder.run, stored.run), r)
		if err := w.foldRun(placeholder, stored.id, merged); err != nil {
			return 0, model.RunKey{}, err
		}
		return stored.id, r.Key, w.writeRun(stored.id, merged)
	case hasPlaceholder:
		merged := mergeRun(placeholder.run, r)
		if err := w.writeRun(placeholder.id, merged); err != nil {
			return 0, model.RunKey{}, err
		}
		return placeholder.id, r.Key, w.repointRun(placeholder.run.Key, r.Key)
	case found:
		return stored.id, r.Key, w.writeRun(stored.id, mergeRun(stored.run, r))
	}
	id, err := w.insertRun(r)
	return id, r.Key, err
}

// savePlaceholder stores a run keyed by pod: under the UID of the one
// stored run with that pod, if there is one.
func (w *saver) savePlaceholder(r model.Run) (int64, model.RunKey, error) {
	rows, err := w.tx.Query(w.ctx, `SELECT id, body FROM runs
		WHERE cluster = $1 AND namespace = $2 AND pod = $3 AND uid IS NOT NULL LIMIT 2`,
		r.Key.Cluster, r.Key.Namespace, r.Key.Pod)
	if err != nil {
		return 0, model.RunKey{}, err
	}
	byPod, err := pgx.CollectRows(rows, scanRun)
	if err != nil {
		return 0, model.RunKey{}, err
	}
	// Two attempts never share a pod; if the store says otherwise, the pod
	// does not establish which one this is, and the placeholder stays.
	if len(byPod) == 1 {
		u := byPod[0]
		return u.id, u.run.Key, w.writeRun(u.id, mergeRun(r, u.run))
	}
	stored, found, err := w.runByRef(r.Key.Ref())
	if err != nil {
		return 0, model.RunKey{}, err
	}
	if found {
		return stored.id, r.Key, w.writeRun(stored.id, mergeRun(stored.run, r))
	}
	id, err := w.insertRun(r)
	return id, r.Key, err
}

// foldRun moves the placeholder's stories and references to the run row
// into, then deletes the placeholder.
func (w *saver) foldRun(placeholder storedRun, into int64, merged model.Run) error {
	if err := w.exec(`INSERT INTO root_runs (root_id, run_id)
		SELECT root_id, $2 FROM root_runs WHERE run_id = $1 ON CONFLICT DO NOTHING`,
		placeholder.id, into); err != nil {
		return err
	}
	if err := w.exec("DELETE FROM runs WHERE id = $1", placeholder.id); err != nil {
		return err
	}
	return w.repointRun(placeholder.run.Key, merged.Key)
}

// repointRun makes the stored events and correlations that name from name
// to instead.
func (w *saver) repointRun(from, to model.RunKey) error {
	key, err := json.Marshal(to)
	if err != nil {
		return err
	}
	if err := w.exec(`UPDATE events SET run_ref = $2, body = jsonb_set(body, '{related,run}', $3)
		WHERE run_ref = $1`, from.Ref().String(), to.Ref().String(), key); err != nil {
		return err
	}
	return w.repointCorrelations(from.Ref(), to.Ref())
}

func (w *saver) runByRef(ref model.Ref) (storedRun, bool, error) {
	var r storedRun
	var body []byte
	err := w.tx.QueryRow(w.ctx, "SELECT id, body FROM runs WHERE ref = $1", ref.String()).Scan(&r.id, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedRun{}, false, nil
	}
	if err == nil {
		err = json.Unmarshal(body, &r.run)
	}
	return r, err == nil, err
}

func scanRun(row pgx.CollectableRow) (storedRun, error) {
	var r storedRun
	var body []byte
	if err := row.Scan(&r.id, &body); err != nil {
		return r, err
	}
	return r, json.Unmarshal(body, &r.run)
}

func (w *saver) insertRun(r model.Run) (int64, error) {
	cols, err := runColumns(r)
	if err != nil {
		return 0, err
	}
	var id int64
	err = w.tx.QueryRow(w.ctx, `INSERT INTO runs (cluster, namespace, uid, key_pod, ref, pod, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, cols...).Scan(&id)
	if err == nil {
		w.touchRun(id, r)
	}
	return id, err
}

// writeRun replaces row id with r, key included.
func (w *saver) writeRun(id int64, r model.Run) error {
	if err := r.Validate(); err != nil {
		return err
	}
	cols, err := runColumns(r)
	if err != nil {
		return err
	}
	w.touchRun(id, r)
	return w.exec(`UPDATE runs SET cluster = $1, namespace = $2, uid = $3, key_pod = $4, ref = $5, pod = $6, body = $7
		WHERE id = $8`, append(cols, id)...)
}

func runColumns(r model.Run) ([]any, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return []any{r.Key.Cluster, r.Key.Namespace, nullable(r.Key.UID), nullable(r.Key.Pod),
		r.Key.Ref().String(), nullable(r.Pod), body}, nil
}

// podKey is the placeholder key of the run of pod in key's namespace.
func podKey(key model.RunKey, pod string) model.RunKey {
	return model.RunKey{Cluster: key.Cluster, Namespace: key.Namespace, Pod: pod}
}
