package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/collector/collectortest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// recollect assembles task 104's story from announcements by minute and
// pod, the namespace's objects, and what a store kept.
func recollect(t *testing.T, pods map[int]string, snap kelos.Snapshot, kept Kept) model.Story {
	t.Helper()
	return recollectFrom(t, announcedTask(104, retryName, pods), snap, kept)
}

// recollectFrom assembles task 104's story from its Vikunja snapshot, the
// namespace's objects, and what a store kept.
func recollectFrom(t *testing.T, task vikunja.Snapshot, snap kelos.Snapshot, kept Kept) model.Story {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{task}
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

// A kept marker joined to a run of the story becomes its phase; one joined
// to a run the story lacks is left out, as a correlation pointing outside
// it is.
func TestRecollectPhasesOfKeptMarkers(t *testing.T) {
	sc := storyCases["phase-markers"]
	markers := collectortest.Markers(t, fixturesDir, sc.Dirs)
	other := markers[0]
	join := *other.Join
	join.Run.UID = "00000000-0000-4000-8000-0000000000ff"
	other.Join = &join
	other.Sender.PodUID, other.Sender.Pod = "pod-uid-elsewhere", "another-task-pod"
	res, err := newStoryCollector(t, sc.Dirs).Recollect(t.Context(), sc.Root, Kept{Markers: append(markers, other)})
	if err != nil {
		t.Fatal(err)
	}
	phases := 0
	for _, r := range res.Story.Runs {
		for _, p := range r.Phases {
			phases++
			if p.PodUID == other.Sender.PodUID {
				t.Errorf("run %s has the phase of a marker joined to another run", r.Key)
			}
		}
	}
	spans := make(map[string]bool)
	for _, m := range markers {
		spans[m.Sender.PodUID+"/"+m.Report.Span] = true
	}
	if phases != len(spans) {
		t.Errorf("phases = %d, want one per span of the story's markers, %d", phases, len(spans))
	}
}

// unreadGitHub fails the test if GitHub is read.
type unreadGitHub struct{ t *testing.T }

func (g unreadGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	g.t.Error("GitHub was read, but the caller kept its last read")
	return nil, errors.New("GitHub must not be read")
}

// A skipped GitHub read keeps what the store holds of the last: every
// fixture story assembled again over its own records, GitHub unread, has
// the same records.
func TestRecollectSkippingGitHubKeepsTheStory(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			full := collectStory(t, name)
			sc := storyCases[name]
			cl := collectortest.New(t, fixturesDir, sc.Dirs, collectortest.Report{Elsewhere: t.Errorf, Broken: t.Errorf})
			c, err := New(testConfig(), Sources{Vikunja: cl.Vikunja, Kelos: cl.Kelos, GitHub: unreadGitHub{t}})
			if err != nil {
				t.Fatal(err)
			}
			c.now = func() time.Time { return testNow }
			res, err := c.Recollect(t.Context(), sc.Root, Kept{
				Runs: full.Runs, Correlations: full.Correlations, Markers: collectortest.Markers(t, fixturesDir, sc.Dirs),
				SkipGitHub: true, Events: full.Events, PullRequests: full.PullRequests,
			}, sc.Repos...)
			if err != nil {
				t.Fatal(err)
			}
			got := res.Story
			for what, same := range map[string]bool{
				"events":        reflect.DeepEqual(unread(got.Events), unread(full.Events)),
				"runs":          reflect.DeepEqual(got.Runs, full.Runs),
				"pull requests": reflect.DeepEqual(got.PullRequests, full.PullRequests),
				"correlations":  reflect.DeepEqual(got.Correlations, full.Correlations),
			} {
				if !same {
					t.Errorf("%s differ from the full read's", what)
				}
			}
			if cov := coverageOf(t, got, model.SourceGitHub); cov.LastSuccessfulRead != nil || len(cov.Notes) != 0 {
				t.Errorf("GitHub coverage = %+v, want no read and no note: the caller keeps its own", cov)
			}
		})
	}
}

// unread returns events apart from when they were read: an observation
// (no source time) is identified by its read, so it loses its id too.
func unread(events []model.Event) []model.Event {
	out := slices.Clone(events)
	for i := range out {
		out[i].ObservedAt, out[i].LastObservedAt = time.Time{}, nil
		if out[i].SourceTime == nil {
			out[i].ID = ""
		}
	}
	// Observations are ordered by when they were read.
	slices.SortFunc(out, func(a, b model.Event) int { return bytes.Compare(marshal(a), marshal(b)) })
	return out
}

func marshal(e model.Event) []byte {
	data, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	return data
}
