package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/collector/collectortest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
	"github.com/sperano/sillage/internal/overview"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
	"github.com/sperano/sillage/internal/store"
)

func TestNewAPI(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"no wiki", nil, false},
		{"wiki", map[string]string{
			wiki.EnvURL: "https://wiki.example.com", wiki.EnvToken: "token", wiki.EnvAgentUsers: "21",
		}, false},
		{"wiki without a token", map[string]string{wiki.EnvURL: "https://wiki.example.com"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := newAPI(func(key string) string { return tt.env[key] }, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && h == nil {
				t.Error("no handler")
			}
		})
	}
}

// The edge-case corpus as the collection loop reads it: the fabricated
// responses of each story's sources, and the Vikunja project its task is
// listed in.
const (
	corpusFixtures = "../../testdata/fixtures"
	corpusProject  = 1
	// overviewPath is the overview's route.
	overviewPath = "/api/v1/overview"
	// hoursPerDay counts the windows of a day the overview is asked for.
	hoursPerDay = 24
)

// corpusUpdated is when the project listing says each corpus task last
// changed.
var corpusUpdated = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// corpusProjects lists task, when set, as changed in corpusProject.
type corpusProjects struct{ task *model.TaskKey }

func (p corpusProjects) ChangedTasks(_ context.Context, project int64, _ time.Time) ([]vikunja.TaskChange, error) {
	if p.task == nil || project != corpusProject {
		return nil, nil
	}
	return []vikunja.TaskChange{{ID: p.task.ID, Updated: corpusUpdated}}, nil
}

// collectCorpusStory runs one cycle of the collection loop over st, the
// story's responses read by the real clients: Kelos names the roots its
// runs point to, and the project listing the story's task. Reads of the
// other roots the runs name fail, as a source without them would answer.
func collectCorpusStory(t *testing.T, st *store.Store, story collectortest.Story) {
	t.Helper()
	cl := collectortest.New(t, corpusFixtures, story.Dirs, t.Logf)
	ccfg := collector.Config{
		VikunjaInstance: collectortest.VikunjaHost, VikunjaServiceAccount: collectortest.ServiceAccount,
		VikunjaAgentUsers: []string{collectortest.Agent},
		Kelos:             collectortest.KelosConfig(),
		GitHubInstance:    collectortest.GitHubHost, WikiInstance: collectortest.WikiHost,
		WikiAgentUsers: []int64{collectortest.WikiUser},
		Workspaces:     map[string]github.Repository{collectortest.Workspace: collectortest.Repo},
		KelosTaskTTL:   collector.DefaultKelosTaskTTL,
	}
	cfg := reconcile.DefaultConfig()
	cfg.VikunjaProjects = []int64{corpusProject}
	src := reconcile.Sources{
		Sources:  collector.Sources{Vikunja: cl.Vikunja, Kelos: cl.Kelos, GitHub: cl.GitHub, Wiki: cl.Wiki},
		Projects: corpusProjects{task: story.Root.Task},
	}
	runCycle(t, st, cfg, ccfg, src)
}

// liveChecks are what the overview served from a collected corpus story
// must show beyond what the stored stories build, by story; rowsIn asks
// the API for the rows of the hour from an RFC 3339 time.
var liveChecks = map[string]func(t *testing.T, rowsIn func(from string) []overview.Row){
	// The other run of the agent profile is not the page's: it is in its
	// own task's row, which only the loop's Kelos discovery stores.
	"wiki-rooted-request": func(t *testing.T, rowsIn func(string) []overview.Row) {
		rows := rowsIn("2026-03-04T15:00:00Z")
		if got := rowRuns(t, rows, "wiki.example.com/pages/57/en"); !slices.Equal(got, []string{"00000000-0000-4000-8000-000000000057"}) {
			t.Errorf("page 57's runs = %v, want its own run only", got)
		}
		if got := rowRuns(t, rows, "vikunja.example.com/tasks/103"); !slices.Equal(got, []string{"00000000-0000-4000-8000-000000000103"}) {
			t.Errorf("task 103's runs = %v, want its run", got)
		}
	},
	"failed-run-retry": func(t *testing.T, rowsIn func(string) []overview.Row) {
		got := rowRuns(t, rowsIn("2026-03-05T14:00:00Z"), "vikunja.example.com/tasks/104")
		if want := []string{"00000000-0000-4000-8000-00000000104a", "00000000-0000-4000-8000-00000000104b"}; !slices.Equal(got, want) {
			t.Errorf("task 104's runs = %v, want the failure and its retry %v", got, want)
		}
	},
}

// rowRuns returns the run UIDs of the row of root key, failing the test
// without one.
func rowRuns(t *testing.T, rows []overview.Row, key string) []string {
	t.Helper()
	for _, row := range rows {
		if rootKey(row.Root) != key {
			continue
		}
		uids := []string{}
		for _, r := range row.Runs {
			uids = append(uids, r.Key.UID)
		}
		return uids
	}
	t.Fatalf("no row of %s", key)
	return nil
}

func rootKey(root model.Root) string {
	if root.Task != nil {
		return root.Task.String()
	}
	return root.Page.String()
}

// TestOverviewOverTheCollectedCorpus runs the collection loop over each
// story of the edge-case corpus into PostgreSQL, then asks GET
// /api/v1/overview for every hour of the stored records' days. It serves
// the overview of the stories the store holds, read whole: Activity, over
// what the loop committed, misses nothing a row needs; a walk of its
// cursors, one row a page, gives the same rows; and every row says its
// history is missing, since the loop first read Kelos months after the
// corpus, past the Task TTL.
func TestOverviewOverTheCollectedCorpus(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			st := newTestStore(t)
			collectCorpusStory(t, st, collectortest.Stories[name])
			h, err := newAPI(func(string) string { return "" }, st)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			stories := storedStories(t, st)
			for _, w := range hoursOf(stories) {
				got := servedOverview(t, srv.URL, w, api.MaxOverviewLimit, "")
				if g, want := mustJSON(t, got), mustJSON(t, builtOverview(t, w, stories)); g != want {
					t.Errorf("window %s: served\n%s\nwant the stored stories'\n%s", w.From, g, want)
				}
				checkCursorWalk(t, srv.URL, w, got.Rows)
				for _, row := range got.Rows {
					if !row.MissingHistory {
						t.Errorf("window %s: %s reads as complete before the Task TTL", w.From, rootKey(row.Root))
					}
				}
			}
			if check, ok := liveChecks[name]; ok {
				check(t, func(from string) []overview.Row {
					start, err := time.Parse(time.RFC3339, from)
					if err != nil {
						t.Fatal(err)
					}
					w := overview.Window{From: start, To: start.Add(time.Hour)}
					return servedOverview(t, srv.URL, w, api.MaxOverviewLimit, "").Rows
				})
			}
		})
	}
}

// storedStories reads every stored root's whole story, as the single-task
// page does.
func storedStories(t *testing.T, st *store.Store) []overview.Input {
	t.Helper()
	roots, err := st.SearchRoots(t.Context(), store.RootQuery{Limit: store.MaxSearchLimit})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]overview.Input, 0, len(roots))
	for _, r := range roots {
		story, err := st.Story(t.Context(), r.Root)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, overview.Input{Root: r.Root, Title: r.Title, Records: story.Records})
	}
	return out
}

// hoursOf returns each hour of the UTC days the stories' events and runs
// fall on, in order.
func hoursOf(stories []overview.Input) []overview.Window {
	var days []time.Time
	add := func(t time.Time, ok bool) {
		if d := t.UTC().Truncate(hoursPerDay * time.Hour); ok && !slices.ContainsFunc(days, d.Equal) {
			days = append(days, d)
		}
	}
	for _, s := range stories {
		for _, e := range s.Records.Events {
			add(e.Time(), true)
		}
		for _, r := range s.Records.Runs {
			add(r.Interval.Start.Time())
			add(r.Interval.End.Time())
		}
	}
	slices.SortFunc(days, time.Time.Compare)
	var out []overview.Window
	for _, d := range days {
		for h := range hoursPerDay {
			from := d.Add(time.Duration(h) * time.Hour)
			out = append(out, overview.Window{From: from, To: from.Add(time.Hour)})
		}
	}
	return out
}

// builtOverview is the overview of w over stories, on one page.
func builtOverview(t *testing.T, w overview.Window, stories []overview.Input) overview.Page {
	t.Helper()
	rows, err := overview.Build(w, stories)
	if err != nil {
		t.Fatal(err)
	}
	page, err := overview.Paginate(w, rows, overview.Filter{}, "", api.MaxOverviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// servedOverview asks the API at base for a page of w's overview.
func servedOverview(t *testing.T, base string, w overview.Window, limit int, after string) overview.Page {
	t.Helper()
	q := url.Values{
		"from": {w.From.Format(time.RFC3339)}, "to": {w.To.Format(time.RFC3339)}, "limit": {strconv.Itoa(limit)},
	}
	if after != "" {
		q.Set("after", after)
	}
	resp, err := http.Get(base + overviewPath + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", overviewPath, resp.Status)
	}
	var page overview.Page
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	return page
}

// checkCursorWalk follows w's cursors one row a page: it gives rows, in
// order, each page counting them all.
func checkCursorWalk(t *testing.T, base string, w overview.Window, rows []overview.Row) {
	t.Helper()
	walked := []overview.Row{}
	for after := ""; ; {
		page := servedOverview(t, base, w, 1, after)
		if page.Total != len(rows) {
			t.Errorf("window %s: total %d, want %d", w.From, page.Total, len(rows))
		}
		walked = append(walked, page.Rows...)
		if page.Next == "" || len(walked) > len(rows) {
			break
		}
		after = page.Next
	}
	if g, want := mustJSON(t, walked), mustJSON(t, rows); len(walked) != len(rows) || g != want {
		t.Errorf("window %s: walked rows\n%s\nwant\n%s", w.From, g, want)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
