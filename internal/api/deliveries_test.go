package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// TestRootDeliveries serves each fixture story's deliveries, an empty list
// for a story without any.
func TestRootDeliveries(t *testing.T) {
	fixtures := newFakeStore(t)
	h := New(Config{Store: fixtures})
	withSome := 0
	for _, f := range fixtures.stories {
		path := strings.Replace(storyPath(f.story.Root), "/stories/", "/deliveries/", 1)
		var got DeliveriesResponse
		decode(t, get(t, h, path), &got)
		if got.Root.Task == nil || got.Root.Task.ID != f.story.Root.Task.ID || got.Deliveries == nil || got.Truncated {
			t.Errorf("%s: %+v, want the root, a list and nothing truncated", f.name, got)
		}
		if !slices.EqualFunc(got.Deliveries, f.deliveries, sameDelivery) {
			t.Errorf("%s: deliveries = %+v, want %+v", f.name, got.Deliveries, f.deliveries)
		}
		if len(f.deliveries) > 0 {
			withSome++
		}
	}
	if withSome == 0 {
		t.Error("no fixture story has deliveries")
	}
}

func sameDelivery(a, b model.RootDelivery) bool {
	sameRead := (a.NextRead == nil) == (b.NextRead == nil) && (a.NextRead == nil || a.NextRead.Equal(*b.NextRead))
	return a.ID == b.ID && a.ReceivedAt.Equal(b.ReceivedAt) && a.Outcome == b.Outcome && sameRead
}

// TestRootDeliveriesTruncated lists the latest MaxDeliveries of more, and
// says so.
func TestRootDeliveriesTruncated(t *testing.T) {
	fixtures := newFakeStore(t)
	f := &fixtures.stories[0]
	base := f.story.Coverage[0].LastSuccessfulRead
	if base == nil {
		t.Fatalf("%s: no read to date the deliveries by", f.name)
	}
	f.deliveries = nil
	for i := range MaxDeliveries + 1 {
		f.deliveries = append(f.deliveries, model.RootDelivery{Delivery: model.Delivery{
			Source: model.SourceGitHub, Instance: "github.example.com", ID: fmt.Sprintf("d-%d", i),
			ReceivedAt: base.Add(time.Duration(i) * time.Second), Outcome: model.DeliveryAccepted,
		}})
	}
	var got DeliveriesResponse
	decode(t, get(t, New(Config{Store: fixtures}), strings.Replace(storyPath(f.story.Root), "/stories/", "/deliveries/", 1)), &got)
	if !got.Truncated || len(got.Deliveries) != MaxDeliveries || got.Deliveries[0].ID != "d-1" {
		t.Errorf("truncated %v, %d deliveries from %q; want the latest %d, truncated", got.Truncated,
			len(got.Deliveries), got.Deliveries[0].ID, MaxDeliveries)
	}
}

func TestRootDeliveriesProblems(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	wantProblem(t, get(t, h, "/api/v1/deliveries/task/vikunja.example.com/999"), http.StatusNotFound)
	wantProblem(t, get(t, h, "/api/v1/deliveries/task/vikunja.example.com/abc"), http.StatusBadRequest)
	wantProblem(t, get(t, New(Config{}), "/api/v1/deliveries/task/vikunja.example.com/101"),
		http.StatusServiceUnavailable)
}
