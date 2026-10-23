package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/sperano/sillage/internal/store"
)

const (
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"
	// problemType is RFC 9457's type for a problem its status explains.
	problemType = "about:blank"
	// allowedMethods are the only methods the API serves.
	allowedMethods = "GET, HEAD"
	// maxEchoed bounds the request input a problem's detail quotes, in
	// bytes, so that every problem body fits in MinResponseBytes.
	maxEchoed = 128
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

// echo quotes request input for a problem's detail, cut to maxEchoed
// bytes.
func echo(s string) string {
	if len(s) <= maxEchoed {
		return strconv.Quote(s)
	}
	cut := maxEchoed
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strconv.Quote(s[:cut]) + "..."
}

// errMethod answers a method the API never serves.
var errMethod = problem(http.StatusMethodNotAllowed, "the API is read-only: it serves "+allowedMethods)

// problemOf is the status and detail err is answered with. Errors this
// package did not make are logged and answered without their text, which
// may name the database.
func problemOf(path string, err error) (int, string) {
	var pe *problemError
	switch {
	case errors.As(err, &pe):
		return pe.status, pe.detail
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "no story is stored for it"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "the request took longer than the API allows"
	}
	slog.Error("api request failed", "path", path, "error", err)
	return http.StatusInternalServerError, "reading the store failed"
}

func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		return // the client is gone
	}
	status, detail := problemOf(r.URL.Path, err)
	if status == http.StatusMethodNotAllowed {
		w.Header().Set("Allow", allowedMethods)
	}
	body, _ := json.Marshal(Problem{Type: problemType, Title: http.StatusText(status), Status: status, Detail: detail})
	write(w, status, contentTypeProblem, append(body, '\n'))
}

// writeJSON sends v, or a 500 when its encoding exceeds the size bound:
// a story is never cut short.
func (a *api) writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("api: encoding the response failed", "path", r.URL.Path, "error", err)
		writeProblem(w, r, problem(http.StatusInternalServerError, "encoding the response failed"))
		return
	}
	if len(body) > a.cfg.MaxResponseBytes {
		slog.Error("api: response exceeds the bound", "path", r.URL.Path, "bytes", len(body),
			"bound", a.cfg.MaxResponseBytes)
		writeProblem(w, r, problem(http.StatusInternalServerError,
			"the response exceeds the API's bound of "+strconv.Itoa(a.cfg.MaxResponseBytes)+" bytes"))
		return
	}
	write(w, http.StatusOK, contentTypeJSON, body)
}

func write(w http.ResponseWriter, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
