package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

func TestWikiLinks(t *testing.T) {
	agent, person := vikunja.User{Username: testAgent}, vikunja.User{Username: "owner"}
	s := vikunja.Snapshot{Comments: []vikunja.Comment{
		{ID: 1, Author: agent, Body: `<p>See <a href="https://wiki.example.com/en/widgets/a?x=1&amp;y=2">a</a> ` +
			`and https://WIKI.example.com/en/widgets/a.</p>`},
		{ID: 2, Author: agent, Body: `<p>Also https://wiki.example.com:8443/i/41, not ` +
			`https://wiki.example.org/en/widgets/b nor https://other.example.com/en/widgets/c.</p>`},
		{ID: 3, Author: agent, Body: `<p>No link.</p>`},
		{ID: 4, Author: person, Body: `<p>Context: https://wiki.example.com/en/widgets/runbook</p>`},
	}}
	want := []pageLink{
		{comment: 1, Link: wiki.Link{Locale: "en", Path: "widgets/a"}},
		{comment: 2, Link: wiki.Link{ID: 41}},
	}
	byAgent := func(u vikunja.User) bool { return u == agent }
	if got := wikiLinks(s, wikiURLPattern("wiki.example.com"), byAgent); !slices.Equal(got, want) {
		t.Errorf("wikiLinks() = %+v, want %+v", got, want)
	}
}
