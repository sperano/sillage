package store

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

const (
	// usageFeedInstance is the one usage feed's row in usage_feed.
	usageFeedInstance = "loki"
	// usageInsertChunk bounds the rows one SaveUsage round trip carries.
	usageInsertChunk = 500
)

// StoryUsage is the usage a story's runs have in the store.
type StoryUsage struct {
	// Events: the events of the pods the story's runs were seen with
	// (run_pods, and each run's Pod), and those naming one of the story's
	// runs' Task names, in the runs' clusters and namespaces; ordered by
	// (at, pod_uid, event, stream, seq).
	Events []model.UsageEvent
	// Pods: every pod each run keyed by Task UID was seen with, sorted.
	Pods map[model.RunKey][]string
	// ReadThrough: the feed has read every event before it; nil when it
	// never read.
	ReadThrough *time.Time
}

// SaveUsage stores events, each unless the store holds one of its key
// already (a usage event is immutable: the held one stays), and returns
// how many were new. Every event is validated first; one invalid event
// fails the call and nothing is saved. It does not take the save lock: a
// usage event is evidence apart from every story.
func (s *Store) SaveUsage(ctx context.Context, events []model.UsageEvent) (int, error) {
	norm := make([]model.UsageEvent, len(events))
	for i, e := range events {
		if err := e.Validate(); err != nil {
			return 0, fmt.Errorf("store: save usage: %w", err)
		}
		norm[i] = e.Normalize()
	}
	inserted := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		inserted = 0
		for chunk := range slices.Chunk(norm, usageInsertChunk) {
			n, err := insertUsage(ctx, tx, chunk)
			if err != nil {
				return err
			}
			inserted += n
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("store: save usage: %w", err)
	}
	return inserted, nil
}

func insertUsage(ctx context.Context, tx pgx.Tx, events []model.UsageEvent) (int, error) {
	var batch pgx.Batch
	for _, e := range events {
		body, err := json.Marshal(e)
		if err != nil {
			return 0, err
		}
		batch.Queue(`INSERT INTO usage_events
			(cluster, namespace, pod, pod_uid, task_name, runner, kind, event, stream, seq, at, body)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (pod_uid, event, stream, seq) DO NOTHING`,
			e.Cluster, e.Namespace, e.Pod, e.PodUID, nullable(e.TaskName), string(e.Runner), string(e.Kind),
			e.Event, e.Stream, e.Seq, e.At, body)
	}
	res := tx.SendBatch(ctx, &batch)
	n := 0
	for range events {
		tag, err := res.Exec()
		if err != nil {
			_ = res.Close()
			return 0, err
		}
		n += int(tag.RowsAffected())
	}
	return n, res.Close()
}

// UsageFeed returns how far the usage feed has read: every event before
// the returned time was read. It is nil when the feed never read.
func (s *Store) UsageFeed(ctx context.Context) (*time.Time, error) {
	return usageFeed(ctx, s.pool)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func usageFeed(ctx context.Context, q queryRower) (*time.Time, error) {
	var t time.Time
	err := q.QueryRow(ctx, "SELECT read_through FROM usage_feed WHERE instance = $1", usageFeedInstance).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: usage feed: %w", err)
	}
	t = t.UTC()
	return &t, nil
}

// SetUsageFeed records that the feed has read every event before t. It
// never moves the feed backwards: an earlier t changes nothing.
func (s *Store) SetUsageFeed(ctx context.Context, t time.Time) error {
	if t.IsZero() {
		return errors.New("store: set usage feed: a time is required")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usage_feed (instance, read_through, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (instance) DO UPDATE SET
			read_through = GREATEST(usage_feed.read_through, EXCLUDED.read_through),
			updated_at = EXCLUDED.updated_at`,
		usageFeedInstance, t.UTC())
	if err != nil {
		return fmt.Errorf("store: set usage feed: %w", err)
	}
	return nil
}

// UsageOf returns the usage events of story's runs, every pod each of its
// runs keyed by Task UID was seen with, and how far the usage feed has
// read.
func (s *Store) UsageOf(ctx context.Context, story model.Story) (StoryUsage, error) {
	out := StoryUsage{Pods: make(map[model.RunKey][]string)}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		if out.Pods, err = runPodsOf(ctx, tx, story); err != nil {
			return err
		}
		if out.Events, err = usageEventsOf(ctx, tx, story, out.Pods); err != nil {
			return err
		}
		out.ReadThrough, err = usageFeed(ctx, tx)
		return err
	})
	if err != nil {
		return StoryUsage{}, fmt.Errorf("store: usage of story: %w", err)
	}
	return out, nil
}

// runPodsOf returns, for each run of story keyed by Task UID, the pods it
// was seen with: the stored ones and the run's own.
func runPodsOf(ctx context.Context, tx pgx.Tx, story model.Story) (map[model.RunKey][]string, error) {
	out := make(map[model.RunKey][]string)
	for _, r := range story.Runs {
		if r.Key.UID == "" {
			continue
		}
		key := model.RunKey{Cluster: r.Key.Cluster, Namespace: r.Key.Namespace, UID: r.Key.UID}
		pods := out[key]
		if r.Pod != "" {
			pods = append(pods, r.Pod)
		}
		rows, err := tx.Query(ctx, `SELECT pod FROM run_pods WHERE cluster = $1 AND namespace = $2 AND uid = $3`,
			key.Cluster, key.Namespace, key.UID)
		if err != nil {
			return nil, err
		}
		stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		pods = append(pods, stored...)
		slices.Sort(pods)
		out[key] = slices.Compact(pods)
	}
	return out, nil
}

// usageEventsOf reads the events of the pods and Task names of story's
// runs, per namespace, in their order.
func usageEventsOf(ctx context.Context, tx pgx.Tx, story model.Story,
	seen map[model.RunKey][]string) ([]model.UsageEvent, error) {
	type want struct{ pods, names map[string]bool }
	byNS := make(map[namespaceKey]*want)
	of := func(cluster, namespace string) *want {
		k := namespaceKey{cluster, namespace}
		if byNS[k] == nil {
			byNS[k] = &want{pods: make(map[string]bool), names: make(map[string]bool)}
		}
		return byNS[k]
	}
	for k, pods := range seen {
		w := of(k.Cluster, k.Namespace)
		for _, p := range pods {
			w.pods[p] = true
		}
	}
	for _, r := range story.Runs {
		w := of(r.Key.Cluster, r.Key.Namespace)
		if r.Pod != "" {
			w.pods[r.Pod] = true
		}
		if r.TaskName != "" {
			w.names[r.TaskName] = true
		}
	}
	var out []model.UsageEvent
	for _, ns := range slices.SortedFunc(maps.Keys(byNS), compareNamespaces) {
		w := byNS[ns]
		if len(w.pods) == 0 && len(w.names) == 0 {
			continue
		}
		rows, err := tx.Query(ctx, `SELECT body FROM usage_events
			WHERE cluster = $1 AND namespace = $2 AND (pod = ANY($3) OR task_name = ANY($4))`,
			ns.cluster, ns.namespace, slices.Sorted(maps.Keys(w.pods)), slices.Sorted(maps.Keys(w.names)))
		if err != nil {
			return nil, err
		}
		events, err := pgx.CollectRows(rows, scanUsage)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	slices.SortFunc(out, compareUsage)
	return out, nil
}

func scanUsage(row pgx.CollectableRow) (model.UsageEvent, error) {
	var body []byte
	if err := row.Scan(&body); err != nil {
		return model.UsageEvent{}, err
	}
	var e model.UsageEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return e, err
	}
	e.At = e.At.UTC()
	return e, nil
}

func compareUsage(a, b model.UsageEvent) int {
	return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.PodUID, b.PodUID), cmp.Compare(a.Event, b.Event),
		cmp.Compare(a.Stream, b.Stream), cmp.Compare(a.Seq, b.Seq))
}
