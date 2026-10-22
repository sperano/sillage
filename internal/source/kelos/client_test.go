package kelos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestReadListsTasksAndRecords(t *testing.T) {
	s := readStory(t, "same-name-retry")
	if len(s.Tasks) != 1 || len(s.Records) != 2 {
		t.Fatalf("read %d tasks and %d records, want 1 and 2", len(s.Tasks), len(s.Records))
	}
	if !s.ObservedAt.Equal(testObservedAt) {
		t.Errorf("ObservedAt = %v, want %v", s.ObservedAt, testObservedAt)
	}
	task := s.Tasks[0]
	if task.UID != "00000000-0000-4000-8000-00000000204b" || task.Status.Phase != PhaseSucceeded ||
		task.Status.StartTime == nil || task.Spec.Worker == nil || task.Spec.Worker.Model != "model-a" {
		t.Errorf("task decoded as %+v", task)
	}
	for _, r := range s.Records {
		if r.Spec.TaskRef.Name != task.Name || r.Spec.Usage == nil || r.Spec.CompletionTime == nil {
			t.Errorf("record %s decoded as %+v", r.Name, r.Spec)
		}
	}
}

// The reader only lists, and only in its namespace: it never writes.
func TestReadOnlyListsItsNamespace(t *testing.T) {
	objs := append(storyObjects(t, "task-with-record"),
		object("Task", "elsewhere", "other-task"), object("TaskRecord", "elsewhere", "other-record"))
	client := newFakeClient(objs...)
	s, err := newTestReader(t, client).Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tasks) != 1 || len(s.Records) != 1 {
		t.Errorf("read %d tasks and %d records, want only the namespace's 1 and 1", len(s.Tasks), len(s.Records))
	}
	var got []string
	for _, a := range client.Actions() {
		got = append(got, a.GetVerb()+" "+a.GetResource().Resource+" "+a.GetNamespace())
	}
	want := []string{"list tasks agents", "list taskrecords agents"}
	if !slices.Equal(got, want) {
		t.Errorf("actions = %v, want %v", got, want)
	}
}

func TestReadReportsListErrors(t *testing.T) {
	for _, resource := range []string{tasksResource, recordsResource} {
		t.Run(resource, func(t *testing.T) {
			client := newFakeClient()
			client.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("forbidden")
			})
			_, err := newTestReader(t, client).Read(t.Context())
			if err == nil || !strings.Contains(err.Error(), "list "+resource+" in agents: forbidden") {
				t.Errorf("Read error = %v, want the %s list's", err, resource)
			}
		})
	}
}

// fakePages serves pages of one item each, following the continue token.
type fakePages struct {
	pages [][]string
	opts  []metav1.ListOptions
}

func (f *fakePages) list(_ context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	f.opts = append(f.opts, opts)
	i := len(f.opts) - 1
	page := &unstructured.UnstructuredList{}
	for _, name := range f.pages[i] {
		page.Items = append(page.Items, *object("Task", testNamespace, name))
	}
	if i+1 < len(f.pages) {
		page.SetContinue("token-" + f.pages[i+1][0])
	}
	return page, nil
}

func TestNewReaderRejectsInvalidConfig(t *testing.T) {
	cfg := testConfig()
	cfg.Namespace = ""
	if _, err := NewReader(newFakeClient(), cfg); err == nil || !strings.Contains(err.Error(), EnvNamespace) {
		t.Errorf("NewReader error = %v, want the namespace rejected", err)
	}
}

func TestNewReaderRejectsNilClient(t *testing.T) {
	if _, err := NewReader(nil, testConfig()); err == nil {
		t.Error("NewReader accepted a nil client")
	}
}

func TestListAllFollowsContinue(t *testing.T) {
	f := &fakePages{pages: [][]string{{"a", "b"}, {"c"}, {"d"}}}
	tasks, err := listAll[Task](t.Context(), f.list)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	if want := []string{"a", "b", "c", "d"}; !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantContinue := []string{"", "token-c", "token-d"}
	for i, opts := range f.opts {
		if opts.Limit != listPageSize || opts.Continue != wantContinue[i] {
			t.Errorf("request %d: limit %d, continue %q, want %d, %q", i, opts.Limit, opts.Continue, listPageSize, wantContinue[i])
		}
	}
}

func TestListAllReportsUndecodable(t *testing.T) {
	list := func(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		bad := object("Task", testNamespace, "bad")
		bad.Object["status"] = map[string]any{"startTime": "not a time"}
		return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*bad}}, nil
	}
	_, err := listAll[Task](t.Context(), list)
	if err == nil || !strings.Contains(err.Error(), "decode Task agents/bad") {
		t.Errorf("error = %v, want one naming the object", err)
	}
}

// Outside a cluster, the client is configured from the kubeconfig.
func TestRestConfigFallsBackToKubeconfig(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	path := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: example
  cluster:
    server: https://kube.example.com:6443
users:
- name: reader
  user:
    token: fabricated
contexts:
- name: example
  context: {cluster: example, user: reader}
current-context: example
`
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
	cfg, err := restConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://kube.example.com:6443" {
		t.Errorf("host = %q, want the kubeconfig's", cfg.Host)
	}
	client, err := NewDynamicClient()
	if err != nil || client == nil {
		t.Errorf("NewDynamicClient() = %v, %v", client, err)
	}
}

func TestRestConfigWithoutClusterOrKubeconfig(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("HOME", t.TempDir())
	if _, err := restConfig(); err == nil || !strings.Contains(err.Error(), "no usable kubeconfig") {
		t.Errorf("restConfig() error = %v, want no usable kubeconfig", err)
	}
}
