package ingest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sperano/sillage/internal/model"
)

var errTooLarge = fmt.Errorf("the body is larger than %d bytes", MaxBodyBytes)

// readBody reads r's body, at most MaxBodyBytes within bodyReadTimeout,
// or returns the status that refuses it.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, error) {
	if r.ContentLength > MaxBodyBytes {
		return nil, http.StatusRequestEntityTooLarge, errTooLarge
	}
	// A writer that cannot set deadlines (a test recorder) has its
	// server's timeouts only.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if mbe := (*http.MaxBytesError)(nil); errors.As(err, &mbe) {
		return nil, http.StatusRequestEntityTooLarge, errTooLarge
	}
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("the body could not be read")
	}
	return body, 0, nil
}

// rootSet collects roots, each once, in the order first added.
type rootSet struct {
	roots []model.Root
	seen  map[model.Ref]bool
}

func (s *rootSet) add(root model.Root) {
	ref, ok := refOf(root)
	if !ok || s.seen[ref] {
		return
	}
	if s.seen == nil {
		s.seen = make(map[model.Ref]bool)
	}
	s.seen[ref] = true
	s.roots = append(s.roots, root)
}

// refOf is the reference of a valid root's key.
func refOf(root model.Root) (model.Ref, bool) {
	if root.Validate() != nil {
		return model.Ref{}, false
	}
	if root.Task != nil {
		return root.Task.Ref(), true
	}
	return root.Page.Ref(), true
}
