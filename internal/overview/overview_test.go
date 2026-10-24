package overview

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// update rewrites the golden overview from the fixture stories:
// go test ./internal/overview -run TestOverviewGolden -update
var update = flag.Bool("update", false, "rewrite the golden overview")

// TestOverviewGolden builds the afternoon's overview, unfiltered and on one
// page, and compares it with the golden file the web tests render.
func TestOverviewGolden(t *testing.T) {
	rows, err := Build(afternoon, loadInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	page, err := Paginate(afternoon, rows, Filter{}, "", len(rows))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(goldenFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("overview differs from %s (run with -update after checking the change):\n%s", goldenFile, got)
	}
}

// Rows with something open at the window's end come first, then by their
// latest time in the window; the day before has no row.
func TestRowsInWindowMostRecentFirst(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	want := []int64{taskA, taskB, taskC, taskD, taskF, taskE}
	if got := taskIDs(rows); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// The plan's example: A, C and D were executing at 14:25, and C's two runs
// stay two runs of one profile alongside A's.
func TestConcurrentRunsAtTheCursor(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	if got, want := executingAt(rows, at(t, "14:25")), []int64{taskA, taskC, taskD}; !slices.Equal(got, want) {
		t.Errorf("executing at 14:25 = %v, want %v", got, want)
	}
	a, c := rowOf(t, rows, taskA), rowOf(t, rows, taskC)
	if a.Runs[0].Agent != c.Runs[0].Agent || a.Runs[0].Key == c.Runs[0].Key {
		t.Errorf("A's and C's first runs: %s %s and %s %s, want one profile, two runs",
			a.Runs[0].Agent, a.Runs[0].Key, c.Runs[0].Agent, c.Runs[0].Key)
	}
}

// A closed, unmerged pull request leaves its task as Vikunja says: D has
// no completion, only the close.
func TestClosedUnmergedLeavesTaskOpen(t *testing.T) {
	d := rowOf(t, buildAll(t, afternoon, Filter{}), taskD)
	if len(d.PullRequests) != 1 || d.PullRequests[0].State != PRClosed {
		t.Fatalf("D's pull requests = %+v, want one, closed", d.PullRequests)
	}
	if got, want := marksOf(d), "pr.opened pr.closed"; got != want {
		t.Errorf("D's marks = %q, want %q: no task.done", got, want)
	}
}

// F's completion and merge are two marks, and F ran before the window.
func TestMergeAndCompletionStayApart(t *testing.T) {
	f := rowOf(t, buildAll(t, afternoon, Filter{}), taskF)
	if got, want := marksOf(f), "pr.review_submitted pr.merged task.done"; got != want {
		t.Errorf("F's marks = %q, want %q", got, want)
	}
	if len(f.Runs) != 0 {
		t.Errorf("F's runs = %v, want none: it ran before the window", f.Runs)
	}
	if f.PullRequests[0].State != PRMerged {
		t.Errorf("F's pull request is %s, want merged", f.PullRequests[0].State)
	}
}

// E's history is missing: a Kelos gap covers its run, which the
// announcement alone puts in the window, with nothing known of its
// interval. Every other row's history is complete.
func TestMissingHistoryRow(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	for _, row := range rows {
		if want := row.Root.Task.ID == taskE; row.MissingHistory != want {
			t.Errorf("task %d: missing history %v, want %v", row.Root.Task.ID, row.MissingHistory, want)
		}
	}
	e := rowOf(t, rows, taskE)
	if len(e.Runs) != 1 || e.Runs[0].Interval.Start.State != model.BoundUnknown {
		t.Errorf("E's runs = %+v, want the announced run, start unknown", e.Runs)
	}
}

// A failure stays visible while its retry runs, even once it ended before
// the window.
func TestFailureStaysWithItsRetry(t *testing.T) {
	late := Window{From: at(t, "14:38"), To: afternoon.To}
	c := rowOf(t, buildAll(t, late, Filter{}), taskC)
	outcomes := make([]model.Outcome, 0, len(c.Runs))
	for _, r := range c.Runs {
		outcomes = append(outcomes, r.Outcome)
	}
	if want := []model.Outcome{model.OutcomeFailed, model.OutcomeRunning}; !slices.Equal(outcomes, want) {
		t.Errorf("C's runs from 14:38 = %v, want %v", outcomes, want)
	}
	if !(Filter{FailedRun: true}).Match(c) {
		t.Error("the failed-run filter does not match C from 14:38")
	}
}

// Review waits: A's and B's requests are still open at the last read.
func TestReviewWaits(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{ReviewRequested: true})
	if got, want := taskIDs(rows), []int64{taskA, taskB}; !slices.Equal(got, want) {
		t.Fatalf("rows with a review wait = %v, want %v", got, want)
	}
	for _, row := range rows {
		w := row.ReviewWaits[0]
		if w.Reviewer != "owner-gh" || w.Interval.End.State != model.BoundOpen {
			t.Errorf("task %d: wait %+v, want owner-gh's, open", row.Root.Task.ID, w)
		}
	}
}

// An input naming no root is an error, not a panic.
func TestBuildRejectsAnEmptyRoot(t *testing.T) {
	if _, err := Build(afternoon, []Input{{}}); err == nil {
		t.Error("Build accepted an input with no root")
	}
}
