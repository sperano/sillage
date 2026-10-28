package ingest

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestQueue(t *testing.T) {
	q := NewQueue(2)
	select {
	case <-q.Ready():
		t.Fatal("an empty queue is ready")
	default:
	}
	roots := []model.Root{taskRoot(1), taskRoot(2), taskRoot(1)}
	for _, r := range roots {
		if !q.Schedule(r) {
			t.Errorf("Schedule(%q) refused", refs([]model.Root{r}))
		}
	}
	if q.Schedule(taskRoot(3)) {
		t.Error("a full queue took a third root")
	}
	if q.Schedule(model.Root{}) {
		t.Error("an invalid root was taken")
	}
	select {
	case <-q.Ready():
	default:
		t.Fatal("the queue is not ready")
	}
	if got, want := refs(q.Take(len(roots))), refs([]model.Root{taskRoot(1), taskRoot(2)}); !slices.Equal(got, want) {
		t.Errorf("Take() = %q, want %q", got, want)
	}
	if got := q.Take(len(roots)); len(got) != 0 {
		t.Errorf("second Take() = %q, want nothing", refs(got))
	}
	if !q.Schedule(taskRoot(1)) {
		t.Error("a root taken could not be queued again")
	}
	select {
	case <-q.Ready():
	default:
		t.Error("the queue is not ready after a Take and a Schedule")
	}
}

func TestNewQueueCapacity(t *testing.T) {
	q := NewQueue(0)
	if !q.Schedule(taskRoot(1)) || q.Schedule(taskRoot(2)) {
		t.Error("a queue of capacity 0 does not hold exactly one root")
	}
}

func TestQueueTakeUpToALimit(t *testing.T) {
	q := NewQueue(DefaultQueueCapacity)
	for id := range int64(3) {
		q.Schedule(taskRoot(id + 1))
	}
	<-q.Ready()
	if got, want := refs(q.Take(2)), refs([]model.Root{taskRoot(1), taskRoot(2)}); !slices.Equal(got, want) {
		t.Errorf("Take(2) = %q, want the first two %q", got, want)
	}
	select {
	case <-q.Ready():
	default:
		t.Fatal("the queue is not ready with a root left")
	}
	if !q.Schedule(taskRoot(1)) {
		t.Error("a root taken could not be queued again")
	}
	if got, want := refs(q.Take(0)), refs([]model.Root{taskRoot(3)}); !slices.Equal(got, want) {
		t.Errorf("Take(0) = %q, want one root, the oldest %q", got, want)
	}
	if got, want := refs(q.Take(2)), refs([]model.Root{taskRoot(1)}); !slices.Equal(got, want) {
		t.Errorf("Take(2) = %q, want the root queued again %q", got, want)
	}
}

func TestQueueTakeClearsReady(t *testing.T) {
	q := NewQueue(DefaultQueueCapacity)
	q.Schedule(taskRoot(1))
	q.Take(1)
	select {
	case <-q.Ready():
		t.Error("the queue is ready after a Take left it empty")
	default:
	}
}
