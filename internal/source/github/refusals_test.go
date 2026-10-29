package github

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// checkRunRequests counts the check-runs requests in a request log.
func checkRunRequests(reqs []string) int {
	n := 0
	for _, r := range reqs {
		if strings.HasSuffix(r, "/check-runs") {
			n++
		}
	}
	return n
}

// refusingChecks returns a client of the checks-two-shas story whose
// check runs are refused with code.
func refusingChecks(t *testing.T, code int) (*Client, *fakeGitHub) {
	t.Helper()
	f := newFakeGitHub(t, "checks-two-shas")
	for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		refusePath(f, testRepoPath+"/commits/"+sha+"/check-runs", code, nil)
	}
	return newTestClient(t, f), f
}

// A repository whose check runs were refused is not asked again, yet each
// commit still has its refusal, from the second read on a remembered one.
func TestCheckRunsRefusalRemembered(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, f := refusingChecks(t, code)
			first, err := c.FetchPR(t.Context(), testRepo, 41)
			if err != nil {
				t.Fatal(err)
			}
			if got := checkRunRequests(f.log.seen()); got != 1 {
				t.Fatalf("first read sent %d check-runs requests, want 1: the second commit is remembered", got)
			}
			sent := checkRunRequests(f.log.seen())
			second, err := c.FetchPR(t.Context(), testRepo, 41)
			if err != nil {
				t.Fatal(err)
			}
			if got := checkRunRequests(f.log.seen()); got != sent {
				t.Errorf("second read sent %d more check-runs requests, want none", got-sent)
			}
			for _, snap := range []Snapshot{first, second} {
				if len(snap.Refused) != 2 {
					t.Errorf("refused = %+v, want one per commit", snap.Refused)
				}
			}
			for _, r := range second.Refused {
				if r.Resource == ResourceCheckRuns && (!r.Remembered || r.Code != code) {
					t.Errorf("second read refusal %+v, want remembered with %d", r, code)
				}
			}
		})
	}
}

// A remembered refusal keeps the head's validators incomplete.
func TestCheckRunsRefusalKeepsValidatorsIncomplete(t *testing.T) {
	c, _ := refusingChecks(t, http.StatusForbidden)
	if _, err := c.FetchPR(t.Context(), testRepo, 41); err != nil {
		t.Fatal(err)
	}
	tg := newTagger()
	if _, err := c.fetchDetails(t.Context(), testRepo, PullRequest{Number: 41, Head: Ref{SHA: strings.Repeat("b", 40)}}, tg); err != nil {
		t.Fatal(err)
	}
	if !tg.missing {
		t.Error("validators are complete, want them incomplete: the head's check runs were not read")
	}
}

// Another repository is asked, and the name is matched case-insensitively.
func TestCheckRunsRefusalPerRepository(t *testing.T) {
	c, _ := refusingChecks(t, http.StatusForbidden)
	if _, err := c.FetchPR(t.Context(), testRepo, 41); err != nil {
		t.Fatal(err)
	}
	upper := Repository{Owner: strings.ToUpper(testRepo.Owner), Name: testRepo.Name}
	if _, ok := c.rememberedCheckRunsRefusal(upper); !ok {
		t.Error("the refusal is not remembered for the same repository in another case")
	}
	if _, ok := c.rememberedCheckRunsRefusal(Repository{Owner: testRepo.Owner, Name: "gadgets"}); ok {
		t.Error("the refusal is remembered for another repository")
	}
}

// After the interval the repository is asked again.
func TestCheckRunsRefusalExpires(t *testing.T) {
	c, f := refusingChecks(t, http.StatusForbidden)
	now := testObservedAt
	c.now = func() time.Time { return now }
	if _, err := c.FetchPR(t.Context(), testRepo, 41); err != nil {
		t.Fatal(err)
	}
	before := checkRunRequests(f.log.seen())
	now = testObservedAt.Add(checkRunsRefusalTTL - time.Second)
	if _, err := c.FetchPR(t.Context(), testRepo, 41); err != nil {
		t.Fatal(err)
	}
	if got := checkRunRequests(f.log.seen()); got != before {
		t.Fatalf("asked again before the interval: %d requests, want %d", got, before)
	}
	now = testObservedAt.Add(checkRunsRefusalTTL)
	snap, err := c.FetchPR(t.Context(), testRepo, 41)
	if err != nil {
		t.Fatal(err)
	}
	if got := checkRunRequests(f.log.seen()); got != before+1 {
		t.Errorf("check-runs requests = %d, want %d: asked once again", got, before+1)
	}
	if snap.Refused[len(snap.Refused)-2].Remembered {
		t.Errorf("refused = %+v, want the first refusal answered by GitHub", snap.Refused)
	}
}

// A rate limit is an error, never a remembered refusal.
func TestCheckRunsRateLimitNotRemembered(t *testing.T) {
	f := newFakeGitHub(t, "checks-two-shas")
	h := http.Header{}
	h.Set(headerRetryAfter, "99999")
	refusePath(f, testRepoPath+"/commits/"+strings.Repeat("a", 40)+"/check-runs", http.StatusForbidden, h)
	c := newTestClient(t, f)
	_, err := c.FetchPR(t.Context(), testRepo, 41)
	if _, ok := errors.AsType[*RateLimitError](err); !ok {
		t.Fatalf("error = %v, want a *RateLimitError", err)
	}
	if _, ok := c.rememberedCheckRunsRefusal(testRepo); ok {
		t.Error("a rate limit was remembered as a refusal")
	}
}
