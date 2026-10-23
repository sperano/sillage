package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/source/vikunja"
)

func TestParseWikiURL(t *testing.T) {
	tests := []struct {
		url  string
		want pageLink
		ok   bool
	}{
		{"https://wiki.example.com/en/widgets/reports/caching", pageLink{locale: "en", path: "widgets/reports/caching"}, true},
		{"https://wiki.example.com/widgets/home", pageLink{locale: "en", path: "widgets/home"}, true},
		{"https://wiki.example.com/fr/widgets/home", pageLink{locale: "fr", path: "widgets/home"}, true},
		{"https://wiki.example.com/pt-br/widgets/home", pageLink{locale: "pt-br", path: "widgets/home"}, true},
		{"https://wiki.example.com/h/en/widgets/home", pageLink{locale: "en", path: "widgets/home"}, true},
		{"https://wiki.example.com/en/widgets/home#section", pageLink{locale: "en", path: "widgets/home"}, true},
		{"https://wiki.example.com/i/41", pageLink{id: 41}, true},
		{"https://wiki.example.com/en", pageLink{locale: "en", path: "en"}, true},
		{"https://wiki.example.com/i/zero", pageLink{}, false},
		{"https://wiki.example.com/i/0", pageLink{}, false},
		{"https://wiki.example.com/t/widgets", pageLink{}, false},
		{"https://wiki.example.com/a/pages", pageLink{}, false},
		{"https://wiki.example.com/", pageLink{}, false},
	}
	for _, tt := range tests {
		got, ok := parseWikiURL(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseWikiURL(%q) = %+v, %v; want %+v, %v", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}

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
		{comment: 1, locale: "en", path: "widgets/a"},
		{comment: 2, id: 41},
	}
	byAgent := func(u vikunja.User) bool { return u == agent }
	if got := wikiLinks(s, wikiURLPattern("wiki.example.com"), byAgent); !slices.Equal(got, want) {
		t.Errorf("wikiLinks() = %+v, want %+v", got, want)
	}
}

func TestPageLinkLookups(t *testing.T) {
	tests := []struct {
		link pageLink
		want []pageLink
	}{
		{pageLink{id: 41}, nil},
		{pageLink{locale: "en", path: "widgets/a"}, []pageLink{{locale: "en", path: "widgets/a"}}},
		{pageLink{locale: "ai", path: "notes"}, []pageLink{{locale: "ai", path: "notes"}, {locale: "en", path: "ai/notes"}}},
	}
	for _, tt := range tests {
		if got := tt.link.lookups(); !slices.Equal(got, tt.want) {
			t.Errorf("%+v.lookups() = %+v, want %+v", tt.link, got, tt.want)
		}
	}
}
