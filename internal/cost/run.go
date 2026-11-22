package cost

import (
	"cmp"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// runCost returns r's cost from its events, joined by pod and by name,
// and its accumulated usage, adding its events to the task's model view.
func runCost(t *Table, r model.Run, byPod, byName []model.UsageEvent, pods []string, feed Feed,
	taskModels *modelViews) (RunCost, *acc) {
	ra := newAcc()
	ra.empty = false
	attempts := map[string]*acc{}
	models := newModelViews()
	threads := newThreadViews()
	events := append(slices.Clone(byPod), byName...)
	for _, e := range events {
		p := priceEvent(t, e)
		ra.add(p.acc)
		if attempts[e.Pod] == nil {
			attempts[e.Pod] = newAcc()
		}
		attempts[e.Pod].add(p.acc)
		models.add(e, p)
		taskModels.add(e, p)
		threads.add(e, p)
	}
	if len(byName) > 0 {
		ra.note(NoteAttributedByName)
	}
	notePods(ra, attempts, r, pods)
	noteRun(ra, r, len(events) > 0, feed)
	rc := RunCost{
		Key: r.Key, TaskName: r.TaskName, Role: r.Role, Outcome: r.Outcome, Model: r.Model,
		Breakdown: ra.breakdown(), Attempts: attemptCosts(attempts), Models: models.list(), Threads: threads.list(),
	}
	if end, ok := r.Interval.End.Time(); ok {
		rc.Ended = &end
		rc.Final = feed.ReadThrough != nil && !feed.ReadThrough.Before(end.Add(FinalGrace))
	}
	return rc, ra
}

// notePods adds an attempt with no usage events for each pod the run was
// seen with that reported none: the run's usage is then incomplete.
func notePods(ra *acc, attempts map[string]*acc, r model.Run, pods []string) {
	all := slices.Clone(pods)
	if r.Pod != "" {
		all = append(all, r.Pod)
	}
	if len(attempts) == 0 {
		// The run as a whole has no events: noteRun says why.
		return
	}
	for _, p := range all {
		if attempts[p] == nil {
			a := newAcc()
			a.empty = false
			a.note(NoteNoUsageEvents)
			attempts[p] = a
			ra.note(NoteNoUsageEvents)
		}
	}
}

// noteRun notes why r's usage may be incomplete.
func noteRun(ra *acc, r model.Run, hasEvents bool, feed Feed) {
	if !hasEvents && r.Role == model.RoleAutomation && r.Model == "" {
		// A model-free automation Task: no usage is expected.
		ra.none = true
		return
	}
	if !hasEvents {
		ra.note(NoteNoUsageEvents)
	}
	if !feed.On {
		ra.note(NoteFeedOff)
	}
	if feed.Since != nil && startedBefore(r, *feed.Since) {
		ra.note(NoteBeforeTelemetry)
	}
	switch r.Outcome {
	case model.OutcomeRunning, model.OutcomeWaiting:
		ra.note(NoteInProgress)
	case model.OutcomeFailed:
		if hasEvents {
			ra.note(NoteMayLoseUnsent)
		}
	}
}

// startedBefore reports whether r is known, or not known not, to have
// started before t.
func startedBefore(r model.Run, t time.Time) bool {
	start, ok := r.Interval.Start.Time()
	if !ok && r.Created != nil {
		start, ok = *r.Created, true
	}
	return !ok || start.Before(t)
}

func attemptCosts(attempts map[string]*acc) []AttemptCost {
	out := make([]AttemptCost, 0, len(attempts))
	for pod, a := range attempts {
		out = append(out, AttemptCost{Pod: pod, Breakdown: a.breakdown()})
	}
	slices.SortFunc(out, func(a, b AttemptCost) int { return cmp.Compare(a.Pod, b.Pod) })
	return out
}
