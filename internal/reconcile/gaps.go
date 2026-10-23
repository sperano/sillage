package reconcile

import (
	"context"
	"fmt"
	"maps"

	"github.com/sperano/sillage/internal/model"
)

// failureReason is the reason of the gap a failed read of src opens.
func failureReason(src model.Source, err error) string {
	return fmt.Sprintf("the %s read failed (%s): %v", src, reasonOf(err), err)
}

// openGaps applies open to a copy of each source state of every root,
// and commits the root states it changed in one transaction; they replace
// the loop's either way. A gap is so in the store as soon as it is open,
// whether or not its root is read before the next restart. If the commit
// fails, the roots it changed are unsaved: each is committed with its
// next read, or with the cycle's end, whichever comes first, so that the
// last cycle's end never passes a gap the store does not hold.
func (l *Loop) openGaps(ctx context.Context, open func(*sourceState) bool) error {
	changed := make(map[model.Ref]*rootState)
	cps := make(map[string][]byte)
	for ref, st := range l.roots {
		next := st.clone()
		opened := false
		for _, src := range next.Sources {
			opened = open(src) || opened
		}
		if opened {
			changed[ref] = next
			cps[rootKey(next.Root)] = encode(next)
		}
	}
	if len(cps) == 0 {
		return nil
	}
	maps.Copy(l.roots, changed)
	if err := l.store.Commit(ctx, Commit{Checkpoints: cps}); err != nil {
		for ref := range changed {
			l.unsaved[ref] = true
		}
		return err
	}
	return nil
}

// kelosFailed opens a gap on the Kelos coverage of every root: the
// cycle's one read serves them all, read this cycle or not. Each gap
// starts at the root's own last Kelos success, not at the last shared
// read: a root not read then may have had a run that read would have
// joined, through an announcement, where no Kelos label names the root.
func (l *Loop) kelosFailed(ctx context.Context, err error) {
	l.log.Warn("source read failed", "source", model.SourceKelos, "instance", l.ccfg.Kelos.Cluster,
		"reason", reasonOf(err), "error", err)
	reason := failureReason(model.SourceKelos, err)
	cerr := l.openGaps(ctx, func(st *sourceState) bool {
		return st.Source == model.SourceKelos && st.Instance == l.ccfg.Kelos.Cluster && st.fail(reason)
	})
	if cerr != nil && ctx.Err() == nil {
		l.metrics.storeErrors.WithLabelValues(operationCommit).Inc()
		l.log.Error("store commit of the Kelos failure's gaps failed; they are committed with the roots' "+
			"next reads or the cycle's end", "error", cerr)
	}
}
