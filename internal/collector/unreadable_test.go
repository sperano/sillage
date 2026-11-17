package collector

import (
	"net/http"
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// privateRepo is a repository the credential cannot read.
var privateRepo = github.Repository{Owner: "zzz-org", Name: "private"}

// A repository a comment links to that the credential cannot read is
// noted, and the task's own repository still read.
func TestUnreadableLinkedRepositoryIsNoted(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			const task = "vikunja-widgets-agent-opus-104"
			vk := announcedTask(104, task, map[int]string{2: task + "-abcde"})
			vk.Comments = append(vk.Comments,
				commentedTask(`<p>See https://github.example.com/zzz-org/private/pull/3</p>`).Comments...)
			gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
			gh.refused[privateRepo.String()] = code
			res := collectAsked(t, testConfig(), vk, Kept{}, gh)
			cov := coverageOf(t, res.Story, model.SourceGitHub)
			if len(res.Failures) != 0 || cov.LastSuccessfulRead == nil {
				t.Fatalf("failures %v, coverage %+v; want a successful read", res.Failures, cov)
			}
			if !hasNote(cov, "repository zzz-org/private is not readable") ||
				!hasNote(cov, "comment 9001 links pull request #3 of zzz-org/private") {
				t.Errorf("github notes = %q, want one naming the unreadable repository and its link", cov.Notes)
			}
			checkPRJoined(t, res.Story, "convention for project widgets")
		})
	}
}

// A repository a label names fails the read when GitHub refuses it: the
// configuration is wrong, not a comment.
func TestUnreadableNamedRepositoryFails(t *testing.T) {
	gh := newAskedGitHub(nil)
	gh.refused[testRepo.String()] = http.StatusNotFound
	res := collectAsked(t, testConfig(), announcedTask(104, "", nil), Kept{}, gh, testRepo)
	if len(res.Failures) == 0 {
		t.Errorf("failures = none, want the named repository's refusal")
	}
}

// Another error than a refusal ends the read, whatever led to the
// repository.
func TestServerErrorOfALinkedRepositoryFails(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = nil
	gh := newAskedGitHub(nil)
	gh.refused[privateRepo.String()] = http.StatusBadGateway
	res := collectAsked(t, cfg, commentedTask(`<p>https://github.example.com/zzz-org/private/pull/3</p>`), Kept{}, gh)
	if len(res.Failures) == 0 {
		t.Errorf("failures = none, want the 502")
	}
}

// A comment's link to another task's pull request does not stand for the
// task's repository: when its repository has no pull request of the
// branch, the others are searched.
func TestSearchAfterAnUnrelatedLink(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = map[string]github.Repository{testWorkspace: testRepo, "gadgets": gadgetsRepo}
	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	res := collectAsked(t, cfg, commentedTask(`<p>Blocked on https://github.example.com/other-org/lib/pull/12</p>`),
		Kept{}, gh)
	want := []string{"other-org/lib", "example-org/gadgets", "example-org/widgets"}
	if !slices.Equal(*gh.asked, want) || res.Searches != 2 {
		t.Errorf("asked %v, %d searches; want %v, 2 searches", *gh.asked, res.Searches, want)
	}
	checkPRJoined(t, res.Story, "was looked for in each repository")
}

// A linked repository that has a pull request of the branch is the task's:
// nothing is searched.
func TestNoSearchWhenALinkedRepositoryHasTheBranch(t *testing.T) {
	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	res := collectAsked(t, testConfig(), commentedTask(`<p>https://github.example.com/example-org/widgets/pull/7</p>`),
		Kept{}, gh)
	if want := []string{"example-org/widgets"}; !slices.Equal(*gh.asked, want) || res.Searches != 0 {
		t.Errorf("asked %v, %d searches; want %v and no search", *gh.asked, res.Searches, want)
	}
}

// Links whose repositories have no pull request of the branch, and no
// workspace to search, are noted.
func TestLinkedRepositoriesWithoutTheBranchNoted(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = nil
	res := collectAsked(t, cfg, commentedTask(`<p>https://github.example.com/other-org/lib/pull/12</p>`), Kept{},
		newAskedGitHub(nil))
	cov := coverageOf(t, res.Story, model.SourceGitHub)
	if cov.LastSuccessfulRead == nil || !hasNote(cov, "none of the 1 repositories its comments link to") {
		t.Errorf("github coverage = %+v, want a read and a note that the links found nothing", cov)
	}
}

// A searched repository the credential cannot read is noted, and the
// others still searched.
func TestUnreadableSearchedRepositoryIsNoted(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = map[string]github.Repository{testWorkspace: testRepo, "private": privateRepo}
	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	gh.refused[privateRepo.String()] = http.StatusNotFound
	res := collectAsked(t, cfg, announcedTask(104, "", nil), Kept{}, gh)
	cov := coverageOf(t, res.Story, model.SourceGitHub)
	if len(res.Failures) != 0 || !hasNote(cov, "repository zzz-org/private is not readable") {
		t.Errorf("failures %v, notes %q; want none and a note naming the unreadable repository", res.Failures, cov.Notes)
	}
	checkPRJoined(t, res.Story, "was looked for in each repository")
}
