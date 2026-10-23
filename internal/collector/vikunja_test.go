package collector

import (
	"errors"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// bucketDetail is the detail the Vikunja source names a bucket by.
const bucketDetail = "bucket"

// vikunjaStory assembles the story of task snap from snap alone.
func vikunjaStory(t *testing.T, snap vikunja.Snapshot) Result {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{snap}
	return collectWith(t, testConfig(), src, taskRoot(snap.Task.ID))
}

func TestBucketsOfTwoViewsStayApart(t *testing.T) {
	snap := announcedTask(104, "", nil)
	snap.Task.Buckets = []vikunja.Bucket{
		{ID: 3, Title: "Done", ProjectViewID: 4}, {ID: 9, Title: "Done", ProjectViewID: 5},
	}
	keys := make(map[string]bool)
	for _, e := range vikunjaStory(t, snap).Story.Events {
		if e.Kind == model.KindTaskBucketObserved && e.Details[bucketDetail] == "Done" {
			keys[e.Key()] = true
		}
	}
	if len(keys) != len(snap.Task.Buckets) {
		t.Errorf("Done observed as %d events, want one per view: %v", len(keys), keys)
	}
}

func TestVikunjaEventsNameTheCollectorsInstance(t *testing.T) {
	snap := announcedTask(104, "", nil)
	// The client names the instance by the URL's host, port included; the
	// collector's task keys name it by host alone.
	snap.Instance = testVikunjaHost + ":8443"
	root := taskRoot(104)
	for _, e := range vikunjaStory(t, snap).Story.Events {
		if e.Source != model.SourceVikunja {
			continue
		}
		if e.Instance != testVikunjaHost || e.Evidence.Instance != testVikunjaHost {
			t.Errorf("event %s on %q, evidence on %q, want %q", e.ID, e.Instance, e.Evidence.Instance, testVikunjaHost)
		}
		if e.Related.Task == nil || *e.Related.Task != *root.Task {
			t.Errorf("event %s related to %v, want %s", e.ID, e.Related.Task, root.Task)
		}
	}
}

func TestVikunjaAccountKinds(t *testing.T) {
	snap := announcedTask(104, "vikunja-widgets-agent-opus-104", map[int]string{1: "vikunja-widgets-agent-opus-104-k2m4p"})
	snap.Comments = append(snap.Comments,
		vikunja.Comment{ID: 1, Author: vikunja.User{ID: 7, Username: "owner"}, Created: at(2), Body: "<p>Thanks.</p>"},
		vikunja.Comment{ID: 2, Author: vikunja.User{ID: 21, Username: testAgent}, Created: at(3), Body: "<p>Done.</p>"},
	)
	want := map[string]model.ActorKind{
		"tasks/104":     model.ActorPerson,
		"comments/1":    model.ActorPerson,
		"comments/2":    model.ActorAgent,
		"comments/9001": model.ActorService,
	}
	got := make(map[string]model.ActorKind)
	for _, e := range vikunjaStory(t, snap).Story.Events {
		if _, ok := want[e.ID]; ok {
			got[e.ID] = e.Actor.Kind
		}
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("event %s actor kind = %q, want %q", id, got[id], kind)
		}
	}
}

func TestUnnormalizableTaskReadFails(t *testing.T) {
	snap := announcedTask(104, "", nil)
	// Without the time of the read, no event of it is valid.
	snap.ObservedAt = time.Time{}
	res := vikunjaStory(t, snap)
	if len(res.Failures) != 1 || res.Failures[0].Source != model.SourceVikunja || errors.Is(res.Failures[0], errDown) {
		t.Errorf("failures = %v, want the Vikunja normalization's", res.Failures)
	}
	if cov := coverageOf(t, res.Story, model.SourceVikunja); cov.LastSuccessfulRead != nil {
		t.Errorf("vikunja coverage = %+v, want no successful read", cov)
	}
	for _, e := range res.Story.Events {
		if e.Source == model.SourceVikunja {
			t.Errorf("event %s kept from a read the source could not normalize", e.Key())
		}
	}
	if cov := coverageOf(t, res.Story, model.SourceWiki); !hasNote(cov, "could not be read") {
		t.Errorf("wiki coverage = %+v, want the linked pages unknown", cov)
	}
}
