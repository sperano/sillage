package ingest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// MaxGitHubBodyBytes bounds a GitHub delivery's body: GitHub caps its
	// payloads at 25 MB, and so does the proxy in front of sillage.
	MaxGitHubBodyBytes = 25 << 20
	// MaxVikunjaBodyBytes bounds a Vikunja delivery's body: a task with
	// its description, or a comment, and the accounts that made them.
	MaxVikunjaBodyBytes = 5 << 20
	// maxBodiesInFlight bounds how many bodies are read at once, so that
	// requests nobody authenticated yet hold at most this many bodies in
	// memory.
	maxBodiesInFlight = 4
	// slotWait is how long a request waits for a body to be read before it
	// is refused as busy: a burst of deliveries waits, a flood does not.
	slotWait = 2 * time.Second
	// bodyReadTimeout bounds reading a body, so that a client sending it
	// slowly holds a slot no longer: GitHub gives up on a delivery after
	// 10 seconds, so reading longer only serves a slow client.
	bodyReadTimeout = 10 * time.Second
)

var (
	errBusy       = errors.New("too many deliveries are being read: try again")
	errNoSecret   = errors.New("this source's deliveries are not accepted")
	errNotJSON    = errors.New("the body is not a JSON payload (the webhook must send application/json)")
	errUnreadable = errors.New("the body could not be read")
)

// refusal is a request refused before it is known to be a delivery, with
// the status that answers it.
type refusal struct {
	status int
	err    error
	// busy is a refusal for want of a slot, before the request was read:
	// it says nothing of the request's authenticity.
	busy bool
}

func (r *refusal) Error() string { return r.err.Error() }

func (r *refusal) Unwrap() error { return r.err }

// asRefusal is err as a refusal; any error not one is a bad request.
func asRefusal(err error) *refusal {
	var rf *refusal
	if errors.As(err, &rf) {
		return rf
	}
	return &refusal{status: http.StatusBadRequest, err: err}
}

// storeError is a store operation that failed while settling a delivery.
type storeError struct {
	op  string
	err error
}

func (e *storeError) Error() string { return e.op + ": " + e.err.Error() }

func (e *storeError) Unwrap() error { return e.err }

// authenticate reads the body of a request to ep and checks its signature.
// Otherwise it returns a refusal.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, ep endpoint) ([]byte, error) {
	if ep.secret == "" {
		return nil, &refusal{status: http.StatusForbidden, err: errNoSecret}
	}
	wait := time.NewTimer(slotWait)
	defer wait.Stop()
	select {
	case h.bodies <- struct{}{}:
		defer func() { <-h.bodies }()
	case <-wait.C:
		return nil, &refusal{status: http.StatusServiceUnavailable, err: errBusy, busy: true}
	case <-r.Context().Done():
		return nil, &refusal{status: http.StatusServiceUnavailable, err: errBusy, busy: true}
	}
	body, err := readBody(w, r, ep.maxBody)
	if err != nil {
		return nil, err
	}
	prefix, signature := ep.signature(r.Header)
	if err := verify(ep.secret, prefix, signature, body); err != nil {
		return nil, &refusal{status: http.StatusUnauthorized, err: err}
	}
	return body, nil
}

// readBody reads r's body, at most limit bytes within bodyReadTimeout.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	tooLarge := &refusal{status: http.StatusRequestEntityTooLarge, err: fmt.Errorf("the body is larger than %d bytes", limit)}
	if r.ContentLength > limit {
		return nil, tooLarge
	}
	// A writer that cannot set deadlines (a test recorder) has its
	// server's timeouts only.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		return nil, tooLarge
	case err != nil:
		return nil, &refusal{status: http.StatusBadRequest, err: errUnreadable}
	}
	return body, nil
}
