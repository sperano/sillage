package reconcile

import (
	"context"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// storeTimeout bounds one store operation: a read of a root's records or
// of the checkpoints, or a commit. A store that blocks (a lock wait, a
// connection that hangs) then fails the operation as a store error,
// instead of holding the collector lock while no cycle progresses. It
// bounds the store alone: each source read keeps its own readTimeout.
const storeTimeout = time.Minute

// boundedStore runs each operation of s within timeout.
type boundedStore struct {
	s       Store
	timeout time.Duration
}

func (b boundedStore) Records(ctx context.Context, root model.Root) (model.Records, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	return b.s.Records(ctx, root)
}

func (b boundedStore) Checkpoints(ctx context.Context, prefix string) (map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	return b.s.Checkpoints(ctx, prefix)
}

func (b boundedStore) Commit(ctx context.Context, c Commit) error {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	return b.s.Commit(ctx, c)
}
