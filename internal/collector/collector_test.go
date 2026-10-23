package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
)

// storyCase is a fixture story assembled from its sources' responses.
type storyCase struct {
	root model.Root
	dirs storyDirs
}

// storyCases are the fixture stories of testdata/fixtures/stories, each
// assembled by the collector from fabricated source responses; each must
// keep the invariant its story exists for.
var storyCases = map[string]storyCase{
	"discuss-no-pr": {taskRoot(101), storyDirs{
		vikunja: "collector/discuss-no-pr/vikunja", kelos: "collector/discuss-no-pr/kelos",
		github: "collector/discuss-no-pr/github", wiki: "collector/discuss-no-pr/wiki",
	}},
	"implementation-merged": {taskRoot(102), storyDirs{
		vikunja: "collector/implementation-merged/vikunja", kelos: "collector/implementation-merged/kelos",
		github: "collector/implementation-merged/github",
	}},
	"wiki-rooted-request": {pageRoot(57), storyDirs{
		kelos: "collector/wiki-rooted-request/kelos", github: "collector/wiki-rooted-request/github",
		wiki: "wiki/live-edit-not-in-trail",
	}},
	"failed-run-retry": {taskRoot(104), storyDirs{
		vikunja: "collector/failed-run-retry/vikunja", kelos: "collector/failed-run-retry/kelos",
		github: "collector/failed-run-retry/github",
	}},
	"announced-run-only": {taskRoot(105), storyDirs{
		vikunja: "collector/announced-run-only/vikunja", kelos: "collector/announced-run-only/kelos",
	}},
}

// collectStory assembles a fixture story, failing on any failed read.
func collectStory(t *testing.T, name string) model.Story {
	t.Helper()
	sc, ok := storyCases[name]
	if !ok {
		t.Fatalf("no story case %s", name)
	}
	res, err := newStoryCollector(t, sc.dirs).Collect(t.Context(), sc.root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, f := range res.Failures {
		t.Errorf("failed read: %v", f)
	}
	return res.Story
}

func TestStoryInvariants(t *testing.T) {
	for name, check := range storytest.Invariants {
		t.Run(name, func(t *testing.T) {
			s := collectStory(t, name)
			check(t, s)
			storytest.RefsResolve(t, s)
		})
	}
}

func TestEveryStoryIsAssembled(t *testing.T) {
	for name := range storytest.Invariants {
		if _, ok := storyCases[name]; !ok {
			t.Errorf("story %s has no case the collector assembles", name)
		}
	}
}
