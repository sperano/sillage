package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

func TestReasonOf(t *testing.T) {
	tasks := schema.GroupResource{Group: "kelos.dev", Resource: "tasks"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"none", nil, ""},
		{"shutdown", fmt.Errorf("vikunja: GET /x: %w", context.Canceled), ""},
		{"deadline", fmt.Errorf("vikunja: GET /x: %w", context.DeadlineExceeded), reasonTimeout},
		{"network timeout", fmt.Errorf("wiki: %w", os.ErrDeadlineExceeded), reasonTimeout},
		{"kubernetes timeout", apierrors.NewTimeoutError("slow", 1), reasonTimeout},
		{"408", &wiki.StatusError{Code: http.StatusRequestTimeout}, reasonTimeout},
		{"github rate limit", &github.RateLimitError{Path: "/x", Wait: time.Hour}, reasonRateLimited},
		{"429", &vikunja.StatusError{Code: http.StatusTooManyRequests}, reasonRateLimited},
		{"kubernetes 429", apierrors.NewTooManyRequests("busy", 1), reasonRateLimited},
		{"vikunja 503", &vikunja.StatusError{Code: http.StatusServiceUnavailable}, reasonServerError},
		{"github 502", &github.StatusError{Code: http.StatusBadGateway}, reasonServerError},
		{"wiki 500", &wiki.StatusError{Code: http.StatusInternalServerError}, reasonServerError},
		{"kubernetes 500", apierrors.NewInternalError(errors.New("boom")), reasonServerError},
		{"401", &github.StatusError{Code: http.StatusUnauthorized}, reasonFailed},
		{"kubernetes forbidden", apierrors.NewForbidden(tasks, "", errors.New("no")), reasonFailed},
		{"other", errors.New("decoding"), reasonFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasonOf(tt.err); got != tt.want {
				t.Errorf("reasonOf(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
