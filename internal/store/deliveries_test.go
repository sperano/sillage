package store

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// accept is AcceptDelivery with an act that succeeds.
func accept(ctx context.Context, s *Store, d model.Delivery) error {
	return s.AcceptDelivery(ctx, d, func() error { return nil })
}

func TestDeliveryAttempts(t *testing.T) {
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
	if err := accept(ctx, s, delivery("d-1", 1, model.DeliveryAccepted)); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.DeliveryAccepted(ctx, model.SourceGitHub, testGitHub, "d-1"); err != nil || !ok {
		t.Fatalf("DeliveryAccepted = %v, %v; want true", ok, err)
	}
	ran := false
	err := s.AcceptDelivery(ctx, delivery("d-1", 2, model.DeliveryAccepted), func() error { ran = true; return nil })
	if !errors.Is(err, ErrAlreadyAccepted) || ran {
		t.Fatalf("a second accepted attempt: %v, act ran %v; want ErrAlreadyAccepted, act not run", err, ran)
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
	if err := accept(ctx, s, other); err != nil {
		t.Errorf("the same id from another source: %v, want it accepted", err)
	}
}

func TestAcceptDeliveryActFails(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	errFull := errors.New("queue full")
	// act's error is returned as it is, not wrapped.
	if err := s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), func() error { return errFull }); err != errFull {
		t.Fatalf("AcceptDelivery = %v, want act's error as it is", err)
	}
	if got := attempts(t, s, "d-1"); len(got) != 0 {
		t.Fatalf("attempts = %q, want nothing recorded when act fails", got)
	}
	if err := accept(ctx, s, delivery("d-1", 1, model.DeliveryAccepted)); err != nil {
		t.Errorf("a retry after act failed: %v, want it accepted", err)
	}
}

// An accepted attempt at the time of a failed one is not taken for an
// earlier acceptance.
func TestAcceptDeliveryAtTheTimeOfAnotherAttempt(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	if err := s.RecordDelivery(ctx, delivery("d-1", 0, model.DeliveryFailed)); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), func() error { ran = true; return nil })
	if err == nil || errors.Is(err, ErrAlreadyAccepted) || ran {
		t.Errorf("AcceptDelivery = %v, act ran %v; want an error other than ErrAlreadyAccepted, act not run", err, ran)
	}
}

// actHold keeps each racing act running, so that the racers overlap.
const actHold = 20 * time.Millisecond

func TestAcceptDeliveryRace(t *testing.T) {
	s := newTestStore(t, Retention{})
	const racers = 8
	errs := make([]error, racers)
	var acts atomic.Int32
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() {
			errs[i] = s.AcceptDelivery(t.Context(), delivery("d-race", i, model.DeliveryAccepted), func() error {
				acts.Add(1)
				time.Sleep(actHold)
				return nil
			})
		})
	}
	wg.Wait()
	accepted := 0
	for _, err := range errs {
		switch {
		case err == nil:
			accepted++
		case !errors.Is(err, ErrAlreadyAccepted):
			t.Errorf("AcceptDelivery: %v", err)
		}
	}
	if accepted != 1 || acts.Load() != 1 {
		t.Errorf("%d racing attempts were accepted and %d acts ran, want 1 and 1", accepted, acts.Load())
	}
}

const (
	// waitDeadline bounds each wait on a racing attempt: to reach its act,
	// to wait on another, or to return.
	waitDeadline = 10 * time.Second
	// lockWaitPoll is how often an attempt waiting on another is looked
	// for.
	lockWaitPoll = 10 * time.Millisecond
)

// An attempt racing one whose act is running waits for its outcome, and
// is accepted once that act fails.
func TestAcceptDeliveryWaitsForARacingAttempt(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	inAct, release := make(chan struct{}), make(chan struct{})
	releaseFirst := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseFirst)
	errFull := errors.New("queue full")
	first := make(chan error, 1)
	go func() {
		first <- s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), func() error {
			close(inAct)
			<-release
			return errFull
		})
	}()
	select {
	case <-inAct:
	case err := <-first:
		t.Fatalf("the first attempt returned before its act: %v", err)
	case <-time.After(waitDeadline):
		t.Fatal("the first attempt never reached its act")
	}
	var ran atomic.Bool
	second := make(chan error, 1)
	go func() {
		second <- s.AcceptDelivery(ctx, delivery("d-1", 1, model.DeliveryAccepted), func() error {
			ran.Store(true)
			return nil
		})
	}()
	waitForLockWait(t, s)
	if ran.Load() {
		t.Fatal("the second attempt's act ran while the first was undecided")
	}
	releaseFirst()
	if err := awaitAttempt(t, first, "the first attempt"); err != errFull {
		t.Fatalf("the first attempt: %v, want its act's error", err)
	}
	if err := awaitAttempt(t, second, "the second attempt"); err != nil || !ran.Load() {
		t.Errorf("the second attempt: %v, act ran %v; want it accepted once the first failed", err, ran.Load())
	}
}

// awaitAttempt returns what an attempt returned, failing the test if it
// has not returned within waitDeadline.
func awaitAttempt(t *testing.T, result <-chan error, attempt string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(waitDeadline):
		t.Fatalf("%s did not return", attempt)
		return nil
	}
}

// waitForLockWait waits until an insert of a delivery attempt waits on a
// lock.
func waitForLockWait(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(waitDeadline)
	for {
		var waiting bool
		err := s.pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'INSERT INTO delivery_attempts%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no attempt waited on the one undecided")
		}
		time.Sleep(lockWaitPoll)
	}
}

func TestRecordDeliveryInvalid(t *testing.T) {
	s := newTestStore(t, Retention{})
	if err := s.RecordDelivery(t.Context(), delivery("", 0, model.DeliveryIgnored)); err == nil {
		t.Error("a delivery without an id was recorded")
	}
	if err := accept(t.Context(), s, delivery("", 0, model.DeliveryAccepted)); err == nil {
		t.Error("a delivery without an id was accepted")
	}
	if err := s.RecordDelivery(t.Context(), delivery("d-1", 0, model.DeliveryAccepted)); err == nil {
		t.Error("RecordDelivery recorded an accepted attempt")
	}
	if err := accept(t.Context(), s, delivery("d-1", 0, model.DeliveryIgnored)); err == nil {
		t.Error("AcceptDelivery recorded an ignored attempt")
	}
}
