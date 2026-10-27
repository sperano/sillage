package kelos

import (
	"reflect"
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
			Branch: "kelos/vikunja-0", BranchRef: "tasks/agents/github-fu-widgets-agent-opus-41-r9001",
			BranchFrom: "spec.branch", Workspace: "widgets",
		},
		runKey("00000000-0000-4000-8000-000000002052"): {
			Branch: "kelos/vikunja-0", BranchRef: "tasks/agents/merge-close-widgets-41", BranchFrom: "spec.branch",
			Workspace: "widgets",
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

// A Task's Origin is its placement's, and a Task without spec.branch takes
// its branch annotation.
func TestPlacementsFromAnnotations(t *testing.T) {
	got := Placements(testConfig(), readStory(t, "annotated-no-labels"))
	merge := runKey("00000000-0000-4000-8000-000000003003")
	p := got[merge]
	if p.Branch != "kelos/vikunja-301" || p.BranchRef != "tasks/agents/vikunja-merge-widgets-41-true" ||
		p.BranchFrom != "metadata.annotations["+testDomain+"/branch]" {
		t.Errorf("merge Task placement = %+v, want its branch annotation", p)
	}
	follow := got[runKey("00000000-0000-4000-8000-000000003001")]
	if follow.Branch != "kelos/vikunja-301" || follow.BranchFrom != "spec.branch" {
		t.Errorf("follow-up placement = %+v, want spec.branch", follow)
	}
	if follow.Origin == nil || follow.Origin.Value(AnnotationComment) != "9101" || follow.Origin.remembered {
		t.Errorf("follow-up origin = %+v, want the Task's annotations", follow.Origin)
	}
	if len(got) != 2 {
		t.Errorf("Placements() = %v, want one per Task", got)
	}
}

// A run read from its TaskRecord alone takes the Origin kept of its Task.
func TestPlacementsFromAKeptOrigin(t *testing.T) {
	got := Placements(testConfig(), readStory(t, "record-only-remembered"))
	p := got[runKey("00000000-0000-4000-8000-000000003101")]
	if p.Workspace != "widgets" || p.Origin == nil || !p.Origin.remembered || p.Origin.Value(AnnotationReview) != "7001" {
		t.Errorf("placement = %+v, want the record's workspace and the kept origin", p)
	}
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
		if !reflect.DeepEqual(got[k], w) {
			t.Errorf("Placements()[%s] = %+v, want %+v", k, got[k], w)
		}
	}
}
