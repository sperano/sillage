package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// MarkerResult is what RecordMarker did with a marker.
type MarkerResult string

// The results of RecordMarker.
const (
	// MarkerStored: the marker is new, and stored.
	MarkerStored MarkerResult = "stored"
	// MarkerDuplicate: the store holds the same marker under its pod UID,
	// span and edge; nothing changed.
	MarkerDuplicate MarkerResult = "duplicate"
	// MarkerConflict: the store holds a different marker under its pod
	// UID, span and edge, and keeps it.
	MarkerConflict MarkerResult = "conflict"
	// MarkerLimited: the store holds the limit of markers of its pod; it
	// is refused, and the pod's refusal noted.
	MarkerLimited MarkerResult = "limited"
	// markerFailed: what RecordMarker returns with an error; the marker
	// may not be stored.
	markerFailed MarkerResult = ""
)

const (
	// unattachedNote is the Kelos coverage's note of the markers joined to
	// no run in its cluster, after their count.
	unattachedNote = " phase markers from pods no run is known for"
	// limitedNote is the Kelos coverage's note of a pod of one of its runs
	// whose markers passed the limit per pod, before the pod's name.
	limitedNote = "phase marker limit reached: later markers refused for pod "
)

// RecordMarker stores m, a marker as received (its Join unset), unless the
// store holds one of its pod UID, span and edge already, or perPod of its
// pod. A new marker is joined to its run at once if the store knows it,
// and again by each save of Kelos records of the pod or the Task name it
// names until its pod joins it. Re-sent markers come back
// MarkerDuplicate or MarkerConflict, even past the limit.
func (s *Store) RecordMarker(ctx context.Context, m model.Marker, perPod int) (MarkerResult, error) {
	if m.Join != nil {
		return markerFailed, errors.New("store: a marker is recorded before it is joined")
	}
	if err := m.Validate(); err != nil {
		return markerFailed, fmt.Errorf("store: %w", err)
	}
	var result MarkerResult
	// The save lock orders the marker with the saves that join it: a save
	// committed before sees it, a save after joins it.
	err := s.write(ctx, func(tx pgx.Tx) error {
		var err error
		result, err = recordMarker(ctx, tx, m, perPod)
		return err
	})
	if err != nil {
		return markerFailed, fmt.Errorf("store: record marker %s %s of pod %s: %w", m.Report.Span, m.Report.Edge,
			m.Sender.PodUID, err)
	}
	return result, nil
}

func recordMarker(ctx context.Context, tx pgx.Tx, m model.Marker, perPod int) (MarkerResult, error) {
	r, snd := m.Report, m.Sender
	var body []byte
	err := tx.QueryRow(ctx, "SELECT body FROM markers WHERE pod_uid = $1 AND span = $2 AND edge = $3",
		snd.PodUID, r.Span, string(r.Edge)).Scan(&body)
	switch {
	case err == nil:
		return compareHeld(body, m)
	case !errors.Is(err, pgx.ErrNoRows):
		return markerFailed, err
	}
	var held int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM markers WHERE pod_uid = $1", snd.PodUID).Scan(&held); err != nil {
		return markerFailed, err
	}
	if held >= perPod {
		return MarkerLimited, noteLimit(ctx, tx, m)
	}
	if body, err = json.Marshal(m); err != nil {
		return markerFailed, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO markers
		(cluster, namespace, pod, pod_uid, span, edge, task_name, at, received_at, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		snd.Cluster, snd.Namespace, snd.Pod, snd.PodUID, r.Span, string(r.Edge), nullable(r.TaskName),
		r.At, m.ReceivedAt, body).Scan(&id)
	if err != nil {
		return markerFailed, err
	}
	j := newJoiner(ctx, tx, snd.Cluster, snd.Namespace)
	return MarkerStored, j.rejoin(`id = $3`, id)
}

// compareHeld compares the marker held under m's key with m: the same
// report from the same sender is a duplicate, whenever it was received.
func compareHeld(body []byte, m model.Marker) (MarkerResult, error) {
	var held model.Marker
	if err := json.Unmarshal(body, &held); err != nil {
		return markerFailed, err
	}
	if held.Report.Equal(m.Report) && held.Sender == m.Sender {
		return MarkerDuplicate, nil
	}
	return MarkerConflict, nil
}

// noteLimit records that a marker of m's pod was refused for the limit.
func noteLimit(ctx context.Context, tx pgx.Tx, m model.Marker) error {
	snd := m.Sender
	_, err := tx.Exec(ctx, `INSERT INTO marker_limits
		(pod_uid, cluster, namespace, pod, first_refused_at, last_refused_at, refused)
		VALUES ($1, $2, $3, $4, $5, $5, 1)
		ON CONFLICT (pod_uid) DO UPDATE SET refused = marker_limits.refused + 1,
			last_refused_at = GREATEST(marker_limits.last_refused_at, EXCLUDED.last_refused_at)`,
		snd.PodUID, snd.Cluster, snd.Namespace, snd.Pod, m.ReceivedAt)
	return err
}

// RunMarkers returns the markers joined to runs, each with its join, in
// the order of their pod UID, span and edge. A run must be keyed by Task
// UID.
func (s *Store) RunMarkers(ctx context.Context, runs []model.RunKey) ([]model.Marker, error) {
	var clusters, namespaces, uids []string
	for _, k := range runs {
		if err := k.Validate(); err != nil || k.UID == "" {
			return nil, fmt.Errorf("store: run markers: run %s is not keyed by Task UID", k)
		}
		clusters, namespaces, uids = append(clusters, k.Cluster), append(namespaces, k.Namespace), append(uids, k.UID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.cluster, m.namespace, m.run_uid, m.confidence, m.join_evidence, m.body FROM markers m
		JOIN unnest($1::text[], $2::text[], $3::text[]) AS t(c, n, u)
			ON m.cluster = t.c AND m.namespace = t.n AND m.run_uid = t.u
		ORDER BY m.pod_uid, m.span, m.edge DESC`,
		clusters, namespaces, uids)
	if err != nil {
		return nil, fmt.Errorf("store: run markers: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanJoinedMarker)
	if err != nil {
		return nil, fmt.Errorf("store: run markers: %w", err)
	}
	return out, nil
}

func scanJoinedMarker(row pgx.CollectableRow) (model.Marker, error) {
	var m model.Marker
	var j model.MarkerJoin
	var confidence string
	var evidence, body []byte
	err := row.Scan(&j.Run.Cluster, &j.Run.Namespace, &j.Run.UID, &confidence, &evidence, &body)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, err
	}
	if err := json.Unmarshal(evidence, &j.Evidence); err != nil {
		return m, err
	}
	j.Confidence = model.Confidence(confidence)
	m.Join = &j
	return m, nil
}

// addMarkerNotes adds to the root's Kelos coverage what its markers leave
// unknown: how many markers of its cluster no run is known for, and which
// pods of its runs passed the limit of markers.
func addMarkerNotes(ctx context.Context, tx pgx.Tx, root int64, coverage []model.Coverage) error {
	for i := range coverage {
		c := &coverage[i]
		if c.Source != model.SourceKelos {
			continue
		}
		var unattached int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM markers WHERE cluster = $1 AND run_uid IS NULL",
			c.Instance).Scan(&unattached); err != nil {
			return err
		}
		if unattached > 0 {
			c.Notes = append(c.Notes, fmt.Sprint(unattached)+unattachedNote)
		}
		pods, err := limitedPods(ctx, tx, root, c.Instance)
		if err != nil {
			return err
		}
		for _, pod := range pods {
			c.Notes = append(c.Notes, limitedNote+pod)
		}
		slices.Sort(c.Notes)
	}
	return nil
}

// limitedPods returns the pods, joined to the root's runs in cluster, that
// passed the limit of markers.
func limitedPods(ctx context.Context, tx pgx.Tx, root int64, cluster string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT l.pod FROM marker_limits l
		JOIN markers m ON m.pod_uid = l.pod_uid
		JOIN runs r ON r.cluster = m.cluster AND r.namespace = m.namespace AND r.uid = m.run_uid
		JOIN root_runs rr ON rr.run_id = r.id
		WHERE rr.root_id = $1 AND m.cluster = $2 ORDER BY l.pod`, root, cluster)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// pruneMarkers deletes the markers received before cutoff, the limits
// last reached before it, and the pods of runs first seen before the
// origins' cutoff, which outlives a TaskRecord.
func pruneMarkers(ctx context.Context, tx pgx.Tx, cutoff, now time.Time) error {
	for _, q := range []struct {
		sql   string
		since time.Time
	}{
		{"DELETE FROM markers WHERE received_at < $1", cutoff},
		{"DELETE FROM marker_limits WHERE last_refused_at < $1", cutoff},
		{"DELETE FROM run_pods WHERE first_seen_at < $1", originsCutoff(cutoff, now)},
	} {
		if _, err := tx.Exec(ctx, q.sql, q.since); err != nil {
			return err
		}
	}
	return nil
}
