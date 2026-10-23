package collector

import (
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// trailingPunctuation ends a sentence after a bare URL, not the URL.
const trailingPunctuation = ".,;:!?)"

// pageLink is a wiki page a task comment links to: by id, or by locale
// and path.
type pageLink struct {
	comment int64
	wiki.Link
}

// wikiURLPattern matches a URL of the wiki at host, in a comment's HTML
// or text.
func wikiURLPattern(host string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)https?://` + regexp.QuoteMeta(host) + `(?::[0-9]+)?(/[^\s"'<>]*)?`)
}

// wikiLinks lists the pages that the comments of s by the accounts
// linking selects link to, by the URLs wikiURL matches, each comment's
// links once, in comment order.
func wikiLinks(s vikunja.Snapshot, wikiURL *regexp.Regexp, linking func(vikunja.User) bool) []pageLink {
	var out []pageLink
	for _, c := range s.Comments {
		if !linking(c.Author) {
			continue
		}
		var seen []pageLink
		for _, raw := range wikiURL.FindAllString(c.Body, -1) {
			link, ok := parseWikiURL(strings.TrimRight(html.UnescapeString(raw), trailingPunctuation))
			if !ok {
				continue
			}
			link.comment = c.ID
			if !slices.Contains(seen, link) {
				seen = append(seen, link)
			}
		}
		out = append(out, seen...)
	}
	return out
}

// parseWikiURL reads the page a wiki URL names (wiki.ParseLink).
func parseWikiURL(raw string) (pageLink, bool) {
	l, ok := wiki.ParseLink(raw)
	return pageLink{Link: l}, ok
}
