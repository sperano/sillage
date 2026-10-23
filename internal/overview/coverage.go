package overview

import (
	"slices"

	"github.com/sperano/sillage/internal/model"
)

// rowSources lists the coverage keys of the sources the row shows records
// from: the Kelos cluster of each run, the GitHub instance of each pull
// request and review wait, the source of each mark.
func rowSources(w Window, row Row, events []model.Event) []string {
	var keys []string
	add := func(source model.Source, instance string) {
		k := model.Coverage{Source: source, Instance: instance}.Key()
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
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
	for _, e := range events {
		if markKinds[e.Kind] && w.Contains(e.Time()) {
			add(e.Source, e.Instance)
		}
	}
	return keys
}

// missingHistory reports whether part of the window is unknown for one of
// the row's sources: a gap overlaps it, a source was never read or not
// since before the window, or a source the row has records from has no
// coverage at all. The time after a source's last read inside the window
// is unknown too, and shown so, but not counted here: with a window ending
// now, every source's last read is a little before its end.
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
