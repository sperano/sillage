package overview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/testzone"
)

// TestMain runs the tests outside UTC, so that output depending on the
// host's zone fails here too.
func TestMain(m *testing.M) { testzone.Main(m) }

// The overview fixture set: fabricated stories of seven tasks over one
// afternoon, and the golden overview of its window.
const (
	fixturesDir = "../../testdata/fixtures/overview"
	storiesDir  = fixturesDir + "/stories"
	goldenFile  = fixturesDir + "/overview.golden.json"
)

// afternoon is the fixture set's window.
var afternoon = Window{
	From: time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC),
	To:   time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC),
}

// The fixture tasks, by id.
const (
	taskA = 201 // review requested, unanswered
	taskB = 202 // review requested, unanswered
	taskC = 203 // failed run, then a retry still running
	taskD = 204 // pull request closed without a merge
	taskE = 205 // run known from its announcement only, Kelos gap
	taskF = 206 // merged and done in the window, ran before it
	taskG = 207 // the day before: outside the window
)

// fixtureStory is one fixture file: a story's records, its title and
// what it shows.
type fixtureStory struct {
	Description string     `json:"description"`
	Title       string     `json:"title"`
	Root        model.Root `json:"root"`
	model.Records
}

// loadInputs reads every fixture story of the set.
func loadInputs(t *testing.T) []Input {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(storiesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no overview fixture stories in %s: %v", storiesDir, err)
	}
	var inputs []Input
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var s fixtureStory
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if s.Description == "" || s.Title == "" {
			t.Fatalf("%s: a description and a title are required", p)
		}
		inputs = append(inputs, Input{Root: s.Root, Title: s.Title, Records: s.Records})
	}
	return inputs
}

// buildAll builds every fixture row of w, in page order.
func buildAll(t *testing.T, w Window, f Filter) []Row {
	t.Helper()
	rows, err := Build(w, loadInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	page, err := Paginate(w, rows, f, "", len(rows)+1)
	if err != nil {
		t.Fatal(err)
	}
	return page.Rows
}

// taskIDs lists the rows' task ids, in order.
func taskIDs(rows []Row) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Root.Task.ID)
	}
	return ids
}

// rowOf returns the row of task id; it fails the test when there is none.
func rowOf(t *testing.T, rows []Row, id int64) Row {
	t.Helper()
	i := slices.IndexFunc(rows, func(r Row) bool { return r.Root.Task != nil && r.Root.Task.ID == id })
	if i < 0 {
		t.Fatalf("no row of task %d in %v", id, taskIDs(rows))
	}
	return rows[i]
}

// executingAt lists the tasks with a run whose recorded interval holds at:
// started at or before it and ended after it, or not ended at the last
// read.
func executingAt(rows []Row, at time.Time) []int64 {
	var ids []int64
	for _, row := range rows {
		if slices.ContainsFunc(row.Runs, func(r model.Run) bool { return runHolds(r, at) }) {
			ids = append(ids, row.Root.Task.ID)
		}
	}
	return ids
}

func runHolds(r model.Run, at time.Time) bool {
	start, ok := r.Interval.Start.Time()
	if !ok || start.After(at) {
		return false
	}
	end, ok := r.Interval.End.Time()
	return r.Interval.End.State == model.BoundOpen || (ok && end.After(at))
}

// at is a time of the fixture afternoon: "14:25".
func at(t *testing.T, clock string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, "2026-03-10T"+clock+":00Z")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// marksOf lists a row's mark kinds, in order.
func marksOf(row Row) string {
	kinds := make([]string, 0, len(row.Marks))
	for _, m := range row.Marks {
		kinds = append(kinds, string(m.Kind))
	}
	return strings.Join(kinds, " ")
}
