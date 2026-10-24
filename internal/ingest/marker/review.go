package marker

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// tokenReviewGVR is the TokenReview resource: a create asks the API server
// about a token, and stores nothing.
var tokenReviewGVR = schema.GroupVersionResource{Group: "authentication.k8s.io", Version: "v1", Resource: "tokenreviews"}

// kubeReviewer reviews tokens through the API server. Its credentials need
// create on tokenreviews (system:auth-delegator), and nothing else.
type kubeReviewer struct{ client dynamic.Interface }

// NewKubeReviewer returns the Reviewer that asks client's API server.
func NewKubeReviewer(client dynamic.Interface) (Reviewer, error) {
	if client == nil {
		return nil, errors.New("marker: a TokenReview needs a client")
	}
	return kubeReviewer{client: client}, nil
}

// Review creates a TokenReview of token for Audience and returns its
// status.
func (k kubeReviewer) Review(ctx context.Context, token string) (ReviewStatus, error) {
	review := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": tokenReviewGVR.GroupVersion().String(),
		"kind":       "TokenReview",
		"spec":       map[string]any{"token": token, "audiences": []any{Audience}},
	}}
	out, err := k.client.Resource(tokenReviewGVR).Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return ReviewStatus{}, fmt.Errorf("marker: token review: %w", err)
	}
	status, found, err := unstructured.NestedMap(out.Object, "status")
	if err != nil {
		return ReviewStatus{}, fmt.Errorf("marker: token review status: %w", err)
	}
	if !found {
		return ReviewStatus{}, errors.New("marker: token review: the API server answered no status")
	}
	var st ReviewStatus
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(status, &st); err != nil {
		return ReviewStatus{}, fmt.Errorf("marker: token review status: %w", err)
	}
	return st, nil
}
