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
	return s.AcceptDelivery(ctx, d, nil, func() error { return nil })
}

func TestDeliveryAttempts(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	failed := delivery("d-1", 0, model.DeliveryFailed)
	failed.Detail = "the re-read queue is full"
	if err := s.RecordDelivery(ctx, failed, nil); err != nil {
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
	err := s.AcceptDelivery(ctx, delivery("d-1", 2, model.DeliveryAccepted), nil, func() error { ran = true; return nil })
	if !errors.Is(err, ErrAlreadyAccepted) || ran {
		t.Fatalf("a second accepted attempt: %v, act ran %v; want ErrAlreadyAccepted, act not run", err, ran)
	}
	ignored := delivery("d-1", 2, model.DeliveryIgnored)
	ignored.Detail = "duplicate"
	if err := s.RecordDelivery(ctx, ignored, nil); err != nil {
		t.Fatal(err)
	}
	// The same attempt twice is recorded once.
	if err := s.RecordDelivery(ctx, ignored, nil); err != nil {
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
	if err := s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), nil, func() error { return errFull }); err != errFull {
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
	if err := s.RecordDelivery(ctx, delivery("d-1", 0, model.DeliveryFailed), nil); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), nil, func() error { ran = true; return nil })
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
			errs[i] = s.AcceptDelivery(t.Context(), delivery("d-race", i, model.DeliveryAccepted), nil, func() error {
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
		first <- s.AcceptDelivery(ctx, delivery("d-1", 0, model.DeliveryAccepted), nil, func() error {
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
		second <- s.AcceptDelivery(ctx, delivery("d-1", 1, model.DeliveryAccepted), nil, func() error {
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
	if err := s.RecordDelivery(t.Context(), delivery("", 0, model.DeliveryIgnored), nil); err == nil {
		t.Error("a delivery without an id was recorded")
	}
	if err := accept(t.Context(), s, delivery("", 0, model.DeliveryAccepted)); err == nil {
		t.Error("a delivery without an id was accepted")
	}
	if err := s.RecordDelivery(t.Context(), delivery("d-1", 0, model.DeliveryAccepted), nil); err == nil {
		t.Error("RecordDelivery recorded an accepted attempt")
	}
	if err := accept(t.Context(), s, delivery("d-1", 0, model.DeliveryIgnored)); err == nil {
		t.Error("AcceptDelivery recorded an ignored attempt")
	}
}

// githubCoverage is a GitHub read of the root that started at minute.
func githubCoverage(minute int) model.Coverage {
	return model.Coverage{Source: model.SourceGitHub, Instance: testGitHub, LastSuccessfulRead: ptr(at(minute))}
}

// commitRead commits a read of root that left coverage.
func commitRead(t *testing.T, s *Store, root model.Root, coverage ...model.Coverage) {
	t.Helper()
	err := s.Commit(t.Context(), Commit{Batch: &Batch{Root: root, Records: model.Records{Coverage: coverage}}})
	if err != nil {
		t.Fatal(err)
	}
}

// readsOf returns each delivery's id, outcome and next read (minutes
// after testBase, or -1 for none), in order.
func readsOf(ds []model.RootDelivery) [][3]any {
	out := make([][3]any, 0, len(ds))
	for _, d := range ds {
		read := -1
		if d.NextRead != nil {
			read = int(d.NextRead.Sub(testBase) / time.Minute)
		}
		out = append(out, [3]any{d.ID, d.Outcome, read})
	}
	return out
}

// TestDeliveriesOfARoot records attempts naming a task, and reads them
// back with the next read of the task from each one's source: a GitHub
// read answers the GitHub attempts received by the time it started, not
// a later one nor a Vikunja attempt. A repeat of an accepted delivery
// names its roots; an attempt of another root is not the task's.
func TestDeliveriesOfARoot(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	root, other := taskRoot(1), taskRoot(2)
	commitRead(t, s, root, githubCoverage(0))
	both := []model.Root{root, other}
	if err := s.AcceptDelivery(ctx, delivery("d-1", 1, model.DeliveryAccepted), both, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	full := delivery("d-2", 2, model.DeliveryFailed)
	full.Detail = "the queue is full"
	if err := s.RecordDelivery(ctx, full, []model.Root{root}); err != nil {
		t.Fatal(err)
	}
	vikunja := delivery("v-1", 2, model.DeliveryAccepted)
	vikunja.Source, vikunja.Instance = model.SourceVikunja, testVikunja
	if err := s.AcceptDelivery(ctx, vikunja, []model.Root{root}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptDelivery(ctx, delivery("d-other", 3, model.DeliveryAccepted), []model.Root{other}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	commitRead(t, s, root, githubCoverage(3))
	repeat := delivery("d-1", 4, model.DeliveryIgnored)
	repeat.Detail = "duplicate"
	if err := s.RecordDelivery(ctx, repeat, nil); err != nil {
		t.Fatal(err)
	}
	commitRead(t, s, root, githubCoverage(5), vikunjaCoverage(5))

	got, err := s.Deliveries(ctx, root, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]any{
		{"d-1", model.DeliveryAccepted, 3},
		{"d-2", model.DeliveryFailed, 3},
		{"v-1", model.DeliveryAccepted, 5},
		{"d-1", model.DeliveryIgnored, 5},
	}
	if !slices.Equal(readsOf(got), want) {
		t.Errorf("Deliveries = %v, want %v", readsOf(got), want)
	}
	if got[1].Detail != full.Detail || got[0].Event != "check_run" || !got[0].ReceivedAt.Equal(at(1)) {
		t.Errorf("Deliveries[0..1] = %+v, want the attempts as recorded", got[:2])
	}
	latest, err := s.Deliveries(ctx, root, 2)
	if err != nil || !slices.Equal(readsOf(latest), want[2:]) {
		t.Errorf("Deliveries(limit 2) = %v, %v; want the latest two, oldest first", readsOf(latest), err)
	}
}

// TestDeliveriesOfAnUnstoredRoot: a root not stored has no story, so no
// deliveries either, even when an attempt named it.
func TestDeliveriesOfAnUnstoredRoot(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(1)
	if err := s.AcceptDelivery(t.Context(), delivery("d-1", 1, model.DeliveryAccepted), []model.Root{root}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deliveries(t.Context(), root, 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("Deliveries = %v, want ErrNotFound", err)
	}
}

// TestPruneDeletesDeliveryRoots: an attempt pruned takes the roots it
// named with it.
func TestPruneDeletesDeliveryRoots(t *testing.T) {
	s := newTestStore(t, Retention{keep: time.Hour})
	root := taskRoot(1)
	if err := s.AcceptDelivery(t.Context(), delivery("d-1", 0, model.DeliveryAccepted), []model.Root{root}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(t.Context(), at(0).Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM delivery_roots").Scan(&n); err != nil || n != 0 {
		t.Errorf("delivery_roots after the prune: %d rows, %v; want none", n, err)
	}
}
