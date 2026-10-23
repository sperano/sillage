package store

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func bucket(value string, minute int) model.Event {
	return observation(7, model.KindTaskBucketObserved, "bucket", value, minute)
}

func label(value string, minute int) model.Event {
	return observation(7, model.KindTaskLabelObserved, "label", value, minute)
}

// observedRange is an observation as stored: value, first and last seen.
type observedRange struct {
	value       string
	first, last time.Time
}

func ranges(story model.Story) []observedRange {
	var out []observedRange
	for _, e := range story.Events {
		r := observedRange{first: e.ObservedAt, last: e.ObservedAt}
		for _, v := range e.Details {
			r.value = v
		}
		if e.LastObservedAt != nil {
			r.last = *e.LastObservedAt
		}
		out = append(out, r)
	}
	return out
}

// TestObservationsStoredOncePerValue reads a card in Doing twice, in Done,
// then in Doing again, with a label throughout the first three reads.
func TestObservationsStoredOncePerValue(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	reads := [][]model.Event{
		{bucket("Doing", 0), label("in progress", 0)},
		{bucket("Doing", 10), label("in progress", 10)},
		{bucket("Done", 20), label("in progress", 20)},
		{bucket("Doing", 30)},
	}
	for _, events := range reads {
		mustSave(t, s, root, model.Records{Events: events})
	}
	// Saving a read again changes nothing.
	mustSave(t, s, root, model.Records{Events: reads[1]})

	want := []observedRange{
		{"Doing", at(0), at(10)},
		{"in progress", at(0), at(20)},
		{"Done", at(20), at(20)},
		{"Doing", at(30), at(30)},
	}
	story := mustStory(t, s, root)
	got := ranges(story)
	if len(got) != len(want) {
		t.Fatalf("observations = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].value != want[i].value || !got[i].first.Equal(want[i].first) || !got[i].last.Equal(want[i].last) {
			t.Errorf("observation %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The story read back, ranges included, saved into an empty store or
	// again into this one, reads back unchanged.
	for _, into := range []*Store{newTestStore(t, Retention{}), s} {
		mustSave(t, into, root, story.Records)
		if again := marshalStory(t, mustStory(t, into, root)); string(again) != string(marshalStory(t, story)) {
			t.Errorf("story saved again differs:\n%s", again)
		}
	}
}

// TestObservationsInOneBatch saves two reads in one batch.
func TestObservationsInOneBatch(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	mustSave(t, s, root, model.Records{Events: []model.Event{bucket("Doing", 10), bucket("Doing", 0)}})
	got := ranges(mustStory(t, s, root))
	if len(got) != 1 || !got[0].first.Equal(at(0)) || !got[0].last.Equal(at(10)) {
		t.Errorf("observations = %+v, want Doing from minute 0 to 10", got)
	}
}

// TestObservationCorrelationFollowsRange links the second read's
// observation, stored as the first's range, to a run.
func TestObservationCorrelationFollowsRange(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	first, second := bucket("Doing", 0), bucket("Doing", 10)
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{Events: []model.Event{first}})
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{second},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(second.Ref(), run.Key.Ref())},
	})
	story := mustStory(t, s, root)
	if len(story.Correlations) != 1 || story.Correlations[0].From != first.Ref() {
		t.Errorf("correlations = %+v, want one from the stored observation %s", story.Correlations, first.Key())
	}
}
