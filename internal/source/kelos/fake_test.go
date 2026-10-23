package kelos

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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
