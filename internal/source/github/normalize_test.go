package github

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// update rewrites every records.golden.json from its recorded responses:
// go test ./internal/source/github -run TestNormalizeGolden -update
var update = flag.Bool("update", false, "rewrite the GitHub fixtures' golden files")

const (
	goldenFile = "records.golden.json"
	// goldenPerm is the mode of a rewritten golden file.
	goldenPerm = 0o644
)

// storyRecords reads a story as the collector would and normalizes every
// pull request into one set of records.
func storyRecords(t *testing.T, story string) model.Records {
	t.Helper()
	var snaps []Snapshot
	switch story {
	case "no-pr":
		snaps, _ = fetchBranch(t, story, "kelos/vikunja-4300")
	case "two-prs":
		snaps, _ = fetchBranch(t, story, "kelos/vikunja-4301")
	case "closed-unmerged":
		snaps = []Snapshot{fetchPR(t, story, 31)}
	case "checks-two-shas":
		snaps = []Snapshot{fetchPR(t, story, 41)}
	default:
		t.Fatalf("story %s: add how to read it", story)
	}
	all := model.Records{Events: []model.Event{}, PullRequests: []model.PullRequest{}}
	for _, s := range snaps {
		recs, err := Normalize(s)
		if err != nil {
			t.Fatalf("Normalize pull request %d: %v", s.PullRequest.Number, err)
		}
		all.Events = append(all.Events, recs.Events...)
		all.PullRequests = append(all.PullRequests, recs.PullRequests...)
	}
	return all
}

func stories(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestNormalizeGolden(t *testing.T) {
	for _, story := range stories(t) {
		t.Run(story, func(t *testing.T) {
			recs := storyRecords(t, story)
			// Normalize produces events and pull requests only.
			got, err := json.MarshalIndent(struct {
				Events       []model.Event       `json:"events"`
				PullRequests []model.PullRequest `json:"pullRequests"`
			}{recs.Events, recs.PullRequests}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(fixtureDir, story, goldenFile)
			if *update {
				if err := os.WriteFile(path, got, goldenPerm); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the normalized records; run with -update and review the diff", path)
			}
		})
	}
}

// The story's records must also assemble into a story.
func TestNormalizedRecordsBuildAStory(t *testing.T) {
	root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 4301}}
	for _, story := range stories(t) {
		if _, err := model.BuildStory(root, storyRecords(t, story)); err != nil {
			t.Errorf("%s: %v", story, err)
		}
	}
}

// eventsOf returns the kinds of pull request number's events, in order.
func eventsOf(recs model.Records, number int) []model.EventKind {
	var kinds []model.EventKind
	for _, e := range recs.Events {
		if e.Related.PR != nil && e.Related.PR.Number == number {
			kinds = append(kinds, e.Kind)
		}
	}
	return kinds
}

func eventByID(t *testing.T, recs model.Records, id string) model.Event {
	t.Helper()
	for _, e := range recs.Events {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no event %s", id)
	return model.Event{}
}

func TestTwoPRsStory(t *testing.T) {
	recs := storyRecords(t, "two-prs")
	want21 := []model.EventKind{model.KindPROpened, model.KindReviewRequested, model.KindPRClosed,
		model.KindPRReopened, model.KindComment, model.KindPRClosed}
	if got := eventsOf(recs, 21); !slices.Equal(got, want21) {
		t.Errorf("pull request 21 = %v, want %v", got, want21)
	}
	// The merge's own close is dropped; the pending review is not shown.
	want22 := []model.EventKind{model.KindPROpened, model.KindReviewRequested, model.KindReviewRequestRemoved,
		model.KindRequest, model.KindComment, model.KindReviewSubmitted, model.KindPRMerged}
	if got := eventsOf(recs, 22); !slices.Equal(got, want22) {
		t.Errorf("pull request 22 = %v, want %v", got, want22)
	}
	opened21 := eventByID(t, recs, "pulls/7021")
	if ex := opened21.Executor; ex == nil || *ex != (model.Executor{Name: "agent-opus", Kind: model.ActorAgent}) {
		t.Errorf("pull request 21 executor = %+v, want agent-opus from its label", ex)
	}
	if got, want := opened21.Actor, (model.Account{Login: "owner-gh", Kind: model.ActorPerson}); got != want {
		t.Errorf("pull request 21 actor = %+v, want %+v", got, want)
	}
	if ex := eventByID(t, recs, "pulls/7022").Executor; ex != nil {
		t.Errorf("pull request 22 executor = %+v, want unknown: two agents' labels", ex)
	}
	if got := eventByID(t, recs, "issues/comments/5203").Actor.Kind; got != model.ActorService {
		t.Errorf("bot comment actor kind = %s, want %s", got, model.ActorService)
	}
	if got := eventByID(t, recs, "issues/events/8203").Details["team"]; got != "maintainers" {
		t.Errorf("team review request names %q, want maintainers", got)
	}
	if got := eventByID(t, recs, "reviews/3201").Details["state"]; got != "approved" {
		t.Errorf("review state = %q, want approved", got)
	}
	for _, e := range recs.Events {
		if e.Kind == model.KindTaskDone {
			t.Errorf("event %s: a pull request never marks a task done", e.ID)
		}
	}
}

func TestClosedUnmergedStory(t *testing.T) {
	recs := storyRecords(t, "closed-unmerged")
	want := []model.EventKind{model.KindPROpened, model.KindReviewSubmitted, model.KindPRClosed}
	if got := eventsOf(recs, 31); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	opened := eventByID(t, recs, "pulls/7031")
	if opened.Executor != nil || opened.Actor.Login != "reviewer-gh" {
		t.Errorf("opened by %+v, executor %+v: want reviewer-gh and no executor", opened.Actor, opened.Executor)
	}
	if got := recs.PullRequests[0].Checks[0].Conclusion; got != model.CheckCancelled {
		t.Errorf("check conclusion = %s, want %s", got, model.CheckCancelled)
	}
}

// The first commit's failure is kept beside the head's results, both
// attempts of a re-run check are kept, and checks not completed stay open.
func TestChecksTwoSHAsStory(t *testing.T) {
	recs := storyRecords(t, "checks-two-shas")
	pr := recs.PullRequests[0]
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	if got, want := pr.SHAs(), []string{shaA, shaB}; !slices.Equal(got, want) {
		t.Errorf("SHAs with checks = %v, want %v", got, want)
	}
	byID := make(map[int64]model.CheckRun)
	for _, c := range pr.Checks {
		byID[c.ID] = c
	}
	for id, want := range map[int64]model.CheckConclusion{
		9401: model.CheckFailure, 9402: model.CheckSuccess, 9403: model.CheckFailure,
		9404: model.CheckSuccess, 9405: model.CheckPending, 9406: model.CheckPending,
	} {
		if got := byID[id].Conclusion; got != want {
			t.Errorf("check %d = %s, want %s", id, got, want)
		}
	}
	if b := byID[9405].Interval; b.Start.State != model.BoundKnown || b.End.State != model.BoundOpen {
		t.Errorf("in-progress check interval = %+v, want a known start and an open end", b)
	}
	if b := byID[9406].Interval; b.Start.State != model.BoundUnknown || b.End.State != model.BoundOpen {
		t.Errorf("queued check interval = %+v, want an unknown start and an open end", b)
	}
	// The second agent label came an hour after creation: it routes
	// follow-ups and names nobody.
	if ex := eventByID(t, recs, "pulls/7041").Executor; ex == nil || ex.Name != "agent-opus" {
		t.Errorf("executor = %+v, want agent-opus", ex)
	}
}

func TestIsRequest(t *testing.T) {
	for body, want := range map[string]bool{
		"/kelos rebase":        true,
		"\n  /kelos\tplease":   true,
		"/kelos":               true,
		"/kelos @agent-a fix":  true,
		"/kelosx":              false,
		"please /kelos rebase": false,
		"":                     false,
	} {
		if got := isRequest(body); got != want {
			t.Errorf("isRequest(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestAccount(t *testing.T) {
	tests := []struct {
		user *User
		want model.Account
	}{
		{nil, model.Account{Kind: model.ActorUnknown}},
		{&User{}, model.Account{Kind: model.ActorUnknown}},
		{&User{Login: "owner-gh", Type: "User"}, model.Account{Login: "owner-gh", Kind: model.ActorPerson}},
		{&User{Login: "helper[bot]", Type: botType}, model.Account{Login: "helper[bot]", Kind: model.ActorService}},
	}
	for _, tt := range tests {
		if got := account(tt.user); got != tt.want {
			t.Errorf("account(%+v) = %+v, want %+v", tt.user, got, tt.want)
		}
	}
}

func TestCheckRunConversionErrors(t *testing.T) {
	stale, bogus := "stale", "exploded"
	started := testObservedAt
	base := CheckRun{ID: 1, HeadSHA: "c", Name: "test", Status: checkStatusCompleted, StartedAt: &started, CompletedAt: &started}
	tests := []struct {
		name       string
		conclusion *string
		want       string
	}{
		{"stale", &stale, ""},
		{"no conclusion", nil, "without a conclusion"},
		{"unknown conclusion", &bogus, "unknown conclusion"},
	}
	for _, tt := range tests {
		s := testSnapshot()
		c := base
		c.Conclusion = tt.conclusion
		s.Checks = []CheckRun{c, c}
		recs, err := Normalize(s)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.want == "" && len(recs.PullRequests[0].Checks) != 1:
			t.Errorf("%s: %d checks, want the repeated one once", tt.name, len(recs.PullRequests[0].Checks))
		case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// A completed check without times keeps both ends unknown.
func TestCheckRunUnknownTimes(t *testing.T) {
	success := "success"
	got, err := checkRun(CheckRun{ID: 1, HeadSHA: "c", Name: "test", Status: checkStatusCompleted, Conclusion: &success})
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval.Start.State != model.BoundUnknown || got.Interval.End.State != model.BoundUnknown {
		t.Errorf("interval = %+v, want both ends unknown", got.Interval)
	}
}

// A comment repeated across two pages counts once.
func TestNormalizeDedupesRecords(t *testing.T) {
	s := testSnapshot()
	c := IssueComment{ID: 5, User: testOwner, CreatedAt: testCreated}
	s.Comments = []IssueComment{c, c}
	recs, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(recs.Events); got != 2 {
		t.Errorf("%d events, want the opening and one comment", got)
	}
}
