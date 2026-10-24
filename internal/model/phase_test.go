package model

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testPhaseRun  = "uid-1"
	testPhasePod  = "task-42-abcde"
	testPhaseUID  = "pod-uid-1"
	testOtherSpan = "0123456789abcdef0123456789abcdef"
)

// phaseMarker returns a marker of testSpan's edge at minute minutes,
// joined to run uid by its pod.
func phaseMarker(kind MarkerKind, edge MarkerEdge, minutes int) Marker {
	r := MarkerReport{Version: MarkerVersion, Emitter: markerKinds[kind].emitter, Kind: kind, Edge: edge,
		Span: testSpan, At: at(minutes).In(testEST)}
	if edge == EdgeEnd {
		r.Outcome = markerKinds[kind].outcomes[0]
		if markerKinds[kind].attempts {
			one := 1
			r.Attempts = &one
		}
	}
	return Marker{
		Report: r,
		Sender: MarkerSender{Cluster: testCluster, Namespace: testNamespace, ServiceAccount: "default",
			Pod: testPhasePod, PodUID: testPhaseUID},
		ReceivedAt: at(minutes).Add(time.Second),
		Join: &MarkerJoin{Run: testRunKey(testPhaseRun), Confidence: ConfidenceIdentifier,
			Evidence: []Evidence{{Source: SourceKelos, Instance: testCluster, Ref: "tasks/task-42",
				Note: "pod " + testPhasePod + " named by tasks/task-42 status.podName"}}},
	}
}

// phaseStory builds the story of run with markers, failing the test on an
// error, and returns the run's phases.
func phaseStory(t *testing.T, run Run, markers ...Marker) ([]Phase, Story) {
	t.Helper()
	kelos := Coverage{Source: SourceKelos, Instance: testCluster, LastSuccessfulRead: timePtr(at(60))}
	s, err := BuildStory(testRoot(), Records{Runs: []Run{run}, Coverage: []Coverage{kelos}, Markers: markers})
	if err != nil {
		t.Fatal(err)
	}
	return s.Runs[0].Phases, s
}

func runningRun() Run {
	r := testRun(testPhaseRun)
	r.Interval.End, r.Outcome = OpenEnd(), OutcomeRunning
	return r
}

func TestPhasePairing(t *testing.T) {
	start, end := phaseMarker(MarkerWikiLogin, EdgeStart, 2), phaseMarker(MarkerWikiLogin, EdgeEnd, 4)
	early := phaseMarker(MarkerWikiLogin, EdgeEnd, 1)
	other := phaseMarker(MarkerAgent, EdgeEnd, 4)
	for _, c := range []struct {
		name    string
		run     Run
		markers []Marker
		state   PhaseState
		bounds  [2]BoundState // none for no interval
	}{
		{"both edges", testRun(testPhaseRun), []Marker{end, start}, PhaseComplete, [2]BoundState{BoundKnown, BoundKnown}},
		{"a start, the run running", runningRun(), []Marker{start}, PhaseInProgress,
			[2]BoundState{BoundKnown, BoundOpen}},
		{"a start, the run ended", testRun(testPhaseRun), []Marker{start}, PhaseInterrupted,
			[2]BoundState{BoundKnown, BoundUnknown}},
		{"an end only", runningRun(), []Marker{end}, PhaseStartNotRecorded, [2]BoundState{BoundUnknown, BoundKnown}},
		{"an end before its start", testRun(testPhaseRun), []Marker{start, early}, PhaseInconsistent, [2]BoundState{}},
		{"another kind's end", testRun(testPhaseRun), []Marker{start, other}, PhaseInconsistent, [2]BoundState{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			phases, _ := phaseStory(t, c.run, c.markers...)
			if len(phases) != 1 {
				t.Fatalf("phases = %+v, want one", phases)
			}
			p := phases[0]
			if p.State != c.state {
				t.Errorf("state = %s, want %s", p.State, c.state)
			}
			if c.bounds == ([2]BoundState{}) {
				if p.Interval != nil || p.Start == nil || p.End == nil {
					t.Errorf("phase = %+v, want both edges and no interval", p)
				}
				return
			}
			if p.Interval == nil || p.Interval.Start.State != c.bounds[0] || p.Interval.End.State != c.bounds[1] {
				t.Errorf("interval = %+v, want %v", p.Interval, c.bounds)
			}
		})
	}
}

func TestPhaseIsNeverClosedAtTheRunsEnd(t *testing.T) {
	phases, _ := phaseStory(t, testRun(testPhaseRun), phaseMarker(MarkerAgent, EdgeStart, 2))
	if _, ok := phases[0].Interval.End.Time(); ok {
		t.Errorf("end = %+v, want unknown although the run ended", phases[0].Interval.End)
	}
	if _, ok := phases[0].Interval.Duration(); ok {
		t.Error("an interrupted phase has a duration")
	}
}

func TestPhaseMarksAreInUTC(t *testing.T) {
	phases, _ := phaseStory(t, testRun(testPhaseRun), phaseMarker(MarkerWikiLogin, EdgeStart, 2),
		phaseMarker(MarkerWikiLogin, EdgeEnd, 4))
	p := phases[0]
	for _, tm := range []time.Time{p.Start.At, p.End.At, *p.Interval.Start.At, *p.Interval.End.At} {
		if tm.Location() != time.UTC {
			t.Errorf("time %v is not in UTC", tm)
		}
	}
	if p.End.Outcome != MarkerLoginOK || p.End.Attempts == nil || *p.End.Attempts != 1 || p.Start.Outcome != "" {
		t.Errorf("edges = %+v and %+v, want the end's outcome and attempts only", p.Start, p.End)
	}
}

func TestPhaseHeuristicJoinStaysUncertain(t *testing.T) {
	start, end := phaseMarker(MarkerBlockerWait, EdgeStart, 1), phaseMarker(MarkerBlockerWait, EdgeEnd, 3)
	end.Join = &MarkerJoin{Run: end.Join.Run, Confidence: ConfidenceHeuristic,
		Evidence: []Evidence{{Source: SourceKelos, Instance: testCluster, Ref: "tasks/task-42", Note: "by name"}}}
	phases, _ := phaseStory(t, testRun(testPhaseRun), start, end)
	p := phases[0]
	if p.Confidence != ConfidenceHeuristic {
		t.Errorf("confidence = %s, want heuristic when one edge is", p.Confidence)
	}
	notes := make([]string, 0, len(p.Evidence))
	for _, ev := range p.Evidence {
		notes = append(notes, ev.Note)
	}
	for _, want := range []string{"by name", "named by tasks/task-42", "start of span " + testSpan + " reported by " +
		"wait_for_blockers in pod " + testPhasePod, "end of span"} {
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("evidence notes %q lack %q", notes, want)
		}
	}
	if n := len(p.Evidence); n != 4 {
		t.Errorf("evidence = %d entries, want each edge's report and each distinct join note: 4", n)
	}
}

func TestPhasesPairWithinOnePodAndSpan(t *testing.T) {
	start := phaseMarker(MarkerAgent, EdgeStart, 1)
	otherSpan := phaseMarker(MarkerAgent, EdgeEnd, 5)
	otherSpan.Report.Span = testOtherSpan
	otherPod := phaseMarker(MarkerAgent, EdgeEnd, 6)
	otherPod.Sender.PodUID = "pod-uid-2"
	phases, _ := phaseStory(t, runningRun(), otherPod, otherSpan, start)
	var states []PhaseState
	for _, p := range phases {
		states = append(states, p.State)
	}
	want := []PhaseState{PhaseInProgress, PhaseStartNotRecorded, PhaseStartNotRecorded}
	if !slices.Equal(states, want) {
		t.Errorf("states = %v, want %v, ordered by their first edge", states, want)
	}
}

func TestPhasesReplaceTheInputs(t *testing.T) {
	run := testRun(testPhaseRun)
	run.Phases = []Phase{{Span: testSpan, State: PhaseComplete}}
	if phases, _ := phaseStory(t, run); phases != nil {
		t.Errorf("phases = %+v, want none without markers", phases)
	}
	if len(run.Phases) != 1 {
		t.Error("BuildStory modified its input")
	}
}

func TestPhaseMarkersNote(t *testing.T) {
	_, without := phaseStory(t, testRun(testPhaseRun))
	if slices.Contains(without.Coverage[0].Notes, PhaseMarkersNote) {
		t.Error("a story without phases notes that markers are best effort")
	}
	_, with := phaseStory(t, testRun(testPhaseRun), phaseMarker(MarkerAgent, EdgeStart, 1))
	if !slices.Contains(with.Coverage[0].Notes, PhaseMarkersNote) {
		t.Errorf("notes = %q, want the best-effort note", with.Coverage[0].Notes)
	}
	again, err := BuildStory(with.Root, with.Records)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(again.Coverage[0].Notes); n != 1 {
		t.Errorf("rebuilt notes = %q, want the note once", again.Coverage[0].Notes)
	}
	data, err := json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"markers"`) {
		t.Error("the story holds its markers besides its phases")
	}
}

func TestBuildStoryRejectsBadMarkers(t *testing.T) {
	start := phaseMarker(MarkerAgent, EdgeStart, 1)
	unjoined := start
	unjoined.Join = nil
	elsewhere := phaseMarker(MarkerAgent, EdgeStart, 1)
	elsewhere.Join = &MarkerJoin{Run: testRunKey("uid-2"), Confidence: ConfidenceIdentifier,
		Evidence: start.Join.Evidence}
	otherNamespace := phaseMarker(MarkerAgent, EdgeStart, 1)
	otherNamespace.Sender.Namespace = "other"
	invalid := phaseMarker(MarkerAgent, EdgeStart, 1)
	invalid.Report.Span = "x"
	runStoryCases(t, []storyCase{
		{"unjoined", Records{Runs: []Run{testRun(testPhaseRun)}, Markers: []Marker{unjoined}}, "joined to its runs"},
		{"joined to another run", Records{Runs: []Run{testRun(testPhaseRun)}, Markers: []Marker{elsewhere}},
			"not in the story"},
		{"another namespace's pod", Records{Runs: []Run{testRun(testPhaseRun)}, Markers: []Marker{otherNamespace}},
			"another namespace"},
		{"an edge twice", Records{Runs: []Run{testRun(testPhaseRun)}, Markers: []Marker{start, start}},
			"duplicate record"},
		{"an invalid marker", Records{Runs: []Run{testRun(testPhaseRun)}, Markers: []Marker{invalid}}, "span"},
	})
}
