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
	for _, r := range []model.Root{taskRoot(1), pageRoot(7), taskRoot(1)} {
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
	if got, want := refs(q.Take()), refs([]model.Root{taskRoot(1), pageRoot(7)}); !slices.Equal(got, want) {
		t.Errorf("Take() = %q, want %q", got, want)
	}
	if got := q.Take(); len(got) != 0 {
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
