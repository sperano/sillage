package collectortest

import "github.com/sperano/sillage/internal/model"

// Story is a fixture story's root and the responses the collector
// assembles it from.
type Story struct {
	Root model.Root
	Dirs Dirs
}

// Stories are the fixture stories of testdata/fixtures/stories, by name,
// each assembled from fabricated source responses under
// testdata/fixtures; each must keep the invariant its story exists for.
// Read only.
var Stories = map[string]Story{
	"discuss-no-pr": {taskRoot(101), Dirs{
		Vikunja: "collector/discuss-no-pr/vikunja", Kelos: "collector/discuss-no-pr/kelos",
		GitHub: "collector/discuss-no-pr/github",
	}},
	"implementation-merged": {taskRoot(102), Dirs{
		Vikunja: "collector/implementation-merged/vikunja", Kelos: "collector/implementation-merged/kelos",
		GitHub: "collector/implementation-merged/github",
	}},
	"failed-run-retry": {taskRoot(104), Dirs{
		Vikunja: "collector/failed-run-retry/vikunja", Kelos: "collector/failed-run-retry/kelos",
		GitHub: "collector/failed-run-retry/github",
	}},
	"announced-run-only": {taskRoot(105), Dirs{
		Vikunja: "collector/announced-run-only/vikunja", Kelos: "collector/announced-run-only/kelos",
		GitHub: "collector/announced-run-only/github",
	}},
	"checks-after-force-push": {taskRoot(106), Dirs{
		Vikunja: "collector/checks-after-force-push/vikunja",
		GitHub:  "collector/checks-after-force-push/github",
	}},
	"checks-forbidden": {taskRoot(110), Dirs{
		Vikunja: "collector/checks-forbidden/vikunja",
		GitHub:  "collector/checks-forbidden/github",
	}},
	"reopened-task": {taskRoot(107), Dirs{
		Vikunja: "collector/reopened-task/vikunja", GitHub: "collector/reopened-task/github",
	}},
	"second-pr-after-close": {taskRoot(108), Dirs{
		Vikunja: "collector/second-pr-after-close/vikunja",
		GitHub:  "collector/second-pr-after-close/github",
	}},
	"phase-markers": {taskRoot(109), Dirs{
		Vikunja: "collector/phase-markers/vikunja", Kelos: "collector/phase-markers/kelos",
		GitHub: "collector/phase-markers/github", Markers: "collector/phase-markers/markers",
	}},
}

func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: VikunjaHost, ID: id}}
}
