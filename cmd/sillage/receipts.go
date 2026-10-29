package main

import (
	"context"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/store"
)

// receipts are the GitHub deliveries the store recorded, and the roots a
// delivery names as POST /ingest/github reads them, for the collection
// loop's look at the hooks' delivery logs.
type receipts struct {
	st  *store.Store
	cfg collector.Config
}

var _ reconcile.Receipts = receipts{}

// Accepted reports whether an attempt of the GitHub instance's delivery
// guid was accepted.
func (r receipts) Accepted(ctx context.Context, guid string) (bool, error) {
	return r.st.DeliveryAccepted(ctx, model.SourceGitHub, r.cfg.GitHubInstance, guid)
}

// Roots returns the roots a delivery of event with payload names.
func (r receipts) Roots(ctx context.Context, event string, payload []byte) ([]model.Root, error) {
	return ingest.GitHubRoots(ctx, r.cfg, r.st, event, payload)
}
