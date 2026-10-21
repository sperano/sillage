package model

import (
	"slices"
	"testing"
	"time"
)

type prCase = struct {
	name   string
	mutate func(*PullRequest)
	want   string
}

func runPRCases(t *testing.T, tests []prCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPullRequest()
			tt.mutate(&p)
			checkErr(t, p.Validate(), tt.want)
		})
	}
}

func TestPullRequestValidate(t *testing.T) {
	tests := []prCase{
		{"valid", func(*PullRequest) {}, ""},
		{"no checks", func(p *PullRequest) { p.Checks = nil }, ""},
		{"invalid key", func(p *PullRequest) { p.Key = PRKey{} }, "pull request key"},
		{"missing branch", func(p *PullRequest) { p.Branch = "" }, "branch is required"},
		{
			"duplicate check ids",
			func(p *PullRequest) { p.Checks = append(p.Checks, testCheck(testCheckID, "fedcba")) },
			"duplicate check 1001",
		},
		{
			"same name on another commit",
			func(p *PullRequest) { p.Checks = append(p.Checks, testCheck(testCheckID+1, "fedcba")) },
			"",
		},
	}
	runPRCases(t, tests)
}

func TestPullRequestValidateChecks(t *testing.T) {
	tests := []prCase{
		{"zero check id", func(p *PullRequest) { p.Checks[0].ID = 0 }, "positive id"},
		{"negative check id", func(p *PullRequest) { p.Checks[0].ID = -5 }, "positive id"},
		{"check without sha", func(p *PullRequest) { p.Checks[0].SHA = "" }, "a sha"},
		{"check without name", func(p *PullRequest) { p.Checks[0].Name = "" }, "a name"},
		{
			"unknown conclusion",
			func(p *PullRequest) { p.Checks[0].Conclusion = "exploded" },
			`unknown conclusion "exploded"`,
		},
		{"empty conclusion", func(p *PullRequest) { p.Checks[0].Conclusion = "" }, "unknown conclusion"},
		{
			"check interval ending before it starts",
			func(p *PullRequest) {
				p.Checks[0].Interval = Interval{Start: KnownAt(at(4)), End: KnownAt(at(1))}
			},
			"ends before it starts",
		},
		{
			"pending with an open end",
			func(p *PullRequest) {
				p.Checks[0].Conclusion = CheckPending
				p.Checks[0].Interval.End = Open()
			},
			"",
		},
		{
			"pending with a known end",
			func(p *PullRequest) { p.Checks[0].Conclusion = CheckPending },
			"only a pending check, has an open end",
		},
		{
			"pending with an unknown end",
			func(p *PullRequest) {
				p.Checks[0].Conclusion = CheckPending
				p.Checks[0].Interval.End = Unknown()
			},
			"only a pending check, has an open end",
		},
		{
			"open end without pending",
			func(p *PullRequest) { p.Checks[0].Interval.End = Open() },
			"only a pending check, has an open end",
		},
		{
			"completed with unknown bounds",
			func(p *PullRequest) {
				p.Checks[0].Interval = Interval{Start: Unknown(), End: Unknown()}
			},
			"",
		},
	}
	runPRCases(t, tests)
}

func TestCheckConclusionsValid(t *testing.T) {
	completed := []CheckConclusion{
		CheckSuccess, CheckFailure, CheckCancelled, CheckTimedOut,
		CheckNeutral, CheckSkipped, CheckActionRequired,
	}
	for _, c := range completed {
		check := testCheck(testCheckID, testSHA)
		check.Conclusion = c
		if err := check.validate(); err != nil {
			t.Errorf("conclusion %q: %v", c, err)
		}
	}
}

func TestPullRequestValidateNamesCheck(t *testing.T) {
	p := testPullRequest()
	p.Checks[0].Name = ""
	checkErr(t, p.Validate(), "check 1001:")
}

func TestPullRequestSHAs(t *testing.T) {
	tests := []struct {
		name string
		shas []string // one check per entry, in order
		want []string
	}{
		{"no checks", nil, nil},
		{"one commit", []string{"aaa"}, []string{"aaa"}},
		{"distinct commits", []string{"aaa", "bbb", "ccc"}, []string{"aaa", "bbb", "ccc"}},
		{"repeats collapse", []string{"aaa", "aaa", "bbb", "aaa"}, []string{"aaa", "bbb"}},
		{"first-seen order", []string{"ccc", "aaa", "ccc", "bbb"}, []string{"ccc", "aaa", "bbb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPullRequest()
			p.Checks = nil
			for i, sha := range tt.shas {
				p.Checks = append(p.Checks, testCheck(testCheckID+int64(i), sha))
			}
			if got := p.SHAs(); !slices.Equal(got, tt.want) {
				t.Errorf("SHAs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPullRequestNormalize(t *testing.T) {
	p := testPullRequest()
	p.Checks = []CheckRun{
		{
			ID: 2, SHA: "bbb", Name: "build",
			Interval:   Interval{Start: KnownAt(atEST(9)), End: KnownAt(atEST(12))},
			Conclusion: CheckSuccess,
		},
		{
			ID: 1, SHA: "aaa", Name: "build",
			Interval:   Interval{Start: KnownAt(atEST(1)), End: KnownAt(atEST(4))},
			Conclusion: CheckFailure,
		},
	}
	p.normalize()
	if p.Checks[0].ID != 1 || p.Checks[1].ID != 2 {
		t.Errorf("check ids = %d, %d, want 1, 2", p.Checks[0].ID, p.Checks[1].ID)
	}
	for _, c := range p.Checks {
		for name, b := range map[string]Bound{"start": c.Interval.Start, "end": c.Interval.End} {
			if b.At.Location() != time.UTC {
				t.Errorf("check %d %s location = %v, want UTC", c.ID, name, b.At.Location())
			}
		}
	}
}
