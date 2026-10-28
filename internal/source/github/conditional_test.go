package github

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// etagBranch is the branch etagServer's one pull request is the head of.
	etagBranch = "kelos/vikunja-7"
	// etagNumber is that pull request's number.
	etagNumber = 5
	// etagLimit is the rate limit etagServer reports.
	etagLimit = 5000
	// etagHashLen is how much of a body's hash an ETag holds.
	etagHashLen = 16
)

// etagHead is the head commit of etagServer's pull request.
var etagHead = strings.Repeat("e", 40)

// etagReset is when etagServer's rate-limit window resets.
var etagReset = time.Unix(1775400000, 0).UTC()

// etagServer serves one branch with one pull request, each list in one
// page with an ETag of its body, and answers 304 Not Modified to a
// request whose If-None-Match is that ETag, as GitHub does.
type etagServer struct {
	t      *testing.T
	mu     sync.Mutex
	bodies map[string]string
	// status, when set for a path, answers it instead of its body.
	status map[string]int
	// paged lists have a next page; noETag lists come without an ETag.
	paged, noETag map[string]bool
	// seen are the paths requested, in order, and remaining the budget.
	seen      []string
	remaining int
}

func newETagServer(t *testing.T) *etagServer {
	pr := `[{"id":7005,"number":5,"state":"open","created_at":"2026-04-05T09:00:00Z",` +
		`"user":{"login":"owner-gh","type":"User"},` +
		`"head":{"ref":"` + etagBranch + `","sha":"` + etagHead + `","repo":{"full_name":"example-org/widgets"}},` +
		`"base":{"ref":"main","sha":"` + strings.Repeat("0", 40) + `","repo":{"full_name":"example-org/widgets"}}}]`
	n := strconv.Itoa(etagNumber)
	return &etagServer{
		t: t, remaining: etagLimit,
		bodies: map[string]string{
			testRepoPath + "/pulls":                               pr,
			testRepoPath + "/issues/" + n + "/timeline":           `[]`,
			testRepoPath + "/issues/" + n + "/comments":           `[]`,
			testRepoPath + "/pulls/" + n + "/comments":            `[]`,
			testRepoPath + "/pulls/" + n + "/reviews":             `[]`,
			testRepoPath + "/commits/" + etagHead + "/check-runs": `{"check_runs":[]}`,
		},
		status: make(map[string]int), paged: make(map[string]bool), noETag: make(map[string]bool),
	}
}

func (s *etagServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, r.URL.Path)
	body, ok := s.bodies[r.URL.Path]
	if !ok {
		s.t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256([]byte(body))
	etag := `W/"` + hex.EncodeToString(sum[:])[:etagHashLen] + `"`
	notModified := r.Header.Get(headerIfNoneMatch) == etag
	if !notModified {
		s.remaining--
	}
	w.Header().Set(headerRateLimit, strconv.Itoa(etagLimit))
	w.Header().Set(headerRateRemaining, strconv.Itoa(s.remaining))
	w.Header().Set(headerRateReset, strconv.FormatInt(etagReset.Unix(), 10))
	if code := s.status[r.URL.Path]; code != 0 {
		http.Error(w, `{"message":"fabricated"}`, code)
		return
	}
	if !s.noETag[r.URL.Path] {
		w.Header().Set(headerETag, etag)
	}
	if s.paged[r.URL.Path] && r.URL.Query().Get("page") == "" {
		q := r.URL.Query()
		q.Set("page", "2")
		u := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}
		w.Header().Set(headerLink, "<"+u.String()+`>; rel="next"`)
	}
	if notModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write([]byte(body))
}

// set changes the body of path, and so its ETag.
func (s *etagServer) set(path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[path] = body
}

func (s *etagServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.seen)
}

func fetchValidated(t *testing.T, c *Client) Validators {
	t.Helper()
	snaps, v, err := c.FetchBranchValidated(t.Context(), testRepo, Branch{Repository: testRepo, Name: etagBranch})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("read %d pull requests, want 1", len(snaps))
	}
	return v
}

func TestFetchBranchValidatedKeepsTheListsETags(t *testing.T) {
	srv := newETagServer(t)
	v := fetchValidated(t, newTestClient(t, srv))
	paths := make([]string, 0, len(v))
	for target := range v {
		path, _, _ := strings.Cut(target, "?")
		paths = append(paths, path)
	}
	slices.Sort(paths)
	want := []string{
		testRepoPath + "/commits/" + etagHead + "/check-runs",
		testRepoPath + "/issues/5/timeline",
		testRepoPath + "/pulls",
	}
	if !slices.Equal(paths, want) {
		t.Errorf("validated lists = %v, want the pulls list, the timeline and the head's check runs %v", paths, want)
	}
	for target, etag := range v {
		if !strings.HasPrefix(target, "/repos/") || !strings.Contains(target, "per_page=") || etag == "" {
			t.Errorf("validator %q = %q: want a target under the API with its query, and an ETag", target, etag)
		}
	}
}

func TestUnchangedAnswersFromNotModified(t *testing.T) {
	srv := newETagServer(t)
	c := newTestClient(t, srv)
	v := fetchValidated(t, c)
	before := c.Usage()
	unchanged, err := c.Unchanged(t.Context(), v)
	if err != nil || !unchanged {
		t.Fatalf("Unchanged = %v, %v; want true: every list answered 304", unchanged, err)
	}
	after := c.Usage()
	if got := after.Requests - before.Requests; got != int64(len(v)) {
		t.Errorf("requests sent = %d, want one per validator (%d)", got, len(v))
	}
	if got := after.NotModified - before.NotModified; got != int64(len(v)) {
		t.Errorf("304s = %d, want %d", got, len(v))
	}
	if after.Budget.Remaining != before.Budget.Remaining {
		t.Errorf("remaining budget %d, then %d: a 304 must not cost any", before.Budget.Remaining,
			after.Budget.Remaining)
	}
}

// The lists are compared in target order (commits, issues, pulls): a
// changed timeline stops the check before the pulls list.
func TestUnchangedStopsAtTheFirstChange(t *testing.T) {
	srv := newETagServer(t)
	c := newTestClient(t, srv)
	v := fetchValidated(t, c)
	srv.set(testRepoPath+"/issues/5/timeline", `[{"id":1,"event":"labeled","created_at":"2026-04-05T10:00:00Z"}]`)
	from := len(srv.requests())
	unchanged, err := c.Unchanged(t.Context(), v)
	if err != nil || unchanged {
		t.Fatalf("Unchanged = %v, %v; want false: the timeline changed", unchanged, err)
	}
	want := []string{testRepoPath + "/commits/" + etagHead + "/check-runs", testRepoPath + "/issues/5/timeline"}
	if got := srv.requests()[from:]; !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v and nothing after the change", got, want)
	}
}

// A list the validators cannot tell unchanged makes them none: the
// backstop then reads the branch whole.
func TestFetchBranchValidatedWithoutAComparableList(t *testing.T) {
	checks := testRepoPath + "/commits/" + etagHead + "/check-runs"
	for name, spoil := range map[string]func(*etagServer){
		"timeline in two pages": func(s *etagServer) {
			s.paged[testRepoPath+"/issues/5/timeline"] = true
			s.bodies[testRepoPath+"/issues/5/timeline"] = `[]`
		},
		"pulls list without an ETag": func(s *etagServer) { s.noETag[testRepoPath+"/pulls"] = true },
		"head checks refused":        func(s *etagServer) { s.status[checks] = http.StatusForbidden },
	} {
		t.Run(name, func(t *testing.T) {
			srv := newETagServer(t)
			spoil(srv)
			if v := fetchValidated(t, newTestClient(t, srv)); v != nil {
				t.Errorf("validators = %v, want none", v)
			}
		})
	}
}

func TestUsageReportsTheBudget(t *testing.T) {
	srv := newETagServer(t)
	c := newTestClient(t, srv)
	if b := c.Usage().Budget; b.Limit != 0 {
		t.Errorf("budget before any response = %+v, want unknown", b)
	}
	fetchValidated(t, c)
	u := c.Usage()
	sent := len(srv.requests())
	if u.Requests != int64(sent) || u.NotModified != 0 {
		t.Errorf("usage = %+v, want %d requests and no 304", u, sent)
	}
	want := Budget{Limit: etagLimit, Remaining: etagLimit - sent, Reset: etagReset}
	if u.Budget != want {
		t.Errorf("budget = %+v, want %+v", u.Budget, want)
	}
}

func TestUnchangedFailsOnRefusals(t *testing.T) {
	v := Validators{testRepoPath + "/pulls?per_page=2": `W/"stale"`}
	for name, tc := range map[string]struct {
		header http.Header
		check  func(error) bool
	}{
		"rate limited": {
			header: http.Header{headerRateRemaining: {rateLimitExhausted}, headerRateReset: {"1775400000"}},
			check:  func(err error) bool { _, ok := errors.AsType[*RateLimitError](err); return ok },
		},
		"forbidden": {
			header: http.Header{},
			check: func(err error) bool {
				se, ok := errors.AsType[*StatusError](err)
				return ok && se.Code == http.StatusForbidden
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, vs := range tc.header {
					w.Header().Set(k, vs[0])
				}
				http.Error(w, `{"message":"fabricated"}`, http.StatusForbidden)
			}))
			if unchanged, err := c.Unchanged(t.Context(), v); unchanged || !tc.check(err) {
				t.Errorf("Unchanged = %v, %v", unchanged, err)
			}
		})
	}
}
