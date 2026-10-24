package marker

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// reviewingClient is a fake API server answering each TokenReview with
// status, after checking what it asks.
func reviewingClient(t *testing.T, status map[string]any, err error) *dynamicfake.FakeDynamicClient {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{tokenReviewGVR: "TokenReviewList"})
	client.PrependReactor("create", "tokenreviews", func(a k8stesting.Action) (bool, runtime.Object, error) {
		obj := a.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured)
		token, _, _ := unstructured.NestedString(obj.Object, "spec", "token")
		audiences, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "audiences")
		if token != "the-token" || len(audiences) != 1 || audiences[0] != Audience {
			t.Errorf("review of %q for %q", token, audiences)
		}
		if err != nil {
			return true, nil, err
		}
		out := obj.DeepCopy()
		if status != nil {
			out.Object["status"] = status
		}
		return true, out, nil
	})
	return client
}

func TestKubeReviewerDecodesTheStatus(t *testing.T) {
	client := reviewingClient(t, map[string]any{
		"authenticated": true, "audiences": []any{Audience},
		"user": map[string]any{"username": serviceAccountPrefix + testNamespace + ":default",
			"extra": map[string]any{extraPodName: []any{testPod}, extraPodUID: []any{testPodUID}}},
	}, nil)
	rev, err := NewKubeReviewer(client)
	if err != nil {
		t.Fatal(err)
	}
	st, err := rev.Review(t.Context(), "the-token")
	if err != nil {
		t.Fatal(err)
	}
	a := newAuthenticator(rev, testNamespace, nil)
	id, err := a.identify(st)
	if err != nil || id.pod != testPod || id.podUID != testPodUID || id.serviceAccount != "default" {
		t.Errorf("identity = %+v, %v", id, err)
	}
}

func TestKubeReviewerFails(t *testing.T) {
	rev, err := NewKubeReviewer(reviewingClient(t, nil, errors.New("forbidden")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rev.Review(t.Context(), "the-token"); err == nil {
		t.Error("a refused review succeeded")
	}
	// An answer without a status is a failed review, not a token refused.
	rev, err = NewKubeReviewer(reviewingClient(t, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rev.Review(t.Context(), "the-token"); err == nil {
		t.Error("a review answered without a status succeeded")
	}
	if _, err := NewKubeReviewer(nil); err == nil {
		t.Error("a reviewer without a client was built")
	}
}
