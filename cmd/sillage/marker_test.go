package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/ingest/marker"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/store"
)

// podReviewer is a fake API server whose TokenReviews authenticate every
// token as a pod of namespace.
func podReviewer(namespace string) func() (dynamic.Interface, error) {
	return func() (dynamic.Interface, error) {
		gvr := schema.GroupVersionResource{Group: "authentication.k8s.io", Version: "v1", Resource: "tokenreviews"}
		client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
			map[schema.GroupVersionResource]string{gvr: "TokenReviewList"})
		client.PrependReactor("create", "tokenreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
			out := a.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
			out.Object["status"] = map[string]any{
				"authenticated": true, "audiences": []any{marker.Audience},
				"user": map[string]any{"username": "system:serviceaccount:" + namespace + ":default",
					"extra": map[string]any{
						"authentication.kubernetes.io/pod-name": []any{"pod-a"},
						"authentication.kubernetes.io/pod-uid":  []any{"pod-uid-a"},
					}},
			}
			return true, out, nil
		})
		return client, nil
	}
}

type markerStore struct{ markers []model.Marker }

func (s *markerStore) RecordMarker(_ context.Context, m model.Marker, _ int) (store.MarkerResult, error) {
	s.markers = append(s.markers, m)
	return store.MarkerStored, nil
}

func markerRequest() *http.Request {
	body := `{"version":1,"emitter":"wiki_gql","kind":"wiki-login","edge":"start",` +
		`"span":"5f0c2b3e9a7d4c1e8b6a2f4d3c1e0b9a","at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	r := httptest.NewRequest(http.MethodPost, marker.Path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(`{}`))+".c2ln")
	return r
}

func TestMarkerIngestRecordsThroughTheRoutes(t *testing.T) {
	env := ingestEnv()
	st := &markerStore{}
	h, err := buildMarkerIngest(func(k string) string { return env[k] }, st, prometheus.NewRegistry(),
		podReviewer(env[kelos.EnvNamespace]))
	if err != nil {
		t.Fatal(err)
	}
	routes := ingestRoutes(ingestUnavailable("test"), h)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, markerRequest())
	if rec.Code != http.StatusAccepted || len(st.markers) != 1 || st.markers[0].Sender.Cluster != env[kelos.EnvCluster] {
		t.Errorf("status %d (%s), markers %+v", rec.Code, rec.Body, st.markers)
	}
	rec = httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, ingest.PathGitHub, nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "webhook") {
		t.Errorf("a webhook: status %d (%s), want the webhooks' handler", rec.Code, rec.Body)
	}
}

func TestMarkerIngestOff(t *testing.T) {
	h, err := newMarkerIngest(emptyEnv, nil, prometheus.NewRegistry(), podReviewer("agents"))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, markerRequest())
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), store.EnvDatabaseURL) {
		t.Errorf("status %d (%s), want 503 naming the database", rec.Code, rec.Body)
	}
}

func TestMarkerIngestErrors(t *testing.T) {
	env := ingestEnv()
	getenv := func(k string) string { return env[k] }
	if _, err := buildMarkerIngest(emptyEnv, &markerStore{}, prometheus.NewRegistry(), podReviewer("agents")); err == nil ||
		!strings.Contains(err.Error(), kelos.EnvCluster) {
		t.Errorf("no Kelos configuration: %v", err)
	}
	failing := func() (dynamic.Interface, error) { return nil, errors.New("no kubeconfig") }
	if _, err := buildMarkerIngest(getenv, &markerStore{}, prometheus.NewRegistry(), failing); err == nil {
		t.Error("a handler was built without a client")
	}
}

// emptyEnv is an environment with nothing set.
func emptyEnv(string) string { return "" }
