package reconcile

import (
	"context"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// projectMark is a project listing's checkpoint: the latest updated time
// it saw.
type projectMark struct {
	Project int64     `json:"project"`
	Updated time.Time `json:"updated"`
}

// markKey is the checkpoint key of project's mark on instance.
func markKey(instance string, project int64) string {
	return projectPrefix + instance + "/" + strconv.FormatInt(project, 10)
}

// listProjects lists each configured project's tasks changed since its
// mark, less listingOverlap, and adds to dues each task updated since its
// last read. It returns each listed project's new mark. Without a
// project lister (Vikunja off), it lists nothing.
func (l *Loop) listProjects(ctx context.Context, dues map[model.Ref]*due) map[int64]time.Time {
	marks := make(map[int64]time.Time)
	if l.src.Projects == nil {
		return marks
	}
	m := l.meterFor(model.SourceVikunja, l.ccfg.VikunjaInstance)
	for _, p := range l.cfg.VikunjaProjects {
		var since time.Time
		mark := l.marks[p]
		if !mark.IsZero() {
			since = mark.Add(-listingOverlap)
		}
		changes, err := timed(ctx, m, func(ctx context.Context) ([]vikunja.TaskChange, error) {
			return l.src.Projects.ChangedTasks(ctx, p, since)
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			l.log.Warn("source read failed", "source", model.SourceVikunja, "instance", l.ccfg.VikunjaInstance,
				"project", p, "reason", reasonOf(err), "error", err)
			continue
		}
		for _, ch := range changes {
			if ch.Updated.After(mark) {
				mark = ch.Updated
			}
			l.addChanged(dues, p, ch)
		}
		if !mark.IsZero() {
			marks[p] = mark
		}
	}
	return marks
}

// addChanged adds a listed task to dues, unless it was read at this
// updated time already.
func (l *Loop) addChanged(dues map[model.Ref]*due, project int64, ch vikunja.TaskChange) {
	root := model.Root{Task: &model.TaskKey{Instance: l.ccfg.VikunjaInstance, ID: ch.ID}}
	ref := root.Ref()
	if st := l.roots[ref]; st != nil && st.Updated != nil && !ch.Updated.After(*st.Updated) {
		return
	}
	d, ok := dues[ref]
	if !ok {
		d = &due{root: root}
		dues[ref] = d
	}
	if d.updated == nil || ch.Updated.After(*d.updated) {
		updated := ch.Updated
		d.updated = &updated
	}
	d.projects = append(d.projects, project)
	d.readGitHub(triggerTask)
}

// commitCycle commits when the cycle ended, the marks of the projects
// every listed task of which was read, and the root states whose gaps
// are not committed yet, in one transaction.
func (l *Loop) commitCycle(ctx context.Context, marks map[int64]time.Time, incomplete map[int64]bool) {
	cluster := l.ccfg.Kelos.Cluster
	cps := map[string]Checkpoint{
		heartbeatKey(cluster): {Source: model.SourceKelos, Instance: cluster, Value: encode(l.now())},
	}
	for ref := range l.unsaved {
		cps[rootKey(l.roots[ref].Root)] = l.roots[ref].checkpoint()
	}
	advanced := make(map[int64]time.Time)
	for p, mark := range marks {
		if prev, ok := l.marks[p]; !incomplete[p] && (!ok || mark.After(prev)) {
			cps[markKey(l.ccfg.VikunjaInstance, p)] = Checkpoint{
				Source: model.SourceVikunja, Instance: l.ccfg.VikunjaInstance,
				Value: encode(projectMark{Project: p, Updated: mark}),
			}
			advanced[p] = mark
		}
	}
	if err := l.store.Commit(ctx, Commit{Checkpoints: cps}); err != nil {
		if ctx.Err() == nil {
			l.metrics.storeErrors.WithLabelValues(operationCommit).Inc()
			l.log.Error("store commit failed", "error", err)
		}
		return
	}
	for p, mark := range advanced {
		l.marks[p] = mark
	}
	clear(l.unsaved)
}
