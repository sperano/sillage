package reconcile

import (
	"context"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

// collectRoot reads one root over what the store holds of it, and commits
// its records if they changed, its coverage and its state, in one
// transaction. It reports whether it committed, and whether the root's
// Vikunja task, if any, was read: a project's mark waits for that. A root
// due for the GitHub backstop alone, which the rate limit's reserve holds
// back, is not read.
func (l *Loop) collectRoot(ctx context.Context, cr cycleRun, d *due) (committed, read bool) {
	prev := l.roots[d.root.Ref()]
	if prev == nil {
		prev = newRootState(d.root)
	}
	at := l.now()
	plan, ok := l.planGitHub(ctx, d, prev)
	if !ok || ctx.Err() != nil {
		return false, ctx.Err() == nil
	}
	res, ok := l.recollect(ctx, cr, prev, plan)
	if !ok || ctx.Err() != nil {
		return false, false
	}
	next := prev.clone()
	next.Read = at
	l.loseDelivery(next, d)
	failed, failures := l.account(next, res, cr.kelosAt, plan.kept)
	failed = l.accountGitHub(next, plan, failures, at) || failed
	l.refused(next.Root.Ref(), res.Refusals)
	if d.kelosRuns != "" {
		next.KelosRuns = d.kelosRuns
	}
	if d.updated != nil && !failures[model.SourceVikunja] {
		next.Updated = d.updated
	}
	if !failed {
		next.Settled = settled(res.Story, l.now())
	}
	if !l.commitRoot(ctx, prev, next, res.Story.Records, failed) {
		return false, false
	}
	return true, !failures[model.SourceVikunja]
}

// recollect reads prev's root over what the store holds of it, GitHub as
// plan says.
func (l *Loop) recollect(ctx context.Context, cr cycleRun, prev *rootState, plan githubPlan) (collector.Result, bool) {
	ref := prev.Root.Ref()
	stored, err := l.store.Records(ctx, prev.Root)
	if err != nil {
		l.storeFailed(ctx, operationRead, ref, err)
		return collector.Result{}, false
	}
	since := prev.kelosSince(l.ccfg.Kelos.Cluster)
	if since.IsZero() {
		// This is the first Kelos read of the root, if it succeeds.
		since = cr.kelosAt
	}
	c, err := l.rootCollector(cr, plan.gh)
	if err == nil {
		var res collector.Result
		res, err = c.Recollect(ctx, prev.Root, collector.Kept{
			Runs: stored.Runs, Correlations: stored.Correlations, KelosSince: since,
			SkipGitHub: plan.kept, Events: stored.Events, PullRequests: stored.PullRequests,
		})
		if err == nil {
			return res, true
		}
	}
	if ctx.Err() == nil {
		l.log.Error("the story could not be assembled", "root", ref, "error", err)
	}
	return collector.Result{}, false
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
	commit.Checkpoints = map[string]Checkpoint{rootKey(next.Root): next.checkpoint()}
	if err := l.store.Commit(ctx, commit); err != nil {
		l.storeFailed(ctx, operationCommit, next.Root.Ref(), err)
		return false
	}
	l.roots[next.Root.Ref()] = next
	delete(l.unsaved, next.Root.Ref())
	l.metrics.roots.Set(float64(len(l.roots)))
	l.metrics.observeWritten(commit.Records)
	return true
}

// account advances each source of the root that was read, and opens a
// gap for each that failed. Kelos was read once for the cycle, at kelosAt;
// GitHub not at all when keptGitHub, its state left as it was. It reports
// whether any source failed, and which.
func (l *Loop) account(next *rootState, res collector.Result, kelosAt time.Time, keptGitHub bool) (
	failed bool, sources map[model.Source]bool) {
	failures := make(map[string]collector.Failure)
	for _, f := range res.Failures {
		failures[model.Coverage{Source: f.Source, Instance: f.Instance}.Key()] = f
	}
	sources = make(map[model.Source]bool)
	for _, cov := range res.Story.Coverage {
		if keptGitHub && cov.Source == model.SourceGitHub {
			continue
		}
		st := next.source(cov)
		if f, ok := failures[cov.Key()]; ok {
			reason := reasonOf(f.Err)
			l.log.Warn("source read failed", "root", next.Root.Ref(), "source", f.Source, "instance", f.Instance,
				"reason", reason, "error", f.Err)
			st.fail(failureReason(f.Source, f.Err))
			failed, sources[f.Source] = true, true
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
	return failed, sources
}

// refused reports each resource and status a source refused in root's
// read: the read succeeded, and only the story's coverage notes say so. A
// refusal the client only remembered is neither logged nor counted: no
// request was refused, and the one that was is logged once per repository
// per interval.
func (l *Loop) refused(root model.Ref, refusals []collector.Refusal) {
	for _, r := range refusals {
		if r.Sent() == 0 {
			continue
		}
		l.log.Warn("source read refused in part", "root", root, "source", r.Source, "instance", r.Instance,
			"resource", r.Resource, "code", r.Code, "requests", r.Sent())
		l.metrics.observeRefusal(r)
	}
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
