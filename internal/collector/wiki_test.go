package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// listedWiki holds pages and their history trails, and counts its
// listings.
type listedWiki struct {
	pages  []wiki.Page
	trails map[int64][]wiki.Version
	lists  *int
}

func (w listedWiki) Fetch(_ context.Context, id int64) (wiki.Snapshot, error) {
	for _, p := range w.pages {
		if p.ID == id {
			return wiki.Snapshot{Instance: testWikiHost, Page: p, Trail: w.trails[id], ObservedAt: testNow}, nil
		}
	}
	return wiki.Snapshot{}, errDown
}

func (w listedWiki) Pages(context.Context) ([]wiki.Page, error) {
	*w.lists++
	return w.pages, nil
}

func TestLinkedPagesListTheWikiOnce(t *testing.T) {
	page := func(id int64, path string) wiki.Page {
		return wiki.Page{ID: id, Path: path, Locale: "en", CreatedAt: at(1), UpdatedAt: at(1),
			AuthorID: 7, AuthorName: "Robin Owner", CreatorID: 7, CreatorName: "Robin Owner"}
	}
	lists := 0
	src := emptySources()
	src.Wiki = listedWiki{pages: []wiki.Page{page(41, "widgets/a"), page(42, "ai/notes"), page(43, "widgets/c")}, lists: &lists}
	snap := announcedTask(104, "", nil)
	agent := vikunja.User{ID: 21, Username: testAgent}
	snap.Comments = []vikunja.Comment{
		{ID: 1, Author: agent, Created: at(2),
			Body: `https://wiki.example.com/en/widgets/a and https://wiki.example.com/ai/notes`},
		{ID: 2, Author: agent, Created: at(3),
			Body: `https://wiki.example.com/i/43, https://wiki.example.com/i/44 and https://wiki.example.com/en/widgets/missing`},
		{ID: 3, Author: vikunja.User{ID: 7, Username: "owner"}, Created: at(4), Body: `https://wiki.example.com/i/42`},
	}
	src.Vikunja = fixedVikunja{snap}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	if lists != 1 {
		t.Errorf("the wiki was listed %d times, want once", lists)
	}
	for _, id := range []int64{41, 42, 43} {
		key := model.PageKey{Instance: testWikiHost, ID: id, Locale: "en"}
		if _, ok := linkBetween(s, key.Ref(), taskRoot(104).Task.Ref()); !ok {
			t.Errorf("page %d is not joined to the task", id)
		}
	}
	cov := coverageOf(t, s, model.SourceWiki)
	if cov.LastSuccessfulRead == nil || !hasNote(cov, "links en/widgets/missing") || !hasNote(cov, "links i/44") {
		t.Errorf("wiki coverage = %+v, want a read and both missing pages noted", cov)
	}
	c, _ := linkBetween(s, model.PageKey{Instance: testWikiHost, ID: 42, Locale: "en"}.Ref(), taskRoot(104).Task.Ref())
	if len(c.Evidence) != 1 || !strings.Contains(c.Evidence[0].Note, "comment 1 ") {
		t.Errorf("page 42 joined by %v, want only the agent's comment 1, not the person's", c.Evidence)
	}
}

func TestLinkedPageEditsJoinOnlyTheStorysRuns(t *testing.T) {
	lists := 0
	src := emptySources()
	src.Wiki = listedWiki{lists: &lists, pages: []wiki.Page{{
		ID: 41, Path: "widgets/a", Locale: "en", CreatedAt: at(5), UpdatedAt: at(6),
		AuthorID: testWikiUser, AuthorName: testAgent, CreatorID: testWikiUser, CreatorName: testAgent,
	}}, trails: map[int64][]wiki.Version{41: {{
		// The creation's own version; the live page, saved later, is an edit.
		VersionID: 1, VersionDate: at(5), AuthorID: testWikiUser, AuthorName: testAgent, ActionType: "initial",
	}}}}
	snap := announcedTask(104, "", nil)
	snap.Comments = []vikunja.Comment{{ID: 1, Author: vikunja.User{ID: 21, Username: testAgent}, Created: at(11),
		Body: `https://wiki.example.com/i/41`}}
	src.Vikunja = fixedVikunja{snap}
	src.Kelos = fixedKelos{kelos.Snapshot{Records: []kelos.TaskRecord{
		record("vikunja-widgets-agent-opus-104", "uid-own", taskLabels("104"), 4, 7),
		record("vikunja-widgets-agent-opus-999", "uid-other", taskLabels("999"), 0, 10),
	}}}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	var edits int
	for _, e := range s.Events {
		if e.Source != model.SourceWiki || (e.Kind != model.KindPageCreated && e.Kind != model.KindPageEdited) {
			continue
		}
		if e.Kind == model.KindPageEdited {
			edits++
		}
		if got := linksFrom(s, e.Ref()); len(got) != 1 || got[0].To != runRef("uid-own") || got[0].Ambiguous {
			t.Errorf("the linked page's %s %s links %v, want only the task's own run", e.Kind, e.ID, got)
		}
	}
	if edits == 0 {
		t.Error("the linked page's later edit was not read")
	}
	for _, r := range s.Runs {
		if r.Key.UID == "uid-other" {
			t.Error("another task's run is in the story through a page the task links")
		}
	}
}
