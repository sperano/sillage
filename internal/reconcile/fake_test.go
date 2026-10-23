package reconcile

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// The fabricated instances and records the tests read.
const (
	testVikunjaHost = "vikunja.example.com"
	testGitHubHost  = "github.example.com"
	testWikiHost    = "wiki.example.com"
	testCluster     = "cluster-a"
	testNamespace   = "agents"
	testAgent       = "agent-opus"
	testProject     = 3
	testTaskID      = 42
	testTaskName    = "vikunja-widgets-agent-opus-42"
	testPod         = testTaskName + "-pod00"
	testUID         = "uid-42"
)

// testStart is when the fabricated task was created.
var testStart = time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

// errUnavailable is a Vikunja outage.
var errUnavailable = &vikunja.StatusError{
	Path: "/api/v1/tasks/42", Code: http.StatusServiceUnavailable, Status: "503 Service Unavailable",
}

func testCollectorConfig() collector.Config {
	return collector.Config{
		VikunjaInstance:       testVikunjaHost,
		VikunjaServiceAccount: vikunja.DefaultServiceAccount,
		VikunjaAgentUsers:     []string{testAgent},
		Kelos: kelos.Config{
			Cluster: testCluster, Namespace: testNamespace, LabelDomain: testVikunjaHost, VikunjaInstance: testVikunjaHost,
		},
		GitHubInstance: testGitHubHost,
		WikiInstance:   testWikiHost,
		KelosTaskTTL:   collector.DefaultKelosTaskTTL,
	}
}

func testRoot() model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: testTaskID}}
}

// fakeVikunja serves task testTaskID, announced once, read now; or err.
type fakeVikunja struct {
	mu    sync.Mutex
	err   error
	reads int
	// during runs inside each read, before it answers.
	during func(ctx context.Context)
}

func (f *fakeVikunja) Fetch(ctx context.Context, id int64) (vikunja.Snapshot, error) {
	f.mu.Lock()
	f.reads++
	err, during := f.err, f.during
	f.mu.Unlock()
	if during != nil {
		during(ctx)
		if ctx.Err() != nil {
			return vikunja.Snapshot{}, ctx.Err()
		}
	}
	if err != nil {
		return vikunja.Snapshot{}, err
	}
	return testTask(id, time.Now()), nil
}

func (f *fakeVikunja) set(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeVikunja) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// testTask is task id as read at observed: in a bucket, with a label, and
// a start announcement of testTaskName's pod.
func testTask(id int64, observed time.Time) vikunja.Snapshot {
	announced := testStart.Add(time.Minute)
	return vikunja.Snapshot{
		Task: vikunja.Task{
			ID: id, ProjectID: testProject, Created: testStart, CreatedBy: &vikunja.User{ID: 7, Username: "owner"},
			Labels:  []vikunja.Label{{ID: 1, Title: "in progress"}},
			Buckets: []vikunja.Bucket{{ID: 5, Title: "Doing", ProjectViewID: 2}},
		},
		Comments: []vikunja.Comment{{
			ID: 900, Created: announced, Updated: announced,
			Author: vikunja.User{ID: 23, Username: vikunja.DefaultServiceAccount},
			Body: "<p>Agent <code>" + testAgent + "</code> has started working on this task: Kelos Task <code>" +
				testTaskName + "</code>, pod <code>" + testPod + "</code>.</p>",
		}},
		ObservedAt:         observed,
		CommentsObservedAt: observed,
	}
}

// fakeKelos answers with its snapshot, or err.
type fakeKelos struct {
	mu   sync.Mutex
	snap kelos.Snapshot
	err  error
}

func (f *fakeKelos) Read(context.Context) (kelos.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.err
}

func (f *fakeKelos) set(snap kelos.Snapshot, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap, f.err = snap, err
}

// testKelosTask is task testTaskID's run, in phase.
func testKelosTask(phase kelos.Phase) kelos.Task {
	start := metav1.NewTime(testStart.Add(time.Minute))
	t := kelos.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: testTaskName, UID: types.UID(testUID), CreationTimestamp: start,
			Labels: map[string]string{
				testVikunjaHost + "/project": "widgets", testVikunjaHost + "/variant": testAgent,
				testVikunjaHost + "/task": "42",
			},
		},
		Status: kelos.TaskStatus{Phase: phase, PodName: testPod, StartTime: &start},
	}
	if phase == kelos.PhaseSucceeded {
		end := metav1.NewTime(testStart.Add(time.Hour))
		t.Status.CompletionTime = &end
	}
	return t
}

type noGitHub struct{}

func (noGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	return nil, nil
}

type noWiki struct{}

func (noWiki) Fetch(context.Context, int64) (wiki.Snapshot, error) {
	return wiki.Snapshot{}, errors.New("no page")
}

func (noWiki) Pages(context.Context) ([]wiki.Page, error) { return nil, nil }

// fakeProjects lists changes, by project, and records each since asked.
type fakeProjects struct {
	mu      sync.Mutex
	changes map[int64][]vikunja.TaskChange
	since   []time.Time
}

func (f *fakeProjects) ChangedTasks(_ context.Context, project int64, since time.Time) ([]vikunja.TaskChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	return slices.Clone(f.changes[project]), nil
}

func (f *fakeProjects) set(project int64, changes ...vikunja.TaskChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changes == nil {
		f.changes = make(map[int64][]vikunja.TaskChange)
	}
	f.changes[project] = changes
}

// fakeStore holds what is committed in memory, as the store's contract
// says: upserts, a pod-keyed run re-keyed, coverage replaced.
type fakeStore struct {
	mu      sync.Mutex
	roots   map[model.Ref]*storedRoot
	cps     map[string][]byte
	commits []Commit
	// commitErr fails every commit, and checkpointsErr every checkpoint
	// read, while set.
	commitErr      error
	checkpointsErr error
}

type storedRoot struct {
	events       map[string]model.Event
	runs         map[model.RunKey]model.Run
	prs          map[model.PRKey]model.PullRequest
	correlations map[model.RefPair]model.Correlation
	coverage     map[string]model.Coverage
}

func newFakeStore() *fakeStore {
	return &fakeStore{roots: make(map[model.Ref]*storedRoot), cps: make(map[string][]byte)}
}

func (s *fakeStore) Records(_ context.Context, root model.Root) (model.Records, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roots[root.Ref()]
	if !ok {
		return model.Records{}, nil
	}
	return model.Records{
		Events: slices.Collect(maps.Values(r.events)), Runs: slices.Collect(maps.Values(r.runs)),
		PullRequests: slices.Collect(maps.Values(r.prs)), Correlations: slices.Collect(maps.Values(r.correlations)),
		Coverage: slices.Collect(maps.Values(r.coverage)),
	}, nil
}

func (s *fakeStore) Checkpoints(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpointsErr != nil {
		return nil, s.checkpointsErr
	}
	out := make(map[string][]byte)
	for k, v := range s.cps {
		if strings.HasPrefix(k, prefix) {
			out[k] = slices.Clone(v)
		}
	}
	return out, nil
}

func (s *fakeStore) Commit(ctx context.Context, c Commit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := cmp.Or(ctx.Err(), s.commitErr); err != nil {
		return err
	}
	s.commits = append(s.commits, c)
	maps.Copy(s.cps, c.Checkpoints)
	if c.Root != nil {
		s.root(*c.Root).upsert(c.Records)
	}
	return nil
}

func (s *fakeStore) root(root model.Root) *storedRoot {
	r, ok := s.roots[root.Ref()]
	if !ok {
		r = &storedRoot{
			events: make(map[string]model.Event), runs: make(map[model.RunKey]model.Run),
			prs: make(map[model.PRKey]model.PullRequest), correlations: make(map[model.RefPair]model.Correlation),
			coverage: make(map[string]model.Coverage),
		}
		s.roots[root.Ref()] = r
	}
	return r
}

func (r *storedRoot) upsert(recs model.Records) {
	for _, e := range recs.Events {
		r.events[e.Key()] = e
	}
	for _, run := range recs.Runs {
		r.runs[run.Key] = run
		if run.Key.UID != "" && run.Pod != "" {
			r.rekey(model.RunKey{Cluster: run.Key.Cluster, Namespace: run.Key.Namespace, Pod: run.Pod})
		}
	}
	for _, p := range recs.PullRequests {
		r.prs[p.Key] = p
	}
	for _, c := range recs.Correlations {
		r.correlations[c.Pair()] = c
	}
	for _, c := range recs.Coverage {
		r.coverage[c.Key()] = c
	}
}

// rekey drops the run keyed by pod and its correlations.
func (r *storedRoot) rekey(pod model.RunKey) {
	delete(r.runs, pod)
	for pair, c := range r.correlations {
		if c.From == pod.Ref() || c.To == pod.Ref() {
			delete(r.correlations, pair)
		}
	}
}

func (s *fakeStore) story(t *testing.T, root model.Root) model.Story {
	t.Helper()
	recs, err := s.Records(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	story, err := model.BuildStory(root, recs)
	if err != nil {
		t.Fatalf("the stored story is invalid: %v", err)
	}
	return story
}

func (s *fakeStore) lastCommit(t *testing.T) Commit {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range slices.Backward(s.commits) {
		if c.Root != nil {
			return c
		}
	}
	t.Fatal("no root was committed")
	return Commit{}
}

func (s *fakeStore) setCommitErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitErr = err
}

// harness is a loop over fakes.
type harness struct {
	loop     *Loop
	store    *fakeStore
	vikunja  *fakeVikunja
	kelos    *fakeKelos
	projects *fakeProjects
	reg      *prometheus.Registry
	logs     *bytes.Buffer
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	h := &harness{
		store: newFakeStore(), vikunja: &fakeVikunja{}, projects: &fakeProjects{},
		kelos: &fakeKelos{snap: kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseRunning)}}},
		reg:   prometheus.NewRegistry(), logs: &bytes.Buffer{},
	}
	h.loop = h.newLoop(t, cfg)
	return h
}

// newLoop returns another loop over the same fakes and store: the same
// sillage, restarted.
func (h *harness) newLoop(t *testing.T, cfg Config) *Loop {
	t.Helper()
	src := Sources{
		Sources:  collector.Sources{Vikunja: h.vikunja, Kelos: h.kelos, GitHub: noGitHub{}, Wiki: noWiki{}},
		Projects: h.projects,
	}
	reg := prometheus.NewRegistry()
	h.reg = reg
	l, err := New(cfg, testCollectorConfig(), src, h.store, NewMetrics(reg), slog.New(slog.NewTextHandler(h.logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	l.jitter = func(d time.Duration) time.Duration { return d }
	if err := l.load(t.Context()); err != nil {
		t.Fatal(err)
	}
	return l
}

// fakeLocker runs f unless held, and counts the attempts. While lose is
// set, it loses the lock as soon as it is taken.
type fakeLocker struct {
	mu       sync.Mutex
	held     bool
	lose     bool
	attempts int
}

func (f *fakeLocker) WithLock(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	f.attempts++
	held, lose := f.held, f.lose
	f.mu.Unlock()
	switch {
	case held:
		return ErrLockHeld
	case lose:
		lost, cancel := context.WithCancel(ctx)
		cancel()
		_ = fn(lost)
		return ErrLockLost
	}
	return fn(ctx)
}

func (f *fakeLocker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}
