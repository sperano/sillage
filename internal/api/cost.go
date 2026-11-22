package api

import (
	"net/http"

	"github.com/sperano/sillage/internal/cost"
)

// taskCost answers a task's estimated API-equivalent cost: its story's
// runs, priced from the usage events the store holds of them.
func (a *api) taskCost(r *http.Request) (any, error) {
	root, err := taskRoot(r)
	if err != nil {
		return nil, err
	}
	story, err := a.cfg.Store.Story(r.Context(), root)
	if err != nil {
		return nil, err
	}
	u, err := a.cfg.Store.UsageOf(r.Context(), story)
	if err != nil {
		return nil, err
	}
	feed := cost.Feed{On: a.cfg.Usage.On, ReadThrough: u.ReadThrough}
	if since := a.cfg.Usage.Since; !since.IsZero() {
		since = since.UTC()
		feed.Since = &since
	}
	return cost.Compute(a.prices, cost.Input{Story: story, Events: u.Events, Pods: u.Pods, Feed: feed}), nil
}
