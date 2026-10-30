package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// rollbackTimeout bounds rolling back an acceptance, which runs even once
// its caller's context is done.
const rollbackTimeout = 5 * time.Second

// ErrAlreadyAccepted is returned by AcceptDelivery when an earlier
// attempt of the same delivery was accepted: nothing is recorded, and the
// attempt should be recorded as ignored.
var ErrAlreadyAccepted = errors.New("store: an earlier attempt of this delivery was accepted")

// DeliveryAccepted reports whether an attempt of the source instance's
// delivery id was accepted.
func (s *Store) DeliveryAccepted(ctx context.Context, source model.Source, instance, id string) (bool, error) {
	var accepted bool
	// The outcome is a literal, as in the partial index it matches.
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_attempts
		WHERE source = $1 AND instance = $2 AND delivery_id = $3 AND outcome = 'accepted')`,
		string(source), instance, id).Scan(&accepted)
	if err != nil {
		return false, fmt.Errorf("store: delivery %s: %w", id, err)
	}
	return accepted, nil
}

// AcceptDelivery records d, an accepted attempt, with the roots it names,
// and runs act (asking for their reads) in the same transaction, so that
// of attempts of one delivery racing, only one runs act: another waits
// for its outcome. d is recorded only if act succeeds; act's error is
// returned as it is, with nothing recorded. ErrAlreadyAccepted, with act
// not run, says an attempt of the delivery was accepted. act runs while
// the transaction holds the delivery's key, so it must return at once. An
// attempt with the delivery id and the time, to the microsecond, of an
// attempt recorded as anything else cannot be recorded: an error.
func (s *Store) AcceptDelivery(ctx context.Context, d model.Delivery, roots []model.Root, act func() error) error {
	if d.Outcome != model.DeliveryAccepted {
		return fmt.Errorf("store: delivery %s is %s, not accepted", d.ID, d.Outcome)
	}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: accept delivery %s: %w", d.ID, err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(ctx) // after a commit, pgx.ErrTxClosed
	}()
	var id int64
	// The conflict target is the partial index of accepted attempts alone,
	// so that a conflict on the time of an attempt recorded otherwise is an
	// error, not taken for an earlier acceptance.
	err = tx.QueryRow(ctx, `INSERT INTO delivery_attempts
		(source, instance, delivery_id, event_type, received_at, outcome, detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (source, instance, delivery_id) WHERE outcome = 'accepted' DO NOTHING RETURNING id`,
		string(d.Source), d.Instance, d.ID, d.Event, d.ReceivedAt, string(d.Outcome), nullable(d.Detail)).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrAlreadyAccepted
	case err != nil:
		return fmt.Errorf("store: accept delivery %s: %w", d.ID, err)
	}
	if err := insertDeliveryRoots(ctx, tx, id, roots); err != nil {
		return fmt.Errorf("store: accept delivery %s: %w", d.ID, err)
	}
	if err := act(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: accept delivery %s: %w", d.ID, err)
	}
	return nil
}

// RecordDelivery records one delivery attempt that was not accepted
// (AcceptDelivery records those), apart from every story's records, with
// the roots it names and those an accepted attempt of the same delivery
// named: a repeat of a delivery is shown in the stories of the roots the
// delivery asked to read. An attempt with the delivery id and the time,
// to the microsecond, of one recorded is taken for it, and not recorded
// again.
func (s *Store) RecordDelivery(ctx context.Context, d model.Delivery, roots []model.Root) error {
	if d.Outcome == model.DeliveryAccepted {
		return fmt.Errorf("store: delivery %s is accepted: AcceptDelivery records it", d.ID)
	}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	// Not s.write: a delivery is not held up by the saves' lock.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO delivery_attempts
			(source, instance, delivery_id, event_type, received_at, outcome, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING RETURNING id`,
			string(d.Source), d.Instance, d.ID, d.Event, d.ReceivedAt, string(d.Outcome), nullable(d.Detail)).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		if err := insertDeliveryRoots(ctx, tx, id, roots); err != nil {
			return err
		}
		// The outcome is a literal, as in the partial index it matches.
		_, err = tx.Exec(ctx, `INSERT INTO delivery_roots (attempt_id, root_ref)
			SELECT $1, r.root_ref FROM delivery_roots r JOIN delivery_attempts a ON a.id = r.attempt_id
			WHERE a.source = $2 AND a.instance = $3 AND a.delivery_id = $4 AND a.outcome = 'accepted'
			ON CONFLICT DO NOTHING`, id, string(d.Source), d.Instance, d.ID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: record delivery %s: %w", d.ID, err)
	}
	return nil
}

// insertDeliveryRoots records that the attempt named roots.
func insertDeliveryRoots(ctx context.Context, tx pgx.Tx, attempt int64, roots []model.Root) error {
	for _, root := range roots {
		if err := root.Validate(); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO delivery_roots (attempt_id, root_ref) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, attempt, rootRef(root).String())
		if err != nil {
			return err
		}
	}
	return nil
}

// markDeliveryReads records, for each source instance root's coverage
// says was read, that read as the next read of every attempt naming root
// from that instance that was received by the time the read started and
// had none yet.
func markDeliveryReads(ctx context.Context, tx pgx.Tx, root model.Root, coverage []model.Coverage) error {
	for _, c := range coverage {
		if c.LastSuccessfulRead == nil {
			continue
		}
		_, err := tx.Exec(ctx, `UPDATE delivery_roots r SET read_at = $1
			FROM delivery_attempts a
			WHERE a.id = r.attempt_id AND r.root_ref = $2 AND r.read_at IS NULL
				AND a.source = $3 AND a.instance = $4 AND a.received_at <= $1`,
			*c.LastSuccessfulRead, rootRef(root).String(), string(c.Source), c.Instance)
		if err != nil {
			return fmt.Errorf("next read of the deliveries from %s: %w", c.Key(), err)
		}
	}
	return nil
}

// Deliveries returns the latest limit delivery attempts that named root,
// oldest first, each with the next read of root from its source, or
// ErrNotFound when root is not stored. A limit of zero or less returns
// none.
func (s *Store) Deliveries(ctx context.Context, root model.Root, limit int) ([]model.RootDelivery, error) {
	if err := root.Validate(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var out []model.RootDelivery
	err := s.read(ctx, func(tx pgx.Tx) error {
		if _, err := rootID(ctx, tx, root); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.source, a.instance, a.delivery_id, a.event_type, a.received_at,
				a.outcome, COALESCE(a.detail, ''), r.read_at
			FROM delivery_roots r JOIN delivery_attempts a ON a.id = r.attempt_id
			WHERE r.root_ref = $1
			ORDER BY a.received_at DESC, a.source DESC, a.instance DESC, a.delivery_id DESC, a.id DESC
			LIMIT $2`, rootRef(root).String(), max(limit, 0))
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, scanRootDelivery)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store: deliveries of %s: %w", rootRef(root), err)
	}
	slices.Reverse(out)
	return out, nil
}

func scanRootDelivery(row pgx.CollectableRow) (model.RootDelivery, error) {
	var d model.RootDelivery
	var source, outcome string
	err := row.Scan(&source, &d.Instance, &d.ID, &d.Event, &d.ReceivedAt, &outcome, &d.Detail, &d.NextRead)
	if err != nil {
		return d, err
	}
	d.Source, d.Outcome = model.Source(source), model.DeliveryOutcome(outcome)
	d.ReceivedAt = d.ReceivedAt.UTC()
	if d.NextRead != nil {
		read := d.NextRead.UTC()
		d.NextRead = &read
	}
	return d, d.Validate()
}
