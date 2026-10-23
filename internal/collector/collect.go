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
	links := a.readTask(ctx, id)
	a.readLinkedPages(ctx, links)
	a.readKelos(ctx)
	a.announcedRuns()
	a.taskRuns(id)
	a.readGitHub(ctx, taskBranch(id), repos)
	a.prRuns()
	a.actorRuns()
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
	a.announcedRuns()
	a.pageRuns(key)
	a.readGitHub(ctx, pageBranch(key.ID), repos)
	a.prRuns()
	a.actorRuns()
	return nil
}

// readTask reads the task and its comments, and returns the wiki pages
// they link. A failed read yields neither.
func (a *assembly) readTask(ctx context.Context, id int64) []pageLink {
	at := a.c.now()
	snap, err := a.c.src.Vikunja.Fetch(ctx, id)
	if err != nil {
		a.fail(model.SourceVikunja, err)
		return nil
	}
	a.read(model.SourceVikunja, at)
	a.addEvents(a.vikunjaEvents(snap))
	return wikiLinks(snap, a.c.wikiURL, a.delivering)
}

// delivering reports whether u's comments can name the task's
// deliverable: an agent's or the service account's. A person's link is
// context, not part of the story.
func (a *assembly) delivering(u vikunja.User) bool {
	kind := a.vikunjaAccount(&u).Kind
	return kind == model.ActorAgent || kind == model.ActorService
}
