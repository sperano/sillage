package reconcile

import (
	"context"
	"errors"
	"net"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// The reasons a read fails, as metrics and gaps name them.
const (
	reasonTimeout     = "timeout"
	reasonRateLimited = "rate_limited"
	reasonServerError = "server_error"
	// reasonFailed is any other failure: a refused token, a response that
	// does not decode, a record the source's normalization rejects.
	reasonFailed = "failed"
	// serverErrorStatus is the first HTTP status of a server error.
	serverErrorStatus = http.StatusInternalServerError
)

// reasonOf classifies a failed read; "" for none, or for one the shutdown
// canceled, which is no failure of the source.
func reasonOf(err error) string {
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return ""
	case timedOut(err):
		return reasonTimeout
	}
	if _, ok := errors.AsType[*github.RateLimitError](err); ok {
		return reasonRateLimited
	}
	switch code := statusOf(err); {
	case code == http.StatusRequestTimeout:
		return reasonTimeout
	case code == http.StatusTooManyRequests:
		return reasonRateLimited
	case code >= serverErrorStatus:
		return reasonServerError
	}
	return reasonFailed
}

// timedOut reports whether err is a deadline or a network timeout.
func timedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return true
	}
	ne, ok := errors.AsType[net.Error](err)
	return ok && ne.Timeout()
}

// statusOf is the HTTP status of a source's error response; 0 for none.
func statusOf(err error) int {
	if e, ok := errors.AsType[*vikunja.StatusError](err); ok {
		return e.Code
	}
	if e, ok := errors.AsType[*github.StatusError](err); ok {
		return e.Code
	}
	if e, ok := errors.AsType[*wiki.StatusError](err); ok {
		return e.Code
	}
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		return int(status.Status().Code)
	}
	return 0
}
