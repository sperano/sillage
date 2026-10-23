package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

var (
	refA = model.Ref{Kind: model.RefRun, ID: "a"}
	refB = model.Ref{Kind: model.RefTask, ID: "b"}
	evA  = model.Evidence{Source: model.SourceKelos, Instance: testCluster, Ref: "tasks/x", Note: "a"}
	evB  = model.Evidence{Source: model.SourceKelos, Instance: testCluster, Ref: "tasks/x", Note: "b"}
)

func onlyLink(t *testing.T, l links) model.Correlation {
	t.Helper()
	got := l.within(map[model.Ref]bool{refA: true, refB: true})
	if len(got) != 1 {
		t.Fatalf("correlations = %v, want one", got)
	}
	return got[0]
}

func TestLinksJoinEvidenceOfAPair(t *testing.T) {
	l := newLinks()
	l.heuristic(refA, refB, true, evA)
	l.identifier(refB, refA, evB)
	l.heuristic(refA, refB, false, evA)
	c := onlyLink(t, l)
	if c.From != refA || c.Confidence != model.ConfidenceIdentifier || c.Ambiguous || len(c.Evidence) != 2 {
		t.Errorf("correlation = %+v, want one identifier from a, not ambiguous, with both pieces of evidence", c)
	}
}

func TestLinksStayAmbiguousOnlyWhileEveryPieceIs(t *testing.T) {
	l := newLinks()
	l.heuristic(refA, refB, true, evA)
	l.heuristic(refA, refB, true, evB)
	if c := onlyLink(t, l); !c.Ambiguous || c.Confidence != model.ConfidenceHeuristic {
		t.Errorf("correlation = %+v, want an ambiguous heuristic", c)
	}
	l.heuristic(refA, refB, false, evB)
	if c := onlyLink(t, l); c.Ambiguous {
		t.Errorf("correlation = %+v, want it no longer ambiguous", c)
	}
}

func TestLinksWithinDropsOutsiders(t *testing.T) {
	l := newLinks()
	l.identifier(refA, refB, evA)
	if got := l.within(map[model.Ref]bool{refA: true}); len(got) != 0 {
		t.Errorf("within() = %v, want none: b is not known", got)
	}
	if !l.has(refB, refA) {
		t.Error("has(b, a) = false, want true whatever the direction")
	}
}
