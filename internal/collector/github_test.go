package collector

import (
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
