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
