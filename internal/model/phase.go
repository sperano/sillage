package model

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// PhaseState is what a run's phase markers establish of one span.
type PhaseState string

// The states of a phase. Only a complete phase has a duration; no other
// state is ever closed at a time no marker recorded, the run's end
// included.
const (
	// PhaseComplete: both edges recorded, of one kind, the end not before
	// the start. Its interval is known to known.
	PhaseComplete PhaseState = "complete"
	// PhaseInProgress: only the start recorded, the run had not ended at
	// the last read, and its pod is the last one sillage saw the run with.
	// Its end is open.
	PhaseInProgress PhaseState = "in-progress"
	// PhaseInterrupted: only the start recorded, and the run is not
	// running, or another pod of the run (a retried Job's) was seen after
	// it: no end was recorded. Its end is unknown.
	PhaseInterrupted PhaseState = "interrupted"
	// PhaseStartNotRecorded: only the end recorded. Its start is unknown.
	PhaseStartNotRecorded PhaseState = "start-not-recorded"
	// PhaseInconsistent: both edges recorded, but the end is before the
	// start or of another kind. It has no interval; both edges are kept.
	PhaseInconsistent PhaseState = "inconsistent"
)

// PhaseMark is one recorded edge of a phase, as its pod reported it.
type PhaseMark struct {
	Kind    MarkerKind    `json:"kind"`
	Emitter MarkerEmitter `json:"emitter"`
	// At is when the pod's clock says the edge happened.
	At time.Time `json:"at"`
	// Outcome is an end's.
	Outcome MarkerOutcome `json:"outcome,omitempty"`
}

// Phase is one span of a run that its pod reported with phase markers:
// what part of the run it covers, the edges recorded, and the interval
// they establish. A phase exists only where a marker was received: a pod,
// a run's interval or a gap between two phases never makes one, and a
// missing marker is not a missing phase.
type Phase struct {
	// Span is the span's id; Pod and PodUID the pod that reported it.
	Span   string     `json:"span"`
	Pod    string     `json:"pod"`
	PodUID string     `json:"podUID"`
	State  PhaseState `json:"state"`
	// Start and End are the recorded edges; at least one is set.
	Start *PhaseMark `json:"start,omitempty"`
	End   *PhaseMark `json:"end,omitempty"`
	// Interval is what the edges establish; nil for an inconsistent phase.
	Interval *Interval `json:"interval,omitempty"`
	// Confidence is how strongly the markers are joined to the run: a
	// heuristic join of either edge makes the phase uncertain.
	Confidence Confidence `json:"confidence"`
	// Evidence holds each edge's report and how it was joined to the run.
	Evidence []Evidence `json:"evidence"`
}

// PhaseMarkersNote is the note on the Kelos coverage of a story whose runs
// have phases: pods push markers, so sillage cannot know what an emitter
// failed to send.
const PhaseMarkersNote = "phase markers: best effort; a missing marker is not a missing phase"

// observedPodDetail is the run.observed detail naming the Task's pod at
// the time.
const observedPodDetail = "pod"

// spanKey is one span of one pod.
type spanKey struct{ podUID, span string }

// spanEdges are the markers of one span on one run.
type spanEdges struct{ start, end *Marker }

// validateMarkers checks that each marker is complete, joined to a run of
// the story keyed by UID in the cluster and namespace that sent it, and
// that no edge of a span is recorded twice.
func validateMarkers(markers []Marker, runs []Run) error {
	known := make(map[RunKey]bool, len(runs))
	for _, r := range runs {
		known[r.Key] = true
	}
	var errs []error
	seen := make(map[string]bool, len(markers))
	for _, m := range markers {
		if err := validateMarker(m, known); err != nil {
			errs = append(errs, err)
			continue
		}
		k := m.Sender.PodUID + "/" + m.Report.Span + "/" + string(m.Report.Edge)
		if seen[k] {
			errs = append(errs, fmt.Errorf("marker %s: duplicate record", k))
		}
		seen[k] = true
	}
	return errors.Join(errs...)
}

func validateMarker(m Marker, runs map[RunKey]bool) error {
	if err := m.Validate(); err != nil {
		return err
	}
	name := fmt.Sprintf("marker %s %s of pod %s", m.Report.Span, m.Report.Edge, m.Sender.PodUID)
	switch {
	case m.Join == nil:
		return fmt.Errorf("%s: a story's markers are joined to its runs", name)
	case m.Join.Run.Cluster != m.Sender.Cluster || m.Join.Run.Namespace != m.Sender.Namespace:
		return fmt.Errorf("%s: joined to a run of another namespace than its pod's", name)
	case !runs[m.Join.Run]:
		return fmt.Errorf("%s: joined to run %s, which is not in the story", name, m.Join.Run)
	}
	return nil
}

// addPhases replaces each run's phases with those its markers establish:
// the markers of one pod and span, on one run, are one phase. A phase
// with no end is open only while its run is unfinished and its pod is the
// run's latest: a retried Job's earlier pod is not known to be running.
func (r *Records) addPhases() {
	byRun := make(map[RunKey]map[spanKey]*spanEdges)
	for i := range r.Markers {
		m := &r.Markers[i]
		spans := byRun[m.Join.Run]
		if spans == nil {
			spans = make(map[spanKey]*spanEdges)
			byRun[m.Join.Run] = spans
		}
		k := spanKey{m.Sender.PodUID, m.Report.Span}
		if spans[k] == nil {
			spans[k] = &spanEdges{}
		}
		if m.Report.Edge == EdgeStart {
			spans[k].start = m
		} else {
			spans[k].end = m
		}
	}
	latest := r.latestPods()
	for i := range r.Runs {
		run := &r.Runs[i]
		run.Phases = nil
		for _, edges := range byRun[run.Key] {
			pod := edges.start
			if pod == nil {
				pod = edges.end
			}
			live := run.Interval.End.State == BoundOpen && latest[run.Key] == pod.Sender.Pod
			run.Phases = append(run.Phases, newPhase(*edges, live))
		}
		slices.SortFunc(run.Phases, comparePhases)
	}
}

// sighting is the latest time sillage saw a run with a pod; pod is empty
// when two pods were seen at that instant.
type sighting struct {
	pod string
	at  time.Time
}

// latestPods names, for each run, the pod sillage last saw it with, by its
// own clock: the pod a run.observed event names when it was observed, or
// a marker's sender when it was received. A pod's clock never orders
// pods, and two pods seen at one instant leave the run with none.
func (r *Records) latestPods() map[RunKey]string {
	seen := make(map[RunKey]sighting)
	see := func(run RunKey, pod string, at time.Time) {
		s, ok := seen[run]
		switch {
		case !ok || at.After(s.at):
			seen[run] = sighting{pod, at}
		case at.Equal(s.at) && pod != s.pod:
			seen[run] = sighting{"", at}
		}
	}
	for _, e := range r.Events {
		if pod := e.Details[observedPodDetail]; e.Kind == KindRunObserved && e.Related.Run != nil && pod != "" {
			see(*e.Related.Run, pod, e.ObservedAt)
		}
	}
	for _, m := range r.Markers {
		see(m.Join.Run, m.Sender.Pod, m.ReceivedAt)
	}
	latest := make(map[RunKey]string, len(seen))
	for run, s := range seen {
		latest[run] = s.pod
	}
	return latest
}

// notePhases adds PhaseMarkersNote to the Kelos coverage of each cluster
// a run with phases is in, unless it has it.
func (r *Records) notePhases() {
	clusters := make(map[string]bool)
	for _, run := range r.Runs {
		if len(run.Phases) > 0 {
			clusters[run.Key.Cluster] = true
		}
	}
	for i := range r.Coverage {
		c := &r.Coverage[i]
		if c.Source == SourceKelos && clusters[c.Instance] && !slices.Contains(c.Notes, PhaseMarkersNote) {
			c.Notes = append(c.Notes, PhaseMarkersNote)
			slices.Sort(c.Notes)
		}
	}
}

// newPhase pairs a span's edges. live says whether the run had not ended
// at the last read and the span's pod is its latest, which keeps a phase
// with no end open.
func newPhase(e spanEdges, live bool) Phase {
	first := e.start
	if first == nil {
		first = e.end
	}
	p := Phase{Span: first.Report.Span, Pod: first.Sender.Pod, PodUID: first.Sender.PodUID,
		Confidence: ConfidenceIdentifier}
	for _, m := range []*Marker{e.start, e.end} {
		if m == nil {
			continue
		}
		if m.Join.Confidence != ConfidenceIdentifier {
			p.Confidence = ConfidenceHeuristic
		}
		p.Evidence = append(p.Evidence, markerEvidence(*m))
		p.Evidence = append(p.Evidence, m.Join.Evidence...)
	}
	p.Evidence = uniqueEvidence(p.Evidence)
	p.Start, p.End = phaseMark(e.start), phaseMark(e.end)
	p.State, p.Interval = pair(p.Start, p.End, live)
	return p
}

// pair returns what a span's recorded edges establish.
func pair(start, end *PhaseMark, live bool) (PhaseState, *Interval) {
	switch {
	case start != nil && end != nil:
		if start.Kind != end.Kind || end.At.Before(start.At) {
			return PhaseInconsistent, nil
		}
		return PhaseComplete, &Interval{Start: KnownAt(start.At), End: KnownAt(end.At)}
	case end != nil:
		return PhaseStartNotRecorded, &Interval{Start: UnknownBound(), End: KnownAt(end.At)}
	case live:
		return PhaseInProgress, &Interval{Start: KnownAt(start.At), End: OpenEnd()}
	}
	return PhaseInterrupted, &Interval{Start: KnownAt(start.At), End: UnknownBound()}
}

// phaseMark is the edge m records, in UTC; nil for no marker.
func phaseMark(m *Marker) *PhaseMark {
	if m == nil {
		return nil
	}
	r := m.Report
	mark := &PhaseMark{Kind: r.Kind, Emitter: r.Emitter, At: r.At.UTC(), Outcome: r.Outcome}
	return mark
}

// markerEvidence says who reported m and when sillage received it: the
// pod's claim, never a fact the controller recorded.
func markerEvidence(m Marker) Evidence {
	s, r := m.Sender, m.Report
	return Evidence{Source: SourceKelos, Instance: s.Cluster, Ref: "pods/" + s.Namespace + "/" + s.Pod,
		Note: fmt.Sprintf("%s of span %s reported by %s in pod %s, received %s", r.Edge, r.Span, r.Emitter,
			s.Pod, m.ReceivedAt.UTC().Format(time.RFC3339Nano))}
}

// uniqueEvidence sorts the evidence and drops repeats: both edges of a
// span joined by one pod give the same join evidence.
func uniqueEvidence(evidence []Evidence) []Evidence {
	sortEvidence(evidence)
	return slices.Compact(evidence)
}

// comparePhases orders phases by their first recorded edge, then pod and
// span.
func comparePhases(a, b Phase) int {
	return cmp.Or(
		a.firstAt().Compare(b.firstAt()),
		strings.Compare(a.PodUID, b.PodUID),
		strings.Compare(a.Span, b.Span),
	)
}

// firstAt is the earlier of the phase's recorded edges.
func (p Phase) firstAt() time.Time {
	switch {
	case p.Start == nil:
		return p.End.At
	case p.End == nil || p.Start.At.Before(p.End.At):
		return p.Start.At
	}
	return p.End.At
}
