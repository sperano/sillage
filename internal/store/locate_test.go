package store

import (
	"errors"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// fixtureGitHub and fixtureCluster are the instances the fixture stories
// name.
const (
	fixtureGitHub  = "github.example.com"
	fixtureCluster = "cluster-a"
)

// saveFixtures saves every fixture story into s.
func saveFixtures(t *testing.T, s *Store) {
	t.Helper()
	for _, name := range fixtureNames(t) {
		f, _ := loadFixture(t, name)
		if err := s.Save(t.Context(), Batch{Root: f.Root, Title: name, Records: f.Records}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// titles are the titles of found, which saveFixtures sets to the fixture
// names.
func titles(found []RootSummary) []string {
	out := make([]string, 0, len(found))
	for _, r := range found {
		out = append(out, r.Title)
	}
	return out
}

func wantTitles(t *testing.T, found []RootSummary, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	got := titles(found)
	if len(got) != len(want) {
		t.Fatalf("roots = %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("roots = %q, want %q", got, want)
		}
	}
}

func TestRoot(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveFixtures(t, s)
	got, err := s.Root(t.Context(), taskRoot(101))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "discuss-no-pr" || got.Root.Task == nil || *got.Root.Task != taskKey(101) || got.LastActivity == nil {
		t.Errorf("root = %+v, want task 101 with its title and last activity", got)
	}
	if got.LastActivity.Location() != time.UTC {
		t.Errorf("last activity %v is not in UTC", got.LastActivity)
	}
	if _, err := s.Root(t.Context(), taskRoot(999)); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown root: err = %v, want ErrNotFound", err)
	}
}

func TestPageRoots(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveFixtures(t, s)
	found, err := s.PageRoots(t.Context(), testWiki, 57)
	wantTitles(t, found, err, "wiki-rooted-request")
	found, err = s.PageRoots(t.Context(), testWiki, 5)
	wantTitles(t, found, err)
	found, err = s.PageRoots(t.Context(), "wiki.example.org", 57)
	wantTitles(t, found, err)
}

func TestRootsOfPullRequest(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveFixtures(t, s)
	tests := []struct {
		name string
		key  model.PRKey
		want []string
	}{
		{"stored", model.PRKey{Instance: fixtureGitHub, Repository: "example-org/widgets", Number: 7},
			[]string{"implementation-merged"}},
		{"repository in another case", model.PRKey{Instance: fixtureGitHub, Repository: "Example-Org/Widgets", Number: 7},
			[]string{"implementation-merged"}},
		{"another number", model.PRKey{Instance: fixtureGitHub, Repository: "example-org/widgets", Number: 8}, nil},
		{"another instance", model.PRKey{Instance: "github.example.org", Repository: "example-org/widgets", Number: 7},
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := s.RootsOfPullRequest(t.Context(), tt.key)
			wantTitles(t, found, err, tt.want...)
		})
	}
}

func TestRootsOfRuns(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveFixtures(t, s)
	tests := []struct {
		name  string
		query RunQuery
		want  []string
	}{
		{"name of a retried Task", RunQuery{Name: "vikunja-widgets-agent-opus-104"}, []string{"failed-run-retry"}},
		{"name in its namespace", RunQuery{Namespace: testNamespace, Name: "vikunja-widgets-agent-opus-104"},
			[]string{"failed-run-retry"}},
		{"name in another namespace", RunQuery{Namespace: "other", Name: "vikunja-widgets-agent-opus-104"}, nil},
		{"one attempt's UID", RunQuery{UID: "00000000-0000-4000-8000-00000000104b"}, []string{"failed-run-retry"}},
		{"a placeholder's name", RunQuery{Name: "vikunja-widgets-agent-opus-105"}, []string{"announced-run-only"}},
		{"a wiki follow-up", RunQuery{Name: "wiki-fu-widgets-57-c880"}, []string{"wiki-rooted-request"}},
		{"unknown name", RunQuery{Name: "vikunja-widgets-agent-opus-1"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := s.RootsOfRuns(t.Context(), tt.query)
			wantTitles(t, found, err, tt.want...)
		})
	}
	for _, q := range []RunQuery{{}, {Name: "a", UID: "b"}} {
		if _, err := s.RootsOfRuns(t.Context(), q); err == nil {
			t.Errorf("RootsOfRuns(%+v) succeeded, want an error", q)
		}
	}
}

func TestFreshness(t *testing.T) {
	s := newTestStore(t, Retention{})
	saveFixtures(t, s)
	mustSave(t, s, taskRoot(7), model.Records{Coverage: []model.Coverage{
		{Source: model.SourceKelos, Instance: fixtureCluster},
	}})
	got, err := s.Freshness(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]SourceFreshness)
	var order []string
	for _, f := range got {
		key := string(f.Source) + "/" + f.Instance
		byKey[key] = f
		order = append(order, key)
	}
	want := []string{"github/" + fixtureGitHub, "kelos/" + fixtureCluster, "vikunja/" + testVikunja, "wiki/" + testWiki}
	if len(order) != len(want) {
		t.Fatalf("instances = %q, want %q", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("instances = %q, want %q", order, want)
		}
	}
	kelos := byKey["kelos/"+fixtureCluster]
	if kelos.Roots != 6 || kelos.NeverRead != 1 || kelos.NewestRead == nil || kelos.OldestRead == nil ||
		kelos.OldestRead.After(*kelos.NewestRead) {
		t.Errorf("kelos = %+v, want 6 roots, 1 never read, oldest read not after newest", kelos)
	}
}
