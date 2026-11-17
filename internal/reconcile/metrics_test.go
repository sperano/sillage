package reconcile

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// readCount is how many reads of src's instance the duration histogram
// observed.
func readCount(t *testing.T, reg prometheus.Gatherer, src model.Source, instance string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "sillage_source_read_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := make(map[string]string)
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels[labelSource] == string(src) && labels[labelInstance] == instance {
				return m.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func TestMetricsAfterACycle(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.vikunja.set(errUnavailable)
	h.loop.cycle(t.Context())
	h.vikunja.set(nil)
	h.loop.cycle(t.Context())
	m := h.loop.metrics
	vikunja, kelos := string(model.SourceVikunja), string(model.SourceKelos)
	if got := testutil.ToFloat64(m.readErrors.WithLabelValues(vikunja, testVikunjaHost, reasonServerError)); got != 1 {
		t.Errorf("vikunja server errors = %v, want the one failed read", got)
	}
	if got := testutil.CollectAndCount(m.readErrors); got != 1 {
		t.Errorf("read error series = %d, want only vikunja's server error", got)
	}
	for _, src := range []struct{ source, instance string }{{vikunja, testVikunjaHost}, {kelos, testCluster}} {
		if got := testutil.ToFloat64(m.lastSuccess.WithLabelValues(src.source, src.instance)); got <= 0 {
			t.Errorf("%s last success = %v, want the second cycle's read", src.source, got)
		}
	}
	if got := readCount(t, h.reg, model.SourceVikunja, testVikunjaHost); got != 2 {
		t.Errorf("vikunja reads timed = %d, want both, failed or not", got)
	}
	if got := testutil.ToFloat64(m.written.WithLabelValues(kindEvents)); got <= 0 {
		t.Errorf("events written = %v, want the read story's", got)
	}
	if got := testutil.ToFloat64(m.written.WithLabelValues(kindRuns)); got != 1+1 {
		t.Errorf("runs written = %v, want the run at each of the two commits", got)
	}
	if got := testutil.ToFloat64(m.cycles); got != 2 {
		t.Errorf("cycles = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.roots); got != 1 {
		t.Errorf("roots = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.storeErrors); got != 0 {
		t.Errorf("store error series = %d, want none", got)
	}
}

// A read GitHub refused in part succeeds, and the loop logs and counts
// each resource and status it refused.
func TestRefusalsLoggedAndCounted(t *testing.T) {
	const commits, cycles = 2, 2
	repo := github.Repository{Owner: "acme", Name: "widgets"}
	h := newHarness(t, everyCycle)
	h.ccfg.Workspaces = map[string]github.Repository{"widgets": repo}
	h.github.snaps = []github.Snapshot{refusedChecks(repo, "kelos/vikunja-"+strconv.Itoa(testTaskID))}
	h.loop = h.newLoop(t, everyCycle)
	for range cycles {
		h.loop.cycle(t.Context())
	}
	m := h.loop.metrics
	gh := string(model.SourceGitHub)
	got := testutil.ToFloat64(m.refusals.WithLabelValues(gh, testGitHubHost, "check_runs", "403"))
	if got != commits*cycles {
		t.Errorf("refused check runs = %v, want both commits' in each of the two cycles", got)
	}
	if got := testutil.CollectAndCount(m.readErrors); got != 0 {
		t.Errorf("read error series = %d, want none: the read succeeded", got)
	}
	if got := strings.Count(h.logs.String(), "source read refused in part"); got != cycles {
		t.Errorf("logs warn %d times of the refusal, want once a cycle:\n%s", got, h.logs)
	}
	if !strings.Contains(h.logs.String(), `resource="check runs" code=403 requests=2`) {
		t.Errorf("logs do not say what was refused:\n%s", h.logs)
	}
}

// A refusal the client only remembered is neither logged nor counted: the
// pull request's commits are all remembered, as on a later read.
func TestRememberedRefusalsNeitherLoggedNorCounted(t *testing.T) {
	repo := github.Repository{Owner: "acme", Name: "widgets"}
	h := newHarness(t, everyCycle)
	h.ccfg.Workspaces = map[string]github.Repository{"widgets": repo}
	snap := refusedChecks(repo, "kelos/vikunja-"+strconv.Itoa(testTaskID))
	for i := range snap.Refused {
		snap.Refused[i].Remembered = true
	}
	h.github.snaps = []github.Snapshot{snap}
	h.loop = h.newLoop(t, everyCycle)
	h.loop.cycle(t.Context())
	gh := string(model.SourceGitHub)
	if got := testutil.ToFloat64(h.loop.metrics.refusals.WithLabelValues(gh, testGitHubHost, "check_runs", "403")); got != 0 {
		t.Errorf("refused check runs = %v, want none counted", got)
	}
	if strings.Contains(h.logs.String(), "source read refused in part") {
		t.Errorf("logs warn of a remembered refusal:\n%s", h.logs)
	}
}

// refusedChecks is a pull request of branch in repo whose two commits'
// check runs GitHub answered 403.
func refusedChecks(repo github.Repository, branch string) github.Snapshot {
	r := &github.Repo{FullName: repo.String()}
	head, first := strings.Repeat("c", 40), strings.Repeat("d", 40)
	snap := github.Snapshot{
		Instance: testGitHubHost, Repository: repo, ObservedAt: testStart, SHAs: []string{first, head},
		PullRequest: github.PullRequest{
			ID: 7064, Number: 64, State: "open", CreatedAt: testStart,
			User: &github.User{Login: "owner-gh", Type: "User"},
			Head: github.Ref{Ref: branch, SHA: head, Repo: r},
			Base: github.Ref{Ref: "main", SHA: strings.Repeat("0", 40), Repo: r},
		},
	}
	for _, sha := range snap.SHAs {
		snap.Refused = append(snap.Refused, github.Refusal{
			Resource: github.ResourceCheckRuns, SHA: sha, Code: http.StatusForbidden,
			Path: "/repos/" + repo.String() + "/commits/" + sha + "/check-runs",
		})
	}
	return snap
}

// A root none of whose records names a repository has its branch looked
// for in each repository the workspaces map to, counted; once the store
// holds the pull request found, its repository is read and nothing is
// searched again.
func TestBranchSearchesCounted(t *testing.T) {
	h, _ := githubHarness(t, rarely)
	tools := github.Repository{Owner: "acme", Name: "tools"}
	// The runs' project, widgets, maps to no repository.
	h.ccfg.Workspaces = map[string]github.Repository{"gadgets": testRepo, "tools": tools}
	h.loop = h.newLoop(t, rarely)
	h.loop.cycle(t.Context())
	searches := func() float64 { return testutil.ToFloat64(h.loop.metrics.githubSearches) }
	if got := searches(); got != 2 {
		t.Errorf("branch searches = %v, want one per repository the workspaces map to", got)
	}
	if !strings.Contains(h.logs.String(), "searches=2") {
		t.Errorf("the cycle's log does not count the searches:\n%s", h.logs)
	}
	if s := h.store.story(t, testRoot()); len(s.PullRequests) != 1 {
		t.Fatalf("pull requests = %v, want the one the search found", s.PullRequests)
	}
	h.deliveries.deliver(testRoot())
	h.loop.cycle(t.Context())
	if got := githubReads(h, triggerDelivery); got != 1 {
		t.Fatalf("GitHub reads for a delivery = %v, want the second read whole", got)
	}
	if got := searches(); got != 2 {
		t.Errorf("branch searches = %v after a read with the pull request stored, want no more", got)
	}
}
