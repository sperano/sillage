package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/overview"
	"github.com/sperano/sillage/internal/store"
)

// overviewGolden is the overview fixture set's afternoon, unfiltered.
const overviewGolden = "../../testdata/fixtures/overview/overview.golden.json"

// The fixture afternoon.
const (
	afternoonFrom = "2026-03-10T14:00:00Z"
	afternoonTo   = "2026-03-10T15:00:00Z"
)

func overviewPath(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	return "/api/v1/overview?" + v.Encode()
}

func afternoonPath(extra map[string]string) string {
	params := map[string]string{"from": afternoonFrom, "to": afternoonTo}
	for k, v := range extra {
		params[k] = v
	}
	return overviewPath(params)
}

func getPage(t *testing.T, h http.Handler, path string) overview.Page {
	t.Helper()
	var page overview.Page
	decode(t, get(t, h, path), &page)
	return page
}

func pageTasks(p overview.Page) []int64 {
	ids := make([]int64, 0, len(p.Rows))
	for _, r := range p.Rows {
		ids = append(ids, r.Root.Task.ID)
	}
	return ids
}

// The afternoon's overview is the golden file the web tests render, byte
// for byte but for indentation: the fixture stories of other days have no
// row in it.
func TestOverviewThroughTheHandler(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	rec := get(t, h, afternoonPath(nil))
	golden, err := os.ReadFile(overviewGolden)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, golden); err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSuffix(rec.Body.Bytes(), []byte("\n")); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("body differs from %s:\n got %s\nwant %s", overviewGolden, got, want.Bytes())
	}
}

func TestOverviewFilters(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, c := range []struct {
		params map[string]string
		want   []int64
	}{
		{map[string]string{"project": "gadgets"}, []int64{203}},
		{map[string]string{"task": " route "}, []int64{204}},
		{map[string]string{"executor": "agent-cedar"}, []int64{201, 203}},
		{map[string]string{"review": "true"}, []int64{201, 202}},
		{map[string]string{"failed": "true", "review": "false"}, []int64{203}},
		{map[string]string{"missing": "true"}, []int64{205}},
	} {
		path := afternoonPath(c.params)
		t.Run(path, func(t *testing.T) {
			if got := pageTasks(getPage(t, h, path)); !slices.Equal(got, c.want) {
				t.Errorf("rows %v, want %v", got, c.want)
			}
		})
	}
}

// A client following next sees every row once.
func TestOverviewPages(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	const limit, total = 4, 7
	var seen []int64
	params := map[string]string{"limit": strconv.Itoa(limit)}
	for range total {
		page := getPage(t, h, afternoonPath(params))
		if page.Total != total || len(page.Rows) > limit {
			t.Fatalf("page of %d rows, total %d; want at most %d, total %d", len(page.Rows), page.Total, limit, total)
		}
		seen = append(seen, pageTasks(page)...)
		if page.Next == "" {
			break
		}
		params["after"] = page.Next
	}
	if want := []int64{201, 202, 203, 204, 208, 206, 205}; !slices.Equal(seen, want) {
		t.Errorf("pages = %v, want %v", seen, want)
	}
}

// Without a window, the overview covers the day before now.
func TestOverviewDefaultWindow(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	h := New(Config{Store: newFakeStore(t), Now: func() time.Time { return now }})
	page := getPage(t, h, "/api/v1/overview")
	if !page.Window.To.Equal(now) || !page.Window.From.Equal(now.Add(-DefaultOverviewWindow)) {
		t.Errorf("window = %+v, want the day before %s", page.Window, now)
	}
	if page.Total != 7 {
		t.Errorf("total = %d, want the afternoon's 7 rows", page.Total)
	}
	page = getPage(t, h, overviewPath(map[string]string{"from": "2026-03-10T14:30:00+01:00"}))
	if want := time.Date(2026, 3, 10, 13, 30, 0, 0, time.UTC); !page.Window.From.Equal(want) {
		t.Errorf("from = %s, want %s", page.Window.From, want)
	}
}

// Following next without the page's window is refused: by default the
// window ends now, and moves between two requests.
func TestOverviewCursorOfAnotherWindow(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	page := getPage(t, h, afternoonPath(map[string]string{"limit": "1"}))
	p := wantProblem(t, get(t, h, overviewPath(map[string]string{"after": page.Next})), http.StatusBadRequest)
	if !strings.Contains(p.Detail, "another window") {
		t.Errorf("detail = %q, want the cursor's window named", p.Detail)
	}
}

func TestOverviewBadRequest(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, params := range []map[string]string{
		{"from": "yesterday"},
		{"to": "2026-03-10"},
		{"from": afternoonTo, "to": afternoonFrom},
		{"from": afternoonFrom, "to": afternoonFrom},
		{"from": "2026-01-01T00:00:00Z", "to": afternoonTo},
		{"from": afternoonFrom, "to": afternoonTo, "limit": "0"},
		{"from": afternoonFrom, "to": afternoonTo, "limit": "201"},
		{"from": afternoonFrom, "to": afternoonTo, "review": "yes"},
		{"from": afternoonFrom, "to": afternoonTo, "after": "not-a-cursor"},
		{"from": afternoonFrom, "to": afternoonTo, "after": strings.Repeat("a", maxCursor+1)},
		{"from": afternoonFrom, "to": afternoonTo, "task": strings.Repeat("x", MaxSearchQuery+1)},
	} {
		t.Run(overviewPath(params), func(t *testing.T) {
			wantProblem(t, get(t, h, overviewPath(params)), http.StatusBadRequest)
		})
	}
}

// tooManyStore has more roots in every window than the store reads.
type tooManyStore struct{ *fakeStore }

func (tooManyStore) Activity(context.Context, store.ActivityQuery) ([]store.RootActivity, error) {
	return nil, store.ErrTooManyRoots
}

func TestOverviewTooManyRoots(t *testing.T) {
	h := New(Config{Store: tooManyStore{newFakeStore(t)}})
	p := wantProblem(t, get(t, h, afternoonPath(nil)), http.StatusBadRequest)
	if !strings.Contains(p.Detail, "shorten the window") {
		t.Errorf("detail = %q, want advice to narrow the window", p.Detail)
	}
}

func TestOverviewWithoutStore(t *testing.T) {
	wantProblem(t, get(t, New(Config{}), afternoonPath(nil)), http.StatusServiceUnavailable)
}
