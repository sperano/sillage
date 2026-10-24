package overview

import (
	"slices"

	"github.com/sperano/sillage/internal/model"
)

// rowSources lists the coverage keys of the sources the row shows records
// from: the root's own (its task's Vikunja, its page's wiki), whose state
// the row reports even when it holds no record of it, the Kelos cluster of
// each run, the GitHub instance of each pull request and review wait, the
// source of each mark.
func rowSources(row Row) []string {
	var keys []string
	add := func(source model.Source, instance string) {
		k := model.Coverage{Source: source, Instance: instance}.Key()
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	switch {
	case row.Root.Task != nil:
		add(model.SourceVikunja, row.Root.Task.Instance)
	case row.Root.Page != nil:
		add(model.SourceWiki, row.Root.Page.Instance)
	}
	for _, r := range row.Runs {
		add(model.SourceKelos, r.Key.Cluster)
	}
	for _, p := range row.PullRequests {
		add(model.SourceGitHub, p.Key.Instance)
	}
	for _, rw := range row.ReviewWaits {
		add(model.SourceGitHub, rw.PR.Instance)
	}
	for _, m := range row.Marks {
		add(m.Source, m.Instance)
	}
	return keys
}

// missingHistory reports whether part of the window is unknown for the
// row: a gap of any source of the root overlaps it, a source of the root
// was never read or not since before the window, or a source of the row
// (its root's, or one it has records from) has no coverage at all. Every
// source in the root's coverage counts, not only those the row has records
// from: a source left unread, for want of a repository or after a failure,
// leaves no records, so its absence from the row says nothing. The time
// after a source's last read inside the window is unknown too, and shown
// so, but not counted here: with a window ending now, every source's last
// read is a little before its end.
func missingHistory(w Window, coverage []model.Coverage, sources []string) bool {
	covered := make(map[string]bool, len(coverage))
	for _, c := range coverage {
		covered[c.Key()] = true
		if c.LastSuccessfulRead == nil || c.LastSuccessfulRead.Before(w.From) {
			return true
		}
		for _, g := range c.Gaps {
			if g.Start.Before(w.To) && g.End.After(w.From) {
				return true
			}
		}
	}
	for _, k := range sources {
		if !covered[k] {
			return true
		}
	}
	return false
}
