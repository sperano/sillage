package vikunja

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// testTaskID is the task the hand-built servers below serve.
	testTaskID = 4210
)

// testObservedAt is the fake clock's time.
var testObservedAt = time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)

// pageLog records the comment pages a server was asked for.
type pageLog struct {
	mu    sync.Mutex
	pages []int
}

func (l *pageLog) add(page int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages = append(l.pages, page)
}

func (l *pageLog) seen() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.pages)
}

// fakeVikunja serves one story's recorded responses.
type fakeVikunja struct {
	t      *testing.T
	dir    string
	taskID int64
	log    pageLog
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
		f.serveFile(w, r, "task.json")
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
	f.log.add(page)
	f.serveFile(w, r, "comments-"+strconv.Itoa(page)+".json")
}

func (f *fakeVikunja) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	body, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Errorf("no recorded response %s: %v", name, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// newTestClient starts h and returns a client of it with the test token,
// page size and clock.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
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
	snap, err := newTestClient(t, fake).Fetch(t.Context(), taskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return snap, fake.log.seen()
}

// handcrafted serves task testTaskID and answers comment reads with
// comments(page, w).
func handcrafted(comments func(page int, w http.ResponseWriter)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+taskPath(testTaskID), func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Task{ID: testTaskID})
	})
	mux.HandleFunc("GET "+taskPath(testTaskID)+"/comments", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		comments(page, w)
	})
	return mux
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
	const totalPages = 3
	var log pageLog
	c := newTestClient(t, handcrafted(func(page int, w http.ResponseWriter) {
		log.add(page)
		w.Header().Set(headerTotalPages, strconv.Itoa(totalPages))
		created := testObservedAt.Add(time.Duration(page) * time.Minute)
		_ = json.NewEncoder(w).Encode([]Comment{{ID: int64(page), Created: created}})
	}))
	snap, err := c.Fetch(t.Context(), testTaskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := log.seen(), []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("pages read = %v, want %v", got, want)
	}
	if got, want := commentIDs(snap.Comments), []int64{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("comment ids = %v, want %v", got, want)
	}
}

// Comments come back oldest first and once each, even when the server
// sends them out of order or repeats one across pages.
func TestFetchSortsAndDedupesComments(t *testing.T) {
	t0 := testObservedAt.Add(-time.Hour)
	pages := map[int][]Comment{
		1: {{ID: 3, Created: t0.Add(time.Minute)}, {ID: 2, Created: t0}},
		2: {{ID: 3, Created: t0.Add(time.Minute)}},
	}
	c := newTestClient(t, handcrafted(func(page int, w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(pages[page])
	}))
	snap, err := c.Fetch(t.Context(), testTaskID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := commentIDs(snap.Comments), []int64{2, 3}; !slices.Equal(got, want) {
		t.Errorf("comment ids = %v, want %v", got, want)
	}
}

// A server that never returns a short page is cut off.
func TestFetchStopsAtMaxPages(t *testing.T) {
	const maxPages = 3
	var log pageLog
	c := newTestClient(t, handcrafted(func(page int, w http.ResponseWriter) {
		log.add(page)
		_ = json.NewEncoder(w).Encode(make([]Comment, testPerPage))
	}))
	c.maxPages = maxPages
	_, err := c.Fetch(t.Context(), testTaskID)
	if err == nil || !strings.Contains(err.Error(), "more than 3 comment pages") {
		t.Errorf("Fetch error = %v, want the page limit", err)
	}
	if got := len(log.seen()); got != maxPages {
		t.Errorf("read %d pages, want %d", got, maxPages)
	}
}

func TestFetchStatusError(t *testing.T) {
	fake := &fakeVikunja{t: t, dir: filepath.Join(fixtureDir, "no-comments"), taskID: 4203}
	c := newTestClient(t, fake)
	c.authorize = NewClient(Config{Token: "wrong-" + testToken}, nil).authorize
	_, err := c.Fetch(t.Context(), 4203)
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.Code != http.StatusUnauthorized || se.Path != taskPath(4203) {
		t.Fatalf("Fetch error = %v, want a 401 StatusError for %s", err, taskPath(4203))
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error %q leaks the token", err)
	}
}

func TestFetchRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"malformed JSON", `[{"id": `, "decoding"},
		{"oversize", `[` + strings.Repeat(" ", 2*testPerPage<<10) + `]`, "response larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, handcrafted(func(_ int, w http.ResponseWriter) {
				_, _ = w.Write([]byte(tt.body))
			}))
			c.maxBody = testPerPage << 10
			_, err := c.Fetch(t.Context(), testTaskID)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Fetch error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFetchRejectsOtherTask(t *testing.T) {
	c := newTestClient(t, handcrafted(nil))
	_, err := c.Fetch(t.Context(), testTaskID+1)
	if err == nil {
		t.Fatal("Fetch accepted a 404")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+taskPath(testTaskID), func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Task{ID: testTaskID + 1})
	})
	_, err = newTestClient(t, mux).Fetch(t.Context(), testTaskID)
	if err == nil || !strings.Contains(err.Error(), "answered with task") {
		t.Errorf("Fetch error = %v, want a task mismatch", err)
	}
}

func TestFetchTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := NewClient(Config{BaseURL: base, Token: testToken}, nil).Fetch(t.Context(), testTaskID)
	if err == nil || !strings.Contains(err.Error(), taskPath(testTaskID)) || strings.Contains(err.Error(), "?expand") {
		t.Errorf("Fetch error = %v, want the path and not the full URL", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = newTestClient(t, handcrafted(nil)).Fetch(ctx, testTaskID)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch error = %v, want context.Canceled", err)
	}
}

func TestClientNeverPrintsToken(t *testing.T) {
	c := NewClient(Config{BaseURL: "https://vikunja.example.com", Token: testToken}, nil)
	var logged strings.Builder
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("client", "c", c, "v", *c)
	for _, out := range []string{
		fmt.Sprintf("%v %+v %#v", c, *c, *c),
		logged.String(),
	} {
		if strings.Contains(out, testToken) {
			t.Errorf("token printed: %s", out)
		}
	}
}
