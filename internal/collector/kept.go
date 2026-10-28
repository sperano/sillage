package collector

import (
	"context"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// Kept is what a store already holds of a root's story, for Recollect.
type Kept struct {
	// Runs are the story's stored runs; Correlations its stored
	// correlations.
	Runs         []model.Run
	Correlations []model.Correlation
	// KelosSince is when Kelos was first read for the story; zero if never.
	// A run whose Task Kelos deleted after it was read before then.
	KelosSince time.Time
	// Markers are phase markers joined to runs; those joined to a run of
	// the story become its phases. No source is read for them: pods push
	// them to the store. Only tests set them, so that the collector
	// assembles the phase-markers fixture story: the loop passes none, and
	// its stories get their phases when the store reads them.
	Markers []model.Marker
	// SkipGitHub has GitHub not read: Events' GitHub events, PullRequests
	// and the Correlations joining one of them to the root stand for its
	// read, as the store holds them from the last. The story's GitHub
	// coverage then says nothing of a read; the caller keeps its own.
	SkipGitHub bool
	// Events and PullRequests are the story's stored events and pull
	// requests; only a skipped GitHub read uses them.
	Events       []model.Event
	PullRequests []model.PullRequest
}

// Recollect is Collect for a root whose records a store already holds.
// A kept run keyed by UID that Kelos no longer lists (its Task deleted
// after the TTL, without a TaskRecord), or every such run when Kelos
// cannot be read, stays in the story as stored, with the stored
// correlations that joined it, and an announcement of its pod joins it
// rather than a run keyed by the pod. The Task TTL bounds what may be
// missing only before kept.KelosSince: what came after was read.
func (c *Collector) Recollect(ctx context.Context, root model.Root, kept Kept, repos ...github.Repository) (
	Result, error) {
	return c.collect(ctx, root, kept, repos)
}

// keepRuns adds the kept runs keyed by UID that the Kelos read does not
// list, with the kept correlations that touch them. A kept run keyed by
// pod is not kept: its announcement makes it again, unless a run keyed by
// UID now names its pod.
func (a *assembly) keepRuns() {
	listed := make(map[model.RunKey]bool)
	if a.kelos != nil {
		for _, r := range a.kelos.runs {
			listed[r.Key] = true
		}
	}
	for _, r := range a.kept.Runs {
		if r.Key.UID == "" || listed[r.Key] {
			continue
		}
		a.keptOnly = append(a.keptOnly, r)
		a.addRun(r)
		ref := r.Key.Ref()
		for _, c := range a.kept.Correlations {
			if c.From == ref || c.To == ref {
				a.links.add(c)
			}
		}
	}
}

// ttlCutoff is the time before which a run Kelos deleted may be missing:
// the Task TTL before the first read of the story, else before read.
func (a *assembly) ttlCutoff(read time.Time) time.Time {
	since := a.kept.KelosSince
	if since.IsZero() || since.After(read) {
		since = read
	}
	return since.Add(-a.cfg.KelosTaskTTL)
}

// keptMarkers returns the kept markers joined to one of runs.
func (a *assembly) keptMarkers(runs []model.Run) []model.Marker {
	in := make(map[model.RunKey]bool, len(runs))
	for _, r := range runs {
		in[r.Key] = true
	}
	var out []model.Marker
	for _, m := range a.kept.Markers {
		if m.Join != nil && in[m.Join.Run] {
			out = append(out, m)
		}
	}
	return out
}

// keepGitHub adds what the kept records hold of GitHub's last read, in
// place of a read: the pull requests, the GitHub events and the
// correlations that join a pull request to the root.
func (a *assembly) keepGitHub() {
	var b branchRead
	prs := make(map[model.Ref]bool, len(a.kept.PullRequests))
	for _, p := range a.kept.PullRequests {
		b.recs.PullRequests = append(b.recs.PullRequests, p)
		prs[p.Key.Ref()] = true
	}
	for _, e := range a.kept.Events {
		if e.Source == model.SourceGitHub {
			b.recs.Events = append(b.recs.Events, e)
		}
	}
	for _, c := range a.kept.Correlations {
		if (prs[c.From] && c.To == a.rootRef) || (prs[c.To] && c.From == a.rootRef) {
			b.recs.Correlations = append(b.recs.Correlations, c)
		}
	}
	a.addGitHub(b)
}
