package model

import (
	"testing"
	"time"
)

type runCase = struct {
	name   string
	mutate func(*Run)
	want   string
}

func runRunCases(t *testing.T, tests []runCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRun("uid-1")
			tt.mutate(&r)
			checkErr(t, r.Validate(), tt.want)
		})
	}
}

func TestRunValidate(t *testing.T) {
	podKey := RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: "pod-1"}
	tests := []runCase{
		{"valid", func(*Run) {}, ""},
		{"invalid key", func(r *Run) { r.Key = RunKey{} }, "cluster and namespace"},
		{"key with uid and pod", func(r *Run) { r.Key.Pod = "pod-1" }, "exactly one of uid and pod"},
		{
			"pod-keyed with the same pod",
			func(r *Run) { r.Key, r.Pod = podKey, "pod-1" },
			"",
		},
		{
			"pod-keyed with another pod",
			func(r *Run) { r.Key, r.Pod = podKey, "pod-2" },
			"must name the same pod",
		},
		{
			"pod-keyed without a pod",
			func(r *Run) { r.Key, r.Pod = podKey, "" },
			"must name the same pod",
		},
		{"uid-keyed with a pod", func(r *Run) { r.Pod = "pod-9" }, ""},
		{"unknown role", func(r *Run) { r.Role = "reviewer" }, `unknown role "reviewer"`},
		{"empty role", func(r *Run) { r.Role = "" }, "unknown role"},
		{"role unknown", func(r *Run) { r.Role = RoleUnknown }, ""},
		{"unknown outcome", func(r *Run) { r.Outcome = "exploded" }, `unknown outcome "exploded"`},
		{"empty outcome", func(r *Run) { r.Outcome = "" }, "unknown outcome"},
		{"outcome unknown", func(r *Run) { r.Outcome = OutcomeUnknown }, ""},
		{"failed", func(r *Run) { r.Outcome = OutcomeFailed }, ""},
	}
	runRunCases(t, tests)
}

func TestRunValidateOutcomeAndInterval(t *testing.T) {
	tests := []runCase{
		{
			"interval ending before it starts",
			func(r *Run) { r.Interval = Interval{Start: KnownAt(at(30)), End: KnownAt(at(0))} },
			"ends before it starts",
		},
		{
			"running with an open end",
			func(r *Run) {
				r.Outcome = OutcomeRunning
				r.Interval.End = Open()
			},
			"",
		},
		{
			"running with a known end",
			func(r *Run) { r.Outcome = OutcomeRunning },
			"only a running run, has an open end",
		},
		{
			"running with an unknown end",
			func(r *Run) {
				r.Outcome = OutcomeRunning
				r.Interval.End = Unknown()
			},
			"only a running run, has an open end",
		},
		{
			"open end without running",
			func(r *Run) { r.Interval.End = Open() },
			"only a running run, has an open end",
		},
		{
			"unknown outcome with an open end",
			func(r *Run) {
				r.Outcome = OutcomeUnknown
				r.Interval.End = Open()
			},
			"only a running run, has an open end",
		},
		{
			"unknown interval",
			func(r *Run) {
				r.Outcome = OutcomeUnknown
				r.Interval = Interval{Start: Unknown(), End: Unknown()}
			},
			"",
		},
	}
	runRunCases(t, tests)
}

func TestRunValidateEvidence(t *testing.T) {
	tests := []runCase{
		{"no evidence", func(r *Run) { r.Evidence = nil }, "evidence is required"},
		{"empty evidence", func(r *Run) { r.Evidence = []Evidence{} }, "evidence is required"},
		{
			"evidence without ref",
			func(r *Run) { r.Evidence = []Evidence{{Source: SourceKelos, Instance: testCluster}} },
			"evidence needs",
		},
		{
			"second evidence invalid",
			func(r *Run) { r.Evidence = append(r.Evidence, Evidence{Source: SourceKelos, Ref: "x"}) },
			"evidence needs",
		},
	}
	runRunCases(t, tests)
}

func TestRunValidateNamesRun(t *testing.T) {
	r := testRun("uid-1")
	r.Role = "reviewer"
	checkErr(t, r.Validate(), "run cluster-a/agents/uid/uid-1:")
}

func TestRunRolesAndOutcomes(t *testing.T) {
	roles := []RunRole{
		RoleImplementation, RoleDiscussion, RoleTaskFollowUp, RolePRFollowUp,
		RoleWikiFollowUp, RoleConflictFix, RoleUnknown,
	}
	for _, role := range roles {
		if !role.valid() {
			t.Errorf("RunRole(%q).valid() = false, want true", role)
		}
	}
	for _, outcome := range []Outcome{OutcomeSucceeded, OutcomeFailed, OutcomeUnknown} {
		r := testRun("uid-1")
		r.Outcome = outcome
		if err := r.Validate(); err != nil {
			t.Errorf("outcome %q: %v", outcome, err)
		}
	}
}

func TestRunNormalize(t *testing.T) {
	r := testRun("uid-1")
	r.Interval = Interval{Start: KnownAt(atEST(0)), End: KnownAt(atEST(30))}
	r.Evidence = []Evidence{
		{Source: SourceKelos, Instance: testCluster, Ref: "b"},
		{Source: SourceKelos, Instance: testCluster, Ref: "a"},
	}
	r.normalize()
	if got := r.Interval.Start.At.Location(); got != time.UTC {
		t.Errorf("start location = %v, want UTC", got)
	}
	if r.Evidence[0].Ref != "a" || r.Evidence[1].Ref != "b" {
		t.Errorf("evidence refs = %q, %q, want a, b", r.Evidence[0].Ref, r.Evidence[1].Ref)
	}
}
