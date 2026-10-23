package kelos

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// Placement is where a run worked and what started it, as its objects
// say: what a model.Run does not carry, but what joins it to a branch's
// pull requests, to a repository and to the comment that asked for it.
type Placement struct {
	// Branch is the Task's spec.branch, else its branch annotation (the
	// Tasks with no spec.branch); empty when neither is known (a TaskRecord
	// copies neither).
	Branch string
	// BranchRef locates the Task the branch was read from, and BranchFrom
	// is the field it was read from, for evidence.
	BranchRef, BranchFrom string
	// Workspace is the workspace label of the Task, else of its
	// TaskRecord's copy; empty when neither has one.
	Workspace string
	// Origin is what started the Task, from its correlation annotations,
	// else from s.Remembered; nil when neither has any.
	Origin *Origin
}

// Placements returns the placement of each run Normalize makes of s that
// names a branch, a workspace or an Origin, by run key. Objects Normalize
// rejects are skipped.
func Placements(cfg Config, s Snapshot) map[model.RunKey]Placement {
	out := make(map[model.RunKey]Placement)
	workspace := cfg.label(labelWorkspace)
	for _, r := range s.Records {
		uid := r.Spec.TaskRef.UID
		if uid == "" {
			continue
		}
		p := Placement{Workspace: r.Labels[workspace]}
		if o, ok := s.Remembered[uid]; ok && len(o.Annotations) > 0 {
			o.remembered = true
			p.Origin = &o
			p.branchFromOrigin(cfg)
		}
		if p != (Placement{}) {
			out[cfg.runKey(uid)] = p
		}
	}
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.UID == "" {
			continue
		}
		if p, ok := cfg.taskPlacement(t, out[cfg.runKey(t.UID)]); ok {
			out[cfg.runKey(t.UID)] = p
		}
	}
	return out
}

// taskPlacement returns the placement Task t gives, over what its
// TaskRecord gave, and whether it names anything.
func (c Config) taskPlacement(t *Task, p Placement) (Placement, bool) {
	if ws := t.Labels[c.label(labelWorkspace)]; ws != "" {
		p.Workspace = ws
	}
	p.Origin, p.Branch, p.BranchRef, p.BranchFrom = nil, "", "", ""
	if o, ok := c.originOf(t, time.Time{}); ok {
		p.Origin = &o
		p.branchFromOrigin(c)
	}
	if t.Spec.Branch != "" {
		p.Branch, p.BranchRef, p.BranchFrom = t.Spec.Branch, c.taskRef(t.Name), "spec.branch"
	}
	return p, p != (Placement{})
}

// branchFromOrigin sets the branch from the Origin's branch annotation.
func (p *Placement) branchFromOrigin(cfg Config) {
	if b := p.Origin.Value(AnnotationBranch); b != "" {
		p.Branch, p.BranchRef = b, p.Origin.Task
		p.BranchFrom = "metadata.annotations[" + cfg.label(AnnotationBranch) + "]"
	}
}
