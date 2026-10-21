package github

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// fetchBranch reads branch of testRepo from a story's recorded responses
// and returns the snapshots and the requests the client sent.
func fetchBranch(t *testing.T, story, branch string) ([]Snapshot, []string) {
	t.Helper()
	fake := newFakeGitHub(t, story)
	snaps, err := newTestClient(t, fake).FetchBranch(t.Context(), testRepo, Branch{Repository: testRepo, Name: branch})
	if err != nil {
		t.Fatalf("FetchBranch: %v", err)
	}
	return snaps, fake.log.seen()
}

// fetchPR reads pull request number of testRepo from a story's recorded
// responses.
func fetchPR(t *testing.T, story string, number int) Snapshot {
	t.Helper()
	snap, err := newTestClient(t, newFakeGitHub(t, story)).FetchPR(t.Context(), testRepo, number)
	if err != nil {
		t.Fatalf("FetchPR: %v", err)
	}
	return snap
}

func prNumbers(snaps []Snapshot) []int {
	numbers := make([]int, len(snaps))
	for i, s := range snaps {
		numbers[i] = s.PullRequest.Number
	}
	return numbers
}

func TestFetchBranchNoPR(t *testing.T) {
	snaps, reqs := fetchBranch(t, "no-pr", "kelos/vikunja-4300")
	if len(snaps) != 0 {
		t.Errorf("got %d pull requests, want none", len(snaps))
	}
	if want := []string{testRepoPath + "/pulls"}; !slices.Equal(reqs, want) {
		t.Errorf("requests = %v, want %v", reqs, want)
	}
}

// Both pull requests on the branch are read, the closed one too; the
// listed pull requests whose head is a fork's branch of the same name, or
// another branch, are dropped without being read.
func TestFetchBranchTwoPRs(t *testing.T) {
	snaps, reqs := fetchBranch(t, "two-prs", "kelos/vikunja-4301")
	if got, want := prNumbers(snaps), []int{21, 22}; !slices.Equal(got, want) {
		t.Fatalf("pull requests = %v, want %v", got, want)
	}
	for _, r := range reqs {
		if strings.Contains(r, "/23") || strings.Contains(r, "/24") {
			t.Errorf("read %s, a pull request of another head", r)
		}
	}
	if !slices.Contains(reqs, testRepoPath+"/pulls?page=2") {
		t.Errorf("requests = %v, want the second page of pulls", reqs)
	}
	for _, s := range snaps {
		if s.Instance != testInstance || s.Repository != testRepo || !s.ObservedAt.Equal(testObservedAt) {
			t.Errorf("pull request %d: instance %q, repository %v, observed %v", s.PullRequest.Number,
				s.Instance, s.Repository, s.ObservedAt)
		}
	}
	if got, want := len(snaps[0].Timeline), 6; got != want {
		t.Errorf("pull request 21: %d timeline items over 3 pages, want %d", got, want)
	}
}

func TestFetchPRClosedUnmerged(t *testing.T) {
	snap := fetchPR(t, "closed-unmerged", 31)
	if snap.PullRequest.MergedAt != nil || snap.PullRequest.ClosedAt == nil {
		t.Errorf("merged_at %v, closed_at %v: want closed and not merged",
			snap.PullRequest.MergedAt, snap.PullRequest.ClosedAt)
	}
	if got, want := len(snap.Reviews), 1; got != want {
		t.Errorf("%d reviews, want %d", got, want)
	}
}

// Each commit's check runs are read and kept, the earlier commit's as
// well as the head's, re-run attempts included.
func TestFetchPRChecksOnTwoSHAs(t *testing.T) {
	snap := fetchPR(t, "checks-two-shas", 41)
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	if want := []string{shaA, shaB}; !slices.Equal(snap.SHAs, want) {
		t.Errorf("SHAs = %v, want %v", snap.SHAs, want)
	}
	var ids []int64
	for _, c := range snap.Checks {
		ids = append(ids, c.ID)
	}
	if want := []int64{9401, 9402, 9403, 9404, 9405, 9406}; !slices.Equal(ids, want) {
		t.Errorf("check runs = %v, want %v", ids, want)
	}
}

func TestCommitSHAs(t *testing.T) {
	timeline := []TimelineEvent{
		{Event: timelineCommitted, SHA: "a"},
		{Event: timelineLabeled},
		{Event: timelineForcePushed, CommitID: "c"},
		{Event: timelineCommitted, SHA: "a"},
		{Event: timelineCommitted},
	}
	if got, want := commitSHAs(timeline, "d"), []string{"a", "c", "d"}; !slices.Equal(got, want) {
		t.Errorf("commitSHAs = %v, want %v", got, want)
	}
	if got, want := commitSHAs(timeline, "c"), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("commitSHAs with the head named = %v, want %v", got, want)
	}
}

func TestSplitLatest(t *testing.T) {
	shas := []string{"a", "b", "c"}
	tests := []struct {
		limit               int
		wantOlder, wantLast []string
	}{
		{3, []string{}, []string{"a", "b", "c"}},
		{5, []string{}, []string{"a", "b", "c"}},
		{2, []string{"a"}, []string{"b", "c"}},
	}
	for _, tt := range tests {
		older, latest := splitLatest(shas, tt.limit)
		if !slices.Equal(older, tt.wantOlder) || !slices.Equal(latest, tt.wantLast) {
			t.Errorf("splitLatest(%d) = %v, %v; want %v, %v", tt.limit, older, latest, tt.wantOlder, tt.wantLast)
		}
	}
}

func TestFetchPRRejectsOtherNumber(t *testing.T) {
	f := newFakeGitHub(t, "closed-unmerged")
	f.mux.HandleFunc("GET "+testRepoPath+"/pulls/32", func(w http.ResponseWriter, r *http.Request) {
		f.serveFile(w, r, "pull-31.json")
	})
	_, err := newTestClient(t, f).FetchPR(t.Context(), testRepo, 32)
	if err == nil || !strings.Contains(err.Error(), "answered with pull request 31") {
		t.Errorf("FetchPR error = %v, want a number mismatch", err)
	}
}

func TestFetchRejectsBadArguments(t *testing.T) {
	c := newTestClient(t, http.NotFoundHandler())
	tests := []struct {
		name string
		err  error
	}{
		{"bad base", fetchErr(c.FetchBranch(t.Context(), Repository{Owner: "o"}, Branch{Repository: testRepo, Name: "b"}))},
		{"bad head repository", fetchErr(c.FetchBranch(t.Context(), testRepo, Branch{Repository: Repository{Name: "r"}, Name: "b"}))},
		{"no branch", fetchErr(c.FetchBranch(t.Context(), testRepo, Branch{Repository: testRepo, Name: " "}))},
		{"bad repository", snapErr(c.FetchPR(t.Context(), Repository{Owner: "o/x", Name: "r"}, 1))},
		{"zero number", snapErr(c.FetchPR(t.Context(), testRepo, 0))},
	}
	for _, tt := range tests {
		if tt.err == nil {
			t.Errorf("%s: no error", tt.name)
		}
	}
}

func fetchErr(_ []Snapshot, err error) error { return err }

func snapErr(_ Snapshot, err error) error { return err }
