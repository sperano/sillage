package kelos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// userAgent identifies sillage's requests in the API server's audit
	// log.
	userAgent = "sillage"
	// listPageSize is how many objects one list request asks for.
	listPageSize = 250
)

// NewDynamicClient returns a client for the cluster sillage runs in, or,
// outside a cluster, for the kubeconfig's current context ($KUBECONFIG, else
// ~/.kube/config). Its credentials need only get and list on Kelos's tasks
// and taskrecords.
func NewDynamicClient() (dynamic.Interface, error) {
	cfg, err := restConfig()
	if err != nil {
		return nil, err
	}
	cfg.UserAgent = userAgent
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kelos: client: %w", err)
	}
	return client, nil
}

// restConfig is the in-cluster configuration, else the kubeconfig's when
// not in a cluster.
func restConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, rest.ErrNotInCluster) {
		return nil, fmt.Errorf("kelos: in-cluster config: %w", err)
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kelos: not in a cluster, and no usable kubeconfig: %w", err)
	}
	return cfg, nil
}

// Snapshot is one read of a namespace's Tasks and TaskRecords. The Tasks
// are listed first, then the TaskRecords: the two lists are not one atomic
// view, so a Task that completes in between can appear in both.
type Snapshot struct {
	Tasks   []Task
	Records []TaskRecord
	// ObservedAt is when the read started, before the first list: the time
	// a coverage record can claim the namespace was read up to.
	ObservedAt time.Time
}

// Reader lists the Tasks and TaskRecords of the configured namespace.
type Reader struct {
	client dynamic.Interface
	cfg    Config
	now    func() time.Time
}

// NewReader returns a reader of cfg's namespace through client.
func NewReader(client dynamic.Interface, cfg Config) (*Reader, error) {
	if client == nil {
		return nil, errors.New("kelos: reader without a client")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Reader{client: client, cfg: cfg, now: time.Now}, nil
}

// Read lists every Task and TaskRecord of the namespace, page by page. Any
// failed request fails the read, including a continue token that expired
// between two pages (410 Gone); the caller reads again later.
func (r *Reader) Read(ctx context.Context) (Snapshot, error) {
	s := Snapshot{ObservedAt: r.now()}
	var err error
	if s.Tasks, err = listAll[Task](ctx, r.lister(taskGVR)); err != nil {
		return Snapshot{}, err
	}
	if s.Records, err = listAll[TaskRecord](ctx, r.lister(recordGVR)); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// listPage lists one page of one resource.
type listPage func(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)

func (r *Reader) lister(gvr schema.GroupVersionResource) listPage {
	res := r.client.Resource(gvr).Namespace(r.cfg.Namespace)
	return func(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		list, err := res.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("kelos: list %s in %s: %w", gvr.Resource, r.cfg.Namespace, err)
		}
		return list, nil
	}
}

// listAll follows the continue token until the last page and decodes every
// object into T.
func listAll[T any](ctx context.Context, list listPage) ([]T, error) {
	var out []T
	opts := metav1.ListOptions{Limit: listPageSize}
	for {
		page, err := list(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			v, err := decode[T](item)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if opts.Continue = page.GetContinue(); opts.Continue == "" {
			return out, nil
		}
	}
}

// decode converts an object into T, dropping every field T does not name.
func decode[T any](item unstructured.Unstructured) (T, error) {
	var v T
	data, err := item.MarshalJSON()
	if err == nil {
		err = json.Unmarshal(data, &v)
	}
	if err != nil {
		return v, fmt.Errorf("kelos: decode %s %s/%s: %w", item.GetKind(), item.GetNamespace(), item.GetName(), err)
	}
	return v, nil
}
