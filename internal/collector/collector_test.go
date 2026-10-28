package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/collector/collectortest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
	"github.com/sperano/sillage/internal/testzone"
)

// TestMain runs the tests outside UTC, so that output depending on the
// host's zone fails here too.
func TestMain(m *testing.M) { testzone.Main(m) }

// storyCases are the fixture stories of testdata/fixtures/stories, each
// assembled by the collector from fabricated source responses; each must
// keep the invariant its story exists for.
var storyCases = collectortest.Stories

// collectStory assembles a fixture story, with the markers a store would
// keep of it, failing on any failed read.
func collectStory(t *testing.T, name string) model.Story {
	t.Helper()
	sc, ok := storyCases[name]
	if !ok {
		t.Fatalf("no story case %s", name)
	}
	kept := Kept{Markers: collectortest.Markers(t, fixturesDir, sc.Dirs)}
	res, err := newStoryCollector(t, sc.Dirs).Recollect(t.Context(), sc.Root, kept, sc.Repos...)
	if err != nil {
		t.Fatalf("Recollect: %v", err)
	}
	for _, f := range res.Failures {
		t.Errorf("failed read: %v", f)
	}
	return res.Story
}

func TestStoryInvariants(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			s := collectStory(t, name)
			storytest.Check(t, name, s)
			storytest.RefsResolve(t, s)
		})
	}
}

func TestEveryStoryIsAssembled(t *testing.T) {
	for _, name := range storytest.Names() {
		if _, ok := storyCases[name]; !ok {
			t.Errorf("story %s has no case the collector assembles", name)
		}
	}
}

func TestOffSourceIsNotRead(t *testing.T) {
	const reason = "SILLAGE_EXAMPLE_TOKEN is not set"
	tests := []struct {
		source model.Source
		// unset removes the source's client; fail sets a failing one,
		// which only Config.Off turns off.
		unset  func(*Sources)
		fail   func(*Sources)
		absent func(model.Story) bool
	}{
		{
			source: model.SourceVikunja,
			unset:  func(s *Sources) { s.Vikunja = nil }, fail: func(s *Sources) { s.Vikunja = failingVikunja{} },
			absent: func(s model.Story) bool { return len(eventsFrom(s, model.SourceVikunja)) == 0 },
		},
		{
			source: model.SourceKelos,
			unset:  func(s *Sources) { s.Kelos = nil }, fail: func(s *Sources) { s.Kelos = failingKelos{} },
			absent: func(s model.Story) bool { return len(s.Runs) == 1 && s.Runs[0].Key.Pod != "" },
		},
		{
			source: model.SourceGitHub,
			unset:  func(s *Sources) { s.GitHub = nil }, fail: func(s *Sources) { s.GitHub = failingGitHub{} },
			absent: func(s model.Story) bool {
				return len(s.PullRequests) == 0 && len(eventsFrom(s, model.SourceGitHub)) == 0
			},
		},
	}
	for _, tt := range tests {
		for _, variant := range []struct {
			name       string
			off        map[model.Source]string
			mutate     func(*Sources)
			wantReason string
		}{
			{"without a client", nil, tt.unset, NoClient},
			{"configured off", map[model.Source]string{tt.source: reason}, tt.fail, reason},
		} {
			t.Run(string(tt.source)+" "+variant.name, func(t *testing.T) {
				sc := storyCases["implementation-merged"]
				src := newStoryCollector(t, sc.Dirs).src
				variant.mutate(&src)
				cfg := testConfig()
				cfg.Off = variant.off
				c, err := New(cfg, src)
				if err != nil {
					t.Fatal(err)
				}
				if got := c.Off()[tt.source]; got != variant.wantReason {
					t.Errorf("Off()[%s] = %q, want %q", tt.source, got, variant.wantReason)
				}
				res, err := c.Collect(t.Context(), sc.Root)
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Failures) != 0 {
					t.Errorf("failures = %v, want none: an off source is not read", res.Failures)
				}
				cov := coverageOf(t, res.Story, tt.source)
				if cov.LastSuccessfulRead != nil || !hasNote(cov, "the source is off") || !hasNote(cov, variant.wantReason) {
					t.Errorf("%s coverage = %+v, want no read and a note saying it is off and why", tt.source, cov)
				}
				if !tt.absent(res.Story) {
					t.Errorf("the story holds records %s supplies", tt.source)
				}
			})
		}
	}
}
