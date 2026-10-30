package collector

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// gadgetsRepo is a second repository a workspace maps to.
var gadgetsRepo = github.Repository{Owner: "example-org", Name: "gadgets"}

// askedGitHub answers every branch read with snaps in found, nothing in
// the others, and records the repositories it was asked about.
type askedGitHub struct {
	found map[string][]github.Snapshot
	asked *[]string
}

func newAskedGitHub(found map[string][]github.Snapshot) askedGitHub {
	return askedGitHub{found: found, asked: new([]string)}
}

func (g askedGitHub) FetchBranch(_ context.Context, base github.Repository, _ github.Branch) (
	[]github.Snapshot, error) {
	*g.asked = append(*g.asked, base.String())
	return g.found[base.String()], nil
}

// collectAsked assembles task 104's story from vk, recalling kept, with
// GitHub answered by gh, over cfg.
func collectAsked(t *testing.T, cfg Config, vk vikunja.Snapshot, kept Kept, gh askedGitHub) Result {
	t.Helper()
	c, err := New(cfg, Sources{Vikunja: fixedVikunja{vk}, Kelos: fixedKelos{kelos.Snapshot{}}, GitHub: gh})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Recollect(t.Context(), taskRoot(104), kept)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// commentedTask is task 104 with one comment of body by its owner.
func commentedTask(body string) vikunja.Snapshot {
	s := announcedTask(104, "", nil)
	s.Comments = []vikunja.Comment{{
		ID: 9001, Body: body, Created: at(1), Updated: at(1), Author: vikunja.User{ID: 7, Username: "owner"},
	}}
	return s
}

// checkPRJoined fails the test unless pull request 7 is joined to the
// root with evidence whose note holds note.
func checkPRJoined(t *testing.T, s model.Story, note string) {
	t.Helper()
	c, ok := linkBetween(s, prRef(7), taskRoot(104).Ref())
	if !ok {
		t.Fatalf("pull request 7 is not joined to the task: %v", s.Correlations)
	}
	if !slices.ContainsFunc(c.Evidence, func(ev model.Evidence) bool { return strings.Contains(ev.Note, note) }) {
		t.Errorf("evidence = %+v, want one saying %q", c.Evidence, note)
	}
}

func TestRepositoryOfAnAnnouncedTaskName(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		asked    []string
		searches int
		wantNote string
	}{
		{"its project", "vikunja-widgets-agent-opus-104", []string{"example-org/widgets"}, 0,
			"vikunja-widgets-agent-opus-104, follows the spawners' convention for project widgets"},
		{"a follow-up's project", "vikunja-fu-widgets-104-c9001", []string{"example-org/widgets"}, 0,
			"convention for project widgets"},
		{"another task's Task", "vikunja-widgets-agent-opus-105", []string{"example-org/gadgets", "example-org/widgets"},
			2, "was looked for in each repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Workspaces = map[string]github.Repository{testWorkspace: testRepo, "gadgets": gadgetsRepo}
			gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
			res := collectAsked(t, cfg, announcedTask(104, tt.task, map[int]string{1: tt.task + "-abcde"}), Kept{}, gh)
			if !slices.Equal(*gh.asked, tt.asked) || res.Searches != tt.searches {
				t.Errorf("asked %v, %d searches; want %v, %d", *gh.asked, res.Searches, tt.asked, tt.searches)
			}
			checkPRJoined(t, res.Story, tt.wantNote)
		})
	}
}

func TestRepositoryOfACommentsPullRequestLink(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = nil
	body := `<p>Opened <a href="https://GITHUB.example.com/example-org/widgets/pull/7">#7</a>, and see ` +
		`https://github.example.org/example-org/gadgets/pull/3 elsewhere.</p>`
	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	res := collectAsked(t, cfg, commentedTask(body), Kept{}, gh)
	if want := []string{"example-org/widgets"}; !slices.Equal(*gh.asked, want) || res.Searches != 0 {
		t.Errorf("asked %v, %d searches; want %v and no search", *gh.asked, res.Searches, want)
	}
	checkPRJoined(t, res.Story, "comment 9001 links pull request #7 of example-org/widgets")
}

func TestNoRepositoryOfALinkToAnotherHost(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = nil
	gh := newAskedGitHub(nil)
	res := collectAsked(t, cfg, commentedTask(`<p>https://github.example.org/example-org/widgets/pull/7</p>`), Kept{}, gh)
	if len(*gh.asked) != 0 || !hasNote(coverageOf(t, res.Story, model.SourceGitHub), "no repository known") {
		t.Errorf("asked %v, coverage %+v; want nothing read and a note saying why", *gh.asked, res.Story.Coverage)
	}
}

func TestRepositoryOfAKeptPullRequest(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = map[string]github.Repository{"gadgets": gadgetsRepo}
	kept := Kept{PullRequests: []model.PullRequest{{
		Key: model.PRKey{Instance: testGitHubHost, Repository: testRepo.String(), Number: 7}, Branch: taskBranch(104),
	}}}
	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	res := collectAsked(t, cfg, announcedTask(104, "", nil), kept, gh)
	if want := []string{"example-org/widgets"}; !slices.Equal(*gh.asked, want) || res.Searches != 0 {
		t.Errorf("asked %v, %d searches; want %v and no search", *gh.asked, res.Searches, want)
	}
	checkPRJoined(t, res.Story, "the store holds pull request #7 of this repository")
}

// A story no record names a repository of has its branch looked for in
// each repository the workspaces map to, once each; finding nothing is
// noted.
func TestBranchSearchedWhenNoRecordNamesARepository(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = map[string]github.Repository{testWorkspace: testRepo, "gadgets": gadgetsRepo, "more": testRepo}
	want := []string{"example-org/gadgets", "example-org/widgets"}

	gh := newAskedGitHub(map[string][]github.Snapshot{testRepo.String(): {pullRequest(7, taskBranch(104))}})
	res := collectAsked(t, cfg, announcedTask(104, "", nil), Kept{}, gh)
	if !slices.Equal(*gh.asked, want) || res.Searches != len(want) {
		t.Errorf("asked %v, %d searches; want %v, once each", *gh.asked, res.Searches, want)
	}
	checkPRJoined(t, res.Story, "branch kelos/vikunja-104 was looked for in each repository "+EnvWorkspaces+" lists")
	if cov := coverageOf(t, res.Story, model.SourceGitHub); hasNote(cov, "none of the") {
		t.Errorf("github notes = %q, want none: the search found the pull request", cov.Notes)
	}

	gh = newAskedGitHub(nil)
	res = collectAsked(t, cfg, announcedTask(104, "", nil), Kept{}, gh)
	cov := coverageOf(t, res.Story, model.SourceGitHub)
	if cov.LastSuccessfulRead == nil || !hasNote(cov, "none of the 2 repositories "+EnvWorkspaces+" lists") {
		t.Errorf("github coverage = %+v, want a read and a note that the search found nothing", cov)
	}
}

// A root whose runs' labels name its repository is not searched for.
func TestNoSearchWhenALabelNamesTheRepository(t *testing.T) {
	sc := storyCases["implementation-merged"]
	c := newStoryCollector(t, sc.Dirs)
	c.cfg.Workspaces = map[string]github.Repository{testWorkspace: testRepo, "gadgets": gadgetsRepo}
	res, err := c.Collect(t.Context(), sc.Root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Searches != 0 {
		t.Errorf("searches = %d, want none", res.Searches)
	}
}
