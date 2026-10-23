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
	// links are the runs' correlations with the tasks their labels name,
	// and with the pages and pull requests their Origins name.
	links []model.Correlation
	// linksFrom indexes links by run.
	linksFrom  map[model.Ref][]model.Correlation
	placements map[model.RunKey]kelos.Placement
}

// readKelos reads the namespace, keeping and recalling the Tasks' origins
// when the collector has an OriginMemory: a failure to do either fails the
// read. A failed read leaves a.kelos nil.
func (a *assembly) readKelos(ctx context.Context) {
	a.source(model.SourceKelos)
	at := a.c.now()
	snap, err := a.c.src.Kelos.Read(ctx)
	if err == nil {
		snap, err = a.c.origins.remember(ctx, a.cfg.Kelos, snap)
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

// runsWhere returns the namespace's runs that match.
func (a *assembly) runsWhere(match func(model.Run) bool) []model.Run {
	if a.kelos == nil {
		return nil
	}
	var out []model.Run
	for _, r := range a.kelos.runs {
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

// linkedRuns adds the runs whose label or Origin names the root.
func (a *assembly) linkedRuns() {
	for _, c := range a.kelos.links {
		if c.To == a.rootRef {
			a.includeRun(c.From)
			a.links.add(c)
		}
	}
}

// taskRuns adds the runs that belong to task id: by their task label, by
// the branch their Task works on (identifiers), or by the task and comment
// spawners' name conventions (a heuristic, only for a run with neither the
// label nor an Origin).
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
		if a.originOf(r) != nil {
			continue
		}
		if n, _, ok := taskOfName(r.TaskName); ok && n == id && !a.links.has(r.Key.Ref(), a.rootRef) {
			a.addRun(r)
			a.links.heuristic(r.Key.Ref(), a.rootRef, false, a.kelosEvidence(r,
				"Task name "+r.TaskName+" follows the spawners' convention for task "+strconv.FormatInt(id, 10)+
					", but no task label says so"))
		}
	}
}

// pageRuns adds the runs that belong to page key: by their Origin, by
// the branch their Task works on, or, for a run with no Origin, by the
// wiki spawner's Task name, which names the page and the requesting
// comment.
func (a *assembly) pageRuns(key model.PageKey) {
	if a.kelos == nil {
		return
	}
	a.linkedRuns()
	branch := pageBranch(key.ID)
	for _, r := range a.kelos.runs {
		if req, ok := pageOfName(r.TaskName); ok && req.page == key.ID && a.originOf(r) == nil {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, a.kelosEvidence(r, "Task name names page "+
				strconv.FormatInt(req.page, 10)))
			request := model.Event{Source: model.SourceWiki, Instance: key.Instance, ID: commentObject(req.comment)}
			a.links.identifier(r.Key.Ref(), request.Ref(), a.kelosEvidence(r, "Task name names comment "+
				strconv.FormatInt(req.comment, 10)))
		}
		if p := a.kelos.placements[r.Key]; p.Branch == branch {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, a.branchEvidence(p, ""))
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
// confidence: the run whose pod it names (an identifier); else the attempt
// of its Task name it was posted from, when its time singles one out (a
// heuristic: a Task gone leaves a TaskRecord, which names no pod); else a
// run keyed by that pod, built from the announcement alone. An
// announcement naming no pod is joined to the runs of its Task name, a
// heuristic: a retry reuses the name.
func (a *assembly) announcedRuns() {
	claimed := make(map[model.RunKey]bool)
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		name, pod := e.Details[detailTaskName], e.Details[detailPod]
		if pod == "" {
			candidates := a.sameNameRuns(e.Ref(), name, func(model.Run) model.Evidence {
				return a.announcementEvidence(e, "names Kelos Task "+name+" but no pod; a retry reuses the name")
			})
			if on, ev, ok := a.relatedOf(e); ok {
				for _, r := range candidates {
					a.links.heuristic(r.Key.Ref(), on, len(candidates) > 1, ev)
				}
			}
			continue
		}
		ns := cmp.Or(e.Details[detailNamespace], a.cfg.Kelos.Namespace)
		ev := a.announcementEvidence(e, "names pod "+pod)
		on, onEv, related := a.relatedOf(e)
		matches := a.runsWhere(func(r model.Run) bool { return r.Pod == pod && r.Key.Namespace == ns })
		for _, r := range matches {
			a.addRun(r)
			a.links.identifier(e.Ref(), r.Key.Ref(), ev)
			if related {
				a.links.identifier(r.Key.Ref(), on, onEv)
			}
		}
		if len(matches) > 0 {
			continue
		}
		if r, ok := a.announcedAttempt(e, ns, name, claimed); ok {
			claimed[r.Key] = true
			a.addRun(r)
			a.links.heuristic(e.Ref(), r.Key.Ref(), false, a.announcementEvidence(e, "names pod "+pod+
				", which no Task read names; it was posted inside this attempt of "+name+" and no other, "+
				"which, read without its Task, names no pod"))
			if related {
				a.links.heuristic(r.Key.Ref(), on, false, onEv)
			}
			continue
		}
		a.podRun(e, ns, name, pod)
	}
}

// relatedOf returns the task, page or pull request announcement e is a
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
// its Task name was running then, and that run's pod is unknown (its Task
// is gone). A run another announcement was joined to this way is not
// joined again.
func (a *assembly) announcedAttempt(e model.Event, ns, name string, claimed map[model.RunKey]bool) (model.Run, bool) {
	if name == "" || e.SourceTime == nil {
		return model.Run{}, false
	}
	running := a.runsWhere(func(r model.Run) bool {
		return r.TaskName == name && r.Key.Namespace == ns && during(r, *e.SourceTime)
	})
	if len(running) != 1 || running[0].Pod != "" || claimed[running[0].Key] {
		return model.Run{}, false
	}
	return running[0], true
}

// podRun adds the run an announcement alone shows, keyed by its pod: no
// Task or TaskRecord of it was read, so its interval and outcome are
// unknown. Runs of the same Task name whose pod is unknown (read from a
// TaskRecord alone) may be the same attempt: each is a heuristic
// candidate, never merged.
func (a *assembly) podRun(e model.Event, ns, name, pod string) {
	key := model.RunKey{Cluster: a.cfg.Kelos.Cluster, Namespace: ns, Pod: pod}
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
	a.sameNameRuns(key.Ref(), name, func(r model.Run) model.Evidence {
		return a.kelosEvidence(r, "same Task name "+name+"; read without its Task, it names no pod, so "+
			"whether it is the announced attempt is unknown")
	})
}

// sameNameRuns joins from to each run named name whose pod is unknown,
// as heuristic candidates, ambiguous when there are several, and returns
// them.
func (a *assembly) sameNameRuns(from model.Ref, name string, evidence func(model.Run) model.Evidence) []model.Run {
	if name == "" {
		return nil
	}
	candidates := a.runsWhere(func(r model.Run) bool { return r.TaskName == name && r.Pod == "" })
	for _, r := range candidates {
		a.addRun(r)
		a.links.heuristic(from, r.Key.Ref(), len(candidates) > 1, evidence(r))
	}
	return candidates
}

// announcedRole is the role of a run known from its announcement alone:
// a wiki announcement starts a wiki follow-up; otherwise the Task name
// says, by convention, else the announcement's wording. The name comes
// first: the wording names no discussion.
func (a *assembly) announcedRole(e model.Event, name string) model.RunRole {
	if e.Source == model.SourceWiki {
		return model.RoleWikiFollowUp
	}
	if role := roleOfName(name); role != model.RoleUnknown {
		return role
	}
	return cmp.Or(model.RunRole(e.Details[detailRole]), model.RoleUnknown)
}

func (a *assembly) announcementEvidence(e model.Event, note string) model.Evidence {
	return model.Evidence{Source: e.Source, Instance: e.Instance, Ref: e.Evidence.Ref, Note: e.ID + " " + note}
}

// workspaceNames lists the workspace, else the project, of every run of
// the story whose Origin names no repository.
func (a *assembly) workspaceNames() []string {
	var names []string
	for _, r := range a.runs {
		if _, ok := a.originRepository(r); ok {
			continue
		}
		if a.kelos != nil {
			if ws := a.kelos.placements[r.Key].Workspace; ws != "" {
				names = append(names, ws)
				continue
			}
		}
		if r.Project != "" {
			names = append(names, r.Project)
		}
		if req, ok := pageOfName(r.TaskName); ok {
			names = append(names, req.workspace)
		}
	}
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		if ws := e.Details[detailWorkspace]; ws != "" {
			names = append(names, ws)
		}
	}
	return names
}

// relatedRef is the task, page or pull request a record names.
func relatedRef(r model.Related) (model.Ref, bool) {
	switch {
	case r.Task != nil:
		return r.Task.Ref(), true
	case r.Page != nil:
		return r.Page.Ref(), true
	case r.PR != nil:
		return r.PR.Ref(), true
	}
	return model.Ref{}, false
}
