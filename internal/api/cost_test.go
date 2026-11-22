package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/cost"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const mixedModelsCost = "/api/v1/cost/task/vikunja.example.com/301"

func TestTaskCostReportsTheFeed(t *testing.T) {
	since := time.Date(2026, time.November, 22, 10, 0, 0, 0, time.FixedZone("UTC-8", -8*60*60))
	var on cost.TaskCost
	decode(t, get(t, New(Config{Store: newFakeStore(t), Usage: UsageConfig{On: true, Since: since}}), mixedModelsCost), &on)
	if !on.Feed.On || on.Feed.Since == nil || !on.Feed.Since.Equal(since) || on.Feed.Since.Location() != time.UTC ||
		on.Feed.ReadThrough == nil || !on.Final || on.Total.Status != cost.StatusPartial || on.Total.EstimatedUSD != "18.886400" {
		t.Errorf("feed on: got feed %+v final %v total %s %q; want the feed on since %v in UTC, final, partial 18.886400",
			on.Feed, on.Final, on.Total.Status, on.Total.EstimatedUSD, since)
	}
	var off cost.TaskCost
	decode(t, get(t, New(Config{Store: newFakeStore(t)}), mixedModelsCost), &off)
	if off.Feed.On || off.Feed.Since != nil || !slices.Contains(off.Total.Notes, cost.NoteFeedOff) {
		t.Errorf("feed off: got feed %+v notes %v; want it off, without a start, and noted", off.Feed, off.Total.Notes)
	}
}

func TestTaskCostProblems(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	wantProblem(t, get(t, h, "/api/v1/cost/task/vikunja.example.com/999"), http.StatusNotFound)
	wantProblem(t, get(t, h, "/api/v1/cost/task/vikunja.example.com/not-a-number"), http.StatusBadRequest)
}

// usageFailingStore serves the fixture stories and fails every usage read.
type usageFailingStore struct {
	*fakeStore
	err error
}

func (u usageFailingStore) UsageOf(context.Context, model.Story) (store.StoryUsage, error) {
	return store.StoryUsage{}, u.err
}

func TestTaskCostUsageFailureHidesItsError(t *testing.T) {
	secret := "reading usage_events on db.example.com"
	rec := get(t, New(Config{Store: usageFailingStore{newFakeStore(t), errors.New(secret)}}), mixedModelsCost)
	wantProblem(t, rec, http.StatusInternalServerError)
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("body %s quotes the store's error", rec.Body)
	}
}
