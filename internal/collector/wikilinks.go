package collector

import (
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/source/vikunja"
)

const (
	// defaultWikiLocale is the locale of a wiki URL that names none.
	defaultWikiLocale = "en"
	// wikiIDRoute starts a link to a page by id: /i/<id>.
	wikiIDRoute = "i"
	// trailingPunctuation ends a sentence after a bare URL, not the URL.
	trailingPunctuation = ".,;:!?)"
)

var (
	// wikiLocale matches a Wiki.js locale code: en, fr, pt-br, zh-hans.
	wikiLocale = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]{2,4})?$`)
	// wikiPageRoutes are the routes whose rest is a page's locale and
	// path: its history, source and editor.
	wikiPageRoutes = []string{"h", "s", "e"}
	// wikiOtherRoutes lead to no page: administration, tags, profile,
	// login, assets, the API.
	wikiOtherRoutes = []string{"a", "t", "p", "login", "logout", "register", "_assets", "graphql"}
)

// pageLink is a wiki page a task comment links to: by id, or by locale
// and path.
type pageLink struct {
	comment int64
	id      int64
	locale  string
	path    string
}

// wikiLinks lists the pages of the wiki at host that s's comments link
// to, each comment's links once, in comment order.
func wikiLinks(s vikunja.Snapshot, host string) []pageLink {
	urlPattern := regexp.MustCompile(`(?i)https?://` + regexp.QuoteMeta(host) + `(?::[0-9]+)?(/[^\s"'<>]*)?`)
	var out []pageLink
	for _, c := range s.Comments {
		var seen []pageLink
		for _, raw := range urlPattern.FindAllString(c.Body, -1) {
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

// parseWikiURL reads the page a wiki URL names: /i/<id>, or
// [/<route>][/<locale>]/<path>.
func parseWikiURL(raw string) (pageLink, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return pageLink{}, false
	}
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 || slices.Contains(wikiOtherRoutes, parts[0]) {
		return pageLink{}, false
	}
	if parts[0] == wikiIDRoute {
		if len(parts) != 2 {
			return pageLink{}, false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return pageLink{id: id}, err == nil && id > 0
	}
	if slices.Contains(wikiPageRoutes, parts[0]) {
		parts = parts[1:]
	}
	locale := defaultWikiLocale
	if len(parts) > 1 && wikiLocale.MatchString(parts[0]) {
		locale, parts = parts[0], parts[1:]
	}
	if len(parts) == 0 {
		return pageLink{}, false
	}
	return pageLink{locale: locale, path: strings.Join(parts, "/")}, true
}
