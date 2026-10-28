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
		if reason, off := a.c.off[s]; off {
			a.note(s, offNote(reason))
		}
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

// offNote is the note on the coverage of a source that is off.
func offNote(reason string) string {
	return "not read: the source is off, as sillage's configuration of it is incomplete (" + reason + ")"
}

// readTask reads the task and its comments. A failed read, or one the
// source cannot normalize, adds no events; nor does Vikunja being off.
func (a *assembly) readTask(ctx context.Context, id int64) {
	if !a.c.on(model.SourceVikunja) {
		return
	}
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
