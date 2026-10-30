package collector

import (
	"cmp"
	"context"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// The details of the start announcements the collector reads, as the
// sources set them.
const (
	detailTaskName  = "taskName"
	detailPod       = "pod"
	detailNamespace = "namespace"
	detailWorkspace = "workspace"
	detailRole      = "role"
)

// kelosView is one read of the namespace: every run in it, whether it
// belongs to the story or not, and what joins them to it.
type kelosView struct {
	runs []model.Run
	// links are the runs' correlations with the tasks their labels name and
	// the pull requests their Origins name.
	links []model.Correlation
	// linksFrom indexes links by run.
	linksFrom  map[model.Ref][]model.Correlation
	placements map[model.RunKey]kelos.Placement
}

// readKelos reads the namespace, keeping and recalling the Tasks' origins
// when the collector has an OriginMemory. A failure to do either fails the
// read: an origin not kept may be lost with its Task, and runs read
// without their recalled origins would be joined by heuristics the next
// read contradicts. A failed read, or Kelos being off, leaves a.kelos nil.
func (a *assembly) readKelos(ctx context.Context) {
	a.source(model.SourceKelos)
	if !a.c.on(model.SourceKelos) {
		return
	}
	at := a.c.now()
	snap, err := a.c.src.Kelos.Read(ctx)
	if err == nil {
		snap, err = remember(ctx, a.c.origins, a.cfg.Kelos, snap)
	}
	if err != nil {
		a.fail(model.SourceKelos, err)
		return
	}
	recs, err := kelos.Normalize(a.cfg.Kelos, snap)
	if err != nil {
		a.fail(model.SourceKelos, err)
		return
	}
	a.read(model.SourceKelos, at)
	a.kelos = &kelosView{
		runs: recs.Runs, links: recs.Correlations, linksFrom: make(map[model.Ref][]model.Correlation),
		placements: kelos.Placements(a.cfg.Kelos, snap),
	}
	for _, c := range recs.Correlations {
		a.kelos.linksFrom[c.From] = append(a.kelos.linksFrom[c.From], c)
	}
}

// runsWhere returns the namespace's runs that match, and the kept runs
// it no longer lists.
func (a *assembly) runsWhere(match func(model.Run) bool) []model.Run {
	var out []model.Run
	if a.kelos != nil {
		for _, r := range a.kelos.runs {
			if match(r) {
				out = append(out, r)
			}
		}
	}
	for _, r := range a.keptOnly {
		if match(r) {
			out = append(out, r)
		}
	}
	return out
}

// kelosEvidence points to the object a run was first read from.
func (a *assembly) kelosEvidence(r model.Run, note string) model.Evidence {
	ref := r.Key.String()
	if len(r.Evidence) > 0 {
		ref = r.Evidence[0].Ref
	}
	return model.Evidence{Source: model.SourceKelos, Instance: a.cfg.Kelos.Cluster, Ref: ref, Note: note}
}

// branchEvidence points to the Task whose spec.branch (or branch
// annotation) p names, saying more after it.
func (a *assembly) branchEvidence(p kelos.Placement, more string) model.Evidence {
	return model.Evidence{
		Source: model.SourceKelos, Instance: a.cfg.Kelos.Cluster, Ref: p.BranchRef, Note: p.BranchFrom + " " + p.Branch + more,
	}
}

// linkedRuns adds the runs whose label or Origin names the root, and
// returns them.
func (a *assembly) linkedRuns() map[model.Ref]bool {
	linked := make(map[model.Ref]bool)
	for _, c := range a.kelos.links {
		if c.To == a.rootRef {
			a.includeRun(c.From)
			a.links.add(c)
			linked[c.From] = true
		}
	}
	return linked
}

// taskRuns adds the runs that belong to task id: by their task label, by
// the branch their Task works on (identifiers), or by the task and comment
// spawners' name conventions (a heuristic, only for a run nothing else
// joins).
func (a *assembly) taskRuns(id int64) {
	if a.kelos == nil {
		return
	}
	a.linkedRuns()
	branch := taskBranch(id)
	for _, r := range a.kelos.runs {
		if p := a.kelos.placements[r.Key]; p.Branch == branch {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, a.branchEvidence(p, ""))
		}
		if n, _, ok := taskOfName(r.TaskName); ok && n == id && !a.links.has(r.Key.Ref(), a.rootRef) {
			a.addRun(r)
			a.links.heuristic(r.Key.Ref(), a.rootRef, false, a.kelosEvidence(r,
				"Task name "+r.TaskName+" follows the spawners' convention for task "+strconv.FormatInt(id, 10)+
					", but no task label says so"))
		}
	}
}

// includeRun adds the namespace's run ref points to.
func (a *assembly) includeRun(ref model.Ref) {
	for _, r := range a.runsWhere(func(r model.Run) bool { return r.Key.Ref() == ref }) {
		a.addRun(r)
	}
}

// announcedRuns joins each start announcement to its run, and that run
// to the record the announcement is a comment on, with the same
// confidence (announcedPod). An announcement naming no pod is joined to
// the runs of its Task name in the namespace it names, else in any, a
// heuristic: a retry reuses the name. Each announcement is joined once,
// however often it is called: again once more of them are read.
func (a *assembly) announcedRuns() {
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		if a.joined[e.Ref()] {
			continue
		}
		a.joined[e.Ref()] = true
		name, pod, ns := e.Details[detailTaskName], e.Details[detailPod], e.Details[detailNamespace]
		if pod != "" {
			a.announcedPod(e, ns, name, pod)
			continue
		}
		candidates := a.sameNameRuns(e.Ref(), ns, name, func(model.Run) model.Evidence {
			return a.announcementEvidence(e, "names Kelos Task "+name+" but no pod; a retry reuses the name")
		})
		if on, ev, ok := a.relatedOf(e); ok {
			for _, r := range candidates {
				a.links.heuristic(r.Key.Ref(), on, len(candidates) > 1, ev)
			}
		}
	}
}

// relatedOf returns the task or pull request announcement e is a
// comment on, and the evidence that joins e's run to it.
func (a *assembly) relatedOf(e model.Event) (model.Ref, model.Evidence, bool) {
	on, ok := relatedRef(e.Related)
	if !ok {
		return model.Ref{}, model.Evidence{}, false
	}
	return on, a.announcementEvidence(e, "is a comment on "+on.ID), true
}

// announcedAttempt returns the attempt an announcement whose pod no Task
// names was posted from, when its time singles one out: exactly one run of
// its Task name, in namespace ns or in any when ns is empty, was running
// then, and that run's pod is unknown (its Task is gone). A run another
// announcement was joined to this way is not joined again.
func (a *assembly) announcedAttempt(e model.Event, ns, name string) (model.Run, bool) {
	if name == "" || e.SourceTime == nil {
		return model.Run{}, false
	}
	running := a.runsWhere(func(r model.Run) bool {
		return r.TaskName == name && inNamespace(r, ns) && during(r, *e.SourceTime)
	})
	if len(running) != 1 || running[0].Pod != "" || a.claimed[running[0].Key] {
		return model.Run{}, false
	}
	return running[0], true
}

// podRun adds the run an announcement alone shows, keyed by its pod in
// namespace ns, else in the collector's: no Task or TaskRecord of it was
// read, so its interval and outcome are unknown. Runs of the same Task
// name whose pod is unknown (read from a TaskRecord alone), in ns or in
// any when ns is empty, may be the same attempt: each is a heuristic
// candidate, never merged.
func (a *assembly) podRun(e model.Event, ns, name, pod string) {
	key := model.RunKey{Cluster: a.cfg.Kelos.Cluster, Namespace: cmp.Or(ns, a.cfg.Kelos.Namespace), Pod: pod}
	note := "announces the run; no Task or TaskRecord of it was read"
	if a.kelos == nil {
		note = "announces the run; the Kelos namespace could not be read"
	}
	run := model.Run{
		Key: key, TaskName: name, Pod: pod, Role: a.announcedRole(e, name),
		Interval: model.Interval{Start: model.UnknownBound(), End: model.UnknownBound()},
		Outcome:  model.OutcomeUnknown,
		Evidence: []model.Evidence{a.announcementEvidence(e, note)},
	}
	if e.Executor != nil {
		run.Agent = e.Executor.Name
	}
	if _, seen := a.runs[key]; !seen {
		a.addRun(run)
		if e.SourceTime != nil {
			a.announcedOnly[key] = *e.SourceTime
		}
	}
	a.links.identifier(e.Ref(), key.Ref(), a.announcementEvidence(e, "names pod "+pod))
	if on, ev, ok := a.relatedOf(e); ok {
		a.links.identifier(key.Ref(), on, ev)
	}
	a.sameNameRuns(key.Ref(), ns, name, func(r model.Run) model.Evidence {
		return a.kelosEvidence(r, "same Task name "+name+"; read without its Task, it names no pod, so "+
			"whether it is the announced attempt is unknown")
	})
}

// sameNameRuns joins from to each run named name whose pod is unknown,
// in namespace ns or in any when ns is empty, as heuristic candidates,
// ambiguous when there are several, and returns them.
func (a *assembly) sameNameRuns(from model.Ref, ns, name string, evidence func(model.Run) model.Evidence) []model.Run {
	if name == "" {
		return nil
	}
	candidates := a.runsWhere(func(r model.Run) bool { return r.TaskName == name && r.Pod == "" && inNamespace(r, ns) })
	for _, r := range candidates {
		a.addRun(r)
		a.links.heuristic(from, r.Key.Ref(), len(candidates) > 1, evidence(r))
	}
	return candidates
}

// inNamespace reports whether r is in namespace ns, any when ns is empty.
func inNamespace(r model.Run, ns string) bool { return ns == "" || r.Key.Namespace == ns }

// announcedRole is the role of a run known from its announcement alone:
// the Task name says, by convention, else the announcement's wording.
// The name comes first: the wording names no discussion.
func (a *assembly) announcedRole(e model.Event, name string) model.RunRole {
	if role := roleOfName(name); role != model.RoleUnknown {
		return role
	}
	return cmp.Or(model.RunRole(e.Details[detailRole]), model.RoleUnknown)
}

func (a *assembly) announcementEvidence(e model.Event, note string) model.Evidence {
	return model.Evidence{Source: e.Source, Instance: e.Instance, Ref: e.Evidence.Ref, Note: e.ID + " " + note}
}

// relatedRef is the task or pull request a record names.
func relatedRef(r model.Related) (model.Ref, bool) {
	switch {
	case r.Task != nil:
		return r.Task.Ref(), true
	case r.PR != nil:
		return r.PR.Ref(), true
	}
	return model.Ref{}, false
}
