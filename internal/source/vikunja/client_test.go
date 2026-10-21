package vikunja

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// fixtureDir holds the recorded, fabricated API responses.
	fixtureDir = "../../../testdata/fixtures/vikunja"
	// testToken is a made-up token the fake server requires.
	testToken = "fabricated-token-0123456789"
	// testPerPage is the page size the fixtures are cut for.
	testPerPage = 2
)

// testObservedAt is the fake clock's time.
var testObservedAt = time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)

// fakeVikunja serves one story's recorded responses and records the
// comment pages asked for.
type fakeVikunja struct {
	t      *testing.T
	dir    string
	taskID int64

	mu    sync.Mutex
	pages []int
}

func (f *fakeVikunja) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.t.Errorf("%s %s: the client must only GET", r.Method, r.URL.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		http.Error(w, `{"message":"missing, invalid or expired token"}`, http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case taskPath(f.taskID):
		if got := r.URL.Query().Get("expand"); got != expandBuckets {
			f.t.Errorf("task read with expand=%q, want %q", got, expandBuckets)
		}
		f.serveFile(w, "task.json")
	case taskPath(f.taskID) + "/comments":
		f.serveCommentPage(w, r)
	default:
		f.t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
	}
}

func (f *fakeVikunja) serveCommentPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if got := q.Get("per_page"); got != strconv.Itoa(testPerPage) {
		f.t.Errorf("per_page = %q, want %d", got, testPerPage)
	}
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil {
		f.t.Errorf("comments read without a page: %s", r.URL)
	}
	f.mu.Lock()
	f.pages = append(f.pages, page)
	f.mu.Unlock()
	f.serveFile(w, "comments-"+strconv.Itoa(page)+".json")
}

func (f *fakeVikunja) serveFile(w http.ResponseWriter, name string) {
	body, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Errorf("no recorded response %s: %v", name, err)
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// newTestClient returns a client of srv with the test page size and clock.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient(Config{BaseURL: srv.URL, Token: testToken}, srv.Client())
	c.perPage = testPerPage
	c.now = func() time.Time { return testObservedAt }
	return c
}

// fetchStory reads task taskID of a recorded story and returns the snapshot
// and the comment pages the client asked for.
func fetchStory(t *testing.T, story string, taskID int64) (Snapshot, []int) {
	t.Helper()
	fake := &fakeVikunja{t: t, dir: filepath.Join(fixtureDir, story), taskID: taskID}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	snap, err := newTestClient(srv).Fetch(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return snap, fake.pages
}

func commentIDs(cs []Comment) []int64 {
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

func TestFetchPagination(t *testing.T) {
	tests := []struct {
		story     string
		taskID    int64
		wantPages []int
		wantIDs   []int64
	}{
		{"two-pages", 4201, []int{1, 2}, []int64{9001, 9002, 9005}},
		{"one-full-page", 4202, []int{1, 2}, []int64{9010, 9011}},
		{"no-comments", 4203, []int{1}, []int64{}},
	}
	for _, tt := range tests {
		t.Run(tt.story, func(t *testing.T) {
			snap, pages := fetchStory(t, tt.story, tt.taskID)
			if !slices.Equal(pages, tt.wantPages) {
				t.Errorf("pages read = %v, want %v", pages, tt.wantPages)
			}
			if got := commentIDs(snap.Comments); !slices.Equal(got, tt.wantIDs) {
				t.Errorf("comment ids = %v, want %v", got, tt.wantIDs)
			}
			if snap.Task.ID != tt.taskID {
				t.Errorf("task id = %d, want %d", snap.Task.ID, tt.taskID)
			}
			if !snap.ObservedAt.Equal(testObservedAt) {
				t.Errorf("ObservedAt = %v, want %v", snap.ObservedAt, testObservedAt)
			}
		})
	}
}

// A server whose per-page cap is below the requested size returns short
// pages; its total-pages header must keep the client reading.
func TestFetchHonoursTotalPagesHeader(t *testing.T) {
	const capped, totalPages, taskID = 1, 3, 4210
	var pages []int
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+taskPath(taskID), func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Task{ID: taskID})
	})
	mux.HandleFunc("GET "+taskPath(taskID)+"/comments", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		w.Header().Set(headerTotalPages, strconv.Itoa(totalPages))
		batch := make([]Comment, capped)
		for i := range batch {
			batch[i] = Comment{
				ID:      int64(page),
				Created: testObservedAt.Add(time.Duration(page) * time.Minute),
			}
		}
		_ = json.NewEncoder(w).Encode(batch)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	snap, err := newTestClient(srv).Fetch(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if want := []int{1, 2, 3}; !slices.Equal(pages, want) {
		t.Errorf("pages read = %v, want %v", pages, want)
	}
	if got, want := commentIDs(snap.Comments), []int64{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("comment ids = %v, want %v", got, want)
	}
}

// Comments come back oldest first even when the server sends them out of
// order.
func TestFetchSortsCommentsOldestFirst(t *testing.T) {
	const taskID = 4211
	t0 := testObservedAt.Add(-time.Hour)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+taskPath(taskID), func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Task{ID: taskID})
	})
	mux.HandleFunc("GET "+taskPath(taskID)+"/comments", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Comment{
			{ID: 3, Created: t0.Add(time.Minute)},
			{ID: 2, Created: t0},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	c.perPage = DefaultPerPage
	snap, err := c.Fetch(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := commentIDs(snap.Comments), []int64{2, 3}; !slices.Equal(got, want) {
		t.Errorf("comment ids = %v, want %v", got, want)
	}
}

func TestFetchErrorNamesStatusNotToken(t *testing.T) {
	fake := &fakeVikunja{t: t, dir: filepath.Join(fixtureDir, "no-comments"), taskID: 4203}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Token: "wrong-" + testToken}, srv.Client())
	_, err := c.Fetch(context.Background(), 4203)
	if err == nil {
		t.Fatal("Fetch with a rejected token succeeded")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") || !strings.Contains(msg, taskPath(4203)) {
		t.Errorf("error %q does not name the status and path", msg)
	}
	if strings.Contains(msg, testToken) {
		t.Errorf("error %q leaks the token", msg)
	}
}

func TestFetchTransportErrorOmitsURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	c := NewClient(Config{BaseURL: base, Token: testToken}, nil)
	_, err := c.Fetch(context.Background(), 4203)
	if err == nil {
		t.Fatal("Fetch from a closed server succeeded")
	}
	if strings.Contains(err.Error(), "?expand") {
		t.Errorf("error %q repeats the request URL", err)
	}
}
