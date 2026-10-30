package reconcile

import (
	"context"
	"log/slog"
	"maps"
	"math/rand/v2"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// ConditionalGitHub is a GitHub source that keeps the validators of a
// branch's read, tells from them whether the branch changed without
// costing rate limit, and reports what it asked of GitHub:
// *github.Client. Without one, the backstop reads a root's GitHub side
// whole, and nothing guards the rate limit's reserve.
type ConditionalGitHub interface {
	FetchBranchValidated(ctx context.Context, base github.Repository, head github.Branch) (
		[]github.Snapshot, github.Validators, error)
	Unchanged(ctx context.Context, v github.Validators) (bool, error)
	Usage() github.Usage
}

// githubRead is how a root's read reads its GitHub side.
type githubRead int

const (
	// githubKept: GitHub is not read; the stored records of its last read
	// stand for it.
	githubKept githubRead = iota
	// githubBackstop: nothing says the root's pull requests changed, but
	// they were not read for a backstop interval: GitHub is asked whether
	// its lists changed, and read only if they did.
	githubBackstop
	// githubFull: something says they may have changed.
	githubFull
)

// What made a root's GitHub side read, or not, as labelKind values.
const (
	// triggerRun: a root never read, or named by other runs.
	triggerRun = "run"
	// triggerTask: a project listing said the task changed.
	triggerTask = "task"
	// triggerDelivery: a webhook delivery named the root.
	triggerDelivery = "delivery"
	// triggerLost: a delivery sillage did not receive, and GitHub would
	// not send again, named the root.
	triggerLost = "lost"
	// triggerBackstop: the backstop read it whole: its lists changed, or
	// the last read left none to compare.
	triggerBackstop = "backstop"
	// triggerUnchanged: the backstop found its lists unchanged.
	triggerUnchanged = "unchanged"
	// triggerDeferred: the backstop held back for the rate limit's reserve.
	triggerDeferred = "deferred"
)

// githubTriggers are every trigger, in the order the cycle's log names
// them.
var githubTriggers = []string{
	triggerDelivery, triggerLost, triggerRun, triggerTask, triggerBackstop, triggerUnchanged, triggerDeferred,
}

// githubReserve is the share of GitHub's rate limit the backstop leaves
// to the reads deliveries, runs and task changes ask for, and to the
// other clients of the same token: below it, no backstop read is made.
const githubReserve = 0.2

// readGitHub has d read its root's GitHub side whole, for trigger.
func (d *due) readGitHub(trigger string) {
	d.github, d.trigger, d.backstopOnly = githubFull, trigger, false
}

// spread returns a random duration in [0, d): a restart's overdue
// backstop reads are spread over the backstop interval.
func spread(d time.Duration) time.Duration { return rand.N(d) }

// backstopDue reports whether st's root is due for a GitHub backstop
// read. A settled root is not, unless a gap is open on its GitHub
// coverage (a restart, a failed read): one read closes it.
func (l *Loop) backstopDue(st *rootState) bool {
	if l.src.GitHub == nil || (st.Settled && !st.githubGapOpen(l.ccfg.GitHubInstance)) {
		return false
	}
	return !l.now().Before(st.backstop)
}

// scheduleBackstop sets when the backstop next reads st's root: a backstop
// interval after its last GitHub read, or a refresh interval after a
// failed one.
func (l *Loop) scheduleBackstop(st *rootState) {
	if st.GitHubRead == nil {
		st.backstop = time.Time{}
		return
	}
	wait := l.cfg.BackstopInterval
	if st.GitHubFailed {
		wait = l.cfg.RefreshInterval
	}
	st.backstop = st.GitHubRead.Add(l.jitter(wait))
}

// scheduleLoaded schedules the backstop of a root state the store held.
// One that can close the restart's GitHub gap with a check, which costs
// no rate limit when nothing changed, is checked within a refresh
// interval from now, so the gap is no longer than before the backstop.
// Another overdue at now, the restart's downtime or a state from before
// the backstop, is spread over the backstop interval from now, so that a
// restart does not read every root's GitHub side in its first cycle.
func (l *Loop) scheduleLoaded(st *rootState, now time.Time) {
	l.scheduleBackstop(st)
	switch {
	case l.checkable(st):
		if soon := now.Add(l.spread(l.cfg.RefreshInterval)); !st.backstop.After(now) || st.backstop.After(soon) {
			st.backstop = soon
		}
	case !st.backstop.After(now):
		st.backstop = now.Add(l.spread(l.cfg.BackstopInterval))
	}
}

// checkable reports whether st's root has a gap open on its GitHub
// coverage that the backstop can close by asking whether its lists
// changed: the source answers that, and the last read left validators.
func (l *Loop) checkable(st *rootState) bool {
	return l.conditional != nil && len(st.ETags) > 0 && st.githubGapOpen(l.ccfg.GitHubInstance)
}

// backstopAllowed reports whether the rate limit's window leaves more
// than its reserve. An unknown window, or one past its reset, allows.
func (l *Loop) backstopAllowed() bool {
	if l.conditional == nil {
		return true
	}
	b := l.conditional.Usage().Budget
	if b.Limit <= 0 || !l.now().Before(b.Reset) {
		return true
	}
	return float64(b.Remaining) >= githubReserve*float64(b.Limit)
}

// githubPlan is how one root's read reads its GitHub side, as decided
// before the read.
type githubPlan struct {
	// kept has the collector keep the stored records instead of reading.
	kept bool
	// checked is whether the backstop asked GitHub if the lists changed;
	// unchanged what it answered, checkErr how the asking failed.
	checked   bool
	unchanged bool
	checkErr  error
	// gh is the root's GitHub source, which keeps the read's validators.
	gh *rootGitHub
}

// planGitHub decides how d's read reads GitHub, asking it whether the
// lists changed for the backstop. It reports false when the root is not
// to be read at all: due for the backstop alone, which the reserve holds
// back.
func (l *Loop) planGitHub(ctx context.Context, d *due, prev *rootState) (githubPlan, bool) {
	p := githubPlan{gh: &rootGitHub{
		meteredGitHub: meteredGitHub{src: l.src.GitHub, meter: l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)},
		cond:          l.conditional, tags: make(github.Validators),
	}}
	if l.src.GitHub == nil {
		return p, true
	}
	switch d.github {
	case githubKept:
		p.kept = true
	case githubFull:
		l.countGitHub(d.trigger)
	case githubBackstop:
		if !l.backstopAllowed() {
			l.countGitHub(triggerDeferred)
			p.kept = true
			return p, !d.backstopOnly
		}
		l.checkBackstop(ctx, &p, prev)
	}
	return p, true
}

// checkBackstop asks GitHub whether prev's lists changed, when prev has
// validators and the source can answer: unchanged, the stored records
// stand; changed, or nothing to compare, GitHub is read whole.
func (l *Loop) checkBackstop(ctx context.Context, p *githubPlan, prev *rootState) {
	if len(prev.ETags) == 0 || l.conditional == nil {
		l.countGitHub(triggerBackstop)
		return
	}
	p.checked = true
	p.unchanged, p.checkErr = timed(ctx, p.gh.meter, func(ctx context.Context) (bool, error) {
		return l.conditional.Unchanged(ctx, prev.ETags)
	})
	switch {
	case p.checkErr != nil:
		p.kept = true
	case p.unchanged:
		p.kept = true
		l.countGitHub(triggerUnchanged)
	default:
		l.countGitHub(triggerBackstop)
	}
}

// accountGitHub records on next what its read did with GitHub, at at,
// and reports whether it failed; a failure of the collector's own read
// is account's. It schedules the next backstop read.
func (l *Loop) accountGitHub(next *rootState, p githubPlan, failures map[model.Source]bool, at time.Time) bool {
	if l.src.GitHub == nil || (p.kept && !p.checked) {
		return false
	}
	cov := model.Coverage{Source: model.SourceGitHub, Instance: l.ccfg.GitHubInstance}
	failed := false
	switch {
	case p.checkErr != nil:
		l.log.Warn("source read failed", "root", next.Root.Ref(), "source", model.SourceGitHub,
			"instance", cov.Instance, "reason", reasonOf(p.checkErr), "error", p.checkErr)
		next.source(cov).fail(failureReason(model.SourceGitHub, p.checkErr))
		failed = true
	case p.unchanged:
		next.source(cov).unchanged(at)
	default:
		failed = failures[model.SourceGitHub]
		if !failed {
			next.ETags = p.gh.validators()
		}
	}
	next.GitHubRead, next.GitHubFailed = &at, failed
	l.scheduleBackstop(next)
	return failed
}

// rootGitHub reads GitHub for one root's read, metered, and keeps the
// validators of what it read.
type rootGitHub struct {
	meteredGitHub
	cond ConditionalGitHub
	tags github.Validators
	// unvalidated is whether a read left lists that cannot be compared.
	unvalidated bool
}

func (g *rootGitHub) FetchBranch(ctx context.Context, base github.Repository, head github.Branch) (
	[]github.Snapshot, error) {
	if g.cond == nil {
		g.unvalidated = true
		return g.meteredGitHub.FetchBranch(ctx, base, head)
	}
	return timed(ctx, g.meter, func(ctx context.Context) ([]github.Snapshot, error) {
		snaps, v, err := g.cond.FetchBranchValidated(ctx, base, head)
		if err != nil {
			// The collector can go on without this repository: the
			// backstop must not then take the others' lists for all.
			g.unvalidated = true
			return nil, err
		}
		if v == nil {
			g.unvalidated = true
		}
		maps.Copy(g.tags, v)
		return snaps, nil
	})
}

// validators are those of every branch read; nil if one left lists that
// cannot be compared, or none was read.
func (g *rootGitHub) validators() github.Validators {
	if g.unvalidated || len(g.tags) == 0 {
		return nil
	}
	return g.tags
}

// githubCycle is what one cycle asked of GitHub: its roots' GitHub sides
// by kind, the repositories their branches were searched for in, and the
// client's usage when it started.
type githubCycle struct {
	reads    map[string]int
	searches int
	start    github.Usage
}

// countGitHub counts one root's GitHub side, of kind.
func (l *Loop) countGitHub(kind string) {
	l.metrics.githubReads.WithLabelValues(kind).Inc()
	l.github.reads[kind]++
}

// countSearches counts the repositories one root's read looked for its
// branch in, as no record of the root named one.
func (l *Loop) countSearches(n int) {
	l.metrics.githubSearches.Add(float64(n))
	l.github.searches += n
}

// startGitHubCycle starts counting a cycle's GitHub reads and requests.
func (l *Loop) startGitHubCycle() {
	l.github = githubCycle{reads: make(map[string]int)}
	if l.conditional != nil {
		l.github.start = l.conditional.Usage()
	}
}

// endGitHubCycle counts the cycle's requests to GitHub, and returns what
// the cycle asked of it, for the cycle's log line.
func (l *Loop) endGitHubCycle() slog.Attr {
	var requests, notModified int64
	if l.conditional != nil {
		u := l.conditional.Usage()
		requests, notModified = u.Requests-l.github.start.Requests, u.NotModified-l.github.start.NotModified
		l.metrics.githubRequests.Add(float64(requests))
		l.metrics.githubNotModified.Add(float64(notModified))
		if u.Budget.Limit > 0 {
			l.metrics.githubRemaining.Set(float64(u.Budget.Remaining))
		}
	}
	args := []any{"requests", requests, "notModified", notModified, "searches", l.github.searches}
	for _, kind := range githubTriggers {
		args = append(args, kind, l.github.reads[kind])
	}
	return slog.Group("github", args...)
}
