package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// testBranch is the branch of task testTaskID.
var testBranch = "kelos/vikunja-" + strconv.Itoa(testTaskID)

// testRepo is the repository of the fabricated runs' workspace.
var testRepo = github.Repository{Owner: "acme", Name: "widgets"}

// testValidators are what conditionalGitHub's reads keep.
var testValidators = github.Validators{"/repos/acme/widgets/pulls?per_page=100": `W/"pulls-1"`}

// conditionalGitHub is fakeGitHub as a ConditionalGitHub: its reads keep
// validators, unless it has none; Unchanged answers !changed, or
// checkErr; and it reports budget, each whole read costing it cost.
type conditionalGitHub struct {
	*fakeGitHub
	validators github.Validators
	changed    bool
	checkErr   error
	budget     github.Budget
	cost       int
	checks     int
	// failing answers each repository's read with its error.
	failing map[string]error
	// asked are the validators each Unchanged was asked about.
	asked []github.Validators
}

func (g *conditionalGitHub) FetchBranchValidated(ctx context.Context, base github.Repository, head github.Branch) (
	[]github.Snapshot, github.Validators, error) {
	snaps, err := g.FetchBranch(ctx, base, head)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.budget.Remaining -= g.cost
	if err := g.failing[base.String()]; err != nil {
		return nil, nil, err
	}
	return snaps, g.validators, err
}

func (g *conditionalGitHub) Unchanged(_ context.Context, v github.Validators) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checks++
	g.asked = append(g.asked, v)
	return !g.changed, g.checkErr
}

func (g *conditionalGitHub) Usage() github.Usage {
	g.mu.Lock()
	defer g.mu.Unlock()
	return github.Usage{Requests: int64(g.fetches), NotModified: int64(g.checks), Budget: g.budget}
}

// counts returns how many times GitHub was read whole and asked whether
// it changed.
func (g *conditionalGitHub) counts() (fetches, checks int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fetches, g.checks
}

// githubHarness is a harness whose runs' workspace maps to testRepo, its
// branch holding one open pull request, read through a
// conditionalGitHub.
func githubHarness(t *testing.T, cfg Config) (*harness, *conditionalGitHub) {
	t.Helper()
	h := newHarness(t, cfg)
	h.ccfg.Workspaces = map[string]github.Repository{"widgets": testRepo}
	h.github.snaps = []github.Snapshot{prSnapshot(false)}
	gh := &conditionalGitHub{fakeGitHub: h.github, validators: testValidators}
	h.loop = h.withGitHub(t, cfg, gh)
	return h, gh
}

// withGitHub returns another loop over h's fakes and store, reading
// GitHub through gh.
func (h *harness) withGitHub(t *testing.T, cfg Config, gh collector.GitHubSource) *Loop {
	t.Helper()
	src := h.sources()
	src.GitHub = gh
	return h.loopOf(t, cfg, h.ccfg, src)
}

// prSnapshot is the pull request of testBranch, open or merged.
func prSnapshot(merged bool) github.Snapshot {
	r := &github.Repo{FullName: testRepo.String()}
	head := strings.Repeat("c", 40)
	pr := github.PullRequest{
		ID: 7042, Number: 42, State: "open", CreatedAt: testStart.Add(time.Hour),
		User: &github.User{Login: "owner-gh", Type: "User"},
		Head: github.Ref{Ref: testBranch, SHA: head, Repo: r},
		Base: github.Ref{Ref: "main", SHA: strings.Repeat("0", 40), Repo: r},
	}
	if merged {
		at := testStart.Add(90 * time.Minute)
		pr.State, pr.ClosedAt, pr.MergedAt = "closed", &at, &at
	}
	return github.Snapshot{Instance: testGitHubHost, Repository: testRepo, PullRequest: pr, SHAs: []string{head},
		ObservedAt: testStart.Add(2 * time.Hour)}
}

// rarely refreshes nothing between the cycles of a test unless a test
// says so: only a trigger, or a backstop made due, reads a root.
var rarely = Config{
	Interval: time.Millisecond, RefreshInterval: time.Hour, BackstopInterval: 24 * time.Hour,
	LockRetryInterval: time.Millisecond, VikunjaProjects: []int64{testProject},
}

// makeBackstopDue has the backstop read the root at the next cycle.
func makeBackstopDue(h *harness) { h.loop.roots[testRoot().Ref()].backstop = time.Time{} }

func githubCoverage(t *testing.T, h *harness) model.Coverage {
	t.Helper()
	return coverageOf(t, h.store.story(t, testRoot()), model.SourceGitHub)
}

func githubReads(h *harness, kind string) float64 {
	return testutil.ToFloat64(h.loop.metrics.githubReads.WithLabelValues(kind))
}

// A refresh reads the task and Kelos again but keeps GitHub's records:
// the story is the same, and nothing is written.
func TestRefreshKeepsGitHub(t *testing.T) {
	h, gh := githubHarness(t, Config{
		Interval: time.Millisecond, RefreshInterval: time.Nanosecond, BackstopInterval: 24 * time.Hour,
		LockRetryInterval: time.Millisecond,
	})
	h.loop.cycle(t.Context())
	read := githubCoverage(t, h).LastSuccessfulRead
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 0 {
		t.Errorf("GitHub read %d times and asked %d, want once, by the first read", fetches, checks)
	}
	if n := h.vikunja.count(); n != 2 {
		t.Errorf("task read %d times, want at both cycles", n)
	}
	if n := written(h.store.lastCommit(t)); n != 0 {
		t.Errorf("the refresh wrote %d records, want none: GitHub's kept records are the story's", n)
	}
	if s := h.store.story(t, testRoot()); len(s.PullRequests) != 1 {
		t.Errorf("pull requests = %v, want the one read first", s.PullRequests)
	}
	if got := githubCoverage(t, h).LastSuccessfulRead; !got.Equal(*read) {
		t.Errorf("GitHub's last successful read = %v, want the first read's %v kept", got, read)
	}
	if got := githubReads(h, triggerRun); got != 1 {
		t.Errorf("GitHub reads for a run = %v, want the first read's", got)
	}
}

// A delivery, a new run and a task change each read GitHub whole.
func TestTriggersReadGitHub(t *testing.T) {
	for _, tc := range []struct {
		trigger string
		cause   func(h *harness)
	}{
		{triggerDelivery, func(h *harness) { h.deliveries.deliver(testRoot()) }},
		{triggerRun, func(h *harness) {
			h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
		}},
		{triggerTask, func(h *harness) {
			h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: time.Now()})
		}},
	} {
		t.Run(tc.trigger, func(t *testing.T) {
			h, gh := githubHarness(t, rarely)
			h.loop.cycle(t.Context())
			h.loop.cycle(t.Context())
			if fetches, _ := gh.counts(); fetches != 1 {
				t.Fatalf("GitHub read %d times before the trigger, want once", fetches)
			}
			before := githubReads(h, tc.trigger)
			tc.cause(h)
			h.loop.cycle(t.Context())
			if fetches, checks := gh.counts(); fetches != 2 || checks != 0 {
				t.Errorf("GitHub read %d times and asked %d, want read again whole", fetches, checks)
			}
			if got := githubReads(h, tc.trigger); got != before+1 {
				t.Errorf("GitHub reads for %s = %v, want %v", tc.trigger, got, before+1)
			}
		})
	}
}

// The backstop asks GitHub with the last read's validators first: lists
// unchanged, the coverage advances without a read; changed, it reads.
func TestBackstopAsksBeforeReading(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	h.loop.cycle(t.Context())
	first := *githubCoverage(t, h).LastSuccessfulRead
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 1 {
		t.Fatalf("GitHub read %d times and asked %d, want asked once and not read again", fetches, checks)
	}
	if len(gh.asked) != 1 || gh.asked[0][firstKey(testValidators)] != testValidators[firstKey(testValidators)] {
		t.Errorf("asked with %v, want the first read's validators %v", gh.asked, testValidators)
	}
	if got := *githubCoverage(t, h).LastSuccessfulRead; !got.After(first) {
		t.Errorf("GitHub's last successful read = %v, want it advanced past %v by the unchanged lists", got, first)
	}
	if got := githubReads(h, triggerUnchanged); got != 1 {
		t.Errorf("unchanged backstop reads = %v, want 1", got)
	}
	if !strings.Contains(h.logs.String(), "github.notModified=1") ||
		!strings.Contains(h.logs.String(), "github.unchanged=1") {
		t.Errorf("the cycle's log line does not count the 304s and the unchanged read:\n%s", h.logs)
	}
	gh.mu.Lock()
	gh.changed = true
	gh.mu.Unlock()
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 2 || checks != 2 {
		t.Errorf("GitHub read %d times and asked %d, want asked, then read whole", fetches, checks)
	}
	if got := githubReads(h, triggerBackstop); got != 1 {
		t.Errorf("whole backstop reads = %v, want 1", got)
	}
}

func firstKey(v github.Validators) string {
	for k := range v {
		return k
	}
	return ""
}

// A read that left no validators is read whole by the backstop, and so
// is any read through a source that cannot compare.
func TestBackstopWithoutValidatorsReadsWhole(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	gh.validators = nil
	h.loop.cycle(t.Context())
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 2 || checks != 0 {
		t.Errorf("GitHub read %d times and asked %d, want read whole twice", fetches, checks)
	}

	plain := newHarness(t, rarely)
	plain.ccfg.Workspaces = map[string]github.Repository{"widgets": testRepo}
	plain.github.snaps = []github.Snapshot{prSnapshot(false)}
	plain.loop = plain.newLoop(t, rarely)
	plain.loop.cycle(t.Context())
	makeBackstopDue(plain)
	plain.loop.cycle(t.Context())
	if plain.github.fetches != 2 {
		t.Errorf("GitHub read %d times through a plain source, want twice", plain.github.fetches)
	}
}

// A root whose read went on past a repository GitHub refused keeps no
// validators: the backstop must read it whole, not take the others' lists
// for all of its branch's.
func TestRefusedRepositoryLeavesNoValidators(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	private := github.Repository{Owner: "acme", Name: "private"}
	gh.failing = map[string]error{private.String(): &github.StatusError{Path: "/repos/acme/private/pulls",
		Code: http.StatusNotFound, Status: "404 Not Found"}}
	g := &rootGitHub{
		meteredGitHub: meteredGitHub{src: gh, meter: h.loop.meterFor(model.SourceGitHub, h.ccfg.GitHubInstance)},
		cond:          gh, tags: make(github.Validators),
	}
	if _, err := g.FetchBranch(t.Context(), testRepo, github.Branch{Repository: testRepo, Name: testBranch}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.FetchBranch(t.Context(), private, github.Branch{Repository: private, Name: testBranch}); err == nil {
		t.Fatal("the refused repository's read succeeded")
	}
	if v := g.validators(); v != nil {
		t.Errorf("validators = %v, want none", v)
	}
}

// A failed check opens a gap on GitHub's coverage and is tried again a
// refresh interval later rather than a backstop interval.
func TestBackstopCheckFailureOpensAGap(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	h.loop.cycle(t.Context())
	gh.checkErr = &github.RateLimitError{Path: "/repos/acme/widgets/pulls", Wait: time.Hour}
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	st := h.loop.roots[testRoot().Ref()]
	if !st.githubGapOpen(testGitHubHost) || !st.GitHubFailed {
		t.Fatalf("after the failed check, GitHub state = %+v: want a gap open and the read failed", st)
	}
	if wait := st.backstop.Sub(*st.GitHubRead); wait < time.Hour {
		t.Errorf("next backstop %v after the rate-limited check, want no earlier than the hour GitHub asked for",
			wait)
	}
	if got := testutil.ToFloat64(h.loop.metrics.readErrors.WithLabelValues("github", testGitHubHost,
		reasonRateLimited)); got != 1 {
		t.Errorf("GitHub rate-limited reads = %v, want 1", got)
	}
}

// settledHarness is a githubHarness whose task is done, pull request
// merged and run succeeded, read once.
func settledHarness(t *testing.T) (*harness, *conditionalGitHub) {
	t.Helper()
	h, gh := githubHarness(t, rarely)
	h.vikunja.done = true
	h.github.snaps = []github.Snapshot{prSnapshot(true)}
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
	h.loop.cycle(t.Context())
	if !h.loop.roots[testRoot().Ref()].Settled {
		t.Fatal("a done task with a merged pull request and a finished run is not settled")
	}
	return h, gh
}

// A settled root gets no backstop read, unless its GitHub coverage has a
// gap open; a delivery still reads it.
func TestSettledRootSkipsTheBackstop(t *testing.T) {
	h, gh := settledHarness(t)
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 0 || h.vikunja.count() != 1 {
		t.Errorf("GitHub read %d times and asked %d, task read %d times: want the settled root left unread",
			fetches, checks, h.vikunja.count())
	}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 2 {
		t.Errorf("GitHub read %d times after a delivery, want again", fetches)
	}

	// A restart opens a gap on every source: the backstop, when due,
	// closes GitHub's.
	h.loop = h.withGitHub(t, rarely, gh)
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if _, checks := gh.counts(); checks != 1 {
		t.Errorf("GitHub asked %d times after the restart, want once, to close its gap", checks)
	}
	if h.loop.roots[testRoot().Ref()].githubGapOpen(testGitHubHost) {
		t.Error("the restart's GitHub gap is still open after the backstop")
	}
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if _, checks := gh.counts(); checks != 1 {
		t.Errorf("GitHub asked %d times, want no backstop once the gap closed", checks)
	}
}

func TestSettledLostWithAnOpenPR(t *testing.T) {
	h, _ := settledHarness(t)
	h.github.snaps = []github.Snapshot{prSnapshot(false)}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if h.loop.roots[testRoot().Ref()].Settled {
		t.Error("a root whose pull request is open again is still settled")
	}
}

// Below the reserve, the backstop reads nothing, and a root due for it
// alone is not read; a delivery still reads GitHub.
func TestReserveHoldsTheBackstopBack(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	h.loop.cycle(t.Context())
	limit := 5000
	gh.budget = github.Budget{
		Limit: limit, Remaining: int(githubReserve*float64(limit)) - 1, Reset: time.Now().Add(time.Hour),
	}
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 0 || h.vikunja.count() != 1 {
		t.Errorf("GitHub read %d times and asked %d, task read %d times: want nothing read below the reserve",
			fetches, checks, h.vikunja.count())
	}
	if got := githubReads(h, triggerDeferred); got != 1 {
		t.Errorf("deferred backstop reads = %v, want 1", got)
	}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if fetches, _ := gh.counts(); fetches != 2 {
		t.Errorf("GitHub read %d times after a delivery below the reserve, want again", fetches)
	}
	gh.budget.Reset = time.Now().Add(-time.Second)
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if _, checks := gh.counts(); checks != 1 {
		t.Errorf("GitHub asked %d times once the window reset, want the backstop back", checks)
	}
}

// A restart checks a root whose last read left validators within a
// refresh interval, whether or not its backstop is overdue, and the
// check closes the restart's gap on its GitHub coverage.
func TestRestartChecksRootsWithValidatorsSoon(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	h.loop.cycle(t.Context())
	h.spread = func(d time.Duration) time.Duration { return d / 2 }
	before := time.Now()
	h.loop = h.withGitHub(t, rarely, gh)
	st := h.loop.roots[testRoot().Ref()]
	if half := rarely.RefreshInterval / 2; st.backstop.Before(before.Add(half)) || st.backstop.After(time.Now().Add(half)) {
		t.Errorf("backstop after the restart = %v, want spread to %v from the restart", st.backstop, half)
	}
	h.loop.cycle(t.Context())
	if _, checks := gh.counts(); checks != 0 {
		t.Errorf("GitHub asked %d times in the restart's first cycle, want not yet", checks)
	}
	makeBackstopDue(h)
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 1 {
		t.Errorf("GitHub read %d times and asked %d, want asked once", fetches, checks)
	}
	if h.loop.roots[testRoot().Ref()].githubGapOpen(h.ccfg.GitHubInstance) {
		t.Error("the restart's gap on GitHub is still open after the check")
	}
}

// A restart spreads the backstop reads it finds overdue over the backstop
// interval, and keeps the others' schedule, for roots whose last read left
// no validators; its first cycle reads the task and Kelos, not GitHub.
func TestRestartSpreadsTheBackstop(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	gh.validators = nil
	h.loop.cycle(t.Context())
	key := rootKey(testRoot())
	var stored rootState
	if err := json.Unmarshal(h.store.cps[key], &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.ETags) != 0 {
		t.Fatalf("stored validators %v, want none", stored.ETags)
	}
	recent := *stored.GitHubRead
	h.loop = h.withGitHub(t, rarely, gh)
	if got, want := h.loop.roots[testRoot().Ref()].backstop, recent.Add(rarely.BackstopInterval); !got.Equal(want) {
		t.Errorf("backstop after the restart = %v, want its own schedule %v", got, want)
	}

	overdue := recent.Add(-2 * rarely.BackstopInterval)
	stored.GitHubRead = &overdue
	h.store.cps[key] = encode(&stored)
	h.spread = func(d time.Duration) time.Duration { return d / 2 }
	before := time.Now()
	h.loop = h.withGitHub(t, rarely, gh)
	got := h.loop.roots[testRoot().Ref()].backstop
	if half := rarely.BackstopInterval / 2; got.Before(before.Add(half)) || got.After(time.Now().Add(half)) {
		t.Errorf("overdue backstop after the restart = %v, want spread to %v from the restart", got, half)
	}
	h.loop.cycle(t.Context())
	if fetches, checks := gh.counts(); fetches != 1 || checks != 0 || h.vikunja.count() != 2 {
		t.Errorf("GitHub read %d times and asked %d, task read %d times: want the task read, GitHub not",
			fetches, checks, h.vikunja.count())
	}
}

func TestCycleCountsGitHubRequests(t *testing.T) {
	h, gh := githubHarness(t, rarely)
	h.loop.cycle(t.Context())
	gh.budget = github.Budget{Limit: 5000, Remaining: 4321, Reset: time.Now().Add(time.Hour)}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	m := h.loop.metrics
	if got := testutil.ToFloat64(m.githubRequests); got != 2 {
		t.Errorf("GitHub requests = %v, want one per read", got)
	}
	if got := testutil.ToFloat64(m.githubRemaining); got != 4321 {
		t.Errorf("GitHub budget remaining = %v, want 4321", got)
	}
	if !strings.Contains(h.logs.String(), "github.requests=1") || !strings.Contains(h.logs.String(), "github.delivery=1") {
		t.Errorf("the cycle's log line does not count the delivery's read:\n%s", h.logs)
	}
}
