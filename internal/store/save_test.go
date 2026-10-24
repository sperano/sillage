package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/testzone"
)

// TestMain runs the tests outside UTC, so that output depending on the
// host's zone fails here too.
func TestMain(m *testing.M) { testzone.Main(m) }

// storiesDir holds the fixture stories: input.json and the golden story.
const storiesDir = "../../testdata/fixtures/stories"

type fixture struct {
	Description string     `json:"description"`
	Root        model.Root `json:"root"`
	model.Records
	// markers are the fixture's phase markers, which a pod reports and
	// RecordMarker records: no batch holds them.
	markers []model.Marker
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(storiesDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("no fixture stories in %s", storiesDir)
	}
	return names
}

func loadFixture(t *testing.T, name string) (fixture, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(storiesDir, name, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	f.markers, f.Records.Markers = f.Records.Markers, nil
	golden, err := os.ReadFile(filepath.Join(storiesDir, name, "story.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	return f, golden
}

func marshalStory(t *testing.T, s model.Story) []byte {
	t.Helper()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// TestFixtureRoundTrip saves every fixture story into one store, twice,
// records its phase markers as received, unjoined, and reads each back:
// the stored story is its golden file, so saving is idempotent, stories do
// not leak into each other, and the store joins each marker as the
// fixture does. Saving the story read back changes nothing either.
func TestFixtureRoundTrip(t *testing.T) {
	s := newTestStore(t, Retention{})
	names := fixtureNames(t)
	for range 2 {
		for _, name := range names {
			f, _ := loadFixture(t, name)
			mustSave(t, s, f.Root, f.Records)
		}
	}
	for _, name := range names {
		f, _ := loadFixture(t, name)
		for _, m := range f.markers {
			m.Join = nil
			mustRecord(t, s, m, testPerPod, MarkerStored)
		}
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, golden := loadFixture(t, name)
			story := mustStory(t, s, f.Root)
			if got := marshalStory(t, story); !bytes.Equal(got, golden) {
				t.Errorf("stored story differs from its golden file:\n%s", got)
			}
			mustSave(t, s, f.Root, story.Records)
			if got := marshalStory(t, mustStory(t, s, f.Root)); !bytes.Equal(got, golden) {
				t.Errorf("story saved back differs from its golden file:\n%s", got)
			}
		})
	}
}

func TestRetriesStaySeparateRuns(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	failed := finishedRun("uid-1", 0, 10, model.OutcomeFailed)
	retry := finishedRun("uid-2", 20, 30, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{Runs: []model.Run{failed}})
	mustSave(t, s, root, model.Records{Runs: []model.Run{retry}})
	story := mustStory(t, s, root)
	if len(story.Runs) != 2 {
		t.Fatalf("runs = %+v, want the failed attempt and its retry", story.Runs)
	}
	for i, want := range []model.Outcome{model.OutcomeFailed, model.OutcomeSucceeded} {
		if story.Runs[i].Outcome != want {
			t.Errorf("run %d outcome = %s, want %s", i, story.Runs[i].Outcome, want)
		}
	}
}

func TestSaveKeepsTitle(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	for _, title := range []string{"Widget cache", ""} {
		if err := s.Save(t.Context(), Batch{Root: root, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SearchRoots(t.Context(), RootQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Widget cache" {
		t.Errorf("roots = %+v, want one titled Widget cache", got)
	}
}

func TestPullRequestEventsStayAsReported(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	pr := model.PRKey{Instance: testGitHub, Repository: testRepo, Number: 3}
	var events []model.Event
	for i, kind := range []model.EventKind{model.KindPRClosed, model.KindPRReopened, model.KindPRMerged} {
		events = append(events, model.Event{
			Source: model.SourceGitHub, Instance: testGitHub, ID: "issues/events/" + strconv.Itoa(i+1),
			IDKind: model.IDSource, Kind: kind, SourceTime: ptr(at(i)), ObservedAt: at(10),
			Actor:    model.Account{Login: "owner", Kind: model.ActorPerson},
			Related:  model.Related{PR: &pr},
			Evidence: model.Evidence{Source: model.SourceGitHub, Instance: testGitHub, Ref: "timeline"},
		})
	}
	mustSave(t, s, root, model.Records{Events: events})
	story := mustStory(t, s, root)
	if len(story.Events) != len(events) {
		t.Fatalf("events = %+v, want %d", story.Events, len(events))
	}
	for i, e := range story.Events {
		if e.Kind != events[i].Kind {
			t.Errorf("event %d kind = %s, want %s", i, e.Kind, events[i].Kind)
		}
	}
}

func TestChecksOfEarlierCommitsStay(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	key := model.PRKey{Instance: testGitHub, Repository: testRepo, Number: 3}
	check := func(id int64, sha string, conclusion model.CheckConclusion, minute int) model.CheckRun {
		return model.CheckRun{ID: id, SHA: sha, Name: "test", Conclusion: conclusion,
			Interval: model.Interval{Start: model.KnownAt(at(minute)), End: model.KnownAt(at(minute + 1))}}
	}
	first := model.PullRequest{Key: key, Branch: "kelos/vikunja-7",
		Checks: []model.CheckRun{check(1, "aaaa", model.CheckFailure, 0)}}
	second := model.PullRequest{Key: key, Branch: "kelos/vikunja-7",
		Checks: []model.CheckRun{check(2, "bbbb", model.CheckSuccess, 10)}}
	mustSave(t, s, root, model.Records{PullRequests: []model.PullRequest{first}})
	mustSave(t, s, root, model.Records{PullRequests: []model.PullRequest{second}})
	story := mustStory(t, s, root)
	if len(story.PullRequests) != 1 {
		t.Fatalf("pull requests = %+v, want one", story.PullRequests)
	}
	checks := story.PullRequests[0].Checks
	if len(checks) != 2 || checks[0].Conclusion != model.CheckFailure || checks[1].Conclusion != model.CheckSuccess {
		t.Errorf("checks = %+v, want the first commit's failure and the second's success", checks)
	}
}
