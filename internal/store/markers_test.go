package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	testSpan     = "5f0c2b3e9a7d4c1e8b6a2f4d3c1e0b9a"
	otherSpan    = "0123456789abcdef0123456789abcdef"
	testPodUID   = "pod-uid-1"
	testPerPod   = 2000
	testTaskName = "vikunja-widgets-agent-7"
	firstPod     = "vikunja-widgets-agent-7-first"
)

// marker is an edge of a blocker wait reported by pod at minute.
func marker(pod, podUID, span string, edge model.MarkerEdge, minute int) model.Marker {
	r := model.MarkerReport{
		Version: model.MarkerVersion, Emitter: model.EmitterWaitForBlockers, Kind: model.MarkerBlockerWait,
		Edge: edge, Span: span, At: at(minute), TaskName: testTaskName,
	}
	if edge == model.EdgeEnd {
		r.Outcome = model.MarkerUnblocked
	}
	return model.Marker{
		Report: r,
		Sender: model.MarkerSender{Cluster: testCluster, Namespace: testNamespace, ServiceAccount: "default",
			Pod: pod, PodUID: podUID},
		ReceivedAt: at(minute).Add(time.Second),
	}
}

func mustRecord(t *testing.T, s *Store, m model.Marker, perPod int, want MarkerResult) {
	t.Helper()
	got, err := s.RecordMarker(t.Context(), m, perPod)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("marker %s %s of %s: %s, want %s", m.Report.Span, m.Report.Edge, m.Sender.PodUID, got, want)
	}
}

func mustRunMarkers(t *testing.T, s *Store, runs ...model.RunKey) []model.Marker {
	t.Helper()
	got, err := s.RunMarkers(t.Context(), runs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// kelosCoverage is the Kelos coverage of a read at minute.
func kelosCoverage(minute int) model.Coverage {
	return model.Coverage{Source: model.SourceKelos, Instance: testCluster, LastSuccessfulRead: ptr(at(minute))}
}

// podObserved is a watch's observation of uid's Task naming pod.
func podObserved(uid, pod, rv string, minute int) model.Event {
	return model.Event{
		Source: model.SourceKelos, Instance: testCluster, ID: "tasks/" + uid + "@" + rv + "/modified",
		IDKind: model.IDSource, Kind: model.KindRunObserved, ObservedAt: at(minute),
		Actor: model.Account{Kind: model.ActorUnknown}, Related: model.Related{Run: ptr(uidKey(uid))},
		Details:  map[string]string{"object": "task", "change": "modified", "pod": pod, "resourceVersion": rv},
		Evidence: kelosEvidence("tasks/agents/"+testTaskName, "modified"),
	}
}

func kelosNotes(t *testing.T, s *Store, root model.Root) []string {
	t.Helper()
	cov, err := s.Coverage(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range mustStory(t, s, root).Coverage {
		if c.Source == model.SourceKelos {
			if i := slices.IndexFunc(cov, func(o model.Coverage) bool { return o.Key() == c.Key() }); i < 0 ||
				!slices.Equal(slices.Sorted(slices.Values(cov[i].Notes)), c.Notes) {
				t.Errorf("Coverage and Story disagree: %v, %v", cov, c.Notes)
			}
			return c.Notes
		}
	}
	return nil
}

// A re-sent marker is the one stored; a different one under its key is
// refused, and the first kept. Edges arriving out of order are each
// stored.
func TestRecordMarkerIsIdempotent(t *testing.T) {
	s := newTestStore(t, Retention{})
	end := marker(testPod, testPodUID, testSpan, model.EdgeEnd, 10)
	mustRecord(t, s, end, testPerPod, MarkerStored)
	again := end
	again.ReceivedAt = end.ReceivedAt.Add(time.Minute)
	again.Report.At = end.Report.At.In(time.FixedZone("west", -7*60*60))
	mustRecord(t, s, again, testPerPod, MarkerDuplicate)
	other := end
	other.Report.Outcome = model.MarkerTimedOut
	mustRecord(t, s, other, testPerPod, MarkerConflict)
	mustRecord(t, s, marker(testPod, testPodUID, testSpan, model.EdgeStart, 1), testPerPod, MarkerStored)
	// Another pod's marker of the same span is its own.
	another := marker(firstPod, "pod-uid-first", testSpan, model.EdgeEnd, 10)
	another.Report.TaskName = "vikunja-widgets-agent-8"
	mustRecord(t, s, another, testPerPod, MarkerStored)

	mustSave(t, s, taskRoot(7), model.Records{Runs: []model.Run{taskRun("uid-1")}})
	got := mustRunMarkers(t, s, uidKey("uid-1"))
	if len(got) != 2 || got[0].Report.Edge != model.EdgeStart || got[1].Report.Outcome != model.MarkerUnblocked {
		t.Fatalf("markers = %+v, want the start and the first end", got)
	}
	if !got[1].ReceivedAt.Equal(end.ReceivedAt) {
		t.Errorf("received at %s, want the first receipt's", got[1].ReceivedAt)
	}
}

// Past the limit per pod a new marker is refused, and the coverage of the
// pod's run says so; a re-sent one is still recognized.
func TestMarkerLimitPerPod(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	mustSave(t, s, root, model.Records{Runs: []model.Run{taskRun("uid-1")}, Coverage: []model.Coverage{kelosCoverage(0)}})
	const limit = 2
	start := marker(testPod, testPodUID, testSpan, model.EdgeStart, 1)
	mustRecord(t, s, start, limit, MarkerStored)
	mustRecord(t, s, marker(testPod, testPodUID, testSpan, model.EdgeEnd, 2), limit, MarkerStored)
	mustRecord(t, s, marker(testPod, testPodUID, otherSpan, model.EdgeStart, 3), limit, MarkerLimited)
	mustRecord(t, s, marker(testPod, testPodUID, otherSpan, model.EdgeStart, 3), limit, MarkerLimited)
	mustRecord(t, s, start, limit, MarkerDuplicate)
	// Another pod's limit is its own; with no Task name, nothing joins it.
	unnamed := marker("another-pod", "pod-uid-2", otherSpan, model.EdgeStart, 3)
	unnamed.Report.TaskName = ""
	mustRecord(t, s, unnamed, limit, MarkerStored)

	var refused int
	if err := s.pool.QueryRow(t.Context(), "SELECT refused FROM marker_limits WHERE pod_uid = $1",
		testPodUID).Scan(&refused); err != nil || refused != 2 {
		t.Errorf("refused = %d (%v), want 2", refused, err)
	}
	notes := kelosNotes(t, s, root)
	if !slices.Contains(notes, limitedNote+testPod) {
		t.Errorf("notes = %q, want the pod's limit", notes)
	}
	if !slices.Contains(notes, "1"+unattachedNote) {
		t.Errorf("notes = %q, want another pod's marker unattached", notes)
	}
}

// A marker that arrives before its Task is seen stays unattached, counted
// in the Kelos coverage, until a read of the Task names its pod.
func TestMarkerBeforeItsTask(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	mustSave(t, s, root, model.Records{Coverage: []model.Coverage{kelosCoverage(0)}})
	m := marker(testPod, testPodUID, testSpan, model.EdgeStart, 1)
	m.Report.TaskName = ""
	mustRecord(t, s, m, testPerPod, MarkerStored)
	if notes := kelosNotes(t, s, root); !slices.Equal(notes, []string{"1" + unattachedNote}) {
		t.Errorf("notes = %q, want the marker unattached", notes)
	}

	mustSave(t, s, root, model.Records{Runs: []model.Run{taskRun("uid-1")}})
	got := mustRunMarkers(t, s, uidKey("uid-1"))
	if len(got) != 1 || got[0].Join == nil || got[0].Join.Confidence != model.ConfidenceIdentifier ||
		got[0].Join.Evidence[0].Ref != "tasks/agents/"+testTaskName {
		t.Fatalf("markers = %+v, want the marker joined by its pod", got)
	}
	if notes := kelosNotes(t, s, root); len(notes) != 0 {
		t.Errorf("notes = %q once joined", notes)
	}
}

// A retried Job's first pod still joins its run once the Task's status
// names the second: the store keeps every pod a run was seen with, from
// the runs and from the watch's observations.
func TestRetriedJobsFirstPod(t *testing.T) {
	for name, first := range map[string]model.Records{
		"read":     {Runs: []model.Run{withPod(taskRun("uid-1"), firstPod)}},
		"observed": {Runs: []model.Run{taskRun("uid-1")}, Events: []model.Event{podObserved("uid-1", firstPod, "5", 1)}},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t, Retention{})
			root := taskRoot(7)
			mustSave(t, s, root, first)
			mustSave(t, s, root, model.Records{Runs: []model.Run{taskRun("uid-1")}})
			mustRecord(t, s, marker(firstPod, "pod-uid-first", testSpan, model.EdgeStart, 1), testPerPod, MarkerStored)
			mustRecord(t, s, marker(testPod, testPodUID, testSpan, model.EdgeStart, 5), testPerPod, MarkerStored)
			got := mustRunMarkers(t, s, uidKey("uid-1"))
			if len(got) != 2 {
				t.Fatalf("markers = %+v, want both pods'", got)
			}
			for _, m := range got {
				if m.Join.Confidence != model.ConfidenceIdentifier {
					t.Errorf("pod %s joined by %s", m.Sender.Pod, m.Join.Confidence)
				}
			}
		})
	}
}

func withPod(r model.Run, pod string) model.Run {
	r.Pod = pod
	return r
}

// Without a pod join, the Task name and time join a marker to the one run
// whose interval holds it, as a heuristic, until a pod join replaces it; a
// retry that makes the name ambiguous unattaches it.
func TestMarkerJoinByName(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	first := withPod(taskRun("uid-1"), "")
	mustSave(t, s, root, model.Records{Runs: []model.Run{first}})
	mustRecord(t, s, marker(firstPod, "pod-uid-first", testSpan, model.EdgeStart, 5), testPerPod, MarkerStored)
	got := mustRunMarkers(t, s, uidKey("uid-1"))
	if len(got) != 1 || got[0].Join.Confidence != model.ConfidenceHeuristic ||
		!strings.Contains(got[0].Join.Evidence[0].Note, "uncertain") {
		t.Fatalf("markers = %+v, want a join by name", got)
	}

	// A second run of the name, still open, holds the time too.
	retry := withPod(taskRun("uid-2"), "")
	mustSave(t, s, root, model.Records{Runs: []model.Run{retry}})
	if got := mustRunMarkers(t, s, uidKey("uid-1"), uidKey("uid-2")); len(got) != 0 {
		t.Errorf("markers = %+v, want none once two runs hold the time", got)
	}

	mustSave(t, s, root, model.Records{Runs: []model.Run{withPod(taskRun("uid-1"), firstPod)}})
	got = mustRunMarkers(t, s, uidKey("uid-1"))
	if len(got) != 1 || got[0].Join.Confidence != model.ConfidenceIdentifier {
		t.Errorf("markers = %+v, want the pod join", got)
	}
}

func TestPruneDropsMarkers(t *testing.T) {
	s := newTestStore(t, Retention{keep: 24 * time.Hour})
	mustRecord(t, s, marker(testPod, testPodUID, testSpan, model.EdgeStart, 1), 1, MarkerStored)
	mustRecord(t, s, marker(testPod, testPodUID, otherSpan, model.EdgeStart, 2), 1, MarkerLimited)
	if _, err := s.Prune(t.Context(), at(0).Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"markers", "marker_limits"} {
		var n int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d rows (%v) after pruning", table, n, err)
		}
	}
}

func TestRecordMarkerRefusesInvalid(t *testing.T) {
	s := newTestStore(t, Retention{})
	m := marker(testPod, testPodUID, testSpan, model.EdgeStart, 1)
	m.Sender.PodUID = ""
	if _, err := s.RecordMarker(t.Context(), m, testPerPod); err == nil {
		t.Error("a marker without a pod UID was recorded")
	}
	m = marker(testPod, testPodUID, testSpan, model.EdgeStart, 1)
	m.Join = &model.MarkerJoin{}
	if _, err := s.RecordMarker(t.Context(), m, testPerPod); err == nil {
		t.Error("a joined marker was recorded")
	}
	if _, err := s.RunMarkers(t.Context(), []model.RunKey{podRunKey(testPod)}); err == nil {
		t.Error("markers of a run keyed by pod were read")
	}
}
