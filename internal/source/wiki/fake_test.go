package wiki

import (
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
	// fixtureDir holds the recorded, fabricated GraphQL replies.
	fixtureDir = "../../../testdata/fixtures/wiki"
	// testToken is a made-up token the fake server requires.
	testToken = "fabricated-token-0123456789"
	// testHistoryPageSize is the trail page size the fixtures are cut for.
	testHistoryPageSize = 2
	// testInstance names the instance on every record the tests build.
	testInstance = "wiki.example.com"
)

var (
	// testAgents are the fixtures' agent users: agent-opus and agent-sol.
	testAgents = []int64{21, 22}
	// testObservedAt is the fake clock's reading.
	testObservedAt = time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
)

// gqlRequest is a request as the fake server decodes it.
type gqlRequest struct {
	OperationName string `json:"operationName"`
	Query         string `json:"query"`
	Variables     struct {
		ID         int64  `json:"id"`
		PageID     int64  `json:"pageId"`
		VersionID  int64  `json:"versionId"`
		OffsetPage int    `json:"offsetPage"`
		OffsetSize int    `json:"offsetSize"`
		Locale     string `json:"locale"`
		Path       string `json:"path"`
	} `json:"variables"`
}

// opLog records the operations a fake server answered, with the history
// page or the version for those that have one.
type opLog struct {
	mu  sync.Mutex
	ops []string
}

func (l *opLog) add(op string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = append(l.ops, op)
}

func (l *opLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ops)
}

// fakeWiki serves one story's recorded replies: page.json, pages.json,
// history-<offsetPage>.json, version-<versionId>.json and comments.json.
// It fails a request for another page than pageID.
type fakeWiki struct {
	t      *testing.T
	dir    string
	pageID int64
	log    opLog
}

func (f *fakeWiki) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := f.check(w, r)
	if !ok {
		return
	}
	f.checkPageID(req)
	switch req.OperationName {
	case opPage:
		f.log.add(opPage)
		f.serveFile(w, r, "page.json")
	case opPageList:
		f.log.add(opPageList)
		f.serveFile(w, r, "pages.json")
	case opHistory:
		if req.Variables.OffsetSize != testHistoryPageSize {
			f.t.Errorf("history read with offsetSize %d, want %d", req.Variables.OffsetSize, testHistoryPageSize)
		}
		page := strconv.Itoa(req.Variables.OffsetPage)
		f.log.add(opHistory + " " + page)
		f.serveFile(w, r, "history-"+page+".json")
	case opVersion:
		version := strconv.FormatInt(req.Variables.VersionID, 10)
		f.log.add(opVersion + " " + version)
		f.serveFile(w, r, "version-"+version+".json")
	case opComments:
		f.log.add(opComments + " " + req.Variables.Locale + " " + req.Variables.Path)
		f.serveFile(w, r, "comments.json")
	default:
		f.t.Errorf("unexpected operation %q", req.OperationName)
		http.NotFound(w, r)
	}
}

// check enforces what every request must be: an authorized POST of a
// JSON GraphQL query, never a mutation.
func (f *fakeWiki) check(w http.ResponseWriter, r *http.Request) (gqlRequest, bool) {
	var req gqlRequest
	if r.Method != http.MethodPost || r.URL.Path != graphQLPath {
		f.t.Errorf("%s %s: want POST %s", r.Method, r.URL.Path, graphQLPath)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		http.Error(w, `{"errors":[{"message":"Unauthorized"}]}`, http.StatusUnauthorized)
		return req, false
	}
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		f.t.Errorf("Content-Type = %q", got)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("decoding request: %v", err)
		return req, false
	}
	if !strings.HasPrefix(req.Query, "query "+req.OperationName+"(") &&
		!strings.HasPrefix(req.Query, "query "+req.OperationName+" ") {
		f.t.Errorf("operation %s is not a query of that name: %s", req.OperationName, req.Query)
	}
	return req, true
}

// checkPageID fails a request naming another page than the story's.
func (f *fakeWiki) checkPageID(req gqlRequest) {
	var got int64
	switch req.OperationName {
	case opPage, opHistory:
		got = req.Variables.ID
	case opVersion:
		got = req.Variables.PageID
	default:
		return
	}
	if got != f.pageID {
		f.t.Errorf("%s asks for page %d, want %d", req.OperationName, got, f.pageID)
	}
}

func (f *fakeWiki) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	body, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Errorf("no recorded reply %s: %v", name, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// testConfig is a valid Config of the instance at baseURL.
func testConfig(baseURL string) Config {
	return Config{BaseURL: baseURL, Instance: testInstance, Token: testToken, AgentUsers: testAgents}
}

// newTestClient starts h and returns a client of it with the test token,
// instance, history page size and clock.
func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(testConfig(srv.URL), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.historyPageSize = testHistoryPageSize
	c.now = func() time.Time { return testObservedAt }
	return c
}

// storyFake serves a recorded story, read by the page storyPages names.
func storyFake(t *testing.T, story string) *fakeWiki {
	t.Helper()
	pageID, ok := storyPages[story]
	if !ok {
		t.Fatalf("story %s: add the page it is read by to storyPages", story)
	}
	return &fakeWiki{t: t, dir: filepath.Join(fixtureDir, story), pageID: pageID}
}

// fetchStory reads the page of a recorded story and returns the snapshot
// and the operations the client sent.
func fetchStory(t *testing.T, story string) (Snapshot, []string) {
	t.Helper()
	fake := storyFake(t, story)
	snap, err := newTestClient(t, fake).Fetch(t.Context(), fake.pageID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return snap, fake.log.seen()
}

// storyPages maps each story to the page it is read by.
var storyPages = map[string]int64{
	"live-edit-not-in-trail": 57,
	"two-agents":             58,
	"announcement-no-reply":  59,
}

// graphQLHandler answers every request with reply, whatever it asks.
func graphQLHandler(reply func(op string, w http.ResponseWriter)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		reply(req.OperationName, w)
	})
}
