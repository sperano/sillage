package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// taskRecordTTL is how long Kelos keeps a TaskRecord (Kelos 0.60.0's
// defaultTaskRecordTTL): a run can be read from its record alone for that
// long after its Task was first seen, so its origin is kept at least as
// long, whatever the retention.
const taskRecordTTL = 30 * 24 * time.Hour

// KeepOrigins stores, by run, what started each Kelos Task (the Kelos
// source's Origin, as a JSON object), unless the store already holds that
// run's: the first read that saw the Task wins. Each run must be keyed by
// Task UID.
func (s *Store) KeepOrigins(ctx context.Context, origins map[model.RunKey][]byte) error {
	if len(origins) == 0 {
		return nil
	}
	var clusters, namespaces, uids, bodies []string
	for _, k := range sortedRunKeys(origins) {
		if err := validateOriginKey(k); err != nil {
			return err
		}
		body := origins[k]
		if !json.Valid(body) {
			return fmt.Errorf("store: keep origins: run %s: the origin is not JSON", k)
		}
		clusters, namespaces, uids = append(clusters, k.Cluster), append(namespaces, k.Namespace), append(uids, k.UID)
		bodies = append(bodies, string(body))
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO run_origins (cluster, namespace, uid, origin)
		SELECT c, n, u, o::jsonb FROM unnest($1::text[], $2::text[], $3::text[], $4::text[]) AS t(c, n, u, o)
		ON CONFLICT (cluster, namespace, uid) DO NOTHING`,
		clusters, namespaces, uids, bodies)
	if err != nil {
		return fmt.Errorf("store: keep origins: %w", err)
	}
	return nil
}

// RecallOrigins returns the origins kept of runs, by run; a run with none
// is left out.
func (s *Store) RecallOrigins(ctx context.Context, runs []model.RunKey) (map[model.RunKey][]byte, error) {
	out := make(map[model.RunKey][]byte)
	if len(runs) == 0 {
		return out, nil
	}
	var clusters, namespaces, uids []string
	for _, k := range runs {
		if err := validateOriginKey(k); err != nil {
			return nil, err
		}
		clusters, namespaces, uids = append(clusters, k.Cluster), append(namespaces, k.Namespace), append(uids, k.UID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT o.cluster, o.namespace, o.uid, o.origin FROM run_origins o
		JOIN unnest($1::text[], $2::text[], $3::text[]) AS t(c, n, u)
			ON o.cluster = t.c AND o.namespace = t.n AND o.uid = t.u`,
		clusters, namespaces, uids)
	if err != nil {
		return nil, fmt.Errorf("store: recall origins: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k model.RunKey
		var body []byte
		if err := rows.Scan(&k.Cluster, &k.Namespace, &k.UID, &body); err != nil {
			return nil, fmt.Errorf("store: recall origins: %w", err)
		}
		out[k] = body
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: recall origins: %w", err)
	}
	return out, nil
}

// originsCutoff is the time before which Prune deletes the origins first
// seen: the retention's cutoff, or earlier while a TaskRecord of a run
// first seen after it can still be read.
func originsCutoff(cutoff, now time.Time) time.Time {
	if recordGone := now.Add(-taskRecordTTL); recordGone.Before(cutoff) {
		return recordGone
	}
	return cutoff
}

func validateOriginKey(k model.RunKey) error {
	if err := k.Validate(); err != nil {
		return fmt.Errorf("store: origin: %w", err)
	}
	if k.UID == "" {
		return errors.New("store: origin: run " + k.String() + " is not keyed by Task UID")
	}
	return nil
}

// sortedRunKeys returns the keys of m in string order, so that inserts
// always take their row locks in the same order.
func sortedRunKeys(m map[model.RunKey][]byte) []model.RunKey {
	return slices.SortedFunc(maps.Keys(m), func(a, b model.RunKey) int { return strings.Compare(a.String(), b.String()) })
}
