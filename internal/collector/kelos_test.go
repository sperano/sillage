package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

const retryName = "vikunja-widgets-agent-opus-104"

// collectAnnounced assembles task 104's story from announcements by
// minute and pod, and the namespace's objects.
func collectAnnounced(t *testing.T, pods map[int]string, snap kelos.Snapshot) model.Story {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, retryName, pods)}
	src.Kelos = fixedKelos{snap}
	return collectWith(t, testConfig(), src, taskRoot(104)).Story
}

// linksFrom returns the correlations from ref to a run.
func linksFrom(s model.Story, ref model.Ref) []model.Correlation {
	var out []model.Correlation
	for _, c := range s.Correlations {
		if c.From == ref && c.To.Kind == model.RefRun {
			out = append(out, c)
		}
	}
	return out
}

func announcementRef(minute int) model.Ref {
	return model.Ref{Kind: model.RefEvent, ID: "vikunja/" + testVikunjaHost + "/comments/" + itoa(9000+minute)}
}

func runRef(uid string) model.Ref {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}.Ref()
}

func podRef(pod string) model.Ref {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: pod}.Ref()
}

func TestAnnouncementJoinsTheAttemptItWasPostedFrom(t *testing.T) {
	labels := taskLabels("104")
	s := collectAnnounced(t, map[int]string{3: "p-first", 63: "p-retry"}, kelos.Snapshot{Records: []kelos.TaskRecord{
		record(retryName, "uid-a", labels, 2, 9),
		record(retryName, "uid-b", labels, 62, 80),
	}})
	if len(s.Runs) != 2 {
		t.Fatalf("runs = %v, want the two attempts and no run keyed by pod", s.Runs)
	}
	for minute, uid := range map[int]string{3: "uid-a", 63: "uid-b"} {
		got := linksFrom(s, announcementRef(minute))
		if len(got) != 1 || got[0].To != runRef(uid) || got[0].Confidence != model.ConfidenceHeuristic || got[0].Ambiguous {
			t.Errorf("announcement at minute %d links %v, want one unambiguous heuristic to %s", minute, got, uid)
		}
	}
}

func TestAnnouncementTwoAttemptsRunningKeepsItsPod(t *testing.T) {
	labels := taskLabels("104")
	s := collectAnnounced(t, map[int]string{5: "p-first"}, kelos.Snapshot{Records: []kelos.TaskRecord{
		record(retryName, "uid-a", labels, 2, 9),
		record(retryName, "uid-c", labels, 4, 30),
	}})
	if got := linksFrom(s, announcementRef(5)); len(got) != 1 || got[0].To != podRef("p-first") {
		t.Errorf("announcement links %v, want only its own run keyed by pod", got)
	}
	candidates := linksFrom(s, podRef("p-first"))
	if len(candidates) != 2 {
		t.Fatalf("candidates = %v, want both attempts of the name", candidates)
	}
	for _, c := range candidates {
		if !c.Ambiguous || c.Confidence != model.ConfidenceHeuristic {
			t.Errorf("candidate %s is not an ambiguous heuristic", c)
		}
	}
}

func TestAnnouncementNotJoinedTwice(t *testing.T) {
	s := collectAnnounced(t, map[int]string{3: "p-one", 4: "p-two"}, kelos.Snapshot{Records: []kelos.TaskRecord{
		record(retryName, "uid-a", taskLabels("104"), 2, 9),
	}})
	if got := linksFrom(s, announcementRef(4)); len(got) != 1 || got[0].To != podRef("p-two") {
		t.Errorf("second announcement links %v, want its own run keyed by pod", got)
	}
}

func TestAnnouncementWithoutPod(t *testing.T) {
	labels := taskLabels("104")
	s := collectAnnounced(t, map[int]string{3: ""}, kelos.Snapshot{Records: []kelos.TaskRecord{
		record(retryName, "uid-a", labels, 2, 9),
		record(retryName, "uid-b", labels, 62, 80),
	}})
	got := linksFrom(s, announcementRef(3))
	if len(got) != 2 {
		t.Fatalf("announcement links %v, want both attempts of its Task name", got)
	}
	for _, c := range got {
		if !c.Ambiguous {
			t.Errorf("%s is not ambiguous", c)
		}
	}
	for _, r := range s.Runs {
		if r.Key.Pod != "" {
			t.Errorf("run %s keyed by pod, but the announcement names none", r.Key)
		}
	}
}

func TestRunsJoinedByNameAndBranch(t *testing.T) {
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.Kelos = fixedKelos{kelos.Snapshot{
		Tasks: []kelos.Task{task("pr-conflict-deadbeef", "uid-conflict", taskBranch(104), nil, 1, 2)},
		Records: []kelos.TaskRecord{
			record("vikunja-fu-widgets-104-c77", "uid-unlabelled", nil, 1, 2),
			record("vikunja-widgets-agent-opus-1104", "uid-other", nil, 1, 2),
		},
	}}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	want := map[model.Ref]model.Confidence{
		runRef("uid-conflict"):   model.ConfidenceIdentifier,
		runRef("uid-unlabelled"): model.ConfidenceHeuristic,
	}
	got := make(map[model.Ref]model.Confidence)
	for _, c := range s.Correlations {
		if c.To == taskRoot(104).Task.Ref() {
			got[c.From] = c.Confidence
		}
	}
	if len(got) != len(want) || got[runRef("uid-conflict")] != want[runRef("uid-conflict")] ||
		got[runRef("uid-unlabelled")] != want[runRef("uid-unlabelled")] {
		t.Errorf("runs joined to the task = %v, want %v", got, want)
	}
}

func TestPageRunsNotFromOtherPages(t *testing.T) {
	// The fixture's namespace also holds a follow-up of page 570.
	s := collectStory(t, "wiki-rooted-request")
	for _, r := range s.Runs {
		if req, ok := pageOfName(r.TaskName); ok && req.page != 57 {
			t.Errorf("run %s of page %d is in page 57's story", r.Key, req.page)
		}
	}
}

// customName is a Task name no spawner convention covers.
const customName = "custom-run"

func TestAnnouncedRunJoinsWhatItIsACommentOn(t *testing.T) {
	tests := map[string]struct {
		snap kelos.Snapshot
		run  model.Ref
		want model.Confidence
	}{
		"its pod's Task": {
			kelos.Snapshot{Tasks: []kelos.Task{task(customName, "uid-x", "", nil, 2, 9)}},
			runRef("uid-x"), model.ConfidenceIdentifier,
		},
		"the attempt it was posted from": {
			kelos.Snapshot{Records: []kelos.TaskRecord{record(customName, "uid-y", nil, 2, 9)}},
			runRef("uid-y"), model.ConfidenceHeuristic,
		},
	}
	for name, tt := range tests {
		src := emptySources()
		src.Vikunja = fixedVikunja{announcedTask(104, customName, map[int]string{3: customName + "-pod00"})}
		src.Kelos = fixedKelos{tt.snap}
		s := collectWith(t, testConfig(), src, taskRoot(104)).Story
		c, ok := linkBetween(s, tt.run, taskRoot(104).Task.Ref())
		if !ok || c.Confidence != tt.want || c.Ambiguous {
			t.Errorf("%s: run joined to the task by %+v (%v), want an unambiguous %s", name, c, ok, tt.want)
		}
	}
}

func TestPodlessAnnouncementJoinsEachSameNameRunToTheTask(t *testing.T) {
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, customName, map[int]string{3: ""})}
	src.Kelos = fixedKelos{kelos.Snapshot{Records: []kelos.TaskRecord{
		record(customName, "uid-a", nil, 2, 9),
		record(customName, "uid-b", nil, 62, 80),
	}}}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	for _, uid := range []string{"uid-a", "uid-b"} {
		c, ok := linkBetween(s, runRef(uid), taskRoot(104).Task.Ref())
		if !ok || c.Confidence != model.ConfidenceHeuristic || !c.Ambiguous {
			t.Errorf("run %s joined to the task by %+v (%v), want an ambiguous heuristic", uid, c, ok)
		}
	}
}

func TestAnnouncedRunTakesTheWordingsRole(t *testing.T) {
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, customName, map[int]string{3: "p-custom"})}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	if len(s.Runs) != 1 || s.Runs[0].Role != model.RoleImplementation {
		t.Errorf("runs = %+v, want one implementation: its Task name says nothing, its announcement does", s.Runs)
	}
}

// Two overlapping runs of one agent profile on different tasks stay with
// the task their source identifier names. Author and time-window matching
// must not pull the other task's attempt into either story.
func TestConcurrentAgentAttemptsOnDifferentTasksStaySeparate(t *testing.T) {
	snapshot := kelos.Snapshot{Records: []kelos.TaskRecord{
		record("vikunja-widgets-agent-opus-204", "uid-204", taskLabels("204"), 1, 20),
		record("vikunja-widgets-agent-opus-205", "uid-205", taskLabels("205"), 2, 21),
	}}
	for _, tt := range []struct {
		id      int64
		wantUID string
	}{
		{204, "uid-204"},
		{205, "uid-205"},
	} {
		src := emptySources()
		src.Vikunja = fixedVikunja{announcedTask(tt.id, "", nil)}
		src.Kelos = fixedKelos{snapshot}
		story := collectWith(t, testConfig(), src, taskRoot(tt.id)).Story
		if len(story.Runs) != 1 || story.Runs[0].Key.UID != tt.wantUID {
			t.Errorf("task %d runs = %+v, want only %s", tt.id, story.Runs, tt.wantUID)
		}
		for _, correlation := range story.Correlations {
			if correlation.From.Kind == model.RefRun && correlation.From != runRef(tt.wantUID) {
				t.Errorf("task %d correlation %s crosses to another task's run", tt.id, correlation)
			}
		}
	}
}

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
		checkLinks(t, s, announcementRef(30), model.ConfidenceIdentifier, false, podRef(sharedPod))
		checkLinks(t, s, podRef(sharedPod), model.ConfidenceHeuristic, false, runRef("uid-here"))
	})
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
