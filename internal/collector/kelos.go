package collector

import (
	"cmp"
	"context"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// The details of a start announcement the collector reads.
const (
	detailTaskName  = "taskName"
	detailPod       = "pod"
	detailNamespace = "namespace"
	detailModel     = "model"
	detailWorkspace = "workspace"
	detailBucket    = "bucket"
	detailLabel     = "label"
)

// kelosView is one read of the namespace: every run in it, whether it
// belongs to the story or not, and what joins them to it.
type kelosView struct {
	runs []model.Run
	// labels are the runs' correlations with the tasks their labels name.
	labels     []model.Correlation
	placements map[model.RunKey]kelos.Placement
}

// readKelos reads the namespace. A failed read leaves a.kelos nil.
func (a *assembly) readKelos(ctx context.Context) {
	a.source(model.SourceKelos)
	at := a.c.now()
	snap, err := a.c.src.Kelos.Read(ctx)
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
	a.kelos = &kelosView{runs: recs.Runs, labels: recs.Correlations, placements: kelos.Placements(a.cfg.Kelos, snap)}
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

// taskRuns adds the runs that belong to task id: by their task label, by
// the branch their Task works on (identifiers), or by the task and comment
// spawners' name conventions (a heuristic, for a run without the label).
func (a *assembly) taskRuns(id int64) {
	if a.kelos == nil {
		return
	}
	for _, c := range a.kelos.labels {
		if c.To == a.rootRef {
			a.includeRun(c.From)
			a.links.add(c)
		}
	}
	branch := taskBranch(id)
	for _, r := range a.kelos.runs {
		if p := a.kelos.placements[r.Key]; p.Branch == branch {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, model.Evidence{
				Source: model.SourceKelos, Instance: a.cfg.Kelos.Cluster, Ref: p.BranchRef,
				Note: "spec.branch " + branch,
			})
		}
		if n, _, ok := taskOfName(r.TaskName); ok && n == id && !a.links.has(r.Key.Ref(), a.rootRef) {
			a.addRun(r)
			a.links.heuristic(r.Key.Ref(), a.rootRef, false, a.kelosEvidence(r,
				"Task name "+r.TaskName+" follows the spawners' convention for task "+strconv.FormatInt(id, 10)+
					", but no task label says so"))
		}
	}
}

// pageRuns adds the runs that belong to page key: by the wiki spawner's
// Task name, which names the page and the requesting comment, or by the
// branch their Task works on.
func (a *assembly) pageRuns(key model.PageKey) {
	if a.kelos == nil {
		return
	}
	branch := pageBranch(key.ID)
	for _, r := range a.kelos.runs {
		if req, ok := pageOfName(r.TaskName); ok && req.page == key.ID {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, a.kelosEvidence(r, "Task name names page "+
				strconv.FormatInt(req.page, 10)))
			request := model.Event{Source: model.SourceWiki, Instance: key.Instance, ID: commentObject(req.comment)}
			a.links.identifier(r.Key.Ref(), request.Ref(), a.kelosEvidence(r, "Task name names comment "+
				strconv.FormatInt(req.comment, 10)))
		}
		if p := a.kelos.placements[r.Key]; p.Branch == branch {
			a.addRun(r)
			a.links.identifier(r.Key.Ref(), a.rootRef, model.Evidence{
				Source: model.SourceKelos, Instance: a.cfg.Kelos.Cluster, Ref: p.BranchRef,
				Note: "spec.branch " + branch,
			})
		}
	}
}

// includeRun adds the namespace's run ref points to.
func (a *assembly) includeRun(ref model.Ref) {
	for _, r := range a.runsWhere(func(r model.Run) bool { return r.Key.Ref() == ref }) {
		a.addRun(r)
	}
}

// announcedRuns joins each start announcement to its run: the run whose
// pod it names (an identifier); else the attempt of its Task name it was
// posted from, when its time singles one out (a heuristic: a Task gone
// leaves a TaskRecord, which names no pod); else a run keyed by that pod,
// built from the announcement alone. An announcement naming no pod is
// joined to the runs of its Task name, a heuristic: a retry reuses the
// name.
func (a *assembly) announcedRuns() {
	claimed := make(map[model.RunKey]bool)
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		name, pod := e.Details[detailTaskName], e.Details[detailPod]
		if pod == "" {
			a.sameNameRuns(e.Ref(), name, func(model.Run) model.Evidence {
				return a.announcementEvidence(e, "names Kelos Task "+name+" but no pod; a retry reuses the name")
			})
			continue
		}
		ns := cmp.Or(e.Details[detailNamespace], a.cfg.Kelos.Namespace)
		ev := a.announcementEvidence(e, "names pod "+pod)
		matches := a.runsWhere(func(r model.Run) bool { return r.Pod == pod && r.Key.Namespace == ns })
		for _, r := range matches {
			a.addRun(r)
			a.links.identifier(e.Ref(), r.Key.Ref(), ev)
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
			continue
		}
		a.podRun(e, ns, name, pod)
	}
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
	if on, ok := relatedRef(e.Related); ok {
		a.links.identifier(key.Ref(), on, a.announcementEvidence(e, "is a comment on "+on.ID))
	}
	a.sameNameRuns(key.Ref(), name, func(r model.Run) model.Evidence {
		return a.kelosEvidence(r, "same Task name "+name+"; read without its Task, it names no pod, so "+
			"whether it is the announced attempt is unknown")
	})
}

// sameNameRuns joins from to each run named name whose pod is unknown,
// as heuristic candidates, ambiguous when there are several.
func (a *assembly) sameNameRuns(from model.Ref, name string, evidence func(model.Run) model.Evidence) {
	if name == "" {
		return
	}
	candidates := a.runsWhere(func(r model.Run) bool { return r.TaskName == name && r.Pod == "" })
	for _, r := range candidates {
		a.addRun(r)
		a.links.heuristic(from, r.Key.Ref(), len(candidates) > 1, evidence(r))
	}
}

// announcedRole is the role of a run known from its announcement alone:
// a wiki announcement starts a wiki follow-up; otherwise the Task name
// says, by convention.
func (a *assembly) announcedRole(e model.Event, name string) model.RunRole {
	if e.Source == model.SourceWiki {
		return model.RoleWikiFollowUp
	}
	return roleOfName(name)
}

func (a *assembly) announcementEvidence(e model.Event, note string) model.Evidence {
	return model.Evidence{Source: e.Source, Instance: e.Instance, Ref: e.Evidence.Ref, Note: e.ID + " " + note}
}

// workspaceNames lists the workspace, else the project, of every run of
// the story.
func (a *assembly) workspaceNames() []string {
	var names []string
	for _, r := range a.runs {
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
