package store

import (
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func pending(minute int) pendingMarker {
	return pendingMarker{id: 1, pod: testPod, podUID: "pod-uid-1", taskName: "vikunja-widgets-agent-7", at: at(minute)}
}

func TestPodJoinNeedsOneRun(t *testing.T) {
	one := []runPod{{uid: "uid-1", ref: "tasks/agents/vikunja-widgets-agent-7"}}
	j := podJoin(testCluster, testNamespace, pending(5), one)
	if j == nil || j.Run != uidKey("uid-1") || j.Confidence != model.ConfidenceIdentifier {
		t.Fatalf("join = %+v, want uid-1 by identifier", j)
	}
	if len(j.Evidence) != 2 || !strings.Contains(j.Evidence[0].Note, "status.podName") ||
		!strings.Contains(j.Evidence[1].Note, "pod-uid-1") || j.Evidence[1].Ref != "pods/agents/"+testPod {
		t.Errorf("evidence = %+v, want the Task naming the pod and the token verified for it", j.Evidence)
	}
	if j := podJoin(testCluster, testNamespace, pending(5), nil); j != nil {
		t.Errorf("a pod no run was seen with joins %+v", j)
	}
	two := append(one, runPod{uid: "uid-2", ref: "tasks/agents/other"})
	if j := podJoin(testCluster, testNamespace, pending(5), two); j != nil {
		t.Errorf("a pod two runs were seen with joins %+v", j)
	}
}

func TestNameJoinNeedsOneRunHoldingTheTime(t *testing.T) {
	first := finishedRun("uid-1", 0, 10, model.OutcomeFailed)
	retry := taskRun("uid-2")
	retry.Created, retry.Interval.Start = ptr(at(20)), model.KnownAt(at(21))
	runs := []model.Run{first, retry}
	for _, c := range []struct {
		name   string
		minute int
		want   string // the run's UID, "" for none
	}{
		{"inside the first", 5, "uid-1"},
		{"at the first's end", 10, "uid-1"},
		{"between the two", 15, ""},
		{"after the retry's creation, before its start", 20, "uid-2"},
		{"in the retry, still open", 90, "uid-2"},
		{"before both", -1, ""},
	} {
		j := nameJoin(testCluster, testNamespace, pending(c.minute), runs)
		switch {
		case c.want == "" && j != nil:
			t.Errorf("%s: joins %s", c.name, j.Run)
		case c.want != "" && (j == nil || j.Run.UID != c.want || j.Confidence != model.ConfidenceHeuristic):
			t.Errorf("%s: join = %+v, want %s by heuristic", c.name, j, c.want)
		}
	}
	overlapping := finishedRun("uid-3", 0, 100, model.OutcomeFailed)
	if j := nameJoin(testCluster, testNamespace, pending(5), append(runs, overlapping)); j != nil {
		t.Errorf("a time two runs hold joins %s", j.Run)
	}
}

// An unknown end holds no time after the start, and an unknown start none
// at all: the gap stays unknown.
func TestRunHoldsNothingUnknown(t *testing.T) {
	r := finishedRun("uid-1", 0, 10, model.OutcomeUnknown)
	r.Interval.End = model.UnknownBound()
	if runHolds(r, at(5)) {
		t.Error("a run of unknown end holds a time")
	}
	r = finishedRun("uid-1", 0, 10, model.OutcomeFailed)
	r.Interval.Start = model.UnknownBound()
	if runHolds(r, at(5)) {
		t.Error("a run of unknown start and creation holds a time")
	}
	r.Created = ptr(at(0))
	if !runHolds(r, at(5)) {
		t.Error("a run created before a time and ended after does not hold it")
	}
}

func TestTaskEvidencePrefersTheTask(t *testing.T) {
	r := finishedRun("uid-1", 0, 10, model.OutcomeFailed)
	if got := taskEvidence(r); got != "taskrecords/agents/uid-1" {
		t.Errorf("evidence of a record-only run = %q", got)
	}
	r.Evidence = append(r.Evidence, kelosEvidence("tasks/agents/vikunja-widgets-agent-7", "status"))
	if got := taskEvidence(r); got != "tasks/agents/vikunja-widgets-agent-7" {
		t.Errorf("evidence = %q, want the Task's", got)
	}
}
