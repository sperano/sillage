package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// orphanDeletes delete the records no stored story holds any more, after
// their roots were pruned. Checks, memberships, coverage and per-root
// checkpoints go with their rows (ON DELETE CASCADE).
var orphanDeletes = []string{
	`DELETE FROM correlations c WHERE NOT EXISTS (SELECT 1 FROM root_correlations m WHERE m.correlation_id = c.id)`,
	`DELETE FROM events e WHERE NOT EXISTS (SELECT 1 FROM root_events m WHERE m.event_id = e.id)`,
	`DELETE FROM runs r WHERE NOT EXISTS (SELECT 1 FROM root_runs m WHERE m.run_id = r.id)`,
	`DELETE FROM pull_requests p WHERE NOT EXISTS (SELECT 1 FROM root_pull_requests m WHERE m.pull_request_id = p.id)`,
}

// Prune applies the retention policy at now and returns how many stories
// it deleted. A story goes whole, once its last activity (or, with none,
// its last save) is older than the retention, with the records no other
// story holds and webhook delivery attempts as old: pruning record by
// record would open gaps no coverage records. With the default policy it
// deletes nothing.
func (s *Store) Prune(ctx context.Context, now time.Time) (int64, error) {
	cutoff, ok := s.retention.Cutoff(now)
	if !ok {
		return 0, nil
	}
	var pruned int64
	err := s.write(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM roots WHERE COALESCE(last_activity, updated_at) < $1", cutoff)
		if err != nil {
			return err
		}
		pruned = tag.RowsAffected()
		for _, sql := range orphanDeletes {
			if _, err := tx.Exec(ctx, sql); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "DELETE FROM delivery_attempts WHERE received_at < $1", cutoff)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("store: prune: %w", err)
	}
	return pruned, nil
}

// Retention returns the store's retention policy.
func (s *Store) Retention() Retention { return s.retention }
