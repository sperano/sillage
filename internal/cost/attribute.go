package cost

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// attribution is which run each usage event belongs to.
type attribution struct {
	// byRun are the events of each run (by its index) joined by pod, and
	// byName those joined by Task name and time.
	byRun, byName map[int][]model.UsageEvent
	// none are the events no single run could be joined to.
	none []model.UsageEvent
}

// podRef is one pod of one namespace.
type podRef struct{ cluster, namespace, pod string }

// attribute joins each event to a run: by its pod when one run was seen
// with it, else by its Task name when exactly one run of that name was
// live at its time, else to none.
func attribute(events []model.UsageEvent, runs []model.Run, pods map[model.RunKey][]string) attribution {
	byPod := runsByPod(runs, pods)
	out := attribution{byRun: map[int][]model.UsageEvent{}, byName: map[int][]model.UsageEvent{}}
	for _, e := range events {
		if idx, ok := byPod[podRef{e.Cluster, e.Namespace, e.Pod}]; ok {
			if len(idx) == 1 {
				out.byRun[idx[0]] = append(out.byRun[idx[0]], e)
			} else {
				out.none = append(out.none, e)
			}
			continue
		}
		if i, ok := runByName(e, runs); ok {
			out.byName[i] = append(out.byName[i], e)
			continue
		}
		out.none = append(out.none, e)
	}
	return out
}

// runsByPod returns the runs each pod was seen with, by index.
func runsByPod(runs []model.Run, pods map[model.RunKey][]string) map[podRef][]int {
	out := map[podRef][]int{}
	addPod := func(i int, r model.Run, pod string) {
		k := podRef{r.Key.Cluster, r.Key.Namespace, pod}
		for _, j := range out[k] {
			if j == i {
				return
			}
		}
		out[k] = append(out[k], i)
	}
	for i, r := range runs {
		if r.Pod != "" {
			addPod(i, r, r.Pod)
		}
		for _, p := range pods[r.Key] {
			addPod(i, r, p)
		}
	}
	return out
}

// runByName returns the one run of e's Task name, in its namespace, that
// was live at e's time.
func runByName(e model.UsageEvent, runs []model.Run) (int, bool) {
	found := -1
	for i, r := range runs {
		if e.TaskName == "" || r.TaskName != e.TaskName || r.Key.Cluster != e.Cluster || r.Key.Namespace != e.Namespace {
			continue
		}
		if !liveAt(r, e.At) {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}

// liveAt reports whether r is known to have existed at t: after its
// creation (or start), and before its end unless that is open.
func liveAt(r model.Run, t time.Time) bool {
	start, ok := r.Interval.Start.Time()
	if r.Created != nil {
		start, ok = *r.Created, true
	}
	if !ok || t.Before(start) {
		return false
	}
	if r.Interval.End.State == model.BoundOpen {
		return true
	}
	end, ok := r.Interval.End.Time()
	return ok && !t.After(end)
}
