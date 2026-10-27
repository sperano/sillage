package collector

import (
	"context"
	"fmt"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// collectTask assembles the story of Vikunja task id: the task and its
// comments, the namespace's runs that belong to the task, and the pull
// requests of its branch.
func (a *assembly) collectTask(ctx context.Context, key model.TaskKey, repos []github.Repository) error {
	if key.Instance != a.cfg.VikunjaInstance {
		return fmt.Errorf("collector: task %s is not on %s", key, a.cfg.VikunjaInstance)
	}
	id := key.ID
	for _, s := range []model.Source{model.SourceVikunja, model.SourceKelos, model.SourceGitHub} {
		a.source(s)
	}
	a.readTask(ctx, id)
	a.readKelos(ctx)
	a.keepRuns()
	a.announcedRuns()
	a.taskRuns(id)
	a.readGitHub(ctx, taskBranch(id), repos)
	// The pull requests' announcements are read only now.
	a.announcedRuns()
	a.prRuns()
	a.actorRuns()
	a.requestLinks()
	return nil
}

// readTask reads the task and its comments. A failed read, or one the
// source cannot normalize, adds no events.
func (a *assembly) readTask(ctx context.Context, id int64) {
	at := a.c.now()
	snap, err := a.c.src.Vikunja.Fetch(ctx, id)
	if err != nil {
		a.fail(model.SourceVikunja, err)
		return
	}
	events, err := a.vikunjaEvents(snap)
	if err != nil {
		a.fail(model.SourceVikunja, err)
		return
	}
	a.read(model.SourceVikunja, at)
	a.addEvents(events)
}
