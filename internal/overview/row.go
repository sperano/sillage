package overview

import (
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// markKinds are the events a row shows as marks.
var markKinds = map[model.EventKind]bool{
	model.KindTaskDone:        true,
	model.KindRunAnnounced:    true,
	model.KindPROpened:        true,
	model.KindPRMerged:        true,
	model.KindPRClosed:        true,
	model.KindPRReopened:      true,
	model.KindReviewSubmitted: true,
}

// detailPod is the start announcement's detail naming its pod.
const detailPod = "pod"

// buildRow returns the story's row, and false when nothing of it is in the
// window.
func buildRow(w Window, title string, s model.Story) (Row, bool) {
	row := Row{
		Root:         s.Root,
		Title:        title,
		Projects:     projects(s.Runs),
		Runs:         runsIn(w, s),
		PullRequests: []PullRequest{},
		ReviewWaits:  []ReviewWait{},
		Marks:        marksIn(w, s.Events),
		Coverage:     s.Coverage,
	}
	byPR := prEvents(s.Events)
	for _, pr := range s.PullRequests {
		events := byPR[pr.Key]
		if p, ok := pullRequest(w, pr, events); ok || hasMarkOf(row.Marks, pr.Key) {
			row.PullRequests = append(row.PullRequests, p)
		}
		row.ReviewWaits = append(row.ReviewWaits, reviewWaits(w, pr.Key, events)...)
	}
	if len(row.Runs) == 0 && len(row.PullRequests) == 0 && len(row.ReviewWaits) == 0 && len(row.Marks) == 0 {
		return Row{}, false
	}
	row.MissingHistory = missingHistory(w, row.Coverage, rowSources(w, row, s.Events))
	row.LastActivity = lastActivity(w, row, announcements(s.Events))
	return row, true
}

// projects lists the distinct project labels of the runs, sorted.
func projects(runs []model.Run) []string {
	var out []string
	for _, r := range runs {
		if r.Project != "" && !slices.Contains(out, r.Project) {
			out = append(out, r.Project)
		}
	}
	slices.Sort(out)
	return out
}

// announcements maps each pod a start announcement names to the
// announcements' times: a pod belongs to one attempt, so it identifies the
// run whatever the run's key.
func announcements(events []model.Event) map[string][]time.Time {
	by := make(map[string][]time.Time)
	for _, e := range events {
		if pod := e.Details[detailPod]; e.Kind == model.KindRunAnnounced && pod != "" {
			by[pod] = append(by[pod], e.Time())
		}
	}
	return by
}

// runsIn returns the runs that touch the window, through their extent or
// their start announcement, and each earlier failed attempt of the same
// Kelos Task as one of them: a failure stays visible while its retry runs.
// They keep the story's order.
func runsIn(w Window, s model.Story) []model.Run {
	announced := announcements(s.Events)
	in := make([]bool, len(s.Runs))
	for i, r := range s.Runs {
		in[i] = touches(w, runExtent(r)) || slices.ContainsFunc(announced[r.Pod], w.Contains)
	}
	for i, r := range s.Runs {
		if !in[i] && r.Outcome == model.OutcomeFailed {
			in[i] = retriedIn(r, s.Runs, in)
		}
	}
	out := []model.Run{}
	for i, r := range s.Runs {
		if in[i] {
			out = append(out, r)
		}
	}
	return out
}

// retriedIn reports whether a run in the window is a later attempt of the
// same Kelos Task as failed: same Task name, another key, a later first
// recorded time.
func retriedIn(failed model.Run, runs []model.Run, in []bool) bool {
	at, ok := runAnchor(failed)
	if failed.TaskName == "" || !ok {
		return false
	}
	for i, r := range runs {
		if !in[i] || r.TaskName != failed.TaskName || r.Key == failed.Key {
			continue
		}
		if t, ok := runAnchor(r); ok && t.After(at) {
			return true
		}
	}
	return false
}

// marksIn returns the events of markKinds in the window.
func marksIn(w Window, events []model.Event) []Mark {
	out := []Mark{}
	for _, e := range events {
		if !markKinds[e.Kind] || !w.Contains(e.Time()) {
			continue
		}
		out = append(out, Mark{Kind: e.Kind, At: e.Time(), Event: e.Key(), PR: e.Related.PR, Pod: e.Details[detailPod]})
	}
	return out
}

func hasMarkOf(marks []Mark, pr model.PRKey) bool {
	return slices.ContainsFunc(marks, func(m Mark) bool { return m.PR != nil && *m.PR == pr })
}

// lastActivity is the latest time in the window the row shows anything.
func lastActivity(w Window, row Row, announced map[string][]time.Time) time.Time {
	var last time.Time
	see := func(t time.Time, ok bool) {
		if ok {
			if t.After(last) {
				last = t
			}
		}
	}
	for _, r := range row.Runs {
		see(latestIn(w, runExtent(r)))
		for _, t := range announced[r.Pod] {
			see(t, w.Contains(t))
		}
	}
	for _, p := range row.PullRequests {
		for _, period := range p.Periods {
			see(latestIn(w, period))
		}
	}
	for _, rw := range row.ReviewWaits {
		see(latestIn(w, rw.Interval))
	}
	for _, m := range row.Marks {
		see(m.At, true)
	}
	return last
}
