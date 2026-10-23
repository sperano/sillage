package overview

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// Filter selects rows; every set field must match.
type Filter struct {
	// Project matches a row with a run carrying that project label, in or
	// out of the window.
	Project string
	// Task matches, case-insensitively, part of a row's title or of its
	// root's key ("tasks/12", a host).
	Task string
	// Executor matches a row with a run of that agent profile.
	Executor string
	// ReviewRequested matches a row with a review wait in the window.
	ReviewRequested bool
	// FailedRun matches a row with a failed run, an earlier attempt of a
	// retry in the window included.
	FailedRun bool
	// MissingHistory matches a row whose history is missing in part of the
	// window.
	MissingHistory bool
}

// Match reports whether the row matches every set field.
func (f Filter) Match(row Row) bool {
	return (f.Project == "" || slices.Contains(row.Projects, f.Project)) &&
		(f.Task == "" || matchesText(row, f.Task)) &&
		(f.Executor == "" || slices.ContainsFunc(row.Runs, func(r model.Run) bool { return r.Agent == f.Executor })) &&
		(!f.ReviewRequested || len(row.ReviewWaits) > 0) &&
		(!f.FailedRun || slices.ContainsFunc(row.Runs, func(r model.Run) bool { return r.Outcome == model.OutcomeFailed })) &&
		(!f.MissingHistory || row.MissingHistory)
}

func matchesText(row Row, text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(strings.ToLower(row.Title), text) || strings.Contains(strings.ToLower(rowKey(row)), text)
}

// rowKey is the row's root key: "vikunja.example.com/tasks/12".
func rowKey(row Row) string {
	if row.Root.Task != nil {
		return row.Root.Task.String()
	}
	return row.Root.Page.String()
}

// Page is one page of an overview's rows.
type Page struct {
	Window Window `json:"window"`
	Rows   []Row  `json:"rows"`
	// Total is how many rows match, on every page.
	Total int `json:"total"`
	// Next is the cursor of the next page, empty on the last one.
	Next string `json:"next,omitempty"`
}

// ErrCursor is returned for a cursor Paginate did not make.
var ErrCursor = errors.New("overview: not a cursor of this API")

// cursor is where a page ends: its last row's order.
type cursor struct {
	At  time.Time `json:"at"`
	Key string    `json:"key"`
}

// Paginate returns the rows f matches, most recently active first, then by
// root key: at most limit of them, after the row the cursor after names
// (from the first when empty).
func Paginate(w Window, rows []Row, f Filter, after string, limit int) (Page, error) {
	matched := slices.DeleteFunc(slices.Clone(rows), func(r Row) bool { return !f.Match(r) })
	slices.SortFunc(matched, compareRows)
	start := 0
	if after != "" {
		c, err := decodeCursor(after)
		if err != nil {
			return Page{}, err
		}
		start = sortIndex(matched, c)
	}
	end := min(start+limit, len(matched))
	page := Page{Window: w, Rows: matched[start:end], Total: len(matched)}
	if end < len(matched) {
		last := matched[end-1]
		page.Next = encodeCursor(cursor{At: last.LastActivity, Key: rowKey(last)})
	}
	return page, nil
}

// compareRows orders rows by last activity, most recent first, then by
// root key.
func compareRows(a, b Row) int {
	return cmp.Or(b.LastActivity.Compare(a.LastActivity), strings.Compare(rowKey(a), rowKey(b)))
}

// sortIndex is the index of the first row after c.
func sortIndex(rows []Row, c cursor) int {
	i, _ := slices.BinarySearchFunc(rows, c, func(r Row, c cursor) int {
		return cmp.Or(c.At.Compare(r.LastActivity), strings.Compare(rowKey(r), c.Key), -1)
	})
	return i
}

func encodeCursor(c cursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(s string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, ErrCursor
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil || c.Key == "" || c.At.IsZero() {
		return cursor{}, ErrCursor
	}
	return c, nil
}
