// Package overview builds the multi-task overview: for one window of time,
// a row per task (or wiki page) with the runs, pull requests, review waits
// and state changes it held in that window, and the coverage that says
// where its history is missing. It answers "what was working on what?",
// and leaves every handoff detail to the single-task story.
//
// It reads stored records only and follows the model's rules: a run's
// interval comes from its own bounds, a review wait from a request to its
// answer, a pull request's open period from its opening to its merge or
// close. Nothing is drawn between two unrelated records, so the overview
// never measures idle time, least of all on a row whose history is
// missing. A pull request closed without a merge never closes its task:
// the task's state is Vikunja's alone. A failed attempt stays in its row
// while a retry of the same Kelos Task runs, even when it ended before the
// window. Comments, observations and webhook deliveries are not rows'
// content: they belong to the single-task story.
package overview

import (
	"fmt"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// Window is the span of time an overview covers: from its start,
// inclusive, to its end, exclusive.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Contains reports whether t is in the window.
func (w Window) Contains(t time.Time) bool { return !t.Before(w.From) && t.Before(w.To) }

// Input is one root's stored records that an overview reads: its runs,
// pull requests, coverage, and the events of the kinds in Kinds.
type Input struct {
	Root    model.Root
	Title   string
	Records model.Records
}

// Kinds are the event kinds an overview reads; others are ignored. Read
// only: callers pass it on, never change it.
var Kinds = []model.EventKind{
	model.KindTaskDone,
	model.KindRunAnnounced,
	model.KindPROpened,
	model.KindPRMerged,
	model.KindPRClosed,
	model.KindPRReopened,
	model.KindReviewRequested,
	model.KindReviewRequestRemoved,
	model.KindReviewSubmitted,
}

// Row is one task or page in the window.
type Row struct {
	Root  model.Root `json:"root"`
	Title string     `json:"title,omitempty"`
	// Projects are the project labels of every run of the root, in or out
	// of the window, sorted: what the project filter matches.
	Projects []string `json:"projects,omitempty"`
	// Runs are the runs with a recorded time in the window or an interval
	// across it, and each earlier failed attempt of the same Kelos Task.
	Runs []model.Run `json:"runs"`
	// PullRequests are those whose open period or state changes touch the
	// window.
	PullRequests []PullRequest `json:"pullRequests"`
	// ReviewWaits are the review requests waiting for an answer at some
	// time in the window.
	ReviewWaits []ReviewWait `json:"reviewWaits"`
	// Marks are the state changes in the window: completions, pull request
	// openings, merges, closes, reopenings and reviews, and start
	// announcements.
	Marks []Mark `json:"marks"`
	// Coverage is the root's coverage, for every source instance.
	Coverage []model.Coverage `json:"coverage"`
	// MissingHistory says that part of the window is unknown for one of the
	// row's sources: a gap in it, no successful read up to its start, no
	// read at all, or no coverage of a source the row has records from.
	MissingHistory bool `json:"missingHistory"`
	// LastActivity is the latest time in the window the row shows
	// anything: the window's end for an interval still open across it.
	// Rows are ordered by it, most recent first.
	LastActivity time.Time `json:"lastActivity"`
}

// PRState is a pull request's state at the end of the window.
type PRState string

// The states of a pull request.
const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	// PRClosed: closed without a merge. Its task stays as Vikunja says.
	PRClosed PRState = "closed"
	// PRUnknown: no opening, merge or close recorded before the window's
	// end.
	PRUnknown PRState = "unknown"
)

// PullRequest is one pull request of a row: when it was open, and its state
// at the window's end.
type PullRequest struct {
	Key    model.PRKey `json:"key"`
	Branch string      `json:"branch"`
	// Periods are the spans it was open, from an opening or reopening to a
	// merge or close, in time order. A period without a recorded opening
	// starts unknown; one not merged or closed at the last read ends open.
	Periods []model.Interval `json:"periods"`
	State   PRState          `json:"state"`
}

// ReviewWait is a review request until its answer: the requested reviewer's
// review, or the request's removal. Without one, it is open at the last
// read, unless the pull request was merged or closed since: then its end is
// unknown.
type ReviewWait struct {
	PR model.PRKey `json:"pr"`
	// Reviewer or Team is who was asked.
	Reviewer string `json:"reviewer,omitempty"`
	Team     string `json:"team,omitempty"`
	// Request is the key of the request event.
	Request  string         `json:"request"`
	Interval model.Interval `json:"interval"`
	// Closed is the merge, or the close not followed by a reopening, that
	// left the request unanswered: until then it was waiting, after it its
	// answer is unknown. Set exactly when the interval's end is unknown.
	Closed *time.Time `json:"closed,omitempty"`
}

// Mark is one state change in the window.
type Mark struct {
	Kind model.EventKind `json:"kind"`
	At   time.Time       `json:"at"`
	// Event is the key of the event; Source and Instance are where it was
	// read, whose coverage says whether the row's history there is known.
	Event    string       `json:"event"`
	Source   model.Source `json:"source"`
	Instance string       `json:"instance"`
	PR       *model.PRKey `json:"pr,omitempty"`
	// Pod is the pod a start announcement names, and Namespace its
	// namespace, when the announcement states one.
	Pod       string `json:"pod,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// Build returns the row of each input with something in the window, in no
// particular order: Page filters, orders and pages them. An input whose
// records do not make a valid story is an error.
func Build(w Window, inputs []Input) ([]Row, error) {
	rows := []Row{}
	for _, in := range inputs {
		story, err := model.BuildStory(in.Root, onlyKinds(in.Records))
		if err != nil {
			return nil, fmt.Errorf("overview: root %q: %w", rootKey(in.Root), err)
		}
		if row, ok := buildRow(w, in.Title, story); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// kindSet holds Kinds.
var kindSet = func() map[model.EventKind]bool {
	set := make(map[model.EventKind]bool, len(Kinds))
	for _, k := range Kinds {
		set[k] = true
	}
	return set
}()

// onlyKinds keeps the events of Kinds and drops correlations, which the
// overview does not read.
func onlyKinds(recs model.Records) model.Records {
	var events []model.Event
	for _, e := range recs.Events {
		if kindSet[e.Kind] {
			events = append(events, e)
		}
	}
	recs.Events = events
	recs.Correlations = nil
	return recs
}
