package reconcile

import (
	"cmp"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
)

// rateLimitWait is how long the tests' rate limits ask to wait.
const rateLimitWait = 28 * time.Minute

// runsOf names the runs of tasks testTaskID and each of others, running,
// and has them read.
func runsOf(h *harness, others ...int64) {
	tasks := []kelos.Task{testKelosTask(kelos.PhaseRunning)}
	for _, id := range others {
		tasks = append(tasks, kelosTaskOf(id, kelos.PhaseRunning))
	}
	h.kelos.set(kelos.Snapshot{Tasks: tasks}, nil)
	h.vikunja.also = others
}

// rootOf is task id's root.
func rootOf(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: id}}
}

// Once a read is rate limited, the cycle asks GitHub nothing more: the
// roots it still had due are owed their read, and neither they nor the
// failed root are read on GitHub before the wait is over.
func TestRateLimitStopsGitHub(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	clock := useClock(h)
	runsOf(h, testTaskID+1)
	gh.failing = map[string]error{testRepo.String(): &github.RateLimitError{Path: "/repos/acme/widgets/pulls",
		Wait: rateLimitWait}}
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 1 {
		t.Fatalf("GitHub read %d times in the rate-limited cycle, want only the read it refused", fetches)
	}
	failed, owed := h.loop.roots[testRoot().Ref()], h.loop.roots[rootOf(testTaskID+1).Ref()]
	if owed == nil || owed.GitHubOwed == nil || owed.GitHubOwed.Trigger != triggerRun {
		t.Fatalf("the root left due = %+v, want it owed its run's GitHub read", owed)
	}
	if resume := time.Now().Add(rateLimitWait); failed.backstop.Before(resume.Add(-time.Minute)) {
		t.Errorf("the failed root's retry at %v, want no earlier than the reset at %v", failed.backstop, resume)
	}
	if got := githubReads(h, triggerDeferred); got != 1 {
		t.Errorf("deferred GitHub reads = %v, want the owed root's", got)
	}
	if !strings.Contains(h.logs.String(), "github.deferred=1") || !strings.Contains(h.logs.String(), "GitHub rate limited") {
		t.Errorf("the log does not say GitHub is rate limited and a read deferred:\n%s", h.logs)
	}

	clock.offset = rateLimitWait - time.Minute
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 1 {
		t.Errorf("GitHub read %d times before the reset, want no more", fetches)
	}

	gh.failing = nil
	clock.offset = rateLimitWait + time.Minute
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 1+2 {
		t.Errorf("GitHub read %d times after the reset, want the owed read and the failed root's retry", fetches)
	}
	if st := h.loop.roots[rootOf(testTaskID+1).Ref()]; st.GitHubOwed != nil || st.GitHubRead == nil {
		t.Errorf("the owed root after its read = %+v, want its GitHub side read and owed nothing", st)
	}
}

// A catch-up of more trigger reads than the window affords above the
// reserve reads what it affords, the rest owed, kept across a restart,
// and read in the next window.
func TestCatchUpSpreadsOverWindows(t *testing.T) {
	const roots, limit, cost = 5, 1000, 300
	h, gh := githubHarness(t, rarely)
	others := []int64{testTaskID + 1, testTaskID + 2, testTaskID + 3, testTaskID + 4}
	runsOf(h, others...)
	gh.cost = cost
	gh.budget = github.Budget{Limit: limit, Remaining: limit, Reset: time.Now().Add(time.Hour)}
	h.loop.cycle(t.Context())
	// 1000, 700 and 400 leave more than the reserve, 100 does not.
	const afforded = 3
	if fetches, _ := gh.counts(); fetches != afforded {
		t.Fatalf("GitHub read %d times in the first window, want the %d it affords above the reserve",
			fetches, afforded)
	}
	if got := githubReads(h, triggerDeferred); got != roots-afforded {
		t.Errorf("deferred GitHub reads = %v, want %d", got, roots-afforded)
	}
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != afforded {
		t.Errorf("GitHub read %d times again in the same window, want none", fetches)
	}
	owed := 0
	for _, id := range append([]int64{testTaskID}, others...) {
		var st rootState
		if err := json.Unmarshal(h.store.cps[rootKey(rootOf(id))], &st); err != nil {
			t.Fatal(err)
		}
		if st.GitHubOwed != nil {
			owed++
		}
	}
	if owed != roots-afforded {
		t.Errorf("%d stored root states owe a GitHub read, want %d", owed, roots-afforded)
	}

	gh.budget = github.Budget{Limit: limit, Remaining: limit, Reset: time.Now().Add(time.Hour)}
	h.loop = h.withGitHub(t, rarely, gh)
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != roots {
		t.Errorf("GitHub read %d times in the next window, after a restart, want every root's read", fetches)
	}
	for ref, st := range h.loop.roots {
		if st.GitHubOwed != nil {
			t.Errorf("root %v still owes %+v after the next window", ref, st.GitHubOwed)
		}
	}
}

// A cycle reads the deliveries' roots first, then the other triggers',
// then the periodic reads; the GitHub reads owed longest first within
// each.
func TestCompareDues(t *testing.T) {
	early, late := testStart, testStart.Add(time.Hour)
	delivered := &due{root: rootOf(9), delivery: deliveryCaused, github: githubFull, trigger: triggerDelivery}
	owedDelivery := &due{root: rootOf(8), github: githubFull, trigger: triggerDelivery, owed: &late}
	owedEarly := &due{root: rootOf(7), github: githubFull, trigger: triggerTask, owed: &early}
	owedLate := &due{root: rootOf(1), github: githubFull, trigger: triggerRun, owed: &late}
	triggered := &due{root: rootOf(2), github: githubFull, trigger: triggerRun}
	periodic := &due{root: rootOf(3), github: githubBackstop}
	want := []*due{owedDelivery, delivered, owedEarly, owedLate, triggered, periodic}
	for i := range want {
		for j := range want {
			if got := compareDues(want[i], want[j]); cmp.Compare(got, 0) != cmp.Compare(i, j) {
				t.Errorf("compareDues(%v, %v) = %d, want the sign of %d", want[i].root.Ref(), want[j].root.Ref(),
					got, cmp.Compare(i, j))
			}
		}
	}
}
