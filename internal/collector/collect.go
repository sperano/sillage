package collector

import (
	"context"
	"fmt"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// collectTask assembles the story of Vikunja task id: the task and its
// comments, the wiki pages they link, the namespace's runs that belong to
// the task, and the pull requests of its branch.
func (a *assembly) collectTask(ctx context.Context, key model.TaskKey, repos []github.Repository) error {
	if key.Instance != a.cfg.VikunjaInstance {
		return fmt.Errorf("collector: task %s is not on %s", key, a.cfg.VikunjaInstance)
	}
	id := key.ID
	for _, s := range []model.Source{model.SourceVikunja, model.SourceKelos, model.SourceGitHub, model.SourceWiki} {
		a.source(s)
	}
	if links, ok := a.readTask(ctx, id); ok {
		a.readLinkedPages(ctx, links)
	} else {
		a.note(model.SourceWiki, "the task could not be read, so the pages its comments link are unknown: "+
			"the wiki was not read")
	}
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

// collectPage assembles the story of a wiki page: its versions and
// comments, the runs its follow-ups started, and the pull requests of its
// branch.
func (a *assembly) collectPage(ctx context.Context, key model.PageKey, repos []github.Repository) error {
	if key.Instance != a.cfg.WikiInstance {
		return fmt.Errorf("collector: page %s is not on %s", key, a.cfg.WikiInstance)
	}
	for _, s := range []model.Source{model.SourceWiki, model.SourceKelos, model.SourceGitHub} {
		a.source(s)
	}
	if err := a.readRootPage(ctx, key); err != nil {
		return err
	}
	a.readKelos(ctx)
	a.keepRuns()
	a.announcedRuns()
	a.pageRuns(key)
	a.readGitHub(ctx, pageBranch(key.ID), repos)
	// The pull requests' announcements are read only now.
	a.announcedRuns()
	a.prRuns()
	a.actorRuns()
	a.requestLinks()
	return nil
}

// readTask reads the task and its comments, and returns the wiki pages
// they link. A failed read, or one the source cannot normalize, yields
// neither, and false.
func (a *assembly) readTask(ctx context.Context, id int64) ([]pageLink, bool) {
	at := a.c.now()
	snap, err := a.c.src.Vikunja.Fetch(ctx, id)
	if err != nil {
		a.fail(model.SourceVikunja, err)
		return nil, false
	}
	events, err := a.vikunjaEvents(snap)
	if err != nil {
		a.fail(model.SourceVikunja, err)
		return nil, false
	}
	a.read(model.SourceVikunja, at)
	a.addEvents(events)
	return wikiLinks(snap, a.c.wikiURL, a.delivering), true
}

// delivering reports whether u's comments can name the task's
// deliverable: an agent's or the service account's. A person's link is
// context, not part of the story.
func (a *assembly) delivering(u vikunja.User) bool {
	return u.Username != "" && (u.Username == a.cfg.VikunjaServiceAccount || a.vikunjaAgent(u.Username))
}
