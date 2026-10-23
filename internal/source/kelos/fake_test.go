package kelos

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/sperano/sillage/internal/model"
)

// fixtureDir holds one directory per story: tasks.json and
// taskrecords.json, each a fabricated list as the API server returns it.
const fixtureDir = "../../../testdata/fixtures/kelos"

const (
	testCluster   = "cluster-a"
	testNamespace = "agents"
	testDomain    = "vikunja.example.com"
	testVikunja   = "vikunja.example.com"
	testTaskID    = 1
)

var testObservedAt = time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

func testConfig() Config {
	return Config{Cluster: testCluster, Namespace: testNamespace, LabelDomain: testDomain, VikunjaInstance: testVikunja}
}

// listKinds tells the fake client the list kind of each resource read.
var listKinds = map[schema.GroupVersionResource]string{
	taskGVR:   "TaskList",
	recordGVR: "TaskRecordList",
}

// storyObjects reads a story's Tasks and TaskRecords.
func storyObjects(t *testing.T, story string) []runtime.Object {
	t.Helper()
	var objs []runtime.Object
	for _, name := range []string{"tasks.json", "taskrecords.json"} {
		data, err := os.ReadFile(filepath.Join(fixtureDir, story, name))
		if err != nil {
			t.Fatal(err)
		}
		var list unstructured.UnstructuredList
		if err := list.UnmarshalJSON(data); err != nil {
			t.Fatalf("%s/%s: %v", story, name, err)
		}
		for i := range list.Items {
			objs = append(objs, &list.Items[i])
		}
	}
	return objs
}

func newFakeClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objs...)
}

func newTestReader(t *testing.T, client *dynamicfake.FakeDynamicClient) *Reader {
	t.Helper()
	r, err := NewReader(client, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return testObservedAt }
	return r
}

// readStory reads a story through the fake client.
func readStory(t *testing.T, story string) Snapshot {
	t.Helper()
	s, err := newTestReader(t, newFakeClient(storyObjects(t, story)...)).Read(t.Context())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return s
}

// object returns a minimal object of kind in namespace.
func object(kind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiGroup + "/" + apiVersion)
	u.SetKind(kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

// testStep is how far the fake clock advances on each reading.
const testStep = time.Second

// fakeClock returns testObservedAt, then one testStep later on each call.
type fakeClock struct {
	mu   sync.Mutex
	next time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{next: testObservedAt} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.next
	c.next = c.next.Add(testStep)
	return t
}

// batch is one Commit to a fakeSink, its checkpoint decoded.
type batch struct {
	Records    model.Records
	Checkpoint Checkpoint
}

// fakeSink stores each checkpoint as the bytes it is given, as the store
// would, and hands every committed batch to the test.
type fakeSink struct {
	mu      sync.Mutex
	saved   map[string][]byte
	err     error
	commits chan batch
}

// sinkBuffer is how many batches a fakeSink holds before Commit blocks.
const sinkBuffer = 64

func newFakeSink(t *testing.T, cp *Checkpoint) *fakeSink {
	t.Helper()
	s := &fakeSink{saved: map[string][]byte{}, commits: make(chan batch, sinkBuffer)}
	if cp != nil {
		data, err := json.Marshal(cp)
		if err != nil {
			t.Fatal(err)
		}
		s.saved[testConfig().checkpointKey()] = data
	}
	return s
}

func (s *fakeSink) Checkpoint(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.saved[key]
	return data, ok, nil
}

func (s *fakeSink) Commit(ctx context.Context, key string, recs model.Records, checkpoint []byte) error {
	var cp Checkpoint
	if err := json.Unmarshal(checkpoint, &cp); err != nil {
		return err
	}
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return s.err
	}
	s.saved[key] = checkpoint
	s.mu.Unlock()
	select {
	case s.commits <- batch{Records: recs, Checkpoint: cp}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *fakeSink) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// stored returns the namespace's committed checkpoint.
func (s *fakeSink) stored(t *testing.T) Checkpoint {
	t.Helper()
	data, ok, _ := s.Checkpoint(t.Context(), testConfig().checkpointKey())
	var cp Checkpoint
	if !ok {
		t.Fatal("no checkpoint committed")
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	return cp
}

// fakeLists serves each resource's current list at its resourceVersion.
type fakeLists struct {
	mu      sync.Mutex
	items   map[string][]*unstructured.Unstructured
	version map[string]string
	calls   int
}

func installLists(client *dynamicfake.FakeDynamicClient) *fakeLists {
	f := &fakeLists{items: map[string][]*unstructured.Unstructured{}, version: map[string]string{}}
	client.PrependReactor("list", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		res := action.GetResource().Resource
		list := &unstructured.UnstructuredList{}
		list.SetResourceVersion(f.version[res])
		for _, u := range f.items[res] {
			list.Items = append(list.Items, *u.DeepCopy())
		}
		return true, list, nil
	})
	return f
}

func (f *fakeLists) set(resource, version string, items ...*unstructured.Unstructured) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[resource], f.version[resource] = items, version
}

func (f *fakeLists) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// watchBuffer is how many events a fake watch holds before the test
// blocks sending one.
const watchBuffer = 16

// watchCall is one watch request and the fake watch serving it.
type watchCall struct {
	opts metav1.ListOptions
	w    *watch.FakeWatcher
}

// fakeWatches serves a new fake watch per watch request, after the errors
// queued for its resource.
type fakeWatches struct {
	mu    sync.Mutex
	errs  map[string][]error
	calls map[string]chan watchCall
}

func installWatches(client *dynamicfake.FakeDynamicClient) *fakeWatches {
	f := &fakeWatches{errs: map[string][]error{}, calls: map[string]chan watchCall{
		tasksResource: make(chan watchCall, watchBuffer), recordsResource: make(chan watchCall, watchBuffer),
	}}
	client.PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
		res := action.GetResource().Resource
		f.mu.Lock()
		if errs := f.errs[res]; len(errs) > 0 {
			f.errs[res] = errs[1:]
			f.mu.Unlock()
			return true, nil, errs[0]
		}
		f.mu.Unlock()
		opts := action.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		w := watch.NewFakeWithChanSize(watchBuffer, false)
		f.calls[res] <- watchCall{opts: opts, w: w}
		return true, w, nil
	})
	return f
}

// fail makes the next watch request of resource fail with err.
func (f *fakeWatches) fail(resource string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[resource] = append(f.errs[resource], err)
}

// testTimeout bounds every wait on the watcher's goroutine.
const testTimeout = 5 * time.Second

// next waits for the next watch request of resource.
func (f *fakeWatches) next(t *testing.T, resource string) watchCall {
	t.Helper()
	select {
	case c := <-f.calls[resource]:
		return c
	case <-time.After(testTimeout):
		t.Fatalf("no watch of %s", resource)
		return watchCall{}
	}
}

const (
	uidA  = "00000000-0000-4000-8000-00000000a001"
	uidB  = "00000000-0000-4000-8000-00000000b001"
	taskA = "vikunja-sillage-agent-a-1"
	taskB = "vikunja-sillage-agent-b-1"
)

var (
	testCreated = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	testStart   = time.Date(2026, 3, 15, 10, 5, 0, 0, time.UTC)
	testEnd     = time.Date(2026, 3, 15, 10, 45, 0, 0, time.UTC)
)

// taskObj is a Task labelled for Vikunja task testTaskID, in phase, with
// the start and completion times given.
func taskObj(name, uid, rv string, phase Phase, times ...time.Time) *unstructured.Unstructured {
	u := object("Task", testNamespace, name)
	u.SetUID(types.UID(uid))
	u.SetResourceVersion(rv)
	u.SetCreationTimestamp(metav1.NewTime(testCreated))
	u.SetLabels(map[string]string{testDomain + "/task": "1"})
	status := map[string]any{"phase": string(phase)}
	for i, key := range []string{"startTime", "completionTime"} {
		if i < len(times) {
			status[key] = times[i].Format(time.RFC3339)
		}
	}
	u.Object["status"] = status
	return u
}

// recordObj is the TaskRecord of a Task that succeeded.
func recordObj(taskName, taskUID, rv string) *unstructured.Unstructured {
	u := object("TaskRecord", testNamespace, taskUID)
	u.SetUID(types.UID("record-" + taskUID))
	u.SetResourceVersion(rv)
	u.Object["spec"] = map[string]any{
		"taskRef": map[string]any{"name": taskName, "uid": taskUID},
		"model":   "model-a", "phase": string(PhaseSucceeded),
		"startTime": testStart.Format(time.RFC3339), "completionTime": testEnd.Format(time.RFC3339),
		"usage": map[string]any{"inputTokens": int64(10)},
	}
	return u
}

// harness runs a Watcher over a fake client, lists, watches and sink.
type harness struct {
	client  *dynamicfake.FakeDynamicClient
	lists   *fakeLists
	watches *fakeWatches
	sink    *fakeSink
	// stop is closed once Run returned err.
	stop chan struct{}
	err  error
}

func newHarness(t *testing.T, cp *Checkpoint) *harness {
	t.Helper()
	client := newFakeClient()
	return &harness{
		client: client, lists: installLists(client), watches: installWatches(client),
		sink: newFakeSink(t, cp), stop: make(chan struct{}),
	}
}

// start runs the watcher, with a reconnect delay, until the test ends.
func (h *harness) start(t *testing.T, delay time.Duration) {
	t.Helper()
	w, err := NewWatcher(h.client, testConfig(), h.sink)
	if err != nil {
		t.Fatal(err)
	}
	w.now, w.reconnectDelay = newFakeClock().now, delay
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		h.err = w.Run(ctx)
		close(h.stop)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.stop:
		case <-time.After(testTimeout):
			t.Error("the watcher did not stop")
		}
	})
}

// commit waits for the next batch, which must build into a story.
func (h *harness) commit(t *testing.T) batch {
	t.Helper()
	select {
	case b := <-h.sink.commits:
		root := model.TaskKey{Instance: testVikunja, ID: testTaskID}
		if _, err := model.BuildStory(model.Root{Task: &root}, b.Records); err != nil {
			t.Fatalf("batch does not build into a story: %v", err)
		}
		return b
	case <-h.stop:
		t.Fatalf("the watcher stopped: %v", h.err)
	case <-time.After(testTimeout):
		t.Fatal("no commit")
	}
	return batch{}
}

// stopped waits for Run to return, and returns its error.
func (h *harness) stopped(t *testing.T) error {
	t.Helper()
	select {
	case <-h.stop:
		return h.err
	case <-time.After(testTimeout):
		t.Fatal("the watcher did not stop")
		return nil
	}
}

// listed starts the harness over one Task and no record, and returns the
// list's batch and the two watches.
func (h *harness) listed(t *testing.T, task *unstructured.Unstructured) (batch, *watch.FakeWatcher, *watch.FakeWatcher) {
	t.Helper()
	h.lists.set(tasksResource, "10", task)
	h.lists.set(recordsResource, "11")
	h.start(t, 0)
	b := h.commit(t)
	return b, h.watches.next(t, tasksResource).w, h.watches.next(t, recordsResource).w
}

func onlyEvent(t *testing.T, b batch) model.Event {
	t.Helper()
	if len(b.Records.Events) != 1 {
		t.Fatalf("batch has %d events, want 1", len(b.Records.Events))
	}
	return b.Records.Events[0]
}
