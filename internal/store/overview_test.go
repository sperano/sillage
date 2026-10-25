package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/overview"
)

// overviewStoriesDir holds the overview fixture set: one afternoon of
// eight tasks, one file each.
const overviewStoriesDir = "../../testdata/fixtures/overview/stories"

type overviewFixture struct {
	Title string     `json:"title"`
	Root  model.Root `json:"root"`
	model.Records
}

// saveOverviewFixtures saves every story of the overview fixture set and
// returns them as overview inputs.
func saveOverviewFixtures(t *testing.T, s *Store) []overview.Input {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(overviewStoriesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no overview fixtures in %s: %v", overviewStoriesDir, err)
	}
	var inputs []overview.Input
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var f overviewFixture
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if err := s.Save(t.Context(), Batch{Root: f.Root, Title: f.Title, Records: f.Records}); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		inputs = append(inputs, overview.Input{Root: f.Root, Title: f.Title, Records: f.Records})
	}
	return inputs
}

// fixtureHour is an hour of the fixture set: day 10 or 11 of March 2026.
func fixtureHour(day, hour int) overview.Window {
	from := time.Date(2026, 3, day, hour, 0, 0, 0, time.UTC)
	return overview.Window{From: from, To: from.Add(time.Hour)}
}

func activityIDs(t *testing.T, s *Store, q ActivityQuery) []int64 {
	t.Helper()
	found, err := s.Activity(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(found))
	for _, a := range found {
		ids = append(ids, a.Root.Task.ID)
	}
	return ids
}

func TestActivityRoots(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveOverviewFixtures(t, s)
	afternoon, nextMorning := fixtureHour(10, 14), fixtureHour(11, 10)
	for _, c := range []struct {
		name string
		q    ActivityQuery
		want []int64
	}{
		{"the afternoon", ActivityQuery{From: afternoon.From, To: afternoon.To, Kinds: overview.Kinds},
			[]int64{201, 202, 203, 204, 205, 206, 208}},
		{"one project", ActivityQuery{From: afternoon.From, To: afternoon.To, Project: "gadgets", Kinds: overview.Kinds},
			[]int64{203}},
		// The next morning: the pull request still open and the run still
		// running, nothing that ended (202's pull request closed at 15:02).
		{"what stayed open", ActivityQuery{From: nextMorning.From, To: nextMorning.To, Kinds: overview.Kinds},
			[]int64{201, 203}},
		{"nothing before", ActivityQuery{From: afternoon.From.AddDate(0, 0, -3), To: afternoon.From.AddDate(0, 0, -2),
			Kinds: overview.Kinds}, []int64{}},
	} {
		if got := activityIDs(t, s, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: roots = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestActivityRootsCompletionReads: every read of a task records its
// completion observation, so one selects its root only when it saw the
// task not done after a completion, the read a row shows as a reopening.
func TestActivityRootsCompletionReads(t *testing.T) {
	s := newTestStore(t, Retention{})
	for task, reads := range map[int64][][]model.Event{
		// Never done, first read in the window.
		7: {completionRead(7, false, 10)},
		// Done at minute 5, first read in the window, still done.
		8: {completionRead(8, true, 6)},
		// Done at minute 5, then a read in the window saw it not done.
		9: {completionRead(9, true, 6), completionRead(9, false, 20)},
	} {
		for _, read := range reads {
			mustSave(t, s, taskRoot(task), model.Records{Events: read})
		}
	}
	q := ActivityQuery{From: at(6), To: at(30), Kinds: overview.Kinds}
	if got, want := activityIDs(t, s, q), []int64{9}; !slices.Equal(got, want) {
		t.Errorf("roots = %v, want %v", got, want)
	}
}

func TestActivityTooManyRoots(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveOverviewFixtures(t, s)
	w := fixtureHour(10, 14)
	if _, err := s.Activity(t.Context(), ActivityQuery{From: w.From, To: w.To, Kinds: overview.Kinds, Limit: 2}); !errors.Is(err, ErrTooManyRoots) {
		t.Errorf("error = %v, want ErrTooManyRoots", err)
	}
	if _, err := s.Activity(t.Context(), ActivityQuery{From: w.To, To: w.From}); err == nil {
		t.Error("a window ending before it starts was accepted")
	}
}

// The overview built from what the store reads is the one built from the
// fixture files, in every window: the store reads every record it needs.
func TestActivityBuildsTheSameOverview(t *testing.T) {
	s := newTestStore(t, Retention{})
	inputs := saveOverviewFixtures(t, s)
	for _, w := range []overview.Window{fixtureHour(10, 13), fixtureHour(10, 14), fixtureHour(10, 15), fixtureHour(11, 10)} {
		found, err := s.Activity(t.Context(), ActivityQuery{From: w.From, To: w.To, Kinds: overview.Kinds})
		if err != nil {
			t.Fatal(err)
		}
		stored := make([]overview.Input, 0, len(found))
		for _, a := range found {
			stored = append(stored, overview.Input{Root: a.Root, Title: a.Title, Records: a.Records})
		}
		got, want := pageOf(t, w, stored), pageOf(t, w, inputs)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("window %s: overview from the store\n%+v\nwant\n%+v", w.From, got, want)
		}
	}
}

// TestActivityOverviewOfTheCorpus saves every story of the edge-case
// corpus into one store and builds the overview of each hour of their
// days from what Activity reads: it is the one built from the fixture
// files, so the candidate query keeps every root with a row and Activity
// loads every record a row needs, the correlations that tell a root's own
// runs from its records' candidates included.
func TestActivityOverviewOfTheCorpus(t *testing.T) {
	s := newTestStore(t, Retention{})
	var inputs []overview.Input
	for _, name := range fixtureNames(t) {
		f, _ := loadFixture(t, name)
		mustSave(t, s, f.Root, f.Records)
		inputs = append(inputs, overview.Input{Root: f.Root, Records: f.Records})
	}
	for _, day := range corpusDays(inputs) {
		for w := (overview.Window{From: day, To: day.Add(time.Hour)}); w.From.Before(day.AddDate(0, 0, 1)); w = (overview.Window{
			From: w.To, To: w.To.Add(time.Hour)}) {
			got, want := pageOf(t, w, storedInputs(t, s, w)), pageOf(t, w, inputs)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("window %s: overview from the store\n%+v\nwant\n%+v", w.From, got, want)
			}
		}
	}
}

// corpusDays returns the UTC days the inputs' events and runs fall on, in
// order.
func corpusDays(inputs []overview.Input) []time.Time {
	var days []time.Time
	add := func(t time.Time, ok bool) {
		if d := t.UTC().Truncate(day); ok && !slices.ContainsFunc(days, d.Equal) {
			days = append(days, d)
		}
	}
	for _, in := range inputs {
		for _, e := range in.Records.Events {
			add(e.Time(), true)
		}
		for _, r := range in.Records.Runs {
			add(r.Interval.Start.Time())
			add(r.Interval.End.Time())
		}
	}
	slices.SortFunc(days, time.Time.Compare)
	return days
}

// storedInputs returns what Activity reads for w, as overview inputs.
func storedInputs(t *testing.T, s *Store, w overview.Window) []overview.Input {
	t.Helper()
	found, err := s.Activity(t.Context(), ActivityQuery{From: w.From, To: w.To, Kinds: overview.Kinds})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]overview.Input, 0, len(found))
	for _, a := range found {
		out = append(out, overview.Input{Root: a.Root, Title: a.Title, Records: a.Records})
	}
	return out
}

func pageOf(t *testing.T, w overview.Window, inputs []overview.Input) overview.Page {
	t.Helper()
	rows, err := overview.Build(w, inputs)
	if err != nil {
		t.Fatal(err)
	}
	page, err := overview.Paginate(w, rows, overview.Filter{}, "", len(rows)+1)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// The correlations' query names the run ref kind as an SQL literal: it
// must stay the model's.
func TestActivitySQLRefKindIsTheModels(t *testing.T) {
	if !strings.Contains(activityCorrelationsSQL, "'"+string(model.RefRun)+"'") {
		t.Errorf("the correlations' query does not name %s", model.RefRun)
	}
}

// The candidate query names pull request kinds as SQL literals: they must
// stay the model's.
func TestActivitySQLKindsAreTheModels(t *testing.T) {
	for _, k := range []model.EventKind{
		model.KindPROpened, model.KindPRReopened, model.KindReviewRequested, model.KindPRMerged, model.KindPRClosed,
	} {
		if !strings.Contains(activeRootsSQL, "'"+string(k)+"'") {
			t.Errorf("the candidate query does not name %s", k)
		}
	}
}
