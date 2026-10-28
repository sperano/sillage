package collector

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
)

// pullRequest is a snapshot of pull request number on branch, opened at
// minute 0, with no other record.
func pullRequest(number int, branch string, unchecked ...string) github.Snapshot {
	repo := &github.Repo{FullName: testRepo.String()}
	return github.Snapshot{
		Instance: testGitHubHost, Repository: testRepo, ObservedAt: testNow, UncheckedSHAs: unchecked,
		PullRequest: github.PullRequest{
			ID: int64(7000 + number), Number: number, State: "open", CreatedAt: at(0),
			User: &github.User{Login: "owner-gh", Type: "User"},
			Head: github.Ref{Ref: branch, SHA: strings.Repeat("c", 40), Repo: repo},
			Base: github.Ref{Ref: "main", SHA: strings.Repeat("0", 40), Repo: repo},
		},
	}
}

func prRef(number int) model.Ref {
	return model.PRKey{Instance: testGitHubHost, Repository: testRepo.String(), Number: number}.Ref()
}

// collectPRs assembles task 104's story with the namespace's objects and
// the pull requests of its branch.
func collectPRs(t *testing.T, snap kelos.Snapshot, prs ...github.Snapshot) model.Story {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.Kelos = fixedKelos{snap}
	src.GitHub = fixedGitHub{prs}
	return collectWith(t, testConfig(), src, taskRoot(104), testRepo).Story
}

func linkBetween(s model.Story, from, to model.Ref) (model.Correlation, bool) {
	for _, c := range s.Correlations {
		if c.From == from && c.To == to {
			return c, true
		}
	}
	return model.Correlation{}, false
}

func TestUncheckedSHAsNoted(t *testing.T) {
	s := collectPRs(t, kelos.Snapshot{}, pullRequest(7, taskBranch(104), "d", "e"))
	cov := coverageOf(t, s, model.SourceGitHub)
	if cov.LastSuccessfulRead == nil || !hasNote(cov, "pull/7: the checks of its 2 oldest commits were not read") {
		t.Errorf("github coverage = %+v, want a read and a note naming the unchecked commits", cov)
	}
}

// Refused sub-reads keep the pull request and add one note per resource
// and status, saying what is unknown.
func TestRefusedSubReadsNoted(t *testing.T) {
	pr := pullRequest(7, taskBranch(104))
	pr.SHAs = []string{"a", "b", "c"}
	pr.Refused = []github.Refusal{
		{Resource: github.ResourceCheckRuns, SHA: "a", Code: http.StatusForbidden},
		{Resource: github.ResourceReviewComments, Code: http.StatusNotFound},
		{Resource: github.ResourceCheckRuns, SHA: "b", Code: http.StatusForbidden},
		{Resource: github.ResourceCheckRuns, SHA: "c", Code: http.StatusNotFound},
	}
	s := collectPRs(t, kelos.Snapshot{}, pr)
	if len(s.PullRequests) != 1 {
		t.Fatalf("pull requests = %v, want #7 kept", s.PullRequests)
	}
	cov := coverageOf(t, s, model.SourceGitHub)
	if cov.LastSuccessfulRead == nil {
		t.Errorf("github coverage = %+v, want a read", cov)
	}
	for _, want := range []string{
		"pull/7: check runs: forbidden (GitHub answered 403 for 2 of its 3 commits), so those commits' checks are unknown",
		"pull/7: check runs: not found (GitHub answered 404 for 1 of its 3 commits)",
		"pull/7: review comments: not found (GitHub answered 404), so its inline review comments are unknown",
	} {
		if !hasNote(cov, want) {
			t.Errorf("github notes = %q, want one with %q", cov.Notes, want)
		}
	}
}

// The read's refusals are counted by resource and status over every pull
// request, for the loop to report.
func TestRefusalsCounted(t *testing.T) {
	branch := taskBranch(104)
	first, second := pullRequest(7, branch), pullRequest(8, branch)
	first.Refused = []github.Refusal{
		{Resource: github.ResourceCheckRuns, SHA: "a", Code: http.StatusForbidden},
		{Resource: github.ResourceReviewComments, Code: http.StatusNotFound},
	}
	second.Refused = []github.Refusal{{Resource: github.ResourceCheckRuns, SHA: "b", Code: http.StatusForbidden}}
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.GitHub = fixedGitHub{[]github.Snapshot{first, second}}
	res := collectWith(t, testConfig(), src, taskRoot(104), testRepo)
	instance := coverageOf(t, res.Story, model.SourceGitHub).Instance
	want := []Refusal{
		{Source: model.SourceGitHub, Instance: instance, Resource: github.ResourceCheckRuns,
			Code: http.StatusForbidden, Count: 2},
		{Source: model.SourceGitHub, Instance: instance, Resource: github.ResourceReviewComments,
			Code: http.StatusNotFound, Count: 1},
	}
	if !slices.Equal(res.Refusals, want) {
		t.Errorf("refusals = %+v, want %+v", res.Refusals, want)
	}
}

func TestPRRunsByBranchAndName(t *testing.T) {
	branch := taskBranch(104)
	followUp := map[string]string{
		testVikunjaHost + "/workspace": testWorkspace, testVikunjaHost + "/role": "pr-followup",
	}
	s := collectPRs(t, kelos.Snapshot{
		Tasks: []kelos.Task{
			task("pr-followup-widgets-8-c1", "uid-picked", branch, followUp, 1, 2),
			task("pr-followup-widgets-99-c2", "uid-unnamed", branch, followUp, 1, 2),
		},
		Records: []kelos.TaskRecord{
			record("pr-followup-widgets-7-c3", "uid-gone", followUp, 1, 2),
			record("pr-followup-other-7-c4", "uid-other-repo", followUp, 1, 2),
		},
	}, pullRequest(7, branch), pullRequest(8, branch))
	tests := []struct {
		uid        string
		pr         int
		confidence model.Confidence
		ambiguous  bool
	}{
		{"uid-picked", 8, model.ConfidenceIdentifier, false},
		{"uid-unnamed", 7, model.ConfidenceHeuristic, true},
		{"uid-unnamed", 8, model.ConfidenceHeuristic, true},
		{"uid-gone", 7, model.ConfidenceHeuristic, false},
	}
	for _, tt := range tests {
		c, ok := linkBetween(s, runRef(tt.uid), prRef(tt.pr))
		if !ok || c.Confidence != tt.confidence || c.Ambiguous != tt.ambiguous {
			t.Errorf("%s -> #%d = %+v (found %v), want %s, ambiguous %v", tt.uid, tt.pr, c, ok, tt.confidence, tt.ambiguous)
		}
	}
	if _, ok := linkBetween(s, runRef("uid-picked"), prRef(7)); ok {
		t.Error("the run its name picks #8 for is joined to #7 too")
	}
	for _, r := range s.Runs {
		if r.Key == (model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: "uid-other-repo"}) {
			t.Error("a follow-up of another workspace's #7 is in the story")
		}
	}
}

func TestGitHubAnnouncementJoinsItsRun(t *testing.T) {
	const announcer, name = "gordon", "pr-followup-widgets-7-c5301"
	pr := pullRequest(7, taskBranch(104))
	pr.Comments = []github.Comment{{
		ID: 5301, User: &github.User{Login: announcer, Type: "Bot"}, CreatedAt: at(1),
		Body: "AGENT `" + testAgent + "` IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. KELOS TASK `" + name +
			"`, POD `" + name + "-pod00`.\n\nCOFFEE TIME! REPORT TO FOLLOW.",
	}}
	cfg := testConfig()
	cfg.GitHubAnnouncers = []string{announcer}
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	// Neither a label nor a branch joins the run: only the announcement.
	src.Kelos = fixedKelos{kelos.Snapshot{Tasks: []kelos.Task{task(name, "uid-announced", "", nil, 1, 2)}}}
	src.GitHub = fixedGitHub{[]github.Snapshot{pr}}
	s := collectWith(t, cfg, src, taskRoot(104), testRepo).Story
	c, ok := linkBetween(s, runRef("uid-announced"), prRef(7))
	if !ok || c.Confidence != model.ConfidenceIdentifier {
		t.Errorf("run -> #7 = %+v (found %v), want an identifier from the pull request's announcement", c, ok)
	}
}
