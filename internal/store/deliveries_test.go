package store

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func delivery(id string, minute int, outcome model.DeliveryOutcome) model.Delivery {
	return model.Delivery{
		Source: model.SourceGitHub, Instance: testGitHub, ID: id, Event: "check_run",
		ReceivedAt: at(minute), Outcome: outcome,
	}
}

// attempts returns the outcome and detail of each recorded attempt of id,
// in the order received.
func attempts(t *testing.T, s *Store, id string) [][2]string {
	t.Helper()
	rows, err := s.pool.Query(t.Context(), `SELECT outcome, COALESCE(detail, '') FROM delivery_attempts
		WHERE delivery_id = $1 ORDER BY received_at`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var o, d string
		if err := rows.Scan(&o, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{o, d})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordDelivery(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	failed := delivery("d-1", 0, model.DeliveryFailed)
	failed.Detail = "the re-read queue is full"
	if err := s.RecordDelivery(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.DeliveryAccepted(ctx, model.SourceGitHub, testGitHub, "d-1"); err != nil || ok {
		t.Fatalf("DeliveryAccepted after a failed attempt = %v, %v; want false", ok, err)
	}
	if err := s.RecordDelivery(ctx, delivery("d-1", 1, model.DeliveryAccepted)); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.DeliveryAccepted(ctx, model.SourceGitHub, testGitHub, "d-1"); err != nil || !ok {
		t.Fatalf("DeliveryAccepted = %v, %v; want true", ok, err)
	}
	if err := s.RecordDelivery(ctx, delivery("d-1", 2, model.DeliveryAccepted)); !errors.Is(err, ErrAlreadyAccepted) {
		t.Fatalf("a second accepted attempt: %v, want ErrAlreadyAccepted", err)
	}
	ignored := delivery("d-1", 2, model.DeliveryIgnored)
	ignored.Detail = "duplicate"
	if err := s.RecordDelivery(ctx, ignored); err != nil {
		t.Fatal(err)
	}
	// The same attempt twice is recorded once.
	if err := s.RecordDelivery(ctx, ignored); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"failed", "the re-read queue is full"}, {"accepted", ""}, {"ignored", "duplicate"}}
	if got := attempts(t, s, "d-1"); !slices.Equal(got, want) {
		t.Errorf("attempts = %q, want %q", got, want)
	}
	other := delivery("d-1", 3, model.DeliveryAccepted)
	other.Source, other.Instance = model.SourceVikunja, testVikunja
	if err := s.RecordDelivery(ctx, other); err != nil {
		t.Errorf("the same id from another source: %v, want it accepted", err)
	}
}

func TestRecordDeliveryRace(t *testing.T) {
	s := newTestStore(t, Retention{})
	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() { errs[i] = s.RecordDelivery(t.Context(), delivery("d-race", i, model.DeliveryAccepted)) })
	}
	wg.Wait()
	accepted := 0
	for _, err := range errs {
		switch {
		case err == nil:
			accepted++
		case !errors.Is(err, ErrAlreadyAccepted):
			t.Errorf("RecordDelivery: %v", err)
		}
	}
	if accepted != 1 {
		t.Errorf("%d racing attempts were accepted, want 1", accepted)
	}
}

func TestRecordDeliveryInvalid(t *testing.T) {
	s := newTestStore(t, Retention{})
	if err := s.RecordDelivery(t.Context(), delivery("", 0, model.DeliveryAccepted)); err == nil {
		t.Error("a delivery without an id was recorded")
	}
}
