package store

import (
	"reflect"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// The collector decides a correlation's confidence, evidence and ambiguity
// per story. Two task stories can share a record pair while only one has
// enough candidates to make its correlation ambiguous, its evidence note
// saying how many; each keeps its own correlation whichever is saved last.
// The evidence is in the order a story sorts it: by source.
func TestCorrelationAmbiguityIsKeptPerRoot(t *testing.T) {
	event := comment(7, "comments/1", 1)
	run := finishedRun("uid-1", 2, 3, model.OutcomeSucceeded)
	certain := link(event.Ref(), run.Key.Ref())
	certain.Evidence = append([]model.Evidence{kelosEvidence("pods/pod-1", "no other run in agents was running")}, certain.Evidence...)
	ambiguous := link(event.Ref(), run.Key.Ref())
	ambiguous.Confidence = model.ConfidenceHeuristic
	ambiguous.Ambiguous = true
	ambiguous.Evidence = append([]model.Evidence{kelosEvidence("pods/pod-1", "2 runs in agents were running")}, ambiguous.Evidence...)
	records := func(c model.Correlation) model.Records {
		return model.Records{Events: []model.Event{event}, Runs: []model.Run{run}, Correlations: []model.Correlation{c}}
	}
	type save struct {
		root model.Root
		recs model.Records
	}
	for _, c := range []struct {
		name  string
		order []save
	}{
		{"ambiguous first", []save{{taskRoot(7), records(ambiguous)}, {taskRoot(8), records(certain)}}},
		{"certain first", []save{{taskRoot(8), records(certain)}, {taskRoot(7), records(ambiguous)}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newTestStore(t, Retention{})
			for _, in := range c.order {
				mustSave(t, s, in.root, in.recs)
			}
			wantCorrelation(t, mustStory(t, s, taskRoot(7)).Correlations, ambiguous)
			wantCorrelation(t, mustStory(t, s, taskRoot(8)).Correlations, certain)
		})
	}
}

// wantCorrelation checks that correlations hold want's pair as want.
func wantCorrelation(t *testing.T, correlations []model.Correlation, want model.Correlation) {
	t.Helper()
	for _, c := range correlations {
		if c.Pair() == want.Pair() {
			if !reflect.DeepEqual(c, want) {
				t.Errorf("%s = %#v, want %#v", want.Pair(), c, want)
			}
			return
		}
	}
	t.Errorf("no correlation %s", want.Pair())
}
