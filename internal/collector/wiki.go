package collector

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/wiki"
)

// readPage reads page id into its key and events.
func (a *assembly) readPage(ctx context.Context, id int64) (model.PageKey, []model.Event, error) {
	snap, err := a.c.src.Wiki.Fetch(ctx, id)
	if err != nil {
		return model.PageKey{}, nil, err
	}
	recs, err := wiki.Normalize(snap, a.cfg.WikiAgentUsers)
	if err != nil {
		return model.PageKey{}, nil, err
	}
	return model.PageKey{Instance: snap.Instance, ID: snap.Page.ID, Locale: snap.Page.Locale}, recs.Events, nil
}

// readRootPage reads the root page. A page in another locale than the
// root's is a wrong root, not a failed read.
func (a *assembly) readRootPage(ctx context.Context, key model.PageKey) error {
	at := a.c.now()
	got, events, err := a.readPage(ctx, key.ID)
	if err != nil {
		a.fail(model.SourceWiki, err)
		return nil
	}
	if got != key {
		return fmt.Errorf("collector: page %d is %s, not %s", key.ID, got, key)
	}
	a.read(model.SourceWiki, at)
	a.addEvents(events)
	return nil
}

// readLinkedPages reads the pages the task's comments link to, each
// joined to the task by the comment that links it. Without a link, the
// wiki is not read, and its coverage says so.
func (a *assembly) readLinkedPages(ctx context.Context, links []pageLink) {
	a.source(model.SourceWiki)
	if len(links) == 0 {
		a.note(model.SourceWiki, "no comment of the task links a page of "+a.cfg.WikiInstance+": the wiki was not read")
		return
	}
	at := a.c.now()
	byPage, err := a.resolveLinks(ctx, links)
	if err != nil {
		a.fail(model.SourceWiki, err)
		return
	}
	var events []model.Event
	keys := make(map[int64]model.PageKey)
	for _, id := range slices.Sorted(maps.Keys(byPage)) {
		key, evs, err := a.readPage(ctx, id)
		if err != nil {
			a.fail(model.SourceWiki, fmt.Errorf("page %d: %w", id, err))
			return
		}
		keys[id] = key
		events = append(events, evs...)
	}
	a.read(model.SourceWiki, at)
	a.addEvents(events)
	for id, comments := range byPage {
		for _, c := range comments {
			a.links.identifier(keys[id].Ref(), a.rootRef, model.Evidence{
				Source: model.SourceVikunja, Instance: a.cfg.VikunjaInstance,
				Ref:  "GET /api/v1/" + taskObject(*a.root.Task) + "/comments",
				Note: "comment " + strconv.FormatInt(c, 10) + " links the page",
			})
		}
	}
}

// resolveLinks maps each linked page's id to the comments that link it.
// Links by path are resolved against one listing of the wiki: as written,
// then, when the path's first part read as a locale, as a path of the
// default locale. A path that names no page is noted, not an error.
func (a *assembly) resolveLinks(ctx context.Context, links []pageLink) (map[int64][]int64, error) {
	var byPath map[pageLink]int64
	out := make(map[int64][]int64)
	for _, l := range links {
		id := l.id
		if id == 0 && byPath == nil {
			var err error
			if byPath, err = a.listPages(ctx); err != nil {
				return nil, err
			}
		}
		for _, try := range l.lookups() {
			if id == 0 {
				id = byPath[try]
			}
		}
		if id == 0 {
			a.note(model.SourceWiki, "comment "+strconv.FormatInt(l.comment, 10)+" links "+l.locale+"/"+l.path+
				", which is no page of "+a.cfg.WikiInstance)
			continue
		}
		if !slices.Contains(out[id], l.comment) {
			out[id] = append(out[id], l.comment)
		}
	}
	return out, nil
}

// listPages indexes the wiki's pages by locale and path.
func (a *assembly) listPages(ctx context.Context) (map[pageLink]int64, error) {
	pages, err := a.c.src.Wiki.Pages(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[pageLink]int64, len(pages))
	for _, p := range pages {
		out[pageLink{locale: p.Locale, path: p.Path}] = p.ID
	}
	return out, nil
}

// lookups are the locale and path pairs a link by path may mean.
func (l pageLink) lookups() []pageLink {
	if l.path == "" {
		return nil
	}
	out := []pageLink{{locale: l.locale, path: l.path}}
	if l.locale != defaultWikiLocale {
		out = append(out, pageLink{locale: defaultWikiLocale, path: l.locale + "/" + l.path})
	}
	return out
}
