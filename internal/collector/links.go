package collector

import (
	"slices"

	"github.com/sperano/sillage/internal/model"
)

// links accumulates correlations, one per pair of records whatever the
// direction: a second piece of evidence for a pair joins the first's.
type links struct {
	byPair map[string]*model.Correlation
}

func newLinks() links { return links{byPair: make(map[string]*model.Correlation)} }

// pairKey names the pair of records whatever the direction.
func pairKey(a, b model.Ref) string {
	from, to := a.String(), b.String()
	if to < from {
		from, to = to, from
	}
	return from + " <-> " + to
}

// add records c, or joins its evidence to the pair's correlation: an
// identifier makes the pair an identifier, and the pair stays ambiguous
// only while every piece of evidence is.
func (l links) add(c model.Correlation) {
	key := pairKey(c.From, c.To)
	have, ok := l.byPair[key]
	if !ok {
		c.Evidence = slices.Clone(c.Evidence)
		l.byPair[key] = &c
		return
	}
	for _, ev := range c.Evidence {
		if !slices.Contains(have.Evidence, ev) {
			have.Evidence = append(have.Evidence, ev)
		}
	}
	if c.Confidence == model.ConfidenceIdentifier {
		have.Confidence = model.ConfidenceIdentifier
	}
	have.Ambiguous = have.Ambiguous && c.Ambiguous && have.Confidence == model.ConfidenceHeuristic
}

// identifier records an identifier correlation from one record to another.
func (l links) identifier(from, to model.Ref, ev model.Evidence) {
	l.add(model.Correlation{From: from, To: to, Confidence: model.ConfidenceIdentifier, Evidence: []model.Evidence{ev}})
}

// heuristic records a heuristic correlation, ambiguous when the evidence
// fits other candidates too.
func (l links) heuristic(from, to model.Ref, ambiguous bool, ev model.Evidence) {
	l.add(model.Correlation{
		From: from, To: to, Confidence: model.ConfidenceHeuristic, Ambiguous: ambiguous,
		Evidence: []model.Evidence{ev},
	})
}

// has reports whether a correlation joins a and b.
func (l links) has(a, b model.Ref) bool {
	_, ok := l.byPair[pairKey(a, b)]
	return ok
}

// within returns the correlations whose two ends are both known.
func (l links) within(known map[model.Ref]bool) []model.Correlation {
	out := make([]model.Correlation, 0, len(l.byPair))
	for _, c := range l.byPair {
		if known[c.From] && known[c.To] {
			out = append(out, *c)
		}
	}
	return out
}
