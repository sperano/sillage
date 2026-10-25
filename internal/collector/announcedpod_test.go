package collector

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// otherNamespace is a second namespace running Kelos pods: the collector
// does not read it, so its runs reach a story only as kept runs.
const otherNamespace = "others"

// sharedPod is a pod name of retryName in both namespaces.
const sharedPod = retryName + "-pod00"

// keptRun is a finished run of retryName in ns that a store kept; pod is
// empty for a run read from a TaskRecord alone.
func keptRun(ns, uid, pod string, start, end int) model.Run {
	return model.Run{
		Key: model.RunKey{Cluster: testCluster, Namespace: ns, UID: uid}, TaskName: retryName, Pod: pod,
		Role: model.RoleImplementation, Outcome: model.OutcomeSucceeded,
		Interval: model.Interval{Start: model.KnownAt(at(start)), End: model.KnownAt(at(end))},
		Evidence: []model.Evidence{{Source: model.SourceKelos, Instance: testCluster, Ref: "tasks/" + ns + "/" + retryName}},
	}
}

func otherRunRef(uid string) model.Ref {
	return model.RunKey{Cluster: testCluster, Namespace: otherNamespace, UID: uid}.Ref()
}

// statingNamespace has each announcement of snap name namespace ns, as
// the console paragraph does.
func statingNamespace(snap vikunja.Snapshot, ns string) vikunja.Snapshot {
	snap.Comments = slices.Clone(snap.Comments)
	for i := range snap.Comments {
		snap.Comments[i].Body += "<p>Watch it in the console (namespace " + ns + ").</p>"
	}
	return snap
}

// checkLinks fails unless the correlations from ref to a run are one to
// each of want, of confidence, ambiguous as said.
func checkLinks(t *testing.T, s model.Story, ref model.Ref, confidence model.Confidence, ambiguous bool,
	want ...model.Ref) {
	t.Helper()
	got := linksFrom(s, ref)
	to := make(map[model.Ref]bool)
	for _, c := range got {
		to[c.To] = true
		if c.Confidence != confidence || c.Ambiguous != ambiguous {
			t.Errorf("%s: %s, want a %s, ambiguous %v", ref.ID, c, confidence, ambiguous)
		}
	}
	for _, w := range want {
		if !to[w] {
			t.Errorf("%s links %v, want one to %s", ref.ID, got, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s links %v, want only %v", ref.ID, got, want)
	}
}

func TestNamespacelessAnnouncementOfASharedPodJoinsTheRunItWasPostedDuring(t *testing.T) {
	here := kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-here", "", nil, 60, 80)}}
	tests := map[string]struct {
		there     model.Run
		minute    int
		ambiguous bool
		want      []model.Ref
	}{
		"during the other namespace's run": {keptRun(otherNamespace, "uid-there", sharedPod, 2, 9), 3, false,
			[]model.Ref{otherRunRef("uid-there")}},
		"during the read namespace's run": {keptRun(otherNamespace, "uid-there", sharedPod, 2, 9), 65, false,
			[]model.Ref{runRef("uid-here")}},
		"during both": {keptRun(otherNamespace, "uid-there", sharedPod, 2, 90), 65, true,
			[]model.Ref{runRef("uid-here"), otherRunRef("uid-there")}},
		"during neither": {keptRun(otherNamespace, "uid-there", sharedPod, 2, 9), 30, true,
			[]model.Ref{runRef("uid-here"), otherRunRef("uid-there")}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := recollect(t, map[int]string{tt.minute: sharedPod}, here, Kept{Runs: []model.Run{tt.there}})
			checkLinks(t, s, announcementRef(tt.minute), model.ConfidenceHeuristic, tt.ambiguous, tt.want...)
			for _, w := range tt.want {
				if c, ok := linkBetween(s, w, taskRoot(104).Ref()); ok && c.Confidence == model.ConfidenceIdentifier {
					t.Errorf("run %s joined to the task by %s, which only a heuristic says", w.ID, c)
				}
			}
		})
	}
}

func TestNamespacelessAnnouncementIsNotAnotherNamespacesRunItOutlived(t *testing.T) {
	here := kelos.Snapshot{Records: []kelos.TaskRecord{record(retryName, "uid-here", nil, 60, 80)}}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", sharedPod, 2, 9)}}
	t.Run("posted during a run here naming no pod", func(t *testing.T) {
		s := recollect(t, map[int]string{65: sharedPod}, here, kept)
		checkLinks(t, s, announcementRef(65), model.ConfidenceHeuristic, false, runRef("uid-here"))
	})
	t.Run("posted during no run", func(t *testing.T) {
		s := recollect(t, map[int]string{30: sharedPod}, here, kept)
		checkLinks(t, s, announcementRef(30), model.ConfidenceHeuristic, true, otherRunRef("uid-there"))
		checkNotes(t, s, announcementRef(30), "posted while none of them was running")
		if c, ok := linkBetween(s, otherRunRef("uid-there"), taskRoot(104).Ref()); !ok || !c.Ambiguous {
			t.Errorf("run uid-there joined to the task by %v, want an ambiguous heuristic", c)
		}
		for _, r := range s.Runs {
			if r.Key.Pod != "" {
				t.Errorf("run %s keyed by pod, but a kept run names it", r.Key)
			}
		}
	})
}

func TestNamespacelessAnnouncementOfAnotherNamespacesPodIsNoIdentifierOfIt(t *testing.T) {
	here := kelos.Snapshot{Records: []kelos.TaskRecord{record(retryName, "uid-here", nil, 60, 80)}}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", sharedPod, 2, 90)}}
	s := recollect(t, map[int]string{65: sharedPod}, here, kept)
	checkLinks(t, s, announcementRef(65), model.ConfidenceHeuristic, true, runRef("uid-here"),
		otherRunRef("uid-there"))
	checkNotes(t, s, announcementRef(65), "names no pod")
	for _, r := range []model.Ref{runRef("uid-here"), otherRunRef("uid-there")} {
		c, ok := linkBetween(s, r, taskRoot(104).Ref())
		if !ok || c.Confidence != model.ConfidenceHeuristic || !c.Ambiguous {
			t.Errorf("run %s joined to the task by %v, want an ambiguous heuristic", r.ID, c)
		}
	}
}

func TestNamespacelessAnnouncementOutlivingASharedPodJoinsTheAttemptItWasPostedFrom(t *testing.T) {
	here := kelos.Snapshot{
		Tasks:   []kelos.Task{task(retryName, "uid-here", "", nil, 60, 80)},
		Records: []kelos.TaskRecord{record(retryName, "uid-attempt", nil, 25, 35)},
	}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", sharedPod, 2, 9)}}
	s := recollect(t, map[int]string{30: sharedPod}, here, kept)
	checkLinks(t, s, announcementRef(30), model.ConfidenceHeuristic, false, runRef("uid-attempt"))
}

func TestAnnouncementStatingItsNamespaceIdentifiesThePodThere(t *testing.T) {
	here := kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-here", "", nil, 60, 80)}}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", sharedPod, 2, 9)}}
	announced := statingNamespace(announcedTask(104, retryName, map[int]string{65: sharedPod}), otherNamespace)
	s := recollectFrom(t, announced, here, kept)
	checkLinks(t, s, announcementRef(65), model.ConfidenceIdentifier, false, otherRunRef("uid-there"))
}

func TestNamespacelessAnnouncementOfAPodInOneOtherNamespaceIdentifiesIt(t *testing.T) {
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", sharedPod, 2, 9)}}
	s := recollect(t, map[int]string{3: sharedPod}, kelos.Snapshot{}, kept)
	checkLinks(t, s, announcementRef(3), model.ConfidenceIdentifier, false, otherRunRef("uid-there"))
	for _, r := range s.Runs {
		if r.Key.Pod != "" {
			t.Errorf("run %s keyed by pod, but a kept run names it", r.Key)
		}
	}
}

func TestPodlessAnnouncementStatingItsNamespaceJoinsOnlyRunsThere(t *testing.T) {
	here := kelos.Snapshot{Records: []kelos.TaskRecord{record(retryName, "uid-here", nil, 2, 9)}}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", "", 2, 9)}}
	announced := announcedTask(104, retryName, map[int]string{3: ""})
	t.Run("stated", func(t *testing.T) {
		s := recollectFrom(t, statingNamespace(announced, testNamespace), here, kept)
		checkLinks(t, s, announcementRef(3), model.ConfidenceHeuristic, false, runRef("uid-here"))
	})
	t.Run("unstated", func(t *testing.T) {
		s := recollectFrom(t, announced, here, kept)
		checkLinks(t, s, announcementRef(3), model.ConfidenceHeuristic, true, runRef("uid-here"),
			otherRunRef("uid-there"))
	})
}

func TestNamespacelessAnnouncementIsNoAttemptOfASharedTaskName(t *testing.T) {
	here := kelos.Snapshot{Records: []kelos.TaskRecord{record(retryName, "uid-here", nil, 2, 9)}}
	kept := Kept{Runs: []model.Run{keptRun(otherNamespace, "uid-there", "", 2, 9)}}
	announced := announcedTask(104, retryName, map[int]string{3: "p-new"})
	t.Run("unstated", func(t *testing.T) {
		s := recollectFrom(t, announced, here, kept)
		checkLinks(t, s, announcementRef(3), model.ConfidenceIdentifier, false, podRef("p-new"))
		checkLinks(t, s, podRef("p-new"), model.ConfidenceHeuristic, true, runRef("uid-here"),
			otherRunRef("uid-there"))
	})
	t.Run("stated", func(t *testing.T) {
		s := recollectFrom(t, statingNamespace(announced, testNamespace), here, kept)
		checkLinks(t, s, announcementRef(3), model.ConfidenceHeuristic, false, runRef("uid-here"))
	})
}

// timeless has every announcement of snap carry no time.
func timeless(snap vikunja.Snapshot) vikunja.Snapshot {
	snap.Comments = slices.Clone(snap.Comments)
	for i := range snap.Comments {
		snap.Comments[i].Created, snap.Comments[i].Updated = time.Time{}, time.Time{}
	}
	return snap
}

// checkNotes fails unless every correlation from ref to a run has
// evidence whose note says want.
func checkNotes(t *testing.T, s model.Story, ref model.Ref, want string) {
	t.Helper()
	for _, c := range linksFrom(s, ref) {
		if !slices.ContainsFunc(c.Evidence, func(ev model.Evidence) bool { return strings.Contains(ev.Note, want) }) {
			t.Errorf("%s: evidence %v, want a note saying %q", c, c.Evidence, want)
		}
	}
}

func TestNamespacelessAnnouncementOfASharedPodStatesOnlyTheTimingRead(t *testing.T) {
	const (
		running    = "were running"
		mayHaveRun = "may have been running when it was posted"
	)
	here := kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-here", "", nil, 60, 80)}}
	unended := keptRun(otherNamespace, "uid-there", sharedPod, 2, 9)
	unended.Interval.End = model.UnknownBound()
	tests := map[string]struct {
		announced vikunja.Snapshot
		there     model.Run
		say, not  string
	}{
		"bounds and time known": {announcedTask(104, retryName, map[int]string{65: sharedPod}),
			keptRun(otherNamespace, "uid-there", sharedPod, 2, 90), running, mayHaveRun},
		"no time": {timeless(announcedTask(104, retryName, map[int]string{65: sharedPod})),
			keptRun(otherNamespace, "uid-there", sharedPod, 2, 90), mayHaveRun, running},
		"a run's end unknown": {announcedTask(104, retryName, map[int]string{65: sharedPod}), unended,
			mayHaveRun, running},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := recollectFrom(t, tt.announced, here, Kept{Runs: []model.Run{tt.there}})
			checkLinks(t, s, announcementRef(65), model.ConfidenceHeuristic, true, runRef("uid-here"),
				otherRunRef("uid-there"))
			checkNotes(t, s, announcementRef(65), tt.say)
			for _, c := range linksFrom(s, announcementRef(65)) {
				for _, ev := range c.Evidence {
					if strings.Contains(ev.Note, tt.not) {
						t.Errorf("%s: note %q says %q", c, ev.Note, tt.not)
					}
				}
			}
		})
	}
}
