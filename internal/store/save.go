package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// Batch is what one read assembled about one root.
type Batch struct {
	Root model.Root
	// Title names the root in search results; empty keeps the stored one.
	Title   string
	Records model.Records
}

// Save validates the batch as model.BuildStory does and stores it in one
// transaction. Records are upserted by key and added to the root's story;
// a later batch never removes what an earlier one stored (a Task deleted
// by its TTL keeps its run), except where these rules say:
//
//   - Runs: a run keyed by UID never merges with another UID, so a retry
//     stays a separate run. A later read fills what it lacks from the stored
//     run (see mergeRun). A pod-keyed placeholder is re-keyed by UID once a
//     run with its pod is read, before or after it, and every event and
//     correlation naming it follows.
//   - States seen by polling (bucket, label, assignee): a value seen again,
//     unchanged since the task's previous read, extends the stored record's
//     last observed time instead of adding a record.
//   - Wiki: a live edit named by an archived version's "supersedes" detail
//     is dropped, its correlations moved to the version; of a page's live
//     records only the newest is kept.
//   - Coverage is merged per root and source instance (see mergeCoverage).
//
// Events keep their kinds: a pull request's pr.closed and pr.merged are
// stored as the GitHub source reported them, never derived one from the
// other.
func (s *Store) Save(ctx context.Context, b Batch) error {
	story, err := model.BuildStory(b.Root, b.Records)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	err = s.write(ctx, func(tx pgx.Tx) error {
		w := &saver{
			ctx:     ctx,
			tx:      tx,
			runKeys: make(map[model.RunKey]model.RunKey),
			moved:   make(map[model.Ref]model.Ref),
			dropped: make(map[model.Ref]bool),
		}
		return w.save(b.Title, story)
	})
	if err != nil {
		return fmt.Errorf("store: save %s: %w", rootRef(b.Root), err)
	}
	return nil
}

// saver writes one batch inside one transaction.
type saver struct {
	ctx  context.Context
	tx   pgx.Tx
	root int64
	// runKeys maps the batch's placeholder runs to the UID they are stored
	// under.
	runKeys map[model.RunKey]model.RunKey
	// moved maps the batch's records to the stored record they became: a
	// re-keyed run, an observation folded into an earlier one, a live wiki
	// edit an archived version superseded.
	moved map[model.Ref]model.Ref
	// dropped are the batch's records not stored, whose correlations are
	// dropped too: live wiki edits a newer one replaced.
	dropped map[model.Ref]bool
}

func (w *saver) save(title string, story model.Story) error {
	id, err := w.upsertRoot(story.Root, title)
	if err != nil {
		return err
	}
	w.root = id
	steps := []func() error{
		func() error { return w.saveRuns(story.Runs) },
		func() error { return w.saveEvents(story.Events) },
		func() error { return w.savePullRequests(story.PullRequests) },
		func() error { return w.saveCorrelations(story.Correlations) },
		func() error { return w.saveCoverage(story.Coverage) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if at, ok := activity(story.Records); ok {
		return w.exec("UPDATE roots SET last_activity = GREATEST(last_activity, $2) WHERE id = $1", w.root, at)
	}
	return nil
}

func (w *saver) upsertRoot(root model.Root, title string) (int64, error) {
	body, err := json.Marshal(root)
	if err != nil {
		return 0, err
	}
	kind, instance, sourceID, locale := model.RefTask, "", int64(0), (*string)(nil)
	if root.Task != nil {
		instance, sourceID = root.Task.Instance, root.Task.ID
	} else {
		kind, instance, sourceID, locale = model.RefPage, root.Page.Instance, root.Page.ID, &root.Page.Locale
	}
	var id int64
	err = w.tx.QueryRow(w.ctx, `
		INSERT INTO roots (ref, kind, instance, source_id, locale, title, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (ref) DO UPDATE SET title = COALESCE(EXCLUDED.title, roots.title), updated_at = now()
		RETURNING id`,
		rootRef(root).String(), string(kind), instance, sourceID, locale, nullable(title), body).Scan(&id)
	return id, err
}

// exec runs one statement in the transaction.
func (w *saver) exec(sql string, args ...any) error {
	_, err := w.tx.Exec(w.ctx, sql, args...)
	return err
}

// ref returns where the batch's record r is stored.
func (w *saver) ref(r model.Ref) model.Ref {
	if to, ok := w.moved[r]; ok {
		return to
	}
	return r
}

// nullable returns nil for an empty string, which stores NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
