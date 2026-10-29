package main

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

// The loop matches a hook's attempt against the stored deliveries by its
// guid, and reads its payload's roots as the ingest path does.
func TestReceipts(t *testing.T) {
	st := newTestStore(t)
	cfg := collector.Config{VikunjaInstance: loopVikunjaHost, GitHubInstance: "github.example.com"}
	r := receipts{st: st, cfg: cfg}
	d := model.Delivery{
		Source: model.SourceGitHub, Instance: cfg.GitHubInstance, ID: "guid-accepted", Event: "pull_request",
		ReceivedAt: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC), Outcome: model.DeliveryAccepted,
	}
	if err := st.AcceptDelivery(t.Context(), d, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	failed := d
	failed.ID, failed.Outcome, failed.Detail = "guid-failed", model.DeliveryFailed, "the queue is full"
	if err := st.RecordDelivery(t.Context(), failed); err != nil {
		t.Fatal(err)
	}
	for guid, want := range map[string]bool{"guid-accepted": true, "guid-failed": false, "guid-unseen": false} {
		if got, err := r.Accepted(t.Context(), guid); err != nil || got != want {
			t.Errorf("Accepted(%s) = %v, %v; want %v", guid, got, err, want)
		}
	}
	roots, err := r.Roots(t.Context(), "pull_request",
		[]byte(`{"pull_request":{"number":3,"head":{"ref":"kelos/vikunja-42"}},"repository":{"full_name":"acme/widgets"}}`))
	if err != nil || len(roots) != 1 || roots[0].Task == nil || roots[0].Task.ID != 42 {
		t.Errorf("Roots() = %+v, %v; want task 42's root", roots, err)
	}
}
