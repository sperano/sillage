package wiki

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFetchOperations(t *testing.T) {
	tests := []struct {
		story string
		want  []string
	}{
		{"live-edit-not-in-trail", []string{
			opPage, opHistory + " 0", opVersion + " 121", opComments + " en widgets/design/storage-layout",
		}},
		{"two-agents", []string{
			opPage, opHistory + " 0", opHistory + " 1", opHistory + " 2", opVersion + " 205",
			opComments + " en widgets/runbooks/release",
		}},
		{"announcement-no-reply", []string{
			opPage, opPageList, opHistory + " 0", opVersion + " 300", opComments + " en widgets/notes/capacity",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.story, func(t *testing.T) {
			snap, ops := fetchStory(t, tt.story)
			if !slices.Equal(ops, tt.want) {
				t.Errorf("operations = %v, want %v", ops, tt.want)
			}
			if snap.Instance != testInstance || !snap.ObservedAt.Equal(testObservedAt) {
				t.Errorf("instance %q, observed %v", snap.Instance, snap.ObservedAt)
			}
		})
	}
}

func TestFetchTwoAgents(t *testing.T) {
	snap, _ := fetchStory(t, "two-agents")
	var ids []int64
	for _, v := range snap.Trail {
		ids = append(ids, v.VersionID)
	}
	if want := []int64{200, 201, 202, 203, 204, 205}; !slices.Equal(ids, want) {
		t.Errorf("trail = %v, want %v, oldest first", ids, want)
	}
	if snap.Restricted || snap.Page.AuthorID != 21 || snap.Page.CreatorID != 7 {
		t.Errorf("page = %+v, restricted %v", snap.Page, snap.Restricted)
	}
	if snap.NewestPath != "widgets/runbooks/release" || len(snap.Comments) != 6 {
		t.Errorf("newest path %q, %d comments", snap.NewestPath, len(snap.Comments))
	}
}

func TestFetchRestricted(t *testing.T) {
	snap, _ := fetchStory(t, "announcement-no-reply")
	if !snap.Restricted || snap.Page.ID != 59 || snap.Page.AuthorID != 0 || snap.Page.Path != "widgets/notes/capacity" {
		t.Errorf("page = %+v, restricted %v", snap.Page, snap.Restricted)
	}
}

// encode writes v as a GraphQL reply.
func encode(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handServer answers opPage with page, opHistory with history(offsetPage),
// opVersion with path, and opComments with no comment.
func handServer(page any, history func(offsetPage int) any, path string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.OperationName {
		case opPage:
			encode(w, map[string]any{"data": map[string]any{"pages": map[string]any{"single": page}}})
		case opHistory:
			encode(w, map[string]any{"data": map[string]any{"pages": map[string]any{"history": history(req.Variables.OffsetPage)}}})
		case opVersion:
			encode(w, map[string]any{"data": map[string]any{"pages": map[string]any{"version": map[string]any{"path": path}}}})
		case opComments:
			encode(w, map[string]any{"data": map[string]any{"comments": map[string]any{"list": []any{}}}})
		}
	})
}

func trailPage(total int, versions ...Version) any {
	return map[string]any{"trail": versions, "total": total}
}

// A save between two trail pages shifts the older versions: the one that
// repeats is kept once, and an empty page ends the trail.
func TestFetchTrailShiftedBySave(t *testing.T) {
	v := func(id int64) Version { return handVersion(id, t0.Add(time.Duration(id)*time.Minute), testOpus) }
	pages := map[int]any{0: trailPage(3, v(3), v(2)), 1: trailPage(4, v(2), v(1)), 2: trailPage(4)}
	page := handPage(t0.Add(time.Hour), testOpus)
	c := newTestClient(t, handServer(page, func(p int) any { return pages[p] }, page.Path))
	snap, err := c.Fetch(t.Context(), 60)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var ids []int64
	for _, version := range snap.Trail {
		ids = append(ids, version.VersionID)
	}
	if want := []int64{1, 2, 3}; !slices.Equal(ids, want) {
		t.Errorf("trail = %v, want %v", ids, want)
	}
}

func TestFetchFailures(t *testing.T) {
	page := handPage(t0.Add(time.Hour), testOpus)
	tests := []struct {
		name    string
		pageID  int64
		history func(int) any
		// noPath makes the version reply name no path.
		noPath bool
		want   string
	}{
		{"another page", 61, nil, false, "no page 61"},
		{"no history", 60, func(int) any { return nil }, false, "no history"},
		// A server that ignores offsetPage, or a page shifted by as many
		// saves: the trail would be cut short.
		{"repeating trail", 60, func(int) any {
			return trailPage(9, handVersion(1, t0, testOwner), handVersion(2, t0, testOwner))
		}, false, "history page 1 brings no new version (2 of 9 read)"},
		{"endless trail", 60, func(p int) any {
			return trailPage(9, handVersion(int64(2*p+1), t0, testOwner), handVersion(int64(2*p+2), t0, testOwner))
		}, false, "more than 3 history pages"},
		{"no version path", 60, func(int) any { return trailPage(1, handVersion(1, t0, testOwner)) }, true,
			"no path for version 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := page.Path
			if tt.noPath {
				path = ""
			}
			c := newTestClient(t, handServer(page, tt.history, path))
			c.maxHistoryPages = 3
			_, err := c.Fetch(t.Context(), tt.pageID)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Fetch error = %v, want %q", err, tt.want)
			}
		})
	}
}

// A token refused pages.single falls back to pages.list, which must list
// the page.
func TestFetchRestrictedPageNotListed(t *testing.T) {
	c := newTestClient(t, graphQLHandler(func(op string, w http.ResponseWriter) {
		switch op {
		case opPage:
			encode(w, map[string]any{"errors": []any{map[string]any{
				"message":    "You are not authorized to view this page.",
				"extensions": map[string]any{"exception": map[string]any{"code": codePageViewForbidden}},
			}}})
		case opPageList:
			encode(w, map[string]any{"data": map[string]any{"pages": map[string]any{"list": []Page{{ID: 12}}}}})
		}
	}))
	_, err := c.Fetch(t.Context(), 60)
	if err == nil || !strings.Contains(err.Error(), "page 60 is not listed") {
		t.Errorf("Fetch error = %v, want the page not listed", err)
	}
}

func TestPages(t *testing.T) {
	fake := storyFake(t, "announcement-no-reply")
	pages, err := newTestClient(t, fake).Pages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pages {
		got = append(got, strconv.FormatInt(p.ID, 10)+" "+p.Locale+"/"+p.Path)
	}
	if want := []string{"12 en/widgets/home", "59 en/widgets/notes/capacity"}; !slices.Equal(got, want) {
		t.Errorf("Pages() = %v, want %v", got, want)
	}
	if ops := fake.log.seen(); !slices.Equal(ops, []string{opPageList}) {
		t.Errorf("Pages() sent %v, want only %s", ops, opPageList)
	}
}

func TestPagesReportsFailure(t *testing.T) {
	c := newTestClient(t, graphQLHandler(func(_ string, w http.ResponseWriter) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	if _, err := c.Pages(t.Context()); err == nil {
		t.Error("Pages() error = nil, want the reply's status")
	}
}
