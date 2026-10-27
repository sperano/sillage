package store

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// The collector decides a correlation's ambiguity per story. Two task
// stories can share a record pair while only one has enough candidates to
// make its correlation ambiguous; each keeps its own value whichever is
// saved last.
func TestCorrelationAmbiguityIsKeptPerRoot(t *testing.T) {
	event := comment(7, "comments/1", 1)
	run := finishedRun("uid-1", 2, 3, model.OutcomeSucceeded)
	certain := link(event.Ref(), run.Key.Ref())
	ambiguous := certain
	ambiguous.Ambiguous = true
	pair := certain.Pair()
	records := func(c model.Correlation) model.Records {
		return model.Records{Events: []model.Event{event}, Runs: []model.Run{run}, Correlations: []model.Correlation{c}}
	}
	for _, c := range []struct {
		name  string
		order []struct {
			root model.Root
			recs model.Records
		}
	}{
		{"ambiguous first", []struct {
			root model.Root
			recs model.Records
		}{{taskRoot(7), records(ambiguous)}, {taskRoot(8), records(certain)}}},
		{"certain first", []struct {
			root model.Root
			recs model.Records
		}{{taskRoot(8), records(certain)}, {taskRoot(7), records(ambiguous)}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newTestStore(t, Retention{})
			for _, in := range c.order {
				mustSave(t, s, in.root, in.recs)
			}
			wantAmbiguous(t, mustStory(t, s, taskRoot(7)).Correlations, pair, true)
			wantAmbiguous(t, mustStory(t, s, taskRoot(8)).Correlations, pair, false)
		})
	}
}

func wantAmbiguous(t *testing.T, correlations []model.Correlation, pair model.RefPair, want bool) {
	t.Helper()
	for _, c := range correlations {
		if c.Pair() == pair {
			if c.Ambiguous != want {
				t.Errorf("%s: ambiguous = %t, want %t", pair, c.Ambiguous, want)
			}
			return
		}
	}
	t.Errorf("no correlation %s", pair)
}
