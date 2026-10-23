package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
	"github.com/sperano/sillage/internal/source/github"
)

// storyCase is a fixture story assembled from its sources' responses.
type storyCase struct {
	root  model.Root
	dirs  storyDirs
	repos []github.Repository
}

// storyCases are the fixture stories of testdata/fixtures/stories, each
// assembled by the collector from fabricated source responses; each must
// keep the invariant its story exists for.
var storyCases = map[string]storyCase{
	"discuss-no-pr": {taskRoot(101), storyDirs{
		vikunja: "collector/discuss-no-pr/vikunja", kelos: "collector/discuss-no-pr/kelos",
		github: "collector/discuss-no-pr/github", wiki: "collector/discuss-no-pr/wiki",
	}, nil},
	"implementation-merged": {taskRoot(102), storyDirs{
		vikunja: "collector/implementation-merged/vikunja", kelos: "collector/implementation-merged/kelos",
		github: "collector/implementation-merged/github",
	}, nil},
	"wiki-rooted-request": {pageRoot(57), storyDirs{
		kelos: "collector/wiki-rooted-request/kelos", github: "collector/wiki-rooted-request/github",
		wiki: "wiki/live-edit-not-in-trail",
	}, nil},
	"failed-run-retry": {taskRoot(104), storyDirs{
		vikunja: "collector/failed-run-retry/vikunja", kelos: "collector/failed-run-retry/kelos",
		github: "collector/failed-run-retry/github",
	}, nil},
	"announced-run-only": {taskRoot(105), storyDirs{
		vikunja: "collector/announced-run-only/vikunja", kelos: "collector/announced-run-only/kelos",
	}, nil},
	"checks-after-force-push": {taskRoot(106), storyDirs{
		vikunja: "collector/checks-after-force-push/vikunja",
		github:  "collector/checks-after-force-push/github",
	}, []github.Repository{testRepo}},
	"reopened-task": {taskRoot(107), storyDirs{
		vikunja: "collector/reopened-task/vikunja",
	}, nil},
	"second-pr-after-close": {taskRoot(108), storyDirs{
		vikunja: "collector/second-pr-after-close/vikunja",
		github:  "collector/second-pr-after-close/github",
	}, []github.Repository{testRepo}},
}

// collectStory assembles a fixture story, failing on any failed read.
func collectStory(t *testing.T, name string) model.Story {
	t.Helper()
	sc, ok := storyCases[name]
	if !ok {
		t.Fatalf("no story case %s", name)
	}
	res, err := newStoryCollector(t, sc.dirs).Collect(t.Context(), sc.root, sc.repos...)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, f := range res.Failures {
		t.Errorf("failed read: %v", f)
	}
	return res.Story
}

func TestStoryInvariants(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			s := collectStory(t, name)
			storytest.Check(t, name, s)
			storytest.RefsResolve(t, s)
		})
	}
}

func TestEveryStoryIsAssembled(t *testing.T) {
	for _, name := range storytest.Names() {
		if _, ok := storyCases[name]; !ok {
			t.Errorf("story %s has no case the collector assembles", name)
		}
	}
}
