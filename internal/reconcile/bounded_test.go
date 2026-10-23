package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
)

const (
	// testStoreTimeout replaces storeTimeout, so that a hanging store
	// fails fast.
	testStoreTimeout = 10 * time.Millisecond
	// testStallGuard is how long a cycle over a hanging store may take
	// before the test calls it stalled.
	testStallGuard = 10 * time.Second
)

// hangingStore is a fake store whose records reads or commits, as set,
// block until their context is done.
type hangingStore struct {
	*fakeStore
	records, commit bool
}

func (s hangingStore) Records(ctx context.Context, root model.Root) (model.Records, error) {
	if s.records {
		<-ctx.Done()
		return model.Records{}, ctx.Err()
	}
	return s.fakeStore.Records(ctx, root)
}

func (s hangingStore) Commit(ctx context.Context, c Commit) error {
	if s.commit {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.fakeStore.Commit(ctx, c)
}

// deadlineStore records the deadline of each operation's context.
type deadlineStore struct {
	deadlines []time.Time
}

func (s *deadlineStore) record(ctx context.Context) {
	d, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, d)
}

func (s *deadlineStore) Records(ctx context.Context, _ model.Root) (model.Records, error) {
	s.record(ctx)
	return model.Records{}, nil
}

func (s *deadlineStore) Checkpoints(ctx context.Context, _ string) (map[string][]byte, error) {
	s.record(ctx)
	return nil, nil
}

func (s *deadlineStore) Commit(ctx context.Context, _ Commit) error {
	s.record(ctx)
	return nil
}

func TestBoundedStoreBoundsEachOperation(t *testing.T) {
	inner := &deadlineStore{}
	b := boundedStore{s: inner, timeout: storeTimeout}
	start := time.Now()
	if _, err := b.Records(t.Context(), testRoot()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Checkpoints(t.Context(), checkpointPrefix); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(t.Context(), Commit{}); err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	for i, d := range inner.deadlines {
		if d.IsZero() || d.Before(start.Add(storeTimeout)) || d.After(end.Add(storeTimeout)) {
			t.Errorf("operation %d deadline = %v, want storeTimeout after it started", i, d)
		}
	}
}

func TestHangingStoreDoesNotStallTheCycle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		store     func(*fakeStore) hangingStore
		operation string
		want      float64
	}{
		{"records", func(s *fakeStore) hangingStore { return hangingStore{fakeStore: s, records: true} },
			operationRead, 1},
		{"commit", func(s *fakeStore) hangingStore { return hangingStore{fakeStore: s, commit: true} },
			operationCommit, 1 + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, everyCycle)
			h.loop.store = boundedStore{s: tc.store(h.store), timeout: testStoreTimeout}
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.loop.cycle(t.Context())
			}()
			select {
			case <-done:
			case <-time.After(testStallGuard):
				t.Fatal("the cycle is still waiting on the store")
			}
			if got := testutil.ToFloat64(h.loop.metrics.storeErrors.WithLabelValues(tc.operation)); got != tc.want {
				t.Errorf("%s errors = %v, want %v", tc.operation, got, tc.want)
			}
			h.loop.store = boundedStore{s: h.store, timeout: testStoreTimeout}
			h.loop.cycle(t.Context())
			if c := h.store.lastCommit(t); written(c) == 0 {
				t.Error("once the store answers again, the next read wrote nothing")
			}
		})
	}
}

func TestNewBoundsTheStore(t *testing.T) {
	h := newHarness(t, everyCycle)
	if b, ok := h.loop.store.(boundedStore); !ok || b.timeout != storeTimeout || b.s != Store(h.store) {
		t.Errorf("the loop's store = %#v, want the store given to New within storeTimeout", h.loop.store)
	}
}
