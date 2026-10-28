package reconcile

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
)

// fakeDeliveries queues the roots the tests' deliveries name, as
// *ingest.Queue does, but keeps a root named twice twice: the loop's own
// coalescing is under test.
type fakeDeliveries struct {
	mu    sync.Mutex
	roots []model.Root
	ready chan struct{}
}

func newFakeDeliveries() *fakeDeliveries { return &fakeDeliveries{ready: make(chan struct{}, 1)} }

func (f *fakeDeliveries) deliver(roots ...model.Root) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roots = append(f.roots, roots...)
	f.signal()
}

func (f *fakeDeliveries) Ready() <-chan struct{} { return f.ready }

func (f *fakeDeliveries) Take(limit int) []model.Root {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.roots))
	out := slices.Clone(f.roots[:n])
	f.roots = f.roots[n:]
	if len(f.roots) == 0 {
		select {
		case <-f.ready:
		default:
		}
		return out
	}
	f.signal()
	return out
}

func (f *fakeDeliveries) signal() {
	select {
	case f.ready <- struct{}{}:
	default:
	}
}

func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: id}}
}

func (h *harness) deliveryReads(kind string) float64 {
	return testutil.ToFloat64(h.loop.metrics.deliveryReads.WithLabelValues(kind))
}

func TestDeliveryMakesTheRootDue(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 1 {
		t.Fatalf("task read %d times before any delivery, want once: nothing says it changed", n)
	}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 2 {
		t.Errorf("task read %d times, want again once a delivery named it", n)
	}
	if got := h.deliveryReads(deliveryCaused); got != 1 {
		t.Errorf("reads deliveries caused = %v, want 1", got)
	}
	if !strings.Contains(h.logs.String(), "delivered=1") {
		t.Errorf("the cycle's log does not count the delivered root:\n%s", h.logs)
	}
}

func TestDeliveryReadsARootNoListingNames(t *testing.T) {
	h := newHarness(t, hourly)
	h.vikunja.also = []int64{testTaskID + 1}
	h.deliveries.deliver(taskRoot(testTaskID + 1))
	h.loop.cycle(t.Context())
	if s := h.store.story(t, taskRoot(testTaskID+1)); len(s.Events) == 0 {
		t.Errorf("the delivered task's story = %+v, want its events", s.Records)
	}
}

func TestDeliveriesOfOneRootShareARead(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	h.deliveries.deliver(testRoot(), testRoot())
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 2 {
		t.Errorf("task read %d times, want once more for both deliveries", n)
	}
	if got := h.deliveryReads(deliveryCaused); got != 1 {
		t.Errorf("reads deliveries caused = %v, want 1", got)
	}
}

func TestDeliveryOfARootDueAnywayIsCoalesced(t *testing.T) {
	h := newHarness(t, hourly)
	// The root is new to the loop: Kelos names it, so it is due anyway.
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 1 {
		t.Errorf("task read %d times, want once for Kelos and the delivery", n)
	}
	if caused, coalesced := h.deliveryReads(deliveryCaused), h.deliveryReads(deliveryCoalesced); caused != 0 ||
		coalesced != 1 {
		t.Errorf("reads deliveries caused = %v, coalesced = %v; want 0 and 1", caused, coalesced)
	}
}

func TestDeliveriesAreCappedPerCycle(t *testing.T) {
	cfg := hourly
	cfg.DeliveryRootsPerCycle = 2
	h := newHarness(t, cfg)
	h.vikunja.also = []int64{43, 44, 45}
	h.deliveries.deliver(taskRoot(43), taskRoot(44), taskRoot(45))
	h.loop.cycle(t.Context())
	if got, want := h.vikunja.read(), []int64{43, 44, testTaskID}; !slices.Equal(got, want) {
		t.Errorf("first cycle read %v, want the first two delivered, then the root Kelos names: %v", got, want)
	}
	select {
	case <-h.deliveries.Ready():
	default:
		t.Fatal("the delivery past the cap is not left queued")
	}
	h.loop.cycle(t.Context())
	if got, want := h.vikunja.read()[3:], []int64{45}; !slices.Equal(got, want) {
		t.Errorf("second cycle read %v, want the delivery past the cap %v", got, want)
	}
}

// waitFor runs the loop's wait between two cycles, and returns how long
// it took.
func waitFor(t *testing.T, l *Loop) time.Duration {
	t.Helper()
	start := time.Now()
	if !l.wait(t.Context()) {
		t.Fatal("the wait ended as if the loop were stopping")
	}
	return time.Since(start)
}

func TestWaitWaitsTheDeliveryDelay(t *testing.T) {
	const delay = 30 * time.Millisecond
	h := newHarness(t, Config{
		Interval: time.Hour, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		DeliveryDelay: delay, DeliveryRootsPerCycle: DefaultDeliveryRootsPerCycle,
	})
	h.deliveries.deliver(testRoot())
	if took := waitFor(t, h.loop); took < delay {
		t.Errorf("a delivery started the cycle after %v, want the delivery delay %v", took, delay)
	}
	if got := testutil.ToFloat64(h.loop.metrics.deliveryCycles); got != 1 {
		t.Errorf("cycles deliveries started = %v, want 1", got)
	}
}

func TestWaitEndsWithTheIntervalDuringTheDelay(t *testing.T) {
	h := newHarness(t, Config{
		Interval: 10 * time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		DeliveryDelay: time.Hour, DeliveryRootsPerCycle: DefaultDeliveryRootsPerCycle,
	})
	h.deliveries.deliver(testRoot())
	waitFor(t, h.loop)
	if got := testutil.ToFloat64(h.loop.metrics.deliveryCycles); got != 0 {
		t.Errorf("cycles deliveries started = %v, want none: the interval ended first", got)
	}
}

func TestWaitStopsWithTheLoop(t *testing.T) {
	h := newHarness(t, Config{
		Interval: time.Hour, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		DeliveryDelay: time.Hour, DeliveryRootsPerCycle: DefaultDeliveryRootsPerCycle,
	})
	h.deliveries.deliver(testRoot())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if h.loop.wait(ctx) {
		t.Error("the wait of a stopped loop says to run a cycle")
	}
}

// TestDeliveryCutsTheIntervalShort runs the loop with an interval no test
// waits out: the delivered root is read all the same, and the loop says
// it is collecting while it runs, and not once stopped.
func TestDeliveryCutsTheIntervalShort(t *testing.T) {
	h := newHarness(t, Config{
		Interval: time.Hour, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		DeliveryDelay: time.Millisecond, DeliveryRootsPerCycle: DefaultDeliveryRootsPerCycle,
	})
	h.vikunja.also = []int64{testTaskID + 1}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.loop.Run(ctx, &fakeLocker{})
	}()
	until(t, "the first cycle", func() bool { return testutil.ToFloat64(h.loop.metrics.cycles) == 1 })
	if !h.loop.Collecting() {
		t.Error("the loop holding the lock does not say it is collecting")
	}
	h.deliveries.deliver(taskRoot(testTaskID + 1))
	until(t, "the delivered root's read", func() bool { return slices.Contains(h.vikunja.read(), testTaskID+1) })
	cancel()
	<-done
	if h.loop.Collecting() {
		t.Error("the stopped loop says it is collecting")
	}
	if got := testutil.ToFloat64(h.loop.metrics.deliveryCycles); got != 1 {
		t.Errorf("cycles deliveries started = %v, want 1", got)
	}
}

// until polls cond until it holds, failing the test after a minute.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(time.Minute)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("no %s within a minute", what)
		case <-time.After(time.Millisecond):
		}
	}
}
