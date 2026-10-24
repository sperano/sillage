package collector

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// recollect assembles task 104's story from announcements by minute and
// pod, the namespace's objects, and what a store kept.
func recollect(t *testing.T, pods map[int]string, snap kelos.Snapshot, kept Kept) model.Story {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, retryName, pods)}
	src.Kelos = fixedKelos{snap}
	c, err := New(testConfig(), src)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	res, err := c.Recollect(t.Context(), taskRoot(104), kept)
	if err != nil {
		t.Fatal(err)
	}
	return res.Story
}

func TestRecollectKeepsARunKelosDeleted(t *testing.T) {
	pods := map[int]string{3: retryName + "-pod00"}
	first := recollect(t, pods, kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-a", "", taskLabels("104"), 2, 9)}},
		Kept{})
	if len(first.Runs) != 1 || first.Runs[0].Key.UID != "uid-a" {
		t.Fatalf("first read runs = %v, want the Task's", first.Runs)
	}
	s := recollect(t, pods, kelos.Snapshot{}, Kept{Runs: first.Runs, Correlations: first.Correlations})
	if len(s.Runs) != 1 || s.Runs[0].Key != first.Runs[0].Key {
		t.Fatalf("runs = %v, want the kept run and no run keyed by its pod", s.Runs)
	}
	got := linksFrom(s, announcementRef(3))
	if len(got) != 1 || got[0].To != runRef("uid-a") || got[0].Confidence != model.ConfidenceIdentifier {
		t.Errorf("announcement links %v, want an identifier to the kept run", got)
	}
	if !hasLink(s, runRef("uid-a"), taskRoot(104).Ref()) {
		t.Error("the kept run is not joined to its task")
	}
}

func TestRecollectKeepsRunsWhenKelosFails(t *testing.T) {
	pods := map[int]string{3: retryName + "-pod00"}
	first := recollect(t, pods, kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-a", "", taskLabels("104"), 2, 9)}},
		Kept{})
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, retryName, pods)}
	src.Kelos = failingKelos{}
	c, err := New(testConfig(), src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Recollect(t.Context(), taskRoot(104), Kept{Runs: first.Runs, Correlations: first.Correlations})
	if err != nil {
		t.Fatal(err)
	}
	if runs := res.Story.Runs; len(runs) != 1 || runs[0].Key.UID != "uid-a" {
		t.Errorf("runs = %v, want the kept run alone", runs)
	}
}

func TestRecollectPrefersTheReadRun(t *testing.T) {
	stale := model.Run{
		Key:      model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: "uid-a"},
		TaskName: retryName, Role: model.RoleImplementation,
		Interval: model.Interval{Start: model.UnknownBound(), End: model.UnknownBound()},
		Outcome:  model.OutcomeUnknown,
		Evidence: []model.Evidence{{Source: model.SourceKelos, Instance: testCluster, Ref: "stale"}},
	}
	s := recollect(t, nil, kelos.Snapshot{Tasks: []kelos.Task{task(retryName, "uid-a", "", taskLabels("104"), 2, 9)}},
		Kept{Runs: []model.Run{stale}})
	if len(s.Runs) != 1 || s.Runs[0].Outcome != model.OutcomeSucceeded {
		t.Errorf("runs = %v, want the run as read now", s.Runs)
	}
}

func TestRecollectRemakesAPodKeyedRun(t *testing.T) {
	pods := map[int]string{3: "p-only"}
	first := recollect(t, pods, kelos.Snapshot{}, Kept{})
	s := recollect(t, pods, kelos.Snapshot{}, Kept{Runs: first.Runs, Correlations: first.Correlations})
	if len(s.Runs) != 1 || s.Runs[0].Key.Pod != "p-only" {
		t.Fatalf("runs = %v, want the run keyed by pod", s.Runs)
	}
	announced := 0
	for _, g := range coverageOf(t, s, model.SourceKelos).Gaps {
		if g.Start.Equal(at(3)) {
			announced++
		}
	}
	if announced != 1 {
		t.Errorf("kelos gaps = %v, want the announced run's, from its announcement", coverageOf(t, s, model.SourceKelos).Gaps)
	}
}

func TestRecollectTTLGapEndsBeforeTheFirstRead(t *testing.T) {
	since := at(0).Add(DefaultKelosTaskTTL + time.Hour)
	s := recollect(t, map[int]string{3: "p-only"}, kelos.Snapshot{}, Kept{KelosSince: since})
	var ttl []model.Gap
	for _, g := range coverageOf(t, s, model.SourceKelos).Gaps {
		if g.End.Equal(since.Add(-DefaultKelosTaskTTL)) {
			ttl = append(ttl, g)
		}
	}
	if len(ttl) != 1 {
		t.Errorf("kelos gaps = %v, want one ending a Task TTL before the first read", coverageOf(t, s, model.SourceKelos).Gaps)
	}
}

func hasLink(s model.Story, a, b model.Ref) bool {
	for _, c := range s.Correlations {
		if c.Pair() == model.PairOf(a, b) {
			return true
		}
	}
	return false
}
