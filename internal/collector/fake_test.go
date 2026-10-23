package collector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

const (
	// fixturesDir holds every source's fabricated responses.
	fixturesDir = "../../testdata/fixtures"
	// The fabricated instances the fixtures name.
	testVikunjaHost = "vikunja.example.com"
	testGitHubHost  = "github.example.com"
	testWikiHost    = "wiki.example.com"
	testCluster     = "cluster-a"
	testNamespace   = "agents"
	testWorkspace   = "widgets"
	testAgent       = "agent-opus"
	// testServiceAccount posts the fixtures' Vikunja start announcements.
	// It is configured, not vikunja.DefaultServiceAccount, so that the
	// fixtures do not depend on the source's default.
	testServiceAccount = "kelos"
	// testGitHubAPIPath is where the fake serves GitHub's REST API, as
	// GitHub Enterprise Server does.
	testGitHubAPIPath = "/api/v3"
	// testRepoPath is the fixtures' repository's API path.
	testRepoPath = testGitHubAPIPath + "/repos/example-org/widgets"
	// testToken is a made-up token every client sends.
	testToken = "fabricated-token-0123456789"
	// testTimeout bounds every request of a test.
	testTimeout = 10 * time.Second
	// emptyList and emptyCheckRuns answer a list a story has no file for.
	emptyList      = "[]"
	emptyCheckRuns = `{"total_count":0,"check_runs":[]}`
)

var (
	testRepo     = github.Repository{Owner: "example-org", Name: "widgets"}
	testWikiUser = int64(21)
	// testNow is the collector's clock: after every fixture.
	testNow = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
)

// storyDirs names, under fixturesDir, the responses of each source a
// story reads; an empty one is a source the story must not read.
type storyDirs struct {
	vikunja, kelos, github, wiki string
}

func testConfig() Config {
	return Config{
		VikunjaInstance:       testVikunjaHost,
		VikunjaServiceAccount: testServiceAccount,
		VikunjaAgentUsers:     []string{testAgent},
		Kelos: kelos.Config{
			Cluster: testCluster, Namespace: testNamespace, LabelDomain: testVikunjaHost, VikunjaInstance: testVikunjaHost,
		},
		GitHubInstance: testGitHubHost,
		WikiInstance:   testWikiHost,
		WikiAgentUsers: []int64{testWikiUser},
		Workspaces:     map[string]github.Repository{testWorkspace: testRepo},
		KelosTaskTTL:   DefaultKelosTaskTTL,
	}
}

// newStoryCollector returns a collector of the clients of every source,
// each answered from dirs.
func newStoryCollector(t *testing.T, dirs storyDirs) *Collector {
	t.Helper()
	hc := routedClient(t, newFakeSources(t, dirs))
	gh, err := github.NewClient(github.Config{
		APIURL: "http://" + testGitHubHost + testGitHubAPIPath, Instance: testGitHubHost, Token: testToken,
	}, hc)
	if err != nil {
		t.Fatal(err)
	}
	wk, err := wiki.NewClient(wiki.Config{
		BaseURL: "http://" + testWikiHost, Instance: testWikiHost, Token: testToken, AgentUsers: []int64{testWikiUser},
	}, hc)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := kelos.NewReader(fakeKelos(t, dirs.kelos), testConfig().Kelos)
	if err != nil {
		t.Fatal(err)
	}
	vk := vikunja.NewClient(vikunja.Config{
		BaseURL: "http://" + testVikunjaHost, Token: testToken, ServiceAccount: testServiceAccount,
	}, hc)
	c, err := New(testConfig(), Sources{Vikunja: vk, Kelos: reader, GitHub: gh, Wiki: wk})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	return c
}

// routedClient sends every request to h's server, whatever its host.
func routedClient(t *testing.T, h http.Handler) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	var dialer net.Dialer
	return &http.Client{Timeout: testTimeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

// fakeSources answers each host from its story directory.
type fakeSources struct {
	t    *testing.T
	dirs storyDirs
	mux  *http.ServeMux
	// wikiPage is the page the story's wiki responses hold; 0 without.
	wikiPage int64
}

func newFakeSources(t *testing.T, dirs storyDirs) *fakeSources {
	t.Helper()
	f := &fakeSources{t: t, dirs: dirs, mux: http.NewServeMux()}
	if dirs.wiki != "" {
		f.wikiPage = wikiPageID(t, dirs.wiki)
	}
	vk := func(name func(*http.Request) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { f.serve(w, r, f.dirs.vikunja, name(r), emptyList) }
	}
	f.mux.Handle("GET "+testVikunjaHost+"/api/v1/tasks/{id}", vk(func(*http.Request) string { return "task.json" }))
	f.mux.Handle("GET "+testVikunjaHost+"/api/v1/tasks/{id}/comments", vk(func(r *http.Request) string {
		return "comments-" + r.URL.Query().Get("page") + ".json"
	}))
	gh := func(prefix, empty string, key func(*http.Request) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.serve(w, r, f.dirs.github, prefix+key(r)+".json", empty)
		}
	}
	number := func(r *http.Request) string { return "-" + r.PathValue("n") }
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/pulls", gh("pulls", "", func(*http.Request) string { return "" }))
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/issues/{n}/timeline", gh("timeline", emptyList, number))
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/issues/{n}/comments", gh("comments", emptyList, number))
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/pulls/{n}/comments", gh("review-comments", emptyList, number))
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/pulls/{n}/reviews", gh("reviews", emptyList, number))
	f.mux.Handle("GET "+testGitHubHost+testRepoPath+"/commits/{sha}/check-runs", gh("check-runs", emptyCheckRuns,
		func(r *http.Request) string { return "-" + r.PathValue("sha") }))
	f.mux.HandleFunc("POST "+testWikiHost+"/graphql", f.serveWiki)
	return f
}

func (f *fakeSources) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		f.t.Errorf("%s %s: Authorization %q", r.Method, r.URL, got)
	}
	if _, pattern := f.mux.Handler(r); pattern == "" {
		f.t.Errorf("unexpected request %s %s%s", r.Method, r.Host, r.URL)
	}
	f.mux.ServeHTTP(w, r)
}

// serve answers with dir's file name, or with empty when the story has
// none and empty is set. A request of a source the story must not read
// fails the test.
func (f *fakeSources) serve(w http.ResponseWriter, r *http.Request, dir, name, empty string) {
	if dir == "" {
		f.t.Errorf("%s %s%s: the story reads nothing from %s", r.Method, r.Host, r.URL, r.Host)
		http.NotFound(w, r)
		return
	}
	body, err := os.ReadFile(filepath.Join(fixturesDir, dir, name))
	switch {
	case err == nil:
	case os.IsNotExist(err) && empty != "":
		body = []byte(empty)
	default:
		f.t.Errorf("%s %s%s: no response %s/%s: %v", r.Method, r.Host, r.URL, dir, name, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// serveWiki answers a GraphQL query from its operation's file. A query
// of a page other than the story's fails the test.
func (f *fakeSources) serveWiki(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationName string `json:"operationName"`
		Query         string `json:"query"`
		Variables     struct {
			ID         int64 `json:"id"`
			PageID     int64 `json:"pageId"`
			OffsetPage int   `json:"offsetPage"`
			VersionID  int64 `json:"versionId"`
		} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.HasPrefix(req.Query, "mutation") {
		f.t.Errorf("wiki request %+v: %v", req, err)
	}
	asked := map[string]int64{
		"SillagePage": req.Variables.ID, "SillageHistory": req.Variables.ID, "SillageVersion": req.Variables.PageID,
	}
	if id, ok := asked[req.OperationName]; ok && id != f.wikiPage {
		f.t.Errorf("wiki %s of page %d, want page %d", req.OperationName, id, f.wikiPage)
	}
	files := map[string]string{
		"SillagePage":     "page.json",
		"SillagePageList": "pages.json",
		"SillageHistory":  "history-" + strconv.Itoa(req.Variables.OffsetPage) + ".json",
		"SillageVersion":  "version-" + strconv.FormatInt(req.Variables.VersionID, 10) + ".json",
		"SillageComments": "comments.json",
	}
	f.serve(w, r, f.dirs.wiki, files[req.OperationName], "")
}

// wikiPageID is the id of the page dir's wiki responses hold.
func wikiPageID(t *testing.T, dir string) int64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, dir, "page.json"))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data struct {
			Pages struct {
				Single struct {
					ID int64 `json:"id"`
				} `json:"single"`
			} `json:"pages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("%s/page.json: %v", dir, err)
	}
	return page.Data.Pages.Single.ID
}

// Kelos's resources, as the fake dynamic client lists them.
var (
	tasksGVR   = schema.GroupVersionResource{Group: "kelos.dev", Version: "v1alpha2", Resource: "tasks"}
	recordsGVR = schema.GroupVersionResource{Group: "kelos.dev", Version: "v1alpha2", Resource: "taskrecords"}
)

// fakeKelos returns a dynamic client holding dir's Tasks and TaskRecords.
func fakeKelos(t *testing.T, dir string) *dynamicfake.FakeDynamicClient {
	t.Helper()
	var objs []runtime.Object
	for _, name := range []string{"tasks.json", "taskrecords.json"} {
		if dir == "" {
			break
		}
		data, err := os.ReadFile(filepath.Join(fixturesDir, dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var list unstructured.UnstructuredList
		if err := list.UnmarshalJSON(data); err != nil {
			t.Fatalf("%s/%s: %v", dir, name, err)
		}
		for i := range list.Items {
			objs = append(objs, &list.Items[i])
		}
	}
	kinds := map[schema.GroupVersionResource]string{tasksGVR: "TaskList", recordsGVR: "TaskRecordList"}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), kinds, objs...)
}

// taskRoot and pageRoot are the fixtures' roots.
func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: id}}
}

func pageRoot(id int64) model.Root {
	return model.Root{Page: &model.PageKey{Instance: testWikiHost, ID: id, Locale: defaultWikiLocale}}
}
