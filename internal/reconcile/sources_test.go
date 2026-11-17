package reconcile

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

func testMeter(source model.Source) meter {
	return meter{m: NewMetrics(prometheus.NewRegistry()), now: time.Now, source: source, instance: testGitHubHost}
}

func TestTimedBoundsEachRead(t *testing.T) {
	for range 2 {
		_, err := timed(t.Context(), testMeter(model.SourceGitHub), func(ctx context.Context) (struct{}, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > readTimeout {
				t.Errorf("read deadline = %v (set %v), want one within %v", deadline, ok, readTimeout)
			}
			return struct{}{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourcesOfUnsetClientsAreOff(t *testing.T) {
	l, err := New(withDeliveryDefaults(everyCycle), testCollectorConfig(), SourcesOf(collector.Clients{}, nil),
		newFakeStore(), nil, NewMetrics(prometheus.NewRegistry()), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New() over unset clients = %v, want every source off", err)
	}
	l.jitter = func(d time.Duration) time.Duration { return d }
	if err := l.load(t.Context()); err != nil {
		t.Fatal(err)
	}
	l.cycle(t.Context())
	if got := testutil.ToFloat64(l.metrics.cycles); got != 1 {
		t.Errorf("cycles = %v, want one completed reading nothing", got)
	}
}

// offReason is why the off-source tests' source is off.
const offReason = "SILLAGE_EXAMPLE_TOKEN is not set"

// checkOffCoverage checks that the story's coverage of src says it was
// never read, and why, and that no read of it was attempted.
func checkOffCoverage(t *testing.T, h *harness, src model.Source) {
	t.Helper()
	cov := coverageOf(t, h.store.story(t, testRoot()), src)
	if cov.LastSuccessfulRead != nil || len(cov.Notes) != 1 || !strings.Contains(cov.Notes[0], offReason) ||
		!strings.Contains(cov.Notes[0], "off") {
		t.Errorf("%s coverage = %+v, want never read, with a note saying it is off and why", src, cov)
	}
	if strings.Contains(h.logs.String(), "source read failed") {
		t.Errorf("logs = %q, want no read of an off source attempted", h.logs)
	}
}

func TestNilVikunjaIsSkippedWithCoverage(t *testing.T) {
	h := newHarness(t, everyCycle)
	ccfg := testCollectorConfig()
	ccfg.Off = map[model.Source]string{model.SourceVikunja: offReason}
	src := h.sources()
	src.Vikunja, src.Projects = nil, nil
	cfg := everyCycle
	cfg.VikunjaProjects = []int64{testProject}
	h.loop = h.loopOf(t, cfg, ccfg, src)
	h.loop.cycle(t.Context())
	checkOffCoverage(t, h, model.SourceVikunja)
	if cov := coverageOf(t, h.store.story(t, testRoot()), model.SourceKelos); cov.LastSuccessfulRead == nil {
		t.Errorf("kelos coverage = %+v, want the root Kelos names read", cov)
	}
	if h.vikunja.count() != 0 {
		t.Errorf("vikunja read %d times, want none", h.vikunja.count())
	}
}

func TestNilKelosIsSkippedWithCoverage(t *testing.T) {
	h := newHarness(t, everyCycle)
	ccfg := testCollectorConfig()
	ccfg.Off = map[model.Source]string{model.SourceKelos: offReason}
	src := h.sources()
	src.Kelos = nil
	cfg := everyCycle
	cfg.VikunjaProjects = []int64{testProject}
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: testStart})
	h.loop = h.loopOf(t, cfg, ccfg, src)
	h.loop.cycle(t.Context())
	checkOffCoverage(t, h, model.SourceKelos)
	story := h.store.story(t, testRoot())
	if cov := coverageOf(t, story, model.SourceVikunja); cov.LastSuccessfulRead == nil {
		t.Errorf("vikunja coverage = %+v, want the listed task read", cov)
	}
	for _, r := range story.Runs {
		if r.Key.UID != "" {
			t.Errorf("run %s is keyed by its Task's UID, want none read from Kelos", r.Key)
		}
	}
	if got := testutil.ToFloat64(h.loop.metrics.cycles); got != 1 {
		t.Errorf("cycles = %v, want the cycle completed without Kelos", got)
	}
}
