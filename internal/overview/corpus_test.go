package overview

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
)

// corpusDir holds the history edge-case corpus: one fabricated story per
// directory, each written for one invariant of the single-task story.
const corpusDir = "../../testdata/fixtures/stories"

const (
	// sweepStep is the length of each window of a corpus story's day the
	// sweep builds.
	sweepStep = time.Hour
	// day is a UTC day: the sweep's span, and a whole-day window's.
	day = 24 * time.Hour
)

// corpusCase is what the overview of one corpus story must show: in the
// window from at for an hour, unless span says otherwise.
type corpusCase struct {
	at    string
	span  time.Duration
	check func(*testing.T, Window, []Row)
}

// corpusCases are the overview's reading of each corpus story's point.
// Each corpus story needs one: TestEveryCorpusStoryHasAnOverviewCase
// fails until it has.
var corpusCases = map[string]corpusCase{
	"discuss-no-pr":           {"2026-03-02T14:00:00Z", 0, checkCorpusDiscuss},
	"implementation-merged":   {"2026-03-03T18:00:00Z", 2 * time.Hour, checkCorpusMerged},
	"wiki-rooted-request":     {"2026-03-04T15:00:00Z", 0, checkCorpusWikiRooted},
	"failed-run-retry":        {"2026-03-05T14:00:00Z", 0, checkCorpusRetry},
	"announced-run-only":      {"2026-01-12T14:00:00Z", 0, checkCorpusAnnounced},
	"checks-after-force-push": {"2026-03-11T00:00:00Z", day, checkCorpusChecksOnly},
	"reopened-task":           {"2026-03-12T11:00:00Z", 0, checkCorpusReopened},
	"second-pr-after-close":   {"2026-03-13T09:00:00Z", 0, checkCorpusSecondPR},
}

func (c corpusCase) window(t *testing.T) Window {
	t.Helper()
	from, err := time.Parse(time.RFC3339, c.at)
	if err != nil {
		t.Fatal(err)
	}
	return Window{From: from, To: from.Add(cmp.Or(c.span, sweepStep))}
}

// loadCorpus reads corpus story name's records as an overview input.
func loadCorpus(t *testing.T, name string) Input {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpusDir, name, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s fixtureStory
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return Input{Root: s.Root, Title: name, Records: s.Records}
}

// corpusRows builds the overview of in alone in w, in page order.
func corpusRows(t *testing.T, w Window, in Input) []Row {
	t.Helper()
	rows, err := Build(w, []Input{in})
	if err != nil {
		t.Fatal(err)
	}
	page, err := Paginate(w, rows, Filter{}, "", len(rows)+1)
	if err != nil {
		t.Fatal(err)
	}
	return page.Rows
}

func TestEveryCorpusStoryHasAnOverviewCase(t *testing.T) {
	for _, name := range storytest.Names() {
		if _, ok := corpusCases[name]; !ok {
			t.Errorf("corpus story %s has no overview case", name)
		}
	}
}

func TestCorpusOverview(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			c := corpusCases[name]
			w := c.window(t)
			c.check(t, w, corpusRows(t, w, loadCorpus(t, name)))
		})
	}
}

// TestCorpusSweep builds every corpus story's overview in each hour of its
// day: what a row shows is in its window, and every state change of the
// story shows in the hour it happened.
func TestCorpusSweep(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			in := loadCorpus(t, name)
			from := corpusCases[name].window(t).From.Truncate(day)
			for start := from; start.Before(from.Add(day)); start = start.Add(sweepStep) {
				w := Window{From: start, To: start.Add(sweepStep)}
				rows := corpusRows(t, w, in)
				for _, row := range rows {
					checkRowInWindow(t, w, row)
				}
				checkMarksShown(t, w, in.Records.Events, rows)
			}
		})
	}
}

// checkRowInWindow asserts that the row's marks and last activity are in
// its window.
func checkRowInWindow(t *testing.T, w Window, row Row) {
	t.Helper()
	for _, m := range row.Marks {
		if !w.Contains(m.At) {
			t.Errorf("window %s: mark %s at %s is outside it", w.From, m.Kind, m.At)
		}
	}
	if row.LastActivity.Before(w.From) || row.LastActivity.After(w.To) {
		t.Errorf("window %s: last activity %s is outside it", w.From, row.LastActivity)
	}
}

// wantMarkKinds are the events a row must show as marks, written apart
// from markKinds so that a kind dropped from it fails the sweep.
var wantMarkKinds = []model.EventKind{
	model.KindTaskDone,
	model.KindRunAnnounced,
	model.KindPROpened,
	model.KindPRMerged,
	model.KindPRClosed,
	model.KindPRReopened,
	model.KindReviewSubmitted,
}

// checkMarksShown asserts that each event of a wanted mark kind in w is a
// mark of the window's one row.
func checkMarksShown(t *testing.T, w Window, events []model.Event, rows []Row) {
	t.Helper()
	for _, e := range events {
		if !slices.Contains(wantMarkKinds, e.Kind) || !w.Contains(e.Time()) {
			continue
		}
		if len(rows) != 1 || !slices.ContainsFunc(rows[0].Marks, func(m Mark) bool { return m.Event == e.Key() }) {
			t.Errorf("window %s: %s at %s is not a mark", w.From, e.Kind, e.Time())
		}
	}
}

// onlyRow returns the one row of rows, failing the test when there is not
// exactly one.
func onlyRow(t *testing.T, rows []Row) Row {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	return rows[0]
}

func prStates(row Row) map[int]PRState {
	out := make(map[int]PRState, len(row.PullRequests))
	for _, p := range row.PullRequests {
		out[p.Key.Number] = p.State
	}
	return out
}

func hasMark(row Row, kind model.EventKind) bool {
	return slices.ContainsFunc(row.Marks, func(m Mark) bool { return m.Kind == kind })
}

// The corpus records the cases below name.
const (
	corpusPageRun      = "00000000-0000-4000-8000-000000000057"
	corpusFailedRun    = "00000000-0000-4000-8000-00000000104a"
	corpusRetryRun     = "00000000-0000-4000-8000-00000000104b"
	corpusAnnouncedPod = "vikunja-widgets-agent-opus-105-x8z2w"
	corpusMergedPR     = 7
	corpusClosedPR     = 62
	corpusSecondPR     = 63
)

// A discussion: its run and announcement, no pull request, complete
// history.
func checkCorpusDiscuss(t *testing.T, _ Window, rows []Row) {
	row := onlyRow(t, rows)
	if len(row.Runs) != 1 || len(row.PullRequests) != 0 || len(row.ReviewWaits) != 0 {
		t.Errorf("row = %d runs, %d pull requests, %d review waits; want 1, 0, 0",
			len(row.Runs), len(row.PullRequests), len(row.ReviewWaits))
	}
	if got := marksOf(row); got != string(model.KindRunAnnounced) {
		t.Errorf("marks = %q, want the announcement only", got)
	}
	if row.MissingHistory {
		t.Error("every source was read after the window: history is not missing")
	}
}

// The merge and the completion are two marks, and the pull request reads
// merged at the window's end; an hour earlier, open and the task not done.
func checkCorpusMerged(t *testing.T, w Window, rows []Row) {
	row := onlyRow(t, rows)
	if !hasMark(row, model.KindPRMerged) || !hasMark(row, model.KindTaskDone) {
		t.Errorf("marks = %q, want the merge and the completion", marksOf(row))
	}
	if got := prStates(row)[corpusMergedPR]; got != PRMerged {
		t.Errorf("pull request %d = %s, want merged", corpusMergedPR, got)
	}
	first := Window{From: w.From, To: w.From.Add(sweepStep)}
	early := onlyRow(t, corpusRows(t, first, loadCorpus(t, "implementation-merged")))
	if got := prStates(early)[corpusMergedPR]; got != PROpen || hasMark(early, model.KindTaskDone) {
		t.Errorf("first hour: pull request %s, marks %q; want open, no completion", got, marksOf(early))
	}
}

// The page's own run only: the other run of the same agent profile is in
// the story as an ambiguous candidate for the live edit, not as the
// page's work.
func checkCorpusWikiRooted(t *testing.T, _ Window, rows []Row) {
	row := onlyRow(t, rows)
	if row.Root.Page == nil {
		t.Fatalf("root = %+v, want the page", row.Root)
	}
	if got := runUIDs(row.Runs); !slices.Equal(got, []string{corpusPageRun}) {
		t.Errorf("runs = %v, want the page's own run %s", got, corpusPageRun)
	}
}

// The failed attempt stays while its retry runs, and the failed filter
// keeps the row; the hour before, the failure alone.
func checkCorpusRetry(t *testing.T, w Window, rows []Row) {
	row := onlyRow(t, rows)
	if got := runUIDs(row.Runs); !slices.Equal(got, []string{corpusFailedRun, corpusRetryRun}) {
		t.Errorf("runs = %v, want the failure and its retry", got)
	}
	if !(Filter{FailedRun: true}).Match(row) {
		t.Error("the failed filter drops the row of a retry")
	}
	before := Window{From: w.From.Add(-sweepStep), To: w.From}
	if got := runUIDs(onlyRow(t, corpusRows(t, before, loadCorpus(t, "failed-run-retry"))).Runs); !slices.Equal(got,
		[]string{corpusFailedRun}) {
		t.Errorf("the hour before: runs = %v, want the failure alone", got)
	}
}

// A run known only from its announcement: in the row through the
// announcement, its bounds unknown, the history missing.
func checkCorpusAnnounced(t *testing.T, _ Window, rows []Row) {
	row := onlyRow(t, rows)
	if len(row.Runs) != 1 || row.Runs[0].Pod != corpusAnnouncedPod {
		t.Fatalf("runs = %+v, want the announced pod's", row.Runs)
	}
	if b := row.Runs[0].Interval; b.Start.State != model.BoundUnknown || b.End.State != model.BoundUnknown {
		t.Errorf("interval = %+v, want unknown bounds", b)
	}
	if len(row.Marks) != 1 || row.Marks[0].Pod != corpusAnnouncedPod || !row.LastActivity.Equal(row.Marks[0].At) {
		t.Errorf("marks = %+v, last activity %s; want the announcement, and nothing after it", row.Marks, row.LastActivity)
	}
	if !row.MissingHistory {
		t.Error("a run in a Kelos gap leaves the history complete")
	}
}

// A pull request with only checks: the overview reads no checks, and
// nothing records its opening or close, so no window of the day has a
// row. Its checks are in the single-task story.
func checkCorpusChecksOnly(t *testing.T, w Window, rows []Row) {
	if len(rows) != 0 {
		t.Errorf("window %s: rows = %+v, want none", w.From, rows)
	}
}

// One completion, at the latest done_at: none invented before it.
func checkCorpusReopened(t *testing.T, w Window, rows []Row) {
	if got := marksOf(onlyRow(t, rows)); got != string(model.KindTaskDone) {
		t.Errorf("marks = %q, want one completion", got)
	}
	earlier := Window{From: w.From.Add(-day), To: w.From}
	if got := corpusRows(t, earlier, loadCorpus(t, "reopened-task")); len(got) != 0 {
		t.Errorf("the day before the last completion: rows = %+v, want none", got)
	}
}

// The first pull request closed unmerged, the second open; the task is
// not marked done, and since Vikunja was never read, its completion is
// unknown rather than "not done".
func checkCorpusSecondPR(t *testing.T, _ Window, rows []Row) {
	row := onlyRow(t, rows)
	want := map[int]PRState{corpusClosedPR: PRClosed, corpusSecondPR: PROpen}
	if got := prStates(row); len(got) != len(want) || got[corpusClosedPR] != PRClosed || got[corpusSecondPR] != PROpen {
		t.Errorf("pull requests = %v, want %v", got, want)
	}
	if hasMark(row, model.KindTaskDone) {
		t.Error("a closed pull request completed the task")
	}
	if !row.MissingHistory {
		t.Error("a task row with no Vikunja coverage reads as complete")
	}
}
