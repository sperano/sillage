package wiki

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// DefaultLocale is the locale of a wiki URL that names none.
	DefaultLocale = "en"
	// idRoute starts a link to a page by id: /i/<id>.
	idRoute = "i"
)

var (
	// localePattern matches a Wiki.js locale code: en, fr, pt-br, zh-hans.
	localePattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]{2,4})?$`)
	// pageRoutes are the routes whose rest is a page's locale and path:
	// its history, source and editor.
	pageRoutes = []string{"h", "s", "e"}
	// otherRoutes lead to no page: administration, tags, profile, login,
	// assets, the API.
	otherRoutes = []string{"a", "t", "p", "login", "logout", "register", "_assets", "graphql"}
)

// Link is the page a wiki URL names: by id, or by locale and path. A path
// has no leading slash and no locale, as Wiki.js stores it.
type Link struct {
	ID     int64
	Locale string
	Path   string
}

// ParseLink reads the page a wiki URL names: /i/<id>, or
// [/<route>][/<locale>]/<path>. It does not check the URL's host.
func ParseLink(raw string) (Link, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, false
	}
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 || slices.Contains(otherRoutes, parts[0]) {
		return Link{}, false
	}
	if parts[0] == idRoute {
		if len(parts) != 2 {
			return Link{}, false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return Link{ID: id}, err == nil && id > 0
	}
	if slices.Contains(pageRoutes, parts[0]) {
		parts = parts[1:]
	}
	locale := DefaultLocale
	if len(parts) > 1 && IsLocale(parts[0]) {
		locale, parts = parts[0], parts[1:]
	}
	if len(parts) == 0 {
		return Link{}, false
	}
	return Link{Locale: locale, Path: strings.Join(parts, "/")}, true
}

// IsLocale reports whether s reads as a Wiki.js locale code.
func IsLocale(s string) bool { return localePattern.MatchString(s) }

// String names the page a link points to, as written.
func (l Link) String() string {
	if l.Path == "" {
		return idRoute + "/" + strconv.FormatInt(l.ID, 10)
	}
	return l.Locale + "/" + l.Path
}

// Lookups are the locale and path pairs a link by path may mean: a first
// segment that reads as a locale may be the start of a default-locale
// path instead. A link by id has none.
func (l Link) Lookups() []Link {
	if l.Path == "" {
		return nil
	}
	out := []Link{{Locale: l.Locale, Path: l.Path}}
	if l.Locale != DefaultLocale {
		out = append(out, Link{Locale: DefaultLocale, Path: l.Locale + "/" + l.Path})
	}
	return out
}
