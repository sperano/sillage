// Package collectortest answers the real source clients from the
// fabricated responses under testdata/fixtures: Vikunja and GitHub from
// one httptest server routed by host, Kelos from client-go's
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

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

const (
	// The fabricated instances the fixtures name.
	VikunjaHost = "vikunja.example.com"
	GitHubHost  = "github.example.com"
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
// Markers holds the phase markers the store joined to the story's runs,
// which no source is read for: a caller passes them back in (see
// Markers).
type Dirs struct {
	Vikunja, Kelos, GitHub, Markers string
}

// markersFile holds, in a story's Markers directory, its joined markers.
const markersFile = "markers.json"

// Markers returns the phase markers of dirs under fixtures, each with its
// join; none without a Markers directory.
func Markers(t testing.TB, fixtures string, dirs Dirs) []model.Marker {
	t.Helper()
	if dirs.Markers == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(fixtures, dirs.Markers, markersFile))
	if err != nil {
		t.Fatal(err)
	}
	var markers []model.Marker
	if err := json.Unmarshal(data, &markers); err != nil {
		t.Fatalf("%s/%s: %v", dirs.Markers, markersFile, err)
	}
	return markers
}

// Clients are the real clients of every source, each answered from the
// story's responses.
type Clients struct {
	Vikunja *vikunja.Client
	Kelos   *kelos.Reader
	GitHub  *github.Client
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
	reader, err := kelos.NewReader(fakeKelos(t, fixtures, dirs.Kelos), KelosConfig())
	if err != nil {
		t.Fatal(err)
	}
	vk := vikunja.NewClient(vikunja.Config{BaseURL: "http://" + VikunjaHost, Token: Token, ServiceAccount: ServiceAccount}, hc)
	return Clients{Vikunja: vk, Kelos: reader, GitHub: gh}
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
	// task is the id of the task the story's responses hold; 0 without.
	task int64
}

func newFake(t testing.TB, fixtures string, dirs Dirs, report Report) *fake {
	t.Helper()
	f := &fake{t: t, fixtures: fixtures, dirs: dirs, report: report, mux: http.NewServeMux()}
	if dirs.Vikunja != "" {
		f.task = f.taskID()
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
// none and empty is set. A refused response, name with its status before
// the extension (check-runs-<sha>.403.json), answers that status with its
// body instead.
func (f *fake) serve(w http.ResponseWriter, r *http.Request, dir, name, empty string) {
	if dir == "" {
		f.refuse(w, r, f.report.Elsewhere, "the story reads nothing from %s", r.Host)
		return
	}
	status := http.StatusOK
	body, err := os.ReadFile(filepath.Join(f.fixtures, dir, name))
	if os.IsNotExist(err) {
		if code, refused, ok := f.refused(dir, name); ok {
			status, body, err = code, refused, nil
		}
	}
	switch {
	case err == nil:
	case os.IsNotExist(err) && empty != "":
		body = []byte(empty)
	default:
		f.refuse(w, r, f.report.Broken, "no response %s/%s: %v", dir, name, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// refusedPattern matches a refused response's status and extension, after
// the name it stands for without its extension.
const refusedPattern = ".[1-5][0-9][0-9].json"

// refused returns the status and body of dir's refused response for name,
// if it has one.
func (f *fake) refused(dir, name string) (int, []byte, bool) {
	base := filepath.Join(f.fixtures, dir, strings.TrimSuffix(name, ".json"))
	matches, err := filepath.Glob(base + refusedPattern)
	if err != nil || len(matches) == 0 {
		return 0, nil, false
	}
	if len(matches) > 1 {
		f.t.Fatalf("%s/%s: several refused responses %v", dir, name, matches)
	}
	code, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(matches[0], base+"."), ".json"))
	if err != nil {
		f.t.Fatalf("%s: %v", matches[0], err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		f.t.Fatal(err)
	}
	return code, body, true
}

// taskID is the id of the task the story's Vikunja responses hold.
func (f *fake) taskID() int64 {
	var task struct {
		ID int64 `json:"id"`
	}
	f.decode(filepath.Join(f.dirs.Vikunja, "task.json"), &task)
	return task.ID
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
