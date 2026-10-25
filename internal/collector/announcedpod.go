package collector

import (
	"slices"
	"strings"

	"github.com/sperano/sillage/internal/model"
)

// announcedPod joins an announcement naming pod, in ns or in any when ns
// is empty, to the runs of that pod: by the pod alone when they are in one
// namespace (podIdentified), unless its time contradicts them
// (podContradicted) or a pod-less attempt of its Task name may be its run
// (rivalAttempts). Else, when its time contradicts them or no run of the
// pod was read, to the attempt it was posted from, if its time singles
// one out (announcedAttempt); else to each of them, and those attempts, as
// heuristic candidates (podCandidates); else, with no run of the pod read,
// to a run keyed by that pod, built from the announcement alone (podRun).
func (a *assembly) announcedPod(e model.Event, ns, name, pod string) {
	matches := a.runsWhere(func(r model.Run) bool { return r.Pod == pod && inNamespace(r, ns) })
	matchNS := namespaces(matches)
	contradicted := a.podContradicted(e, ns, matches, matchNS)
	if len(matches) == 0 || contradicted {
		if r, ok := a.announcedAttempt(e, ns, name); ok {
			a.attemptRun(e, name, pod, contradicted, r)
			return
		}
	}
	switch rivals := a.rivalAttempts(e, ns, name, matchNS); {
	case len(matches) == 0:
		a.podRun(e, ns, name, pod)
	case len(matchNS) == 1 && !contradicted && len(rivals) == 0:
		a.podIdentified(e, pod, matches)
	default:
		a.podCandidates(e, name, pod, matches, rivals)
	}
}

// attemptRun joins an announcement naming pod to r, the attempt of its
// Task name it was posted from (announcedAttempt): a heuristic, as a Task
// gone leaves a TaskRecord, which names no pod. outlived says runs of the
// pod were read, none running when it was posted.
func (a *assembly) attemptRun(e model.Event, name, pod string, outlived bool, r model.Run) {
	note := ", which no Task read names"
	if outlived {
		note = ", but was posted while no run of it was running"
	}
	a.claimed[r.Key] = true
	a.addRun(r)
	a.links.heuristic(e.Ref(), r.Key.Ref(), false, a.announcementEvidence(e, "names pod "+pod+note+
		"; it was posted inside this attempt of "+name+" and no other, which, read without its Task, names no pod"))
	if on, onEv, ok := a.relatedOf(e); ok {
		a.links.heuristic(r.Key.Ref(), on, false, onEv)
	}
}

// podIdentified joins an announcement to matches, the runs of the pod it
// names, all in one namespace: the one it names (announcedPod keeps only
// its runs), or, when it names none, the only one a run of that pod is in.
func (a *assembly) podIdentified(e model.Event, pod string, matches []model.Run) {
	on, onEv, related := a.relatedOf(e)
	ev := a.announcementEvidence(e, "names pod "+pod)
	for _, r := range matches {
		a.addRun(r)
		a.links.identifier(e.Ref(), r.Key.Ref(), ev)
		if related {
			a.links.identifier(r.Key.Ref(), on, onEv)
		}
	}
}

// podCandidates joins an announcement naming pod but no namespace to the
// runs the pod name alone does not single out: the matches it was posted
// during, else, posted during none, every match; and rivals
// (rivalAttempts). Each is a heuristic candidate, ambiguous unless they
// are matches in one namespace the announcement was posted during.
func (a *assembly) podCandidates(e model.Event, name, pod string, matches, rivals []model.Run) {
	candidates := slices.DeleteFunc(slices.Clone(matches), func(r model.Run) bool { return !couldHavePosted(e, r) })
	outlived := len(candidates) == 0
	if outlived {
		candidates = matches
	}
	in := namespaces(candidates)
	ambiguous := outlived || len(rivals) > 0 || len(in) != 1
	note := sharedPodNote(e, candidates, in, outlived)
	if len(rivals) > 0 {
		note += "; an attempt of " + name + " read without its Task, which names no pod, may be its run"
	}
	on, onEv, related := a.relatedOf(e)
	join := func(r model.Run, note string) {
		a.addRun(r)
		a.links.heuristic(e.Ref(), r.Key.Ref(), ambiguous, a.announcementEvidence(e, "names pod "+pod+note))
		if related {
			a.links.heuristic(r.Key.Ref(), on, ambiguous, onEv)
		}
	}
	for _, r := range candidates {
		join(r, note)
	}
	for _, r := range rivals {
		join(r, rivalNote(e, name, r, in))
	}
}

// rivalNote says why r, an attempt of Task name that names no pod, is a
// candidate of an announcement whose pod's runs, in namespaces in, are
// candidates.
func rivalNote(e model.Event, name string, r model.Run, in []string) string {
	timing := "may have been running when it was posted"
	if knownPostedDuring(e, r) {
		timing = "was running when it was posted"
	}
	return " but no namespace; runs of it are in " + strings.Join(in, ", ") +
		", and this attempt of " + name + ", read without its Task, names no pod and " + timing
}

// sharedPodNote says why candidates, runs in namespaces ns of the pod an
// announcement naming no namespace names, are its candidates: it was
// posted while they were running, when its time and their bounds all say
// so; it may have been, when one of those is unknown; outlived, it was
// posted while none was.
func sharedPodNote(e model.Event, candidates []model.Run, ns []string, outlived bool) string {
	in := strings.Join(ns, ", ")
	switch {
	case outlived:
		return " but no namespace; runs of it are in " + in + ", and it was posted while none of them was running"
	case slices.ContainsFunc(candidates, func(r model.Run) bool { return !knownPostedDuring(e, r) }):
		return " but no namespace; runs of it are in " + in + ", which may have been running when it was posted"
	default:
		return " but no namespace; it was posted while runs of it in " + in + " were running"
	}
}

// podContradicted reports whether an announcement naming no namespace
// was posted while none of matches, the runs of its pod, was running,
// unless they are all in the collector's namespace (podElsewhere). The
// pod alone does not make them its run then: a run of the collector's
// namespace read without its Task names no pod, and may be the one.
func (a *assembly) podContradicted(e model.Event, ns string, matches []model.Run, matchNS []string) bool {
	posted := func(r model.Run) bool { return couldHavePosted(e, r) }
	return a.podElsewhere(ns, matchNS) && !slices.ContainsFunc(matches, posted)
}

// podElsewhere reports whether an announcement naming namespace ns names
// none and a pod whose runs, in matchNS (namespaces of them), are not all
// in the collector's namespace.
func (a *assembly) podElsewhere(ns string, matchNS []string) bool {
	return ns == "" && len(matchNS) > 0 && !slices.Equal(matchNS, []string{a.cfg.Kelos.Namespace})
}

// rivalAttempts returns the attempts of Task name, in any namespace, that
// an announcement naming a pod but no namespace may have been posted from
// though they name no pod (read without their Task), when the pod's runs
// are not all in the collector's namespace (podElsewhere): the pod read
// elsewhere does not rule them out. A run another announcement was joined
// to as its attempt (announcedAttempt) is none.
func (a *assembly) rivalAttempts(e model.Event, ns, name string, matchNS []string) []model.Run {
	if name == "" || !a.podElsewhere(ns, matchNS) {
		return nil
	}
	return a.runsWhere(func(r model.Run) bool {
		return r.TaskName == name && r.Pod == "" && !a.claimed[r.Key] && couldHavePosted(e, r)
	})
}

// couldHavePosted reports whether announcement e may have been posted
// during run r: neither end of r known on the wrong side of its time. An
// unknown time or end excludes nothing, unlike during's unknown start.
func couldHavePosted(e model.Event, r model.Run) bool {
	if e.SourceTime == nil {
		return true
	}
	if start, ok := r.Interval.Start.Time(); ok && e.SourceTime.Before(start) {
		return false
	}
	end, ok := r.Interval.End.Time()
	return !ok || !e.SourceTime.After(end)
}

// knownPostedDuring reports whether announcement e's time and both ends
// of run r are known, and put its time within r.
func knownPostedDuring(e model.Event, r model.Run) bool {
	start, startKnown := r.Interval.Start.Time()
	end, endKnown := r.Interval.End.Time()
	return e.SourceTime != nil && startKnown && endKnown &&
		!e.SourceTime.Before(start) && !e.SourceTime.After(end)
}

// namespaces lists the namespaces of runs, sorted, each once.
func namespaces(runs []model.Run) []string {
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.Key.Namespace)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
