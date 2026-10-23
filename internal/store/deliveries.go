package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

// ErrAlreadyAccepted is returned for an accepted delivery attempt when an
// earlier attempt of the same delivery was accepted: it is recorded as
// nothing, and should be recorded again as ignored.
var ErrAlreadyAccepted = errors.New("store: an earlier attempt of this delivery was accepted")

// DeliveryAccepted reports whether an attempt of the source instance's
// delivery id was accepted.
func (s *Store) DeliveryAccepted(ctx context.Context, source model.Source, instance, id string) (bool, error) {
	var accepted bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_attempts
		WHERE source = $1 AND instance = $2 AND delivery_id = $3 AND outcome = $4)`,
		string(source), instance, id, string(model.DeliveryAccepted)).Scan(&accepted)
	if err != nil {
		return false, fmt.Errorf("store: delivery %s: %w", id, err)
	}
	return accepted, nil
}

// RecordDelivery records one delivery attempt, apart from every story's
// records. An accepted attempt of a delivery accepted before is not
// recorded: it returns ErrAlreadyAccepted, and only one of two attempts
// racing is accepted. An attempt with the delivery id and the time, to
// the microsecond, of one recorded is taken for it: it is not recorded
// again (and returns ErrAlreadyAccepted if accepted).
func (s *Store) RecordDelivery(ctx context.Context, d model.Delivery) error {
	if err := d.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	var detail *string
	if d.Detail != "" {
		detail = &d.Detail
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO delivery_attempts
		(source, instance, delivery_id, event_type, received_at, outcome, detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING RETURNING id`,
		string(d.Source), d.Instance, d.ID, d.Event, d.ReceivedAt, string(d.Outcome), detail).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && d.Outcome == model.DeliveryAccepted:
		return ErrAlreadyAccepted
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("store: record delivery %s: %w", d.ID, err)
	}
	return nil
}
