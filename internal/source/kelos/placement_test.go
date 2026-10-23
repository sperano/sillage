package kelos

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sperano/sillage/internal/model"
)

func runKey(uid string) model.RunKey {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}
}

func TestPlacementsFromTasks(t *testing.T) {
	got := Placements(testConfig(), readStory(t, "tasks-unfinished"))
	want := map[model.RunKey]Placement{
		runKey("00000000-0000-4000-8000-000000002051"): {
			Branch: "kelos/vikunja-0", BranchRef: "tasks/agents/github-fu-widgets-agent-opus-41-r9001", Workspace: "widgets",
		},
		runKey("00000000-0000-4000-8000-000000002052"): {
			Branch: "kelos/vikunja-0", BranchRef: "tasks/agents/merge-close-widgets-41", Workspace: "widgets",
		},
	}
	checkPlacements(t, got, want)
}

func TestPlacementsFromRecordLabels(t *testing.T) {
	got := Placements(testConfig(), readStory(t, "record-only"))
	want := map[model.RunKey]Placement{
		runKey("00000000-0000-4000-8000-000000000202"): {Workspace: "widgets"},
	}
	checkPlacements(t, got, want)
}

func TestPlacementsSkipsObjectsWithoutEither(t *testing.T) {
	s := Snapshot{
		Tasks: []Task{
			{ObjectMeta: metav1.ObjectMeta{Name: "plain", UID: "uid-plain"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "no-uid"}, Spec: TaskSpec{Branch: "kelos/vikunja-1"}},
		},
		Records: []TaskRecord{{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "plain", UID: "uid-plain"}}}},
	}
	if got := Placements(testConfig(), s); len(got) != 0 {
		t.Errorf("Placements() = %v, want none: no object with a uid names a branch or workspace", got)
	}
}

func checkPlacements(t *testing.T, got, want map[model.RunKey]Placement) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("Placements() = %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("Placements()[%s] = %+v, want %+v", k, got[k], w)
		}
	}
}
