package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

// storiesDir holds the fixture stories and their golden files.
const storiesDir = "../../testdata/fixtures/stories"

// overviewStoriesDir holds the overview fixture set: one afternoon of
// eight tasks, one file each.
const overviewStoriesDir = "../../testdata/fixtures/overview/stories"

// goldenFile is each fixture story's model.BuildStory output.
const goldenFile = "story.golden.json"

// fixture is one fixture story: its name, its golden file's bytes and the
// story they decode to.
type fixture struct {
	name   string
	golden []byte
	story  model.Story
}

// loadFixtures reads every fixture story's golden file.
func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	entries, err := os.ReadDir(storiesDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []fixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(storiesDir, e.Name(), goldenFile))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		f := fixture{name: e.Name(), golden: data}
		if err := json.Unmarshal(data, &f.story); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		t.Fatalf("no fixture stories in %s", storiesDir)
	}
	return out
}

// loadOverviewFixtures reads every story of the overview fixture set.
func loadOverviewFixtures(t *testing.T) []store.RootActivity {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(overviewStoriesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no overview fixtures in %s: %v", overviewStoriesDir, err)
	}
	var out []store.RootActivity
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Title string     `json:"title"`
			Root  model.Root `json:"root"`
			model.Records
		}
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out = append(out, store.RootActivity{RootSummary: store.RootSummary{Root: f.Root, Title: f.Title}, Records: f.Records})
	}
	return out
}

// fakeStore is a store.Reader over the fixture stories, each titled by
// its fixture's name, and the overview fixture set.
type fakeStore struct {
	stories  []fixture
	overview []store.RootActivity
}

var _ store.Reader = (*fakeStore)(nil)

func newFakeStore(t *testing.T) *fakeStore {
	return &fakeStore{stories: loadFixtures(t), overview: loadOverviewFixtures(t)}
}

func rootKey(r model.Root) string {
	return r.Task.Ref().String()
}

func (f *fakeStore) find(root model.Root) (fixture, bool) {
	i := slices.IndexFunc(f.stories, func(s fixture) bool { return rootKey(s.story.Root) == rootKey(root) })
	if i < 0 {
		return fixture{}, false
	}
	return f.stories[i], true
}

func summary(s fixture) store.RootSummary {
	return store.RootSummary{Root: s.story.Root, Title: s.name}
}

// matching summarizes the stories keep selects.
func (f *fakeStore) matching(keep func(model.Story) bool) []store.RootSummary {
	var out []store.RootSummary
	for _, s := range f.stories {
		if keep(s.story) {
			out = append(out, summary(s))
		}
	}
	return out
}

func (f *fakeStore) Story(_ context.Context, root model.Root) (model.Story, error) {
	s, ok := f.find(root)
	if !ok {
		return model.Story{}, store.ErrNotFound
	}
	return s.story, nil
}

func (f *fakeStore) Coverage(_ context.Context, root model.Root) ([]model.Coverage, error) {
	s, ok := f.find(root)
	if !ok {
		return nil, store.ErrNotFound
	}
	return s.story.Coverage, nil
}

func (f *fakeStore) Root(_ context.Context, root model.Root) (store.RootSummary, error) {
	s, ok := f.find(root)
	if !ok {
		return store.RootSummary{}, store.ErrNotFound
	}
	return summary(s), nil
}

// SearchRoots matches q against the stories' keys and titles in any case,
// as the store does, in fixture order.
func (f *fakeStore) SearchRoots(_ context.Context, q store.RootQuery) ([]store.RootSummary, error) {
	text := strings.ToLower(q.Text)
	var out []store.RootSummary
	for _, s := range f.stories {
		if strings.Contains(strings.ToLower(rootKey(s.story.Root)), text) ||
			strings.Contains(strings.ToLower(s.name), text) {
			out = append(out, summary(s))
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *fakeStore) RootsOfPullRequest(_ context.Context, key model.PRKey) ([]store.RootSummary, error) {
	return f.matching(func(s model.Story) bool {
		return slices.ContainsFunc(s.PullRequests, func(p model.PullRequest) bool {
			return p.Key.Instance == key.Instance && strings.EqualFold(p.Key.Repository, key.Repository) &&
				p.Key.Number == key.Number
		})
	}), nil
}

func (f *fakeStore) RootsOfRuns(_ context.Context, q store.RunQuery) ([]store.RootSummary, error) {
	return f.matching(func(s model.Story) bool {
		return slices.ContainsFunc(s.Runs, func(r model.Run) bool {
			named := (q.UID != "" && r.Key.UID == q.UID) || (q.Name != "" && r.TaskName == q.Name)
			return named && (q.Namespace == "" || r.Key.Namespace == q.Namespace)
		})
	}), nil
}

func (f *fakeStore) Freshness(context.Context) ([]store.SourceFreshness, error) {
	by := make(map[string]*store.SourceFreshness)
	for _, s := range f.stories {
		for _, c := range s.story.Coverage {
			key := c.Key()
			if by[key] == nil {
				by[key] = &store.SourceFreshness{Source: c.Source, Instance: c.Instance}
			}
			by[key].Roots++
			countRead(by[key], c.LastSuccessfulRead)
		}
	}
	var out []store.SourceFreshness
	for _, v := range by {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b store.SourceFreshness) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Instance, b.Instance))
	})
	return out, nil
}

// countRead counts a root's last successful read into f, as the store
// does.
func countRead(f *store.SourceFreshness, at *time.Time) {
	switch {
	case at == nil:
		f.NeverRead++
	case f.NewestRead == nil:
		f.NewestRead, f.OldestRead = at, at
	case at.After(*f.NewestRead):
		f.NewestRead = at
	case at.Before(*f.OldestRead):
		f.OldestRead = at
	}
}

// Activity returns every story, the overview set's and the fixture
// stories, with the project q names when it names one: more than the store
// would, which the overview narrows to the window.
func (f *fakeStore) Activity(_ context.Context, q store.ActivityQuery) ([]store.RootActivity, error) {
	all := slices.Clone(f.overview)
	for _, s := range f.stories {
		all = append(all, store.RootActivity{RootSummary: summary(s), Records: s.story.Records})
	}
	out := slices.DeleteFunc(all, func(a store.RootActivity) bool {
		return q.Project != "" && !slices.ContainsFunc(a.Records.Runs, func(r model.Run) bool { return r.Project == q.Project })
	})
	limit := q.Limit
	if limit <= 0 || limit > store.MaxActivityRoots {
		limit = store.MaxActivityRoots
	}
	if len(out) > limit {
		return nil, store.ErrTooManyRoots
	}
	return out, nil
}
