package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestVikunjaIdentify(t *testing.T) {
	v := vikunjaDeliveries{instance: testVikunja}
	body := []byte(vikunjaCommentBody)
	id, event, err := v.identify(nil, body)
	sum := sha256.Sum256(body)
	if err != nil || id != hex.EncodeToString(sum[:]) || event != "task.comment.created" {
		t.Errorf("identify() = %q, %q, %v; want the body's SHA-256 and its event", id, event, err)
	}
	other, _, _ := v.identify(nil, []byte(`{"event_name":"task.comment.created","time":"2026-03-01T12:00:01Z"}`))
	if other == id {
		t.Error("two sends got one id")
	}
	if _, event, err := v.identify(nil, []byte("not json")); err != nil || event != "" {
		t.Errorf("identify(not json) = %q, %v; want no event and no error", event, err)
	}
}

func TestVikunjaRoots(t *testing.T) {
	v := vikunjaDeliveries{instance: testVikunja}
	tests := []struct {
		name string
		body string
		want []model.Root
		err  error
	}{
		{"comment", vikunjaCommentBody, []model.Root{taskRoot(42)}, nil},
		{"task updated", `{"event_name":"task.updated","data":{"task":{"id":9},"doer":{"id":1}}}`,
			[]model.Root{taskRoot(9)}, nil},
		{"project", `{"event_name":"project.updated","data":{"project":{"id":3}}}`, nil, nil},
		{"zero task", `{"event_name":"task.updated","data":{"task":{"id":0}}}`, nil, nil},
		{"not json", `payload=x`, nil, errNotJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, why, err := v.roots(t.Context(), "", []byte(tt.body))
			if !errors.Is(err, tt.err) {
				t.Fatalf("roots() error = %v, want %v", err, tt.err)
			}
			if !slices.Equal(refs(got), refs(tt.want)) {
				t.Errorf("roots() = %q, want %q", refs(got), refs(tt.want))
			}
			if err == nil && len(got) == 0 && why == "" {
				t.Error("no root and no reason")
			}
		})
	}
}
