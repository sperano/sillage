package ingest

import (
	"slices"
	"sync"

	"github.com/sperano/sillage/internal/model"
)

// DefaultQueueCapacity is how many distinct roots a Queue holds before it
// refuses more: far more than one burst of deliveries names, and still a
// bound on what a flood of them can hold in memory.
const DefaultQueueCapacity = 1000

// Queue is a Scheduler for the collector loop: the roots asked for and not
// taken yet, each once, in the order first asked for. Ready signals while
// there are some to Take.
type Queue struct {
	mu       sync.Mutex
	capacity int
	pending  []model.Root
	queued   map[model.Ref]bool
	ready    chan struct{}
}

// NewQueue returns an empty queue of at most capacity roots (at least 1).
func NewQueue(capacity int) *Queue {
	return &Queue{capacity: max(capacity, 1), queued: make(map[model.Ref]bool), ready: make(chan struct{}, 1)}
}

var _ Scheduler = (*Queue)(nil)

// Schedule queues root unless it is queued already. It reports false for
// an invalid root, or when the queue is full.
func (q *Queue) Schedule(root model.Root) bool {
	ref, ok := refOf(root)
	if !ok {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued[ref] {
		return true
	}
	if len(q.pending) >= q.capacity {
		return false
	}
	q.queued[ref] = true
	q.pending = append(q.pending, root)
	q.signal()
	return true
}

// Ready is signalled while roots are queued: Schedule signals it, and a
// Take that leaves none clears it. Once received, the signal is spent
// until the next Schedule or a Take that leaves some queued.
func (q *Queue) Ready() <-chan struct{} { return q.ready }

// Take removes and returns the first limit queued roots (at least 1), in
// the order first asked for; the others stay queued, Ready signalled.
func (q *Queue) Take(limit int) []model.Root {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(max(limit, 1), len(q.pending))
	out := slices.Clone(q.pending[:n])
	q.pending = slices.Delete(q.pending, 0, n)
	for _, root := range out {
		ref, _ := refOf(root)
		delete(q.queued, ref)
	}
	if len(q.pending) == 0 {
		select {
		case <-q.ready:
		default:
		}
		return out
	}
	q.signal()
	return out
}

// signal makes Ready ready, unless it is already; q.mu must be held.
func (q *Queue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
