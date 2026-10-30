package reconcile

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// testClock runs ahead of the host's clock by offset, which a test
// advances.
type testClock struct{ offset time.Duration }

func (c *testClock) now() time.Time { return time.Now().Add(c.offset) }

// useClock has h's loop read the time from a testClock, and returns it.
func useClock(h *harness) *testClock {
	c := &testClock{}
	h.loop.now = c.now
	return c
}

// endBackoff ends the back-off of the root, so that the next cycle reads
// it if it is due.
func endBackoff(h *harness) { h.loop.backoffs[testRoot().Ref()].until = time.Time{} }

// backoffCycle is a cycle interval the back-off starts at, and a root
// due at every cycle whatever the back-off.
var backoffCycle = Config{
	Interval: time.Minute, RefreshInterval: time.Nanosecond, BackstopInterval: 24 * time.Hour,
	LockRetryInterval: time.Millisecond,
}

// A root whose commit keeps failing is not read again, GitHub included,
// until its back-off is over, which doubles at each failure; a commit
// that succeeds ends it.
func TestCommitFailureBacksOff(t *testing.T) {
	h, gh := githubHarness(t, backoffCycle)
	clock := useClock(h)
	h.store.setCommitErr(errors.New("fabricated retired source"))
	reads := func(want int, when string) {
		t.Helper()
		fetches, _ := gh.counts()
		if n := h.vikunja.count(); n != want || fetches != want {
			t.Errorf("%s: task read %d times and GitHub %d, want %d", when, n, fetches, want)
		}
	}
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	reads(1, "within the first back-off")
	if got := testutil.ToFloat64(h.loop.metrics.backoffRoots); got != 1 {
		t.Errorf("roots backing off = %v, want 1", got)
	}
	if !strings.Contains(h.logs.String(), "backoff=1") {
		t.Errorf("the cycle's log line does not count the root backing off:\n%s", h.logs)
	}

	clock.offset += backoffCycle.Interval
	h.loop.cycle(t.Context())
	reads(2, "once the first back-off is over")
	clock.offset += backoffCycle.Interval
	h.loop.cycle(t.Context())
	reads(2, "within the second back-off, twice as long")
	if n := strings.Count(h.logs.String(), "backing off"); n != 1 {
		t.Errorf("the back-off was logged %d times, want once while its error stays the same", n)
	}

	clock.offset += backoffCycle.Interval
	h.store.setCommitErr(nil)
	h.loop.cycle(t.Context())
	reads(3, "once the second back-off is over")
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 4 {
		t.Errorf("task read %d times after a commit succeeded, want again at the next refresh", n)
	}
	if got := testutil.ToFloat64(h.loop.metrics.backoffRoots); got != 0 {
		t.Errorf("roots backing off = %v after a successful commit, want 0", got)
	}
	if !strings.Contains(h.logs.String(), "back-off is over") {
		t.Errorf("the end of the back-off is not logged:\n%s", h.logs)
	}
}

// A delivery naming a root that backs off is read once the back-off is
// over, though nothing else makes the root due.
func TestDeliveryDuringBackoffIsReadAfter(t *testing.T) {
	cfg := rarely
	cfg.Interval = backoffCycle.Interval
	h, gh := githubHarness(t, cfg)
	clock := useClock(h)
	h.loop.cycle(t.Context())
	h.store.setCommitErr(errors.New("fabricated database outage"))
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	h.store.setCommitErr(nil)
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 2 {
		t.Fatalf("GitHub read %d times, want the first read and the failed delivery's", fetches)
	}
	clock.offset += cfg.Interval
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 3 {
		t.Errorf("GitHub read %d times once the back-off is over, want the delivery's read", fetches)
	}
	if got := githubReads(h, triggerDelivery); got != 2 {
		t.Errorf("GitHub reads for a delivery = %v, want the failed one and the one after the back-off", got)
	}
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 3 {
		t.Errorf("GitHub read %d times, want the delivery read once", fetches)
	}
}

func TestBackoffWait(t *testing.T) {
	for _, tc := range []struct {
		base     time.Duration
		failures int
		want     time.Duration
	}{
		{time.Minute, 1, time.Minute},
		{time.Minute, 2, 2 * time.Minute},
		{time.Minute, 4, 8 * time.Minute},
		{time.Minute, 9, maxBackoff},
		{time.Minute, 1000, maxBackoff},
		{2 * maxBackoff, 1, maxBackoff},
	} {
		if got := backoffWait(tc.base, tc.failures); got != tc.want {
			t.Errorf("backoffWait(%v, %d) = %v, want %v", tc.base, tc.failures, got, tc.want)
		}
	}
}
