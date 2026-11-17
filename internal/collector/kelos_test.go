package collector

import (
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

func TestAnnouncementWithoutPodPostedDuringNoRun(t *testing.T) {
	labels := taskLabels("104")
	s := collectAnnounced(t, map[int]string{30: ""}, kelos.Snapshot{Records: []kelos.TaskRecord{
		record(retryName, "uid-a", labels, 2, 9),
		record(retryName, "uid-b", labels, 62, 80),
	}})
	got := linksFrom(s, announcementRef(30))
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
	src.Vikunja = fixedVikunja{announcedTask(104, customName, map[int]string{30: ""})}
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

func TestPodlessAnnouncementJoinsTheRunsItWasPostedDuring(t *testing.T) {
	tests := map[string]struct {
		snap      kelos.Snapshot
		ambiguous bool
		want      []model.Ref
	}{
		"one, its pod known": {kelos.Snapshot{
			Tasks:   []kelos.Task{task(customName, "uid-live", "", nil, 60, 80)},
			Records: []kelos.TaskRecord{record(customName, "uid-old", nil, 2, 9)},
		}, false, []model.Ref{runRef("uid-live")}},
		"one, its pod unknown": {kelos.Snapshot{Records: []kelos.TaskRecord{
			record(customName, "uid-old", nil, 2, 9), record(customName, "uid-live", nil, 60, 80),
		}}, false, []model.Ref{runRef("uid-live")}},
		"two": {kelos.Snapshot{
			Tasks:   []kelos.Task{task(customName, "uid-live", "", nil, 60, 80)},
			Records: []kelos.TaskRecord{record(customName, "uid-other", nil, 50, 70)},
		}, true, []model.Ref{runRef("uid-live"), runRef("uid-other")}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			src := emptySources()
			src.Vikunja = fixedVikunja{announcedTask(104, customName, map[int]string{65: ""})}
			src.Kelos = fixedKelos{tt.snap}
			s := collectWith(t, testConfig(), src, taskRoot(104)).Story
			checkLinks(t, s, announcementRef(65), model.ConfidenceHeuristic, tt.ambiguous, tt.want...)
			checkNotes(t, s, announcementRef(65), "was posted while this run of it was running")
			for _, w := range tt.want {
				c, ok := linkBetween(s, w, taskRoot(104).Task.Ref())
				if !ok || c.Confidence != model.ConfidenceHeuristic || c.Ambiguous != tt.ambiguous {
					t.Errorf("run %s joined to the task by %+v (%v), want a heuristic, ambiguous %v", w.ID, c, ok, tt.ambiguous)
				}
			}
		})
	}
}

// The short Vikunja form names the Task and model, no agent, pod or
// namespace: its run is the Task's that was running when it was posted.
func TestShortAnnouncementJoinsItsRun(t *testing.T) {
	snap := vikunja.Snapshot{
		Task:       vikunja.Task{ID: 104, Created: at(0), CreatedBy: &vikunja.User{ID: 7, Username: "owner"}},
		ObservedAt: testNow,
		Comments: []vikunja.Comment{{
			ID: 9065, Created: at(65), Updated: at(65), Author: vikunja.User{ID: 23, Username: testServiceAccount},
			Body: "<p>THIS ONE HAS THE STUFF!</p><table><tbody><tr><td>MODEL</td><td><code>example-model-2</code></td></tr>" +
				"<tr><td>KELOS TASK</td><td><code>" + retryName + "</code></td></tr></tbody></table>" +
				"<p>COFFEE TIME! REPORT TO FOLLOW.</p>",
		}},
	}
	src := emptySources()
	src.Vikunja = fixedVikunja{snap}
	src.Kelos = fixedKelos{kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-live", "", taskLabels("104"), 60, 80)}}}
	s := collectWith(t, testConfig(), src, taskRoot(104)).Story
	checkLinks(t, s, announcementRef(65), model.ConfidenceHeuristic, false, runRef("uid-live"))
	if len(s.Runs) != 1 || s.Runs[0].Agent != testAgent {
		t.Errorf("runs = %+v, want the Task's run, its agent from the Task", s.Runs)
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
