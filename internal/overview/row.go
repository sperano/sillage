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

// The start announcement's details naming its attempt.
const (
	detailPod       = "pod"
	detailTaskName  = "taskName"
	detailNamespace = "namespace"
)

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
		Completion:   completionIn(w, s),
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
	if len(row.Runs) == 0 && len(row.PullRequests) == 0 && len(row.ReviewWaits) == 0 && len(row.Marks) == 0 &&
		row.Completion == nil {
		return Row{}, false
	}
	row.MissingHistory = missingHistory(w, row.Coverage, rowSources(row))
	row.LastActivity = lastActivity(w, row)
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

// announcement is a start announcement: when it was posted, and what it
// names of its attempt.
type announcement struct {
	at                       time.Time
	pod, taskName, namespace string
}

// announcementsIn returns the start announcements in the window.
func announcementsIn(w Window, events []model.Event) []announcement {
	var out []announcement
	for _, e := range events {
		if e.Kind == model.KindRunAnnounced && w.Contains(e.Time()) {
			out = append(out, announcement{
				at: e.Time(), pod: e.Details[detailPod],
				taskName: e.Details[detailTaskName], namespace: e.Details[detailNamespace],
			})
		}
	}
	return out
}

// names reports whether a may announce r. Only a run in its namespace, when
// it states one, as the collector joins it: a pod or Task name is unique in
// its namespace, not across them. A pod belongs to one attempt, so it
// identifies the run whatever the run's key. An announcement naming no pod
// names a Kelos Task, and so every attempt of it whose pod is unknown (a
// retry reuses the name): ambiguous. Of those, it names here only the ones
// whose recorded times do not rule out running when it was posted.
func (a announcement) names(r model.Run) bool {
	if a.namespace != "" && a.namespace != r.Key.Namespace {
		return false
	}
	if a.pod != "" {
		return a.pod == r.Pod
	}
	if a.taskName == "" || a.taskName != r.TaskName || r.Pod != "" {
		return false
	}
	return mayRunAt(r, a.at)
}

// mayRunAt reports whether the run's recorded times leave open that it ran
// at t: not created or started after it, not ended before it.
func mayRunAt(r model.Run, t time.Time) bool {
	if r.Created != nil && r.Created.After(t) {
		return false
	}
	if start, ok := r.Interval.Start.Time(); ok && start.After(t) {
		return false
	}
	end, ok := r.Interval.End.Time()
	return !ok || !end.Before(t)
}

// runsIn returns the runs that touch the window, through their extent or
// a start announcement in it, and each earlier failed attempt of the same
// Kelos Task as one of them: a failure stays visible while its retry runs.
// They keep the story's order.
func runsIn(w Window, s model.Story) []model.Run {
	announced := announcementsIn(w, s.Events)
	in := make([]bool, len(s.Runs))
	for i, r := range s.Runs {
		in[i] = runTouches(w, r) || slices.ContainsFunc(announced, func(a announcement) bool { return a.names(r) })
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
// same Kelos Task as failed: same cluster, namespace and Task name, another
// key, a later first recorded time.
func retriedIn(failed model.Run, runs []model.Run, in []bool) bool {
	at, ok := runAnchor(failed)
	if failed.TaskName == "" || !ok {
		return false
	}
	for i, r := range runs {
		if !in[i] || r.Key == failed.Key || !sameTask(r, failed) {
			continue
		}
		if t, ok := runAnchor(r); ok && t.After(at) {
			return true
		}
	}
	return false
}

// sameTask reports whether a and b are attempts of one Kelos Task: a name
// is unique in its cluster and namespace only.
func sameTask(a, b model.Run) bool {
	return a.TaskName == b.TaskName && a.Key.Cluster == b.Key.Cluster && a.Key.Namespace == b.Key.Namespace
}

// marksIn returns the events of markKinds in the window.
func marksIn(w Window, events []model.Event) []Mark {
	out := []Mark{}
	for _, e := range events {
		if !markKinds[e.Kind] || !w.Contains(e.Time()) {
			continue
		}
		out = append(out, Mark{
			Kind: e.Kind, At: e.Time(), Event: e.Key(), Source: e.Source, Instance: e.Instance,
			PR: e.Related.PR, Pod: e.Details[detailPod], Namespace: e.Details[detailNamespace],
		})
	}
	return out
}

func hasMarkOf(marks []Mark, pr model.PRKey) bool {
	return slices.ContainsFunc(marks, func(m Mark) bool { return m.PR != nil && *m.PR == pr })
}

// lastActivity is the latest time in the window the row shows anything.
// A start announcement in it is one of the row's marks.
func lastActivity(w Window, row Row) time.Time {
	var last time.Time
	see := func(t time.Time, ok bool) {
		if ok && t.After(last) {
			last = t
		}
	}
	for _, r := range row.Runs {
		see(latestIn(w, runExtent(r)))
		see(latestIn(w, r.Interval))
	}
	for _, p := range row.PullRequests {
		for _, period := range p.Periods {
			see(latestIn(w, period))
		}
	}
	for _, rw := range row.ReviewWaits {
		see(latestIn(w, waitExtent(rw)))
	}
	for _, m := range row.Marks {
		see(m.At, true)
	}
	if c := row.Completion; c != nil && c.Reopened != nil {
		see(c.Reopened.By, w.Contains(c.Reopened.By))
	}
	return last
}

// completionIn returns the task's completion at the window's end, when it
// was completed in the window or the read that first saw it reopened is in
// it.
func completionIn(w Window, s model.Story) *model.TaskCompletion {
	c, ok := s.CompletionAt(w.To)
	if !ok {
		return nil
	}
	if w.Contains(c.DoneAt) || (c.Reopened != nil && w.Contains(c.Reopened.By)) {
		return &c
	}
	return nil
}
