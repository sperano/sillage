package reconcile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// runFor runs the loop under lock until it has tried the lock attempts
// times, then stops it.
func runFor(t *testing.T, h *harness, lock *fakeLocker, attempts int) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.loop.Run(ctx, lock)
	}()
	deadline := time.After(time.Minute)
	for lock.count() < attempts {
		select {
		case <-deadline:
			t.Fatalf("the loop tried the lock %d times, want %d", lock.count(), attempts)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}

func TestStandbyReadsNothingAndSaysWhy(t *testing.T) {
	h := newHarness(t, everyCycle)
	lock := &fakeLocker{held: true}
	runFor(t, h, lock, 3)
	if n := h.vikunja.count(); n != 0 {
		t.Errorf("a standby read Vikunja %d times, want never", n)
	}
	if n := len(h.store.commits); n != 0 {
		t.Errorf("a standby committed %d times, want never", n)
	}
	if got := strings.Count(h.logs.String(), "standing by"); got != 1 {
		t.Errorf("logs say %d times why the process reads nothing, want once:\n%s", got, h.logs)
	}
	if got := testutil.ToFloat64(h.loop.metrics.lockHeld); got != 0 {
		t.Errorf("lock held = %v, want 0", got)
	}
}

func TestRunCollectsWhileItHoldsTheLock(t *testing.T) {
	h := newHarness(t, everyCycle)
	lock := &fakeLocker{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.loop.Run(ctx, lock)
	}()
	deadline := time.After(time.Minute)
	for h.vikunja.count() < 2 {
		select {
		case <-deadline:
			t.Fatal("the loop holding the lock did not collect twice")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if lock.count() != 1 {
		t.Errorf("lock taken %d times, want once", lock.count())
	}
}
