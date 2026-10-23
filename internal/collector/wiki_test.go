package collector

import (
	"context"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// listedWiki holds pages, never edited, and counts its listings.
type listedWiki struct {
	pages []wiki.Page
	lists *int
}

func (w listedWiki) Fetch(_ context.Context, id int64) (wiki.Snapshot, error) {
	for _, p := range w.pages {
		if p.ID == id {
			return wiki.Snapshot{Instance: testWikiHost, Page: p, ObservedAt: testNow}, nil
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
	snap.Comments = []vikunja.Comment{
		{ID: 1, Body: `https://wiki.example.com/en/widgets/a and https://wiki.example.com/ai/notes`, Created: at(2)},
		{ID: 2, Body: `https://wiki.example.com/i/43 and https://wiki.example.com/en/widgets/missing`, Created: at(3)},
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
	if cov := coverageOf(t, s, model.SourceWiki); cov.LastSuccessfulRead == nil || !hasNote(cov, "en/widgets/missing") {
		t.Errorf("wiki coverage = %+v, want a read and the missing page noted", cov)
	}
}
