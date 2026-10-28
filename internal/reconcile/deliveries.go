package reconcile

import (
	"context"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// Deliveries are the roots webhook deliveries ask the loop to read again:
// *ingest.Queue.
type Deliveries interface {
	// Ready is signalled while roots are queued.
	Ready() <-chan struct{}
	// Take removes and returns the first limit queued roots, each once.
	Take(limit int) []model.Root
}

// The kinds of read a delivery asked for, as labelKind values.
const (
	// deliveryCaused: the cycle read the root because a delivery named it.
	deliveryCaused = "caused"
	// deliveryCoalesced: the cycle had the root due anyway, and read it
	// once.
	deliveryCoalesced = "coalesced"
)

// ready is signalled while deliveries ask for reads; never without
// deliveries.
func (l *Loop) ready() <-chan struct{} {
	if l.deliveries == nil {
		return nil
	}
	return l.deliveries.Ready()
}

// wait waits out the interval between two cycles, or less once a delivery
// asks for a read: DeliveryDelay from then, so that the deliveries of one
// change share a cycle and a flood of them starts one at most every
// DeliveryDelay, unless the interval ends first. It reports false if ctx
// is done first.
func (l *Loop) wait(ctx context.Context) bool {
	interval := time.NewTimer(l.jitter(l.cfg.Interval))
	defer interval.Stop()
	select {
	case <-interval.C:
		return true
	case <-ctx.Done():
		return false
	case <-l.ready():
	}
	delay := time.NewTimer(l.cfg.DeliveryDelay)
	defer delay.Stop()
	select {
	case <-interval.C:
	case <-delay.C:
		l.metrics.deliveryCycles.Inc()
	case <-ctx.Done():
		return false
	}
	return true
}

// addDelivered takes at most DeliveryRootsPerCycle of the roots
// deliveries ask for and adds them to dues: one read of a root already
// due serves both. The others stay queued for the next cycle.
func (l *Loop) addDelivered(dues map[model.Ref]*due) {
	if l.deliveries == nil {
		return
	}
	for _, root := range l.deliveries.Take(l.cfg.DeliveryRootsPerCycle) {
		ref := root.Ref()
		if d, ok := dues[ref]; ok {
			if d.delivery == "" {
				d.delivery = deliveryCoalesced
			}
			continue
		}
		dues[ref] = &due{root: root, delivery: deliveryCaused}
	}
}

// compareDues orders the roots a delivery asked for first, so that a
// cycle reads them before the periodic reads, then by ref.
func compareDues(a, b *due) int {
	if (a.delivery != "") != (b.delivery != "") {
		if a.delivery != "" {
			return -1
		}
		return 1
	}
	return compareRefs(a.root.Ref(), b.root.Ref())
}
