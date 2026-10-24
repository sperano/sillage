package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/overview"
)

// ambiguityRun is the run of task 103 that wiki-rooted-request's live edit
// is an ambiguous candidate of.
const ambiguityRun = "cluster-a/agents/uid/00000000-0000-4000-8000-000000000103"

// The collector decides a correlation's ambiguity per story: the live edit
// of page 57 is ambiguous between two runs in the page's story, and
// certain in the story of a task holding only one of them. Each story
// keeps its own, whichever was saved last, so the page's overview row
// never takes the task's run for its own.
func TestCorrelationAmbiguityIsKeptPerRoot(t *testing.T) {
	page, _ := loadFixture(t, "wiki-rooted-request")
	task, edit, pair := certainTaskStory(t, page.Records)
	hour := edit.Time().Truncate(time.Hour)
	w := overview.Window{From: hour, To: hour.Add(time.Hour)}
	inputs := []overview.Input{
		{Root: page.Root, Records: page.Records},
		{Root: task.Root, Records: task.Records},
	}
	for _, c := range []struct {
		name  string
		order []overview.Input
	}{
		{"page first", inputs},
		{"task first", []overview.Input{inputs[1], inputs[0]}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newTestStore(t, Retention{})
			for _, in := range c.order {
				mustSave(t, s, in.Root, in.Records)
			}
			wantAmbiguous(t, mustStory(t, s, page.Root).Correlations, pair, true)
			wantAmbiguous(t, mustStory(t, s, task.Root).Correlations, pair, false)
			if got, want := pageOf(t, w, storedInputs(t, s, w)), pageOf(t, w, inputs); !reflect.DeepEqual(got, want) {
				t.Errorf("overview from the store\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// certainTaskStory returns a story of task 103 holding its run and the
// page's live edit linked to it without ambiguity, the edit, and the pair.
func certainTaskStory(t *testing.T, page model.Records) (overview.Input, model.Event, model.RefPair) {
	t.Helper()
	run := model.Ref{Kind: model.RefRun, ID: ambiguityRun}
	for _, c := range page.Correlations {
		if !c.Ambiguous || (c.To != run && c.From != run) {
			continue
		}
		certain := c
		certain.Ambiguous = false
		recs := model.Records{Correlations: []model.Correlation{certain}}
		var edit model.Event
		for _, e := range page.Events {
			if model.PairOf(e.Ref(), run) == c.Pair() {
				edit = e
				recs.Events = append(recs.Events, e)
			}
		}
		for _, r := range page.Runs {
			if r.Key.Ref() == run {
				recs.Runs = append(recs.Runs, r)
			}
		}
		root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 103}}
		return overview.Input{Root: root, Records: recs}, edit, c.Pair()
	}
	t.Fatalf("no ambiguous correlation names %s", run)
	return overview.Input{}, model.Event{}, model.RefPair{}
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
