package collector

import (
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
	ids := make(map[string]bool)
	for _, e := range s.Events {
		if e.Kind == model.KindTaskBucketObserved && e.Details[detailBucket] == "Done" {
			ids[e.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Errorf("Done observed as %d events, want one per view", len(ids))
	}
}

func TestAgentCommentsHaveAnAgentActor(t *testing.T) {
	snap := announcedTask(104, "", nil)
	snap.Comments = append(snap.Comments, vikunja.Comment{
		ID: 9001, Body: "<p>Done.</p>", Author: vikunja.User{ID: 21, Username: testAgent},
		Created: snap.Task.Created, Updated: snap.Task.Created,
	})
	src := emptySources()
	src.Vikunja = fixedVikunja{snap}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	found := false
	for _, e := range s.Events {
		if e.ID != commentObject(9001) {
			continue
		}
		found = true
		if e.Actor != (model.Account{Login: testAgent, Kind: model.ActorAgent}) {
			t.Errorf("comment actor = %+v, want the agent %s", e.Actor, testAgent)
		}
	}
	if !found {
		t.Error("the agent's comment is not in the story")
	}
}
