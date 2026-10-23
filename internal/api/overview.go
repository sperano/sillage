package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sperano/sillage/internal/overview"
	"github.com/sperano/sillage/internal/store"
)

const (
	day = 24 * time.Hour
	// DefaultOverviewWindow is the window an overview covers when the
	// request names no start: the day before its end.
	DefaultOverviewWindow = day
	// MaxOverviewWindow bounds an overview's window.
	MaxOverviewWindow = 31 * day
	// DefaultOverviewLimit is how many rows a page holds when the request
	// sets no limit.
	DefaultOverviewLimit = 50
	// MaxOverviewLimit caps a page's rows.
	MaxOverviewLimit = 200
	// maxCursor bounds a cursor parameter, in bytes: Paginate's are far
	// shorter.
	maxCursor = 1024
)

// The overview's query parameters.
const (
	paramFrom     = "from"
	paramTo       = "to"
	paramProject  = "project"
	paramTask     = "task"
	paramExecutor = "executor"
	paramReview   = "review"
	paramFailed   = "failed"
	paramMissing  = "missing"
	paramAfter    = "after"
)

// overviewQuery is what an overview request asks for.
type overviewQuery struct {
	window overview.Window
	filter overview.Filter
	after  string
	limit  int
}

// overviewPage answers GET /api/v1/overview: one page of the rows of the
// tasks active in the window that match the filters.
func (a *api) overviewPage(r *http.Request) (any, error) {
	q, err := a.parseOverview(r.URL.Query())
	if err != nil {
		return nil, err
	}
	found, err := a.cfg.Store.Activity(r.Context(), store.ActivityQuery{
		From: q.window.From, To: q.window.To, Project: q.filter.Project, Kinds: overview.Kinds,
	})
	if errors.Is(err, store.ErrTooManyRoots) {
		return nil, badRequest("more than %d tasks may have been active in this window: narrow it, or choose a project",
			store.MaxActivityRoots)
	}
	if err != nil {
		return nil, err
	}
	inputs := make([]overview.Input, 0, len(found))
	for _, f := range found {
		inputs = append(inputs, overview.Input{Root: f.Root, Title: f.Title, Records: f.Records})
	}
	rows, err := overview.Build(q.window, inputs)
	if err != nil {
		return nil, err
	}
	page, err := overview.Paginate(q.window, rows, q.filter, q.after, q.limit)
	if errors.Is(err, overview.ErrCursor) {
		return nil, badRequest("%s is not a cursor this API gave", paramAfter)
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// parseOverview reads and bounds an overview request's parameters.
func (a *api) parseOverview(v url.Values) (overviewQuery, error) {
	w, err := a.overviewWindow(v.Get(paramFrom), v.Get(paramTo))
	if err != nil {
		return overviewQuery{}, err
	}
	q := overviewQuery{window: w, after: v.Get(paramAfter), limit: DefaultOverviewLimit}
	if len(q.after) > maxCursor {
		return q, badRequest("%s is not a cursor this API gave", paramAfter)
	}
	if raw := v.Get(paramLimit); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxOverviewLimit {
			return q, badRequest("%s must be a number from 1 to %d", paramLimit, MaxOverviewLimit)
		}
		q.limit = n
	}
	f := &q.filter
	for _, p := range []struct {
		name string
		dst  *string
	}{{paramProject, &f.Project}, {paramTask, &f.Task}, {paramExecutor, &f.Executor}} {
		if *p.dst, err = textParam(v, p.name); err != nil {
			return q, err
		}
	}
	for _, p := range []struct {
		name string
		dst  *bool
	}{{paramReview, &f.ReviewRequested}, {paramFailed, &f.FailedRun}, {paramMissing, &f.MissingHistory}} {
		if *p.dst, err = flagParam(v, p.name); err != nil {
			return q, err
		}
	}
	return q, nil
}

// overviewWindow reads the window: to defaults to now, from to
// DefaultOverviewWindow before to.
func (a *api) overviewWindow(rawFrom, rawTo string) (overview.Window, error) {
	to := a.cfg.Now().UTC().Truncate(time.Second)
	if rawTo != "" {
		t, err := time.Parse(time.RFC3339, rawTo)
		if err != nil {
			return overview.Window{}, badRequest("%s %s is not an RFC 3339 time", paramTo, echo(rawTo))
		}
		to = t.UTC()
	}
	from := to.Add(-DefaultOverviewWindow)
	if rawFrom != "" {
		t, err := time.Parse(time.RFC3339, rawFrom)
		if err != nil {
			return overview.Window{}, badRequest("%s %s is not an RFC 3339 time", paramFrom, echo(rawFrom))
		}
		from = t.UTC()
	}
	switch {
	case !from.Before(to):
		return overview.Window{}, badRequest("%s must be before %s", paramFrom, paramTo)
	case to.Sub(from) > MaxOverviewWindow:
		return overview.Window{}, badRequest("the window is longer than the %d days allowed",
			MaxOverviewWindow/day)
	}
	return overview.Window{From: from, To: to}, nil
}

// textParam is a trimmed text parameter of at most MaxSearchQuery
// characters.
func textParam(v url.Values, name string) (string, error) {
	s := strings.TrimSpace(v.Get(name))
	if n := utf8.RuneCountInString(s); n > MaxSearchQuery {
		return "", badRequest("%s has %d characters, more than the %d allowed", name, n, MaxSearchQuery)
	}
	return s, nil
}

// flagParam is a boolean parameter: true or false, false when absent.
func flagParam(v url.Values, name string) (bool, error) {
	switch v.Get(name) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, badRequest("%s must be true or false", name)
}
