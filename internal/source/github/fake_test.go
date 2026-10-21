package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	// fixtureDir holds the recorded, fabricated API responses.
	fixtureDir = "../../../testdata/fixtures/github"
	// testToken is a made-up token the fake server requires.
	testToken = "fabricated-token-0123456789"
	// testPerPage is the page size the fixtures are cut for.
	testPerPage = 2
	// testInstance names the instance on every record the tests build.
	testInstance = "github.example.com"
	// testRepoPath is the API path of testRepo.
	testRepoPath = "/repos/example-org/widgets"
)

var (
	testRepo = Repository{Owner: "example-org", Name: "widgets"}
	// testObservedAt is the fake clock's reading.
	testObservedAt = time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
)

// requestLog records the requests a fake server answered, as path and
// page.
type requestLog struct {
	mu   sync.Mutex
	reqs []string
}

func (l *requestLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := r.URL.Path
	if page := r.URL.Query().Get("page"); page != "" {
		entry += "?page=" + page
	}
	l.reqs = append(l.reqs, entry)
}

func (l *requestLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.reqs)
}

// fakeGitHub serves one story's recorded responses. A list's page n is
// the file <name>-<n>.json; a Link header names the next page when its
// file exists.
type fakeGitHub struct {
	t   *testing.T
	dir string
	log requestLog
	mux *http.ServeMux
}

func newFakeGitHub(t *testing.T, story string) *fakeGitHub {
	f := &fakeGitHub{t: t, dir: filepath.Join(fixtureDir, story), mux: http.NewServeMux()}
	f.mux.HandleFunc("GET "+testRepoPath+"/pulls", func(w http.ResponseWriter, r *http.Request) {
		f.serveList(w, r, "pulls")
	})
	f.mux.HandleFunc("GET "+testRepoPath+"/pulls/{n}", func(w http.ResponseWriter, r *http.Request) {
		f.serveFile(w, r, "pull-"+r.PathValue("n")+".json")
	})
	f.mux.HandleFunc("GET "+testRepoPath+"/pulls/{n}/reviews", func(w http.ResponseWriter, r *http.Request) {
		f.serveList(w, r, "reviews-"+r.PathValue("n"))
	})
	f.mux.HandleFunc("GET "+testRepoPath+"/issues/{n}/timeline", func(w http.ResponseWriter, r *http.Request) {
		f.serveList(w, r, "timeline-"+r.PathValue("n"))
	})
	f.mux.HandleFunc("GET "+testRepoPath+"/issues/{n}/comments", func(w http.ResponseWriter, r *http.Request) {
		f.serveList(w, r, "comments-"+r.PathValue("n"))
	})
	f.mux.HandleFunc("GET "+testRepoPath+"/commits/{sha}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter"); got != filterAll {
			f.t.Errorf("check runs read with filter=%q, want %q", got, filterAll)
		}
		f.serveList(w, r, "check-runs-"+r.PathValue("sha"))
	})
	return f
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.t.Errorf("%s %s: the client must only GET", r.Method, r.URL.Path)
	}
	for header, want := range map[string]string{
		"Authorization":  "Bearer " + testToken,
		"Accept":         mediaType,
		headerAPIVersion: apiVersion,
		"User-Agent":     userAgent,
	} {
		if got := r.Header.Get(header); got != want {
			if header == "Authorization" {
				http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
				return
			}
			f.t.Errorf("%s: header %s = %q, want %q", r.URL.Path, header, got, want)
		}
	}
	f.log.add(r)
	f.mux.ServeHTTP(w, r)
}

func (f *fakeGitHub) serveList(w http.ResponseWriter, r *http.Request, name string) {
	q := r.URL.Query()
	if got := q.Get("per_page"); got != strconv.Itoa(testPerPage) {
		f.t.Errorf("%s: per_page = %q, want %d", r.URL.Path, got, testPerPage)
	}
	page := 1
	if p := q.Get("page"); p != "" {
		var err error
		if page, err = strconv.Atoi(p); err != nil {
			f.t.Errorf("%s: page %q", r.URL.Path, p)
		}
	}
	next := name + "-" + strconv.Itoa(page+1) + ".json"
	if _, err := os.Stat(filepath.Join(f.dir, next)); err == nil {
		q.Set("page", strconv.Itoa(page+1))
		u := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}
		w.Header().Set(headerLink, "<"+u.String()+`>; rel="next", <`+u.String()+`>; rel="last"`)
	}
	f.serveFile(w, r, name+"-"+strconv.Itoa(page)+".json")
}

func (f *fakeGitHub) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	body, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Errorf("no recorded response %s for %s: %v", name, r.URL, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// newTestClient starts h and returns a client of it with the test token,
// instance, page size and clock, and a sleep that fails the test: only the
// rate-limit tests replace it.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{APIURL: srv.URL, Instance: testInstance, Token: testToken}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.perPage = testPerPage
	c.now = func() time.Time { return testObservedAt }
	c.sleep = func(_ context.Context, d time.Duration) error {
		t.Errorf("unexpected sleep of %s", d)
		return nil
	}
	return c
}
