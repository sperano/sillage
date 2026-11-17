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
	"strconv"
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
)

// The fabricated instances and records the tests read.
const (
	testVikunjaHost = "vikunja.example.com"
	testGitHubHost  = "github.example.com"
	testCluster     = "cluster-a"
	testNamespace   = "agents"
	testAgent       = "agent-opus"
	testProject     = 3
	testTaskID      = 42
	testTaskName    = "vikunja-widgets-agent-opus-42"
	podSuffix       = "-pod00"
	testPod         = testTaskName + podSuffix
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
		KelosTaskTTL:   collector.DefaultKelosTaskTTL,
	}
}

func testRoot() model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: testTaskID}}
}

// fakeVikunja serves task testTaskID and those of also, each announced
// once, read now; or err. Any other task is not found.
type fakeVikunja struct {
	mu    sync.Mutex
	err   error
	reads int
	// ids are the tasks read, in order.
	ids  []int64
	also []int64
	// done has every task read done.
	done bool
	// during runs inside each read, before it answers.
	during func(ctx context.Context)
}

func (f *fakeVikunja) Fetch(ctx context.Context, id int64) (vikunja.Snapshot, error) {
	f.mu.Lock()
	f.reads++
	f.ids = append(f.ids, id)
	err, during, done, known := f.err, f.during, f.done, id == testTaskID || slices.Contains(f.also, id)
	f.mu.Unlock()
	if !known {
		return vikunja.Snapshot{}, &vikunja.StatusError{
			Path: "/api/v1/tasks/" + strconv.FormatInt(id, 10), Code: http.StatusNotFound, Status: "404 Not Found",
		}
	}
	if during != nil {
		during(ctx)
		if ctx.Err() != nil {
			return vikunja.Snapshot{}, ctx.Err()
		}
	}
	if err != nil {
		return vikunja.Snapshot{}, err
	}
	snap := testTask(id, time.Now())
	if done {
		snap.Task.Done, snap.Task.DoneAt = true, testStart.Add(2*time.Hour)
	}
	return snap, nil
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

// read returns the tasks read, in order.
func (f *fakeVikunja) read() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}

// taskNameOf is the Kelos Task name of task id's run; testTaskName for
// testTaskID.
func taskNameOf(id int64) string { return "vikunja-widgets-agent-opus-" + strconv.FormatInt(id, 10) }

// testTask is task id as read at observed: in a bucket, with a label, and
// a start announcement of its run's pod.
func testTask(id int64, observed time.Time) vikunja.Snapshot {
	announced := testStart.Add(time.Minute)
	name := taskNameOf(id)
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
				name + "</code>, pod <code>" + name + podSuffix + "</code>.</p>",
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
func testKelosTask(phase kelos.Phase) kelos.Task { return kelosTaskOf(testTaskID, phase) }

// kelosTaskOf is task id's run, in phase.
func kelosTaskOf(id int64, phase kelos.Phase) kelos.Task {
	start := metav1.NewTime(testStart.Add(time.Minute))
	name := taskNameOf(id)
	t := kelos.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, UID: types.UID("uid-" + strconv.FormatInt(id, 10)), CreationTimestamp: start,
			Labels: map[string]string{
				testVikunjaHost + "/project": "widgets", testVikunjaHost + "/variant": testAgent,
				testVikunjaHost + "/task": strconv.FormatInt(id, 10),
			},
		},
		Status: kelos.TaskStatus{Phase: phase, PodName: name + podSuffix, StartTime: &start},
	}
	if phase == kelos.PhaseSucceeded {
		end := metav1.NewTime(testStart.Add(time.Hour))
		t.Status.CompletionTime = &end
	}
	return t
}

// fakeGitHub answers every branch with snaps, and counts the reads.
type fakeGitHub struct {
	mu      sync.Mutex
	snaps   []github.Snapshot
	fetches int
}

func (f *fakeGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches++
	return f.snaps, nil
}

// fakeProjects lists changes, by project, and records each since asked.
// While err is set, every listing fails with it.
type fakeProjects struct {
	mu      sync.Mutex
	changes map[int64][]vikunja.TaskChange
	since   []time.Time
	err     error
}

func (f *fakeProjects) ChangedTasks(_ context.Context, project int64, since time.Time) ([]vikunja.TaskChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.changes[project]), nil
}

func (f *fakeProjects) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeProjects) asked() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.since)
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
	if err := cmp.Or(ctx.Err(), s.commitErr, s.unknownRoot(c)); err != nil {
		return err
	}
	s.commits = append(s.commits, c)
	for key, cp := range c.Checkpoints {
		s.cps[key] = slices.Clone(cp.Value)
	}
	if c.Root != nil {
		s.root(*c.Root).upsert(c.Records)
	}
	return nil
}

// unknownRoot fails a commit with a checkpoint tied to a root neither
// stored nor its own, as the store does.
func (s *fakeStore) unknownRoot(c Commit) error {
	for key, cp := range c.Checkpoints {
		if cp.Root == nil || (c.Root != nil && c.Root.Ref() == cp.Root.Ref()) {
			continue
		}
		if _, ok := s.roots[cp.Root.Ref()]; !ok {
			return errors.New("checkpoint " + key + ": its root is not stored")
		}
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
			r.rekey(model.RunKey{Cluster: run.Key.Cluster, Namespace: run.Key.Namespace, Pod: run.Pod}, run.Key)
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

// rekey drops the run keyed by pod and its correlations, and points the
// events that name it to uid.
func (r *storedRoot) rekey(pod, uid model.RunKey) {
	delete(r.runs, pod)
	for key, e := range r.events {
		if e.Related.Run != nil && *e.Related.Run == pod {
			run := uid
			e.Related.Run = &run
			r.events[key] = e
		}
	}
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
	github   *fakeGitHub
	projects *fakeProjects
	// ccfg is the collector's configuration of each loop newLoop starts.
	ccfg collector.Config
	// deliveries are the roots the tests' deliveries name.
	deliveries *fakeDeliveries
	reg        *prometheus.Registry
	logs       *bytes.Buffer
	// spread spreads the overdue backstop reads of each loop newLoop
	// starts; nil makes them due at once.
	spread func(time.Duration) time.Duration
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	h := &harness{
		store: newFakeStore(), vikunja: &fakeVikunja{}, github: &fakeGitHub{}, projects: &fakeProjects{},
		deliveries: newFakeDeliveries(), ccfg: testCollectorConfig(),
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
	return h.loopOf(t, cfg, h.ccfg, h.sources())
}

// sources are every fake of h.
func (h *harness) sources() Sources {
	return Sources{
		Sources:  collector.Sources{Vikunja: h.vikunja, Kelos: h.kelos, GitHub: h.github},
		Projects: h.projects,
	}
}

// withDeliveryDefaults returns cfg, given a delivery delay no test waits
// for, and the default cap and backstop interval where it sets none.
func withDeliveryDefaults(cfg Config) Config {
	if cfg.DeliveryDelay == 0 {
		cfg.DeliveryDelay = time.Millisecond
	}
	if cfg.BackstopInterval == 0 {
		cfg.BackstopInterval = DefaultBackstopInterval
	}
	if cfg.DeliveryRootsPerCycle == 0 {
		cfg.DeliveryRootsPerCycle = DefaultDeliveryRootsPerCycle
	}
	return cfg
}

// loopOf returns another loop over h's store and deliveries, reading src
// as ccfg says. A cfg that does not set the deliveries' settings gets
// withDeliveryDefaults'.
func (h *harness) loopOf(t *testing.T, cfg Config, ccfg collector.Config, src Sources) *Loop {
	t.Helper()
	reg := prometheus.NewRegistry()
	h.reg = reg
	l, err := New(withDeliveryDefaults(cfg), ccfg, src, h.store, h.deliveries, NewMetrics(reg),
		slog.New(slog.NewTextHandler(h.logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	l.jitter = func(d time.Duration) time.Duration { return d }
	l.spread = h.spread
	if l.spread == nil {
		l.spread = func(time.Duration) time.Duration { return 0 }
	}
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
