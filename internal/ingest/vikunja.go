package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/sperano/sillage/internal/model"
)

// vikunjaSignatureHeader carries the hex HMAC-SHA256 of the body, with no
// prefix (Vikunja's pkg/models/webhooks.go).
const vikunjaSignatureHeader = "X-Vikunja-Signature"

// vikunjaPayload is the part of a Vikunja delivery sillage reads: its
// event, and the task its data names, if any (every task.* event's does).
type vikunjaPayload struct {
	EventName string `json:"event_name"`
	Data      struct {
		Task *struct {
			ID int64 `json:"id"`
		} `json:"task"`
	} `json:"data"`
}

// vikunjaParser reads the deliveries of the Vikunja instance.
type vikunjaParser struct {
	instance string
}

func (vikunjaParser) signature(h http.Header) (prefix, value string) {
	return "", h.Get(vikunjaSignatureHeader)
}

// identify names a delivery by the SHA-256 of its body: Vikunja sends no
// delivery id, and its body holds the time it was sent, which a retry of
// the same send keeps.
func (vikunjaParser) identify(_ http.Header, body []byte) (id, event string, err error) {
	sum := sha256.Sum256(body)
	var p vikunjaPayload
	if json.Unmarshal(body, &p) == nil {
		event = p.EventName
	}
	return hex.EncodeToString(sum[:]), event, nil
}

// roots returns the task the delivery names.
func (v vikunjaParser) roots(_ context.Context, _ string, body []byte) ([]model.Root, string, error) {
	var p vikunjaPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", errNotJSON
	}
	if p.Data.Task == nil || p.Data.Task.ID <= 0 {
		return nil, "the event names no task", nil
	}
	return []model.Root{{Task: &model.TaskKey{Instance: v.instance, ID: p.Data.Task.ID}}}, "", nil
}
