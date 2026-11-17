package api

import (
	"net/http"

	"github.com/sperano/sillage/internal/model"
)

// MaxDeliveries caps the deliveries a root's response lists: the latest
// ones.
const MaxDeliveries = 1000

// DeliveriesResponse lists the webhook delivery attempts sillage received
// that named a root, oldest first, each with the root's next read from its
// source. They are evidence that sillage received them, not that Kelos or
// any other subscriber received its own delivery, nor of what they caused:
// they are apart from the story, whose records come from reading the
// sources.
type DeliveriesResponse struct {
	Root       model.Root           `json:"root"`
	Deliveries []model.RootDelivery `json:"deliveries"`
	// Truncated says that more deliveries named the root than the
	// MaxDeliveries latest listed.
	Truncated bool `json:"truncated,omitempty"`
}

func (a *api) taskDeliveries(r *http.Request) (any, error) {
	root, err := taskRoot(r)
	if err != nil {
		return nil, err
	}
	// One more than listed tells whether any was left out.
	ds, err := a.cfg.Store.Deliveries(r.Context(), root, MaxDeliveries+1)
	if err != nil {
		return nil, err
	}
	truncated := len(ds) > MaxDeliveries
	if truncated {
		ds = ds[len(ds)-MaxDeliveries:]
	}
	return DeliveriesResponse{Root: root, Deliveries: nonNil(ds), Truncated: truncated}, nil
}
