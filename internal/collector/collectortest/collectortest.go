// Package collectortest answers the real source clients from the
// fabricated responses under testdata/fixtures: Vikunja, GitHub and
// Wiki.js from one httptest server routed by host, Kelos from client-go's
// fake dynamic client. A test assembles a story from them the way the
// collector reads the live sources. It is imported by tests only.
package collectortest

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

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

const (
	// The fabricated instances the fixtures name.
	VikunjaHost = "vikunja.example.com"
	GitHubHost  = "github.example.com"
	WikiHost    = "wiki.example.com"
	Cluster     = "cluster-a"
	Namespace   = "agents"
	Workspace   = "widgets"
	Agent       = "agent-opus"
	// ServiceAccount posts the fixtures' Vikunja start announcements. It
	// is configured, not vikunja.DefaultServiceAccount, so that the
	// fixtures do not depend on the source's default.
	ServiceAccount = "kelos"
	// GitHubAPIPath is where the fake serves GitHub's REST API, as GitHub
	// Enterprise Server does.
	GitHubAPIPath = "/api/v3"
	// Token is a made-up token every client sends.
	Token = "fabricated-token-0123456789"
	// WikiUser is the agents' Wiki.js user.
	WikiUser = int64(21)

	// repoPath is the fixtures' repository's API path.
	repoPath = GitHubAPIPath + "/repos/example-org/widgets"
	// requestTimeout bounds every request of a test.
	requestTimeout = 10 * time.Second
	// emptyList and emptyCheckRuns answer a list a story has no file for.
	emptyList      = "[]"
	emptyCheckRuns = `{"total_count":0,"check_runs":[]}`
)

// Repo is the fixtures' repository, of Workspace.
var Repo = github.Repository{Owner: "example-org", Name: "widgets"}

// Dirs names, under the fixtures directory, the responses of each source
// a story reads; an empty one is a source the story must not read.
type Dirs struct {
	Vikunja, Kelos, GitHub, Wiki string
}

// Clients are the real clients of every source, each answered from the
// story's responses.
type Clients struct {
	Vikunja *vikunja.Client
	Kelos   *kelos.Reader
	GitHub  *github.Client
	Wiki    *wiki.Client
}

// KelosConfig is the Kelos configuration the fixtures follow.
func KelosConfig() kelos.Config {
	return kelos.Config{Cluster: Cluster, Namespace: Namespace, LabelDomain: VikunjaHost, VikunjaInstance: VikunjaHost}
}

// Report is where the fake reports each request it refuses, answered 404.
type Report struct {
	// Elsewhere gets the requests the story's responses are not for: of
	// another task or page, or of a source the story reads nothing from.
	// A test that reads only the story fails them; the collection loop
	// also reads the other roots Kelos names.
	Elsewhere func(format string, args ...any)
	// Broken gets every other one: a request no source answers, or a
	// response the story's directory lacks.
	Broken func(format string, args ...any)
}

// New returns the clients of every source, answered from dirs under
// fixtures; each request refused goes to report.
func New(t testing.TB, fixtures string, dirs Dirs, report Report) Clients {
	t.Helper()
	hc := routedClient(t, newFake(t, fixtures, dirs, report))
	gh, err := github.NewClient(github.Config{
		APIURL: "http://" + GitHubHost + GitHubAPIPath, Instance: GitHubHost, Token: Token,
	}, hc)
	if err != nil {
		t.Fatal(err)
	}
	wk, err := wiki.NewClient(wiki.Config{
		BaseURL: "http://" + WikiHost, Instance: WikiHost, Token: Token, AgentUsers: []int64{WikiUser},
	}, hc)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := kelos.NewReader(fakeKelos(t, fixtures, dirs.Kelos), KelosConfig())
	if err != nil {
		t.Fatal(err)
	}
	vk := vikunja.NewClient(vikunja.Config{BaseURL: "http://" + VikunjaHost, Token: Token, ServiceAccount: ServiceAccount}, hc)
	return Clients{Vikunja: vk, Kelos: reader, GitHub: gh, Wiki: wk}
}

// routedClient sends every request to h's server, whatever its host.
func routedClient(t testing.TB, h http.Handler) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	var dialer net.Dialer
	return &http.Client{Timeout: requestTimeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

// fake answers each host from its story directory.
type fake struct {
	t        testing.TB
	fixtures string
	dirs     Dirs
	report   Report
	mux      *http.ServeMux
	// task and page are the ids of the task and the page the story's
	// responses hold; 0 without.
	task, page int64
}

func newFake(t testing.TB, fixtures string, dirs Dirs, report Report) *fake {
	t.Helper()
	f := &fake{t: t, fixtures: fixtures, dirs: dirs, report: report, mux: http.NewServeMux()}
	if dirs.Vikunja != "" {
		f.task = f.taskID()
	}
	if dirs.Wiki != "" {
		f.page = f.pageID()
	}
	vk := func(name func(*http.Request) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); f.dirs.Vikunja != "" && (err != nil || id != f.task) {
				f.refuse(w, r, f.report.Elsewhere, "the story's responses are of task %d", f.task)
				return
			}
			f.serve(w, r, f.dirs.Vikunja, name(r), emptyList)
		}
	}
	f.mux.Handle("GET "+VikunjaHost+"/api/v1/tasks/{id}", vk(func(*http.Request) string { return "task.json" }))
	f.mux.Handle("GET "+VikunjaHost+"/api/v1/tasks/{id}/comments", vk(func(r *http.Request) string {
		return "comments-" + r.URL.Query().Get("page") + ".json"
	}))
	f.handleGitHub()
	f.mux.HandleFunc("POST "+WikiHost+"/graphql", f.serveWiki)
	return f
}

func (f *fake) handleGitHub() {
	gh := func(prefix, empty string, key func(*http.Request) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.serve(w, r, f.dirs.GitHub, prefix+key(r)+".json", empty)
		}
	}
	number := func(r *http.Request) string { return "-" + r.PathValue("n") }
	f.mux.Handle("GET "+GitHubHost+repoPath+"/pulls", gh("pulls", "", func(*http.Request) string { return "" }))
	f.mux.Handle("GET "+GitHubHost+repoPath+"/issues/{n}/timeline", gh("timeline", emptyList, number))
	f.mux.Handle("GET "+GitHubHost+repoPath+"/issues/{n}/comments", gh("comments", emptyList, number))
	f.mux.Handle("GET "+GitHubHost+repoPath+"/pulls/{n}/comments", gh("review-comments", emptyList, number))
	f.mux.Handle("GET "+GitHubHost+repoPath+"/pulls/{n}/reviews", gh("reviews", emptyList, number))
	f.mux.Handle("GET "+GitHubHost+repoPath+"/commits/{sha}/check-runs", gh("check-runs", emptyCheckRuns,
		func(r *http.Request) string { return "-" + r.PathValue("sha") }))
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer "+Token {
		f.t.Errorf("%s %s: Authorization %q", r.Method, r.URL, got)
	}
	if _, pattern := f.mux.Handler(r); pattern == "" {
		f.refuse(w, r, f.report.Broken, "no source answers it")
		return
	}
	f.mux.ServeHTTP(w, r)
}

// refuse reports a request to report, saying why, and answers 404.
func (f *fake) refuse(w http.ResponseWriter, r *http.Request, report func(string, ...any), why string, args ...any) {
	report("%s %s%s: "+why, append([]any{r.Method, r.Host, r.URL}, args...)...)
	http.NotFound(w, r)
}

// serve answers with dir's file name, or with empty when the story has
// none and empty is set.
func (f *fake) serve(w http.ResponseWriter, r *http.Request, dir, name, empty string) {
	if dir == "" {
		f.refuse(w, r, f.report.Elsewhere, "the story reads nothing from %s", r.Host)
		return
	}
	body, err := os.ReadFile(filepath.Join(f.fixtures, dir, name))
	switch {
	case err == nil:
	case os.IsNotExist(err) && empty != "":
		body = []byte(empty)
	default:
		f.refuse(w, r, f.report.Broken, "no response %s/%s: %v", dir, name, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// serveWiki answers a GraphQL query from its operation's file. A query
// of a page other than the story's is refused.
func (f *fake) serveWiki(w http.ResponseWriter, r *http.Request) {
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
	if id, ok := asked[req.OperationName]; ok && f.dirs.Wiki != "" && id != f.page {
		f.refuse(w, r, f.report.Elsewhere, "wiki %s of page %d, the story's responses are of page %d", req.OperationName, id, f.page)
		return
	}
	files := map[string]string{
		"SillagePage":     "page.json",
		"SillagePageList": "pages.json",
		"SillageHistory":  "history-" + strconv.Itoa(req.Variables.OffsetPage) + ".json",
		"SillageVersion":  "version-" + strconv.FormatInt(req.Variables.VersionID, 10) + ".json",
		"SillageComments": "comments.json",
	}
	f.serve(w, r, f.dirs.Wiki, files[req.OperationName], "")
}

// taskID is the id of the task the story's Vikunja responses hold.
func (f *fake) taskID() int64 {
	var task struct {
		ID int64 `json:"id"`
	}
	f.decode(filepath.Join(f.dirs.Vikunja, "task.json"), &task)
	return task.ID
}

// pageID is the id of the page the story's wiki responses hold.
func (f *fake) pageID() int64 {
	var page struct {
		Data struct {
			Pages struct {
				Single struct {
					ID int64 `json:"id"`
				} `json:"single"`
			} `json:"pages"`
		} `json:"data"`
	}
	f.decode(filepath.Join(f.dirs.Wiki, "page.json"), &page)
	return page.Data.Pages.Single.ID
}

func (f *fake) decode(name string, v any) {
	data, err := os.ReadFile(filepath.Join(f.fixtures, name))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
}

// Kelos's resources, as the fake dynamic client lists them.
var (
	tasksGVR   = schema.GroupVersionResource{Group: "kelos.dev", Version: "v1alpha2", Resource: "tasks"}
	recordsGVR = schema.GroupVersionResource{Group: "kelos.dev", Version: "v1alpha2", Resource: "taskrecords"}
)

// fakeKelos returns a dynamic client holding dir's Tasks and TaskRecords.
func fakeKelos(t testing.TB, fixtures, dir string) *dynamicfake.FakeDynamicClient {
	t.Helper()
	var objs []runtime.Object
	for _, name := range []string{"tasks.json", "taskrecords.json"} {
		if dir == "" {
			break
		}
		data, err := os.ReadFile(filepath.Join(fixtures, dir, name))
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
