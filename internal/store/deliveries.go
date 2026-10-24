package store

import (
	"context"
	"errors"
	"fmt"
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

// AcceptDelivery records d, an accepted attempt, and runs act (asking for
// the reads of the roots it names) in the same transaction, so that of
// attempts of one delivery racing, only one runs act: another waits for
// its outcome. d is recorded only if act succeeds; act's error is
// returned as it is, with nothing recorded. ErrAlreadyAccepted, with act
// not run, says an attempt of the delivery was accepted. act runs while
// the transaction holds the delivery's key, so it must return at once. An
// attempt with the delivery id and the time, to the microsecond, of an
// attempt recorded as anything else cannot be recorded: an error.
func (s *Store) AcceptDelivery(ctx context.Context, d model.Delivery, act func() error) error {
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
	if err := act(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: accept delivery %s: %w", d.ID, err)
	}
	return nil
}

// RecordDelivery records one delivery attempt that was not accepted
// (AcceptDelivery records those), apart from every story's records. An
// attempt with the delivery id and the time, to the microsecond, of one
// recorded is taken for it, and not recorded again.
func (s *Store) RecordDelivery(ctx context.Context, d model.Delivery) error {
	if d.Outcome == model.DeliveryAccepted {
		return fmt.Errorf("store: delivery %s is accepted: AcceptDelivery records it", d.ID)
	}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO delivery_attempts
		(source, instance, delivery_id, event_type, received_at, outcome, detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
		string(d.Source), d.Instance, d.ID, d.Event, d.ReceivedAt, string(d.Outcome), nullable(d.Detail))
	if err != nil {
		return fmt.Errorf("store: record delivery %s: %w", d.ID, err)
	}
	return nil
}
