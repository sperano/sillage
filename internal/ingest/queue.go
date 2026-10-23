package ingest

import (
	"sync"

	"github.com/sperano/sillage/internal/model"
)

// DefaultQueueCapacity is how many distinct roots a Queue holds before it
// refuses more: far more than one burst of deliveries names, and still a
// bound on what a flood of them can hold in memory.
const DefaultQueueCapacity = 1000

// Queue is a Scheduler for the collector loop: the roots asked for and not
// taken yet, each once, in the order first asked for. Ready signals when
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
	select {
	case q.ready <- struct{}{}:
	default:
	}
	return true
}

// Ready is signalled once roots are queued after the last Take. A
// receiver should Take, even when the signal predates a Take: it then
// finds nothing.
func (q *Queue) Ready() <-chan struct{} { return q.ready }

// Take removes and returns every queued root, in the order first asked
// for.
func (q *Queue) Take() []model.Root {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.pending
	q.pending = nil
	clear(q.queued)
	return out
}
