package model

import (
	"strings"
	"testing"
	"time"
)

const testSpan = "5f0c2b3e9a7d4c1e8b6a2f4d3c1e0b9a"

var testMarkerAt = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func testLoginEnd() MarkerReport {
	attempts := 3
	return MarkerReport{
		Version: MarkerVersion, Emitter: EmitterWikiGQL, Kind: MarkerWikiLogin, Edge: EdgeEnd,
		Span: testSpan, At: testMarkerAt, TaskName: "vikunja-example-agent-42",
		Outcome: MarkerLoginOK, Attempts: &attempts,
	}
}

func testMarker() Marker {
	return Marker{
		Report: testLoginEnd(),
		Sender: MarkerSender{Cluster: "cluster-a", Namespace: "agents", ServiceAccount: "default",
			Pod: "vikunja-example-agent-42-abcde", PodUID: "pod-uid-1"},
		ReceivedAt: testMarkerAt.Add(time.Second),
	}
}

func TestMarkerReportValidate(t *testing.T) {
	zero, two := 0, 2
	for _, c := range []struct {
		name   string
		change func(*MarkerReport)
		want   string // "" when valid
	}{
		{"a wiki login's end", func(*MarkerReport) {}, ""},
		{"a start", func(r *MarkerReport) { r.Edge, r.Outcome, r.Attempts = EdgeStart, "", nil }, ""},
		{"no task name", func(r *MarkerReport) { r.TaskName = "" }, ""},
		{"a blocker wait's end", func(r *MarkerReport) {
			r.Emitter, r.Kind, r.Outcome, r.Attempts = EmitterWaitForBlockers, MarkerBlockerWait, MarkerTaskMissing, nil
		}, ""},
		{"an agent's end", func(r *MarkerReport) {
			r.Emitter, r.Kind, r.Outcome, r.Attempts = EmitterEntrypoint, MarkerAgent, MarkerLimitHit, nil
		}, ""},
		{"another version", func(r *MarkerReport) { r.Version = 2 }, "version"},
		{"no version", func(r *MarkerReport) { r.Version = 0 }, "version"},
		{"a wiki request", func(r *MarkerReport) { r.Kind = "wiki-request" }, "unknown kind"},
		{"another emitter", func(r *MarkerReport) { r.Emitter = EmitterEntrypoint }, "emitted by"},
		{"an unknown emitter", func(r *MarkerReport) { r.Emitter = "curl" }, "emitted by"},
		{"a point marker", func(r *MarkerReport) { r.Edge = "point" }, "unknown edge"},
		{"an upper-case span", func(r *MarkerReport) { r.Span = strings.ToUpper(testSpan) }, "span"},
		{"a short span", func(r *MarkerReport) { r.Span = testSpan[1:] }, "span"},
		{"no time", func(r *MarkerReport) { r.At = time.Time{} }, "time"},
		{"a task name with a path", func(r *MarkerReport) { r.TaskName = "../etc" }, "task name"},
		{"a task name too long", func(r *MarkerReport) { r.TaskName = strings.Repeat("a", maxObjectNameLength+1) },
			"task name"},
		{"a start with an outcome", func(r *MarkerReport) { r.Edge, r.Attempts = EdgeStart, nil }, "start"},
		{"a start with attempts", func(r *MarkerReport) { r.Edge, r.Outcome = EdgeStart, "" }, "start"},
		{"an end without an outcome", func(r *MarkerReport) { r.Outcome = "" }, "outcome"},
		{"another kind's outcome", func(r *MarkerReport) { r.Outcome = MarkerUnblocked }, "outcome"},
		{"a login without attempts", func(r *MarkerReport) { r.Attempts = nil }, "attempts"},
		{"a login with no attempt", func(r *MarkerReport) { r.Attempts = &zero }, "attempts"},
		{"a blocker wait with attempts", func(r *MarkerReport) {
			r.Emitter, r.Kind, r.Outcome, r.Attempts = EmitterWaitForBlockers, MarkerBlockerWait, MarkerUnblocked, &two
		}, "no attempts"},
	} {
		r := testLoginEnd()
		c.change(&r)
		err := r.Validate()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error %v, want one about %q", c.name, err, c.want)
		}
	}
}

func TestMarkerReportEqual(t *testing.T) {
	a, b := testLoginEnd(), testLoginEnd()
	attempts := *a.Attempts
	b.Attempts = &attempts
	b.At = b.At.In(time.FixedZone("west", -7*60*60))
	if !a.Equal(b) {
		t.Error("the same report in another zone is not equal")
	}
	for name, change := range map[string]func(*MarkerReport){
		"attempts": func(r *MarkerReport) { n := 4; r.Attempts = &n },
		"none":     func(r *MarkerReport) { r.Attempts = nil },
		"outcome":  func(r *MarkerReport) { r.Outcome = MarkerLoginRejected },
		"time":     func(r *MarkerReport) { r.At = r.At.Add(time.Millisecond) },
		"task":     func(r *MarkerReport) { r.TaskName = "another" },
	} {
		c := testLoginEnd()
		change(&c)
		if a.Equal(c) {
			t.Errorf("reports differing in %s are equal", name)
		}
	}
}

func TestMarkerValidate(t *testing.T) {
	if err := testMarker().Validate(); err != nil {
		t.Fatal(err)
	}
	evidence := []Evidence{{Source: SourceKelos, Instance: "cluster-a", Ref: "tasks/agents/t"}}
	for name, change := range map[string]func(*Marker){
		"no pod uid":      func(m *Marker) { m.Sender.PodUID = "" },
		"no cluster":      func(m *Marker) { m.Sender.Cluster = "" },
		"not received":    func(m *Marker) { m.ReceivedAt = time.Time{} },
		"an invalid body": func(m *Marker) { m.Report.Kind = "tests" },
		"a join by pod key": func(m *Marker) {
			m.Join = &MarkerJoin{Run: RunKey{Cluster: "c", Namespace: "n", Pod: "p"},
				Confidence: ConfidenceIdentifier, Evidence: evidence}
		},
		"a join without evidence": func(m *Marker) {
			m.Join = &MarkerJoin{Run: RunKey{Cluster: "c", Namespace: "n", UID: "u"}, Confidence: ConfidenceHeuristic}
		},
		"a join of unknown confidence": func(m *Marker) {
			m.Join = &MarkerJoin{Run: RunKey{Cluster: "c", Namespace: "n", UID: "u"}, Confidence: "guess",
				Evidence: evidence}
		},
	} {
		m := testMarker()
		change(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: valid", name)
		}
	}
}
