package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
	"github.com/sperano/sillage/internal/store"
	"github.com/sperano/sillage/internal/testzone"
)

// TestMain runs the tests outside UTC, so that output depending on the
// host's zone fails here too.
func TestMain(m *testing.M) { testzone.Main(m) }

// storyPath is the API path of root's story.
func storyPath(root model.Root) string {
	if root.Task != nil {
		return "/api/v1/stories/task/" + root.Task.Instance + "/" + strconv.FormatInt(root.Task.ID, 10)
	}
	return "/api/v1/stories/page/" + root.Page.Instance + "/" + strconv.FormatInt(root.Page.ID, 10) + "/" +
		root.Page.Locale
}

// TestEveryStoryThroughTheHandlers serves every fixture story: the body
// is its golden file, byte for byte but for indentation, and keeps the
// invariant the story exists for.
func TestEveryStoryThroughTheHandlers(t *testing.T) {
	fixtures := newFakeStore(t)
	h := New(Config{Store: fixtures})
	for _, f := range fixtures.stories {
		t.Run(f.name, func(t *testing.T) {
			rec := get(t, h, storyPath(f.story.Root))
			var story model.Story
			decode(t, rec, &story)
			var want bytes.Buffer
			if err := json.Compact(&want, f.golden); err != nil {
				t.Fatal(err)
			}
			if got := bytes.TrimSuffix(rec.Body.Bytes(), []byte("\n")); !bytes.Equal(got, want.Bytes()) {
				t.Errorf("body differs from %s:\n got %s\nwant %s", goldenFile, got, want.Bytes())
			}
			storytest.Check(t, f.name, story)
		})
	}
}

func TestStoryInstanceInAnyCase(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	var story model.Story
	decode(t, get(t, h, "/api/v1/stories/task/Vikunja.Example.COM/101"), &story)
	if story.Root.Task == nil || story.Root.Task.ID != 101 {
		t.Errorf("root = %+v, want task 101", story.Root)
	}
}

func TestStoryNotFound(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, path := range []string{
		"/api/v1/stories/task/vikunja.example.com/999",
		"/api/v1/stories/task/vikunja.example.org/101",
		"/api/v1/stories/page/wiki.example.com/57/fr",
		"/api/v1/coverage/task/vikunja.example.com/999",
	} {
		wantProblem(t, get(t, h, path), http.StatusNotFound)
	}
}

func TestStoryBadRequest(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, path := range []string{
		"/api/v1/stories/task/vikunja.example.com/abc",
		"/api/v1/stories/task/vikunja.example.com/0",
		"/api/v1/stories/task/vikunja.example.com/-1",
		"/api/v1/stories/task/vikunja.example.com/+101",
		"/api/v1/stories/task/vikunja.example.com/0101",
		"/api/v1/stories/task/vikunja%00.example.com/101",
		"/api/v1/stories/task/vikunja%ff.example.com/101",
		"/api/v1/stories/task/vikunja%20example.com/101",
		"/api/v1/stories/task/vi%E2%84%AAunja.example.com/101", // a Kelvin sign, k in lower case
		"/api/v1/stories/task/vikunja.example.com/99999999999999999999",
		"/api/v1/stories/task/" + strings.Repeat("a", maxInstance+1) + "/101",
		"/api/v1/stories/page/wiki.example.com/57/english",
		"/api/v1/stories/page/wiki.example.com/57/EN",
		"/api/v1/coverage/page/wiki.example.com/x/en",
	} {
		wantProblem(t, get(t, h, path), http.StatusBadRequest)
	}
}

func TestRootCoverage(t *testing.T) {
	fixtures := newFakeStore(t)
	h := New(Config{Store: fixtures})
	for _, f := range fixtures.stories {
		path := strings.Replace(storyPath(f.story.Root), "/stories/", "/coverage/", 1)
		var got CoverageResponse
		decode(t, get(t, h, path), &got)
		if !slices.EqualFunc(got.Coverage, f.story.Coverage, func(a, b model.Coverage) bool { return a.Key() == b.Key() }) {
			t.Errorf("%s: coverage = %+v, want the story's", f.name, got.Coverage)
		}
	}
}

// TestCoverageGapIsData serves the announced-only story, whose Kelos
// coverage has a gap, as a 200 with the gap.
func TestCoverageGapIsData(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	var got CoverageResponse
	decode(t, get(t, h, "/api/v1/coverage/task/vikunja.example.com/105"), &got)
	if !slices.ContainsFunc(got.Coverage, func(c model.Coverage) bool { return len(c.Gaps) > 0 }) {
		t.Errorf("coverage = %+v, want a gap", got.Coverage)
	}
}

func TestFreshness(t *testing.T) {
	fixtures := newFakeStore(t)
	h := New(Config{Store: fixtures})
	var got FreshnessResponse
	decode(t, get(t, h, "/api/v1/coverage"), &got)
	var keys []string
	for _, s := range got.Sources {
		keys = append(keys, string(s.Source)+"/"+s.Instance)
	}
	want := []string{"github/github.example.com", "kelos/cluster-a", "vikunja/vikunja.example.com", "wiki/wiki.example.com"}
	if !slices.Equal(keys, want) {
		t.Errorf("sources = %q, want %q", keys, want)
	}
	stored, err := fixtures.Freshness(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(got.Sources, stored, sameFreshness) {
		t.Errorf("sources = %+v, want the store's %+v", got.Sources, stored)
	}
	if !slices.ContainsFunc(got.Sources, func(s store.SourceFreshness) bool { return s.NewestRead != nil }) {
		t.Error("no source was read: the fixtures check no read times")
	}
}

// sameFreshness compares two freshness records, their reads by instant.
func sameFreshness(a, b store.SourceFreshness) bool {
	sameTime := func(x, y *time.Time) bool { return (x == nil) == (y == nil) && (x == nil || x.Equal(*y)) }
	return a.Source == b.Source && a.Instance == b.Instance && a.Roots == b.Roots && a.NeverRead == b.NeverRead &&
		sameTime(a.NewestRead, b.NewestRead) && sameTime(a.OldestRead, b.OldestRead)
}

func TestSearch(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	tests := []struct {
		path string
		want []string
	}{
		{"/api/v1/search?q=retry", []string{"failed-run-retry"}},
		{"/api/v1/search?q=%20RETRY%20", []string{"failed-run-retry"}},
		{"/api/v1/search?q=tasks/105", []string{"announced-run-only"}},
		{"/api/v1/search?q=nothing-matches", []string{}},
		{"/api/v1/search?limit=2", []string{"announced-run-only", "discuss-no-pr"}},
	}
	for _, tt := range tests {
		var got SearchResponse
		decode(t, get(t, h, tt.path), &got)
		titles := []string{}
		for _, r := range got.Roots {
			titles = append(titles, r.Title)
		}
		if !slices.Equal(titles, tt.want) {
			t.Errorf("%s: roots = %q, want %q", tt.path, titles, tt.want)
		}
	}
}

func TestSearchBadRequest(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	for _, path := range []string{
		"/api/v1/search?q=" + strings.Repeat("é", MaxSearchQuery+1),
		"/api/v1/search?limit=0",
		"/api/v1/search?limit=x",
		"/api/v1/search?limit=" + strconv.Itoa(store.MaxSearchLimit+1),
	} {
		wantProblem(t, get(t, h, path), http.StatusBadRequest)
	}
	var got SearchResponse
	decode(t, get(t, h, "/api/v1/search?q="+strings.Repeat("é", MaxSearchQuery)), &got)
}
