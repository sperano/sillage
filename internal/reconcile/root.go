package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

// collectRoot reads one root over what the store holds of it, and commits
// its records if they changed, its coverage and its state, in one
// transaction. It reports whether it committed, and whether the root's
// Vikunja task, if any, was read: a project's mark waits for that.
func (l *Loop) collectRoot(ctx context.Context, cr cycleRun, d *due) (committed, read bool) {
	rctx, cancel := context.WithTimeout(ctx, rootTimeout)
	defer cancel()
	prev := l.roots[d.root.Ref()]
	if prev == nil {
		prev = newRootState(d.root)
	}
	at := l.now()
	res, ok := l.recollect(rctx, cr, prev)
	if !ok || ctx.Err() != nil {
		return false, false
	}
	next := prev.clone()
	next.Read = at
	failed, vikunjaFailed := l.account(next, res, cr.kelosAt)
	if d.kelosRuns != "" {
		next.KelosRuns = d.kelosRuns
	}
	if d.updated != nil && !vikunjaFailed {
		next.Updated = d.updated
	}
	if !l.commitRoot(ctx, prev, next, res.Story.Records, failed) {
		return false, false
	}
	return true, !vikunjaFailed
}

// recollect reads prev's root over what the store holds of it.
func (l *Loop) recollect(ctx context.Context, cr cycleRun, prev *rootState) (collector.Result, bool) {
	ref := prev.Root.Ref()
	stored, err := l.store.Records(ctx, prev.Root)
	if err != nil {
		l.storeFailed(ctx, operationRead, ref, err)
		return collector.Result{}, false
	}
	since := prev.kelosSince()
	if since.IsZero() {
		// This is the first Kelos read of the root, if it succeeds.
		since = cr.kelosAt
	}
	res, err := cr.c.Recollect(ctx, prev.Root, collector.Kept{
		Runs: stored.Runs, Correlations: stored.Correlations, KelosSince: since,
	})
	if err != nil {
		if ctx.Err() == nil {
			l.log.Error("the story could not be assembled", "root", ref, "error", err)
		}
		return collector.Result{}, false
	}
	return res, true
}

// commitRoot commits a read of next's root: its records unless their
// digest is prev's, its coverage and its state. The digest is kept only
// from a read that failed nowhere, so that a partial read is not taken
// for the whole story. It reports whether the commit succeeded.
func (l *Loop) commitRoot(ctx context.Context, prev, next *rootState, recs model.Records, failed bool) bool {
	commit := Commit{Root: &next.Root}
	if sum := digest(recs); sum != prev.Digest {
		commit.Records = recs
		if !failed {
			next.Digest = sum
		}
	}
	commit.Records.Coverage = next.coverage()
	commit.Checkpoints = map[string][]byte{rootKey(next.Root): encode(next)}
	if err := l.store.Commit(ctx, commit); err != nil {
		l.storeFailed(ctx, operationCommit, next.Root.Ref(), err)
		return false
	}
	l.roots[next.Root.Ref()] = next
	l.metrics.roots.Set(float64(len(l.roots)))
	l.metrics.observeWritten(commit.Records)
	return true
}

// account advances each source of the root that was read, and opens a
// gap for each that failed. Kelos was read once for the cycle, at kelosAt.
// It reports whether any source failed, and whether Vikunja did.
func (l *Loop) account(next *rootState, res collector.Result, kelosAt time.Time) (failed, vikunjaFailed bool) {
	failures := make(map[string]collector.Failure)
	for _, f := range res.Failures {
		failures[model.Coverage{Source: f.Source, Instance: f.Instance}.Key()] = f
	}
	for _, cov := range res.Story.Coverage {
		st := next.source(cov)
		if f, ok := failures[cov.Key()]; ok {
			reason := reasonOf(f.Err)
			l.log.Warn("source read failed", "root", next.Root.Ref(), "source", f.Source, "instance", f.Instance,
				"reason", reason, "error", f.Err)
			st.fail(fmt.Sprintf("the %s read failed (%s): %v", f.Source, reason, f.Err))
			failed = true
			vikunjaFailed = vikunjaFailed || f.Source == model.SourceVikunja
			continue
		}
		if cov.LastSuccessfulRead == nil {
			st.unread(cov)
			continue
		}
		at := *cov.LastSuccessfulRead
		if cov.Source == model.SourceKelos {
			at = kelosAt
		}
		st.succeed(at, cov)
	}
	return failed, vikunjaFailed
}

// storeFailed reports a failed store operation on root, unless the
// shutdown caused it.
func (l *Loop) storeFailed(ctx context.Context, op string, root model.Ref, err error) {
	if ctx.Err() != nil {
		return
	}
	l.metrics.storeErrors.WithLabelValues(op).Inc()
	l.log.Error("store "+op+" failed", "root", root, "error", err)
}
