package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sperano/sillage/internal/store"
)

const (
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"
	// problemType is RFC 9457's type for a problem its status explains.
	problemType = "about:blank"
	// allowedMethods are the only methods the API serves.
	allowedMethods = "GET, HEAD"
)

// Problem is the body of every error response (RFC 9457).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// problemError is an error with the status and detail to answer it with.
type problemError struct {
	status int
	detail string
}

func (e *problemError) Error() string { return e.detail }

func problem(status int, detail string) error { return &problemError{status: status, detail: detail} }

func badRequest(format string, args ...any) error {
	return problem(http.StatusBadRequest, fmt.Sprintf(format, args...))
}

// errMethod answers a method the API never serves.
var errMethod = problem(http.StatusMethodNotAllowed, "the API is read-only: it serves "+allowedMethods)

// errTooLarge stops encoding a response past the size bound.
var errTooLarge = errors.New("response too large")

// problemOf is the status and detail err is answered with. Errors this
// package did not make are logged and answered without their text, which
// may name the database.
func problemOf(r *http.Request, err error) (int, string) {
	var pe *problemError
	switch {
	case errors.As(err, &pe):
		return pe.status, pe.detail
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "no story is stored for it"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "the request took longer than the API allows"
	}
	slog.Error("api request failed", "path", r.URL.Path, "error", err)
	return http.StatusInternalServerError, "reading the store failed"
}

func (a *api) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		return // the client is gone
	}
	status, detail := problemOf(r, err)
	if status == http.StatusMethodNotAllowed {
		w.Header().Set("Allow", allowedMethods)
	}
	body, _ := json.Marshal(Problem{Type: problemType, Title: http.StatusText(status), Status: status, Detail: detail})
	write(w, status, contentTypeProblem, append(body, '\n'))
}

// writeJSON sends v, or a 500 when its encoding exceeds the size bound:
// a story is never cut short.
func (a *api) writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	buf := &cappedBuffer{max: a.cfg.MaxResponseBytes}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		if errors.Is(err, errTooLarge) {
			err = problem(http.StatusInternalServerError,
				"the response exceeds the API's bound of "+strconv.Itoa(a.cfg.MaxResponseBytes)+" bytes")
		}
		a.writeProblem(w, r, err)
		return
	}
	write(w, http.StatusOK, contentTypeJSON, buf.Bytes())
}

func write(w http.ResponseWriter, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// cappedBuffer is a buffer that refuses to grow past max bytes.
type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errTooLarge
	}
	return b.Buffer.Write(p)
}
