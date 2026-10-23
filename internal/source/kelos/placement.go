package kelos

import "github.com/sperano/sillage/internal/model"

// Placement is where a run worked, as its objects say: what a model.Run
// does not carry, but what joins it to a branch's pull requests and to a
// repository.
type Placement struct {
	// Branch is the Task's spec.branch; empty when the Task is gone (its
	// TaskRecord does not copy it) or names none.
	Branch string
	// BranchRef locates the Task the branch was read from, for evidence.
	BranchRef string
	// Workspace is the workspace label of the Task, else of its
	// TaskRecord's copy; empty when neither has one.
	Workspace string
}

// Placements returns the placement of each run Normalize makes of s that
// names a branch or a workspace, by run key. Objects Normalize rejects are
// skipped.
func Placements(cfg Config, s Snapshot) map[model.RunKey]Placement {
	out := make(map[model.RunKey]Placement)
	key := func(uid string) model.RunKey {
		return model.RunKey{Cluster: cfg.Cluster, Namespace: cfg.Namespace, UID: uid}
	}
	workspace := cfg.label(labelWorkspace)
	for _, r := range s.Records {
		if ws := r.Labels[workspace]; ws != "" && r.Spec.TaskRef.UID != "" {
			out[key(string(r.Spec.TaskRef.UID))] = Placement{Workspace: ws}
		}
	}
	for _, t := range s.Tasks {
		if t.UID == "" {
			continue
		}
		p := out[key(string(t.UID))]
		if ws := t.Labels[workspace]; ws != "" {
			p.Workspace = ws
		}
		if t.Spec.Branch != "" {
			p.Branch, p.BranchRef = t.Spec.Branch, cfg.taskRef(t.Name)
		}
		if p != (Placement{}) {
			out[key(string(t.UID))] = p
		}
	}
	return out
}
