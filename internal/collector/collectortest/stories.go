package collectortest

import (
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/wiki"
)

// Story is a fixture story's root, the responses the collector assembles
// it from, and the repositories asked for besides those its runs name.
type Story struct {
	Root  model.Root
	Dirs  Dirs
	Repos []github.Repository
}

// Stories are the fixture stories of testdata/fixtures/stories, by name,
// each assembled from fabricated source responses under
// testdata/fixtures; each must keep the invariant its story exists for.
// Read only.
var Stories = map[string]Story{
	"discuss-no-pr": {taskRoot(101), Dirs{
		Vikunja: "collector/discuss-no-pr/vikunja", Kelos: "collector/discuss-no-pr/kelos",
		GitHub: "collector/discuss-no-pr/github", Wiki: "collector/discuss-no-pr/wiki",
	}, nil},
	"implementation-merged": {taskRoot(102), Dirs{
		Vikunja: "collector/implementation-merged/vikunja", Kelos: "collector/implementation-merged/kelos",
		GitHub: "collector/implementation-merged/github",
	}, nil},
	"wiki-rooted-request": {pageRoot(57), Dirs{
		Kelos: "collector/wiki-rooted-request/kelos", GitHub: "collector/wiki-rooted-request/github",
		Wiki: "wiki/live-edit-not-in-trail",
	}, nil},
	"failed-run-retry": {taskRoot(104), Dirs{
		Vikunja: "collector/failed-run-retry/vikunja", Kelos: "collector/failed-run-retry/kelos",
		GitHub: "collector/failed-run-retry/github",
	}, nil},
	"announced-run-only": {taskRoot(105), Dirs{
		Vikunja: "collector/announced-run-only/vikunja", Kelos: "collector/announced-run-only/kelos",
	}, nil},
	"checks-after-force-push": {taskRoot(106), Dirs{
		Vikunja: "collector/checks-after-force-push/vikunja",
		GitHub:  "collector/checks-after-force-push/github",
	}, []github.Repository{Repo}},
	"reopened-task": {taskRoot(107), Dirs{
		Vikunja: "collector/reopened-task/vikunja",
	}, nil},
	"second-pr-after-close": {taskRoot(108), Dirs{
		Vikunja: "collector/second-pr-after-close/vikunja",
		GitHub:  "collector/second-pr-after-close/github",
	}, []github.Repository{Repo}},
}

func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: VikunjaHost, ID: id}}
}

func pageRoot(id int64) model.Root {
	return model.Root{Page: &model.PageKey{Instance: WikiHost, ID: id, Locale: wiki.DefaultLocale}}
}
