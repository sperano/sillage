package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

func TestBucketsOfTwoViewsStayApart(t *testing.T) {
	snap := announcedTask(104, "", nil)
	snap.Task.Buckets = []vikunja.Bucket{
		{ID: 3, Title: "Done", ProjectViewID: 4}, {ID: 9, Title: "Done", ProjectViewID: 5},
	}
	src := emptySources()
	src.Vikunja = fixedVikunja{snap}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	var views []string
	for _, e := range s.Events {
		if e.Kind == model.KindTaskBucketObserved && e.Details[detailBucket] == "Done" {
			views = append(views, e.Details[detailView])
		}
	}
	slices.Sort(views)
	if want := []string{"4", "5"}; !slices.Equal(views, want) {
		t.Errorf("Done observed in views %v, want %v", views, want)
	}
}
