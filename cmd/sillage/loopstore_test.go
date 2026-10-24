package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/reconcile/storetest"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
	"github.com/sperano/sillage/internal/store"
)

// testDatabaseEnv names the PostgreSQL the database tests run against, as
// in internal/store. They skip without it: agent pods have no database.
const testDatabaseEnv = "SILLAGE_TEST_DATABASE_URL"

// testSchemas numbers the schemas this process creates.
var testSchemas atomic.Int64

// newTestStore opens a store on a new schema, dropped when the test ends,
// or skips the test without a database.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	base := os.Getenv(testDatabaseEnv)
	if base == "" {
		t.Skipf("%s is not set: no PostgreSQL to test against", testDatabaseEnv)
	}
	admin, err := pgx.Connect(t.Context(), base)
	if err != nil {
		t.Fatalf("connecting to %s: %v", testDatabaseEnv, err)
	}
	schema := fmt.Sprintf("sillage_main_test_%d_%d", os.Getpid(), testSchemas.Add(1))
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("dropping %s: %v", schema, err)
		}
		_ = admin.Close(ctx)
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("%s does not parse as a URL", testDatabaseEnv)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	st, err := store.Open(t.Context(), store.Config{DatabaseURL: store.DatabaseURL(u.String())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestLoopStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) reconcile.Store { return loopStore{newTestStore(t)} })
}

// TestLoopStoreRefusesAScopeOfTwoInstances lists a scope stored under two
// source instances, which no loop key is: an error, not a value to choose.
func TestLoopStoreRefusesAScopeOfTwoInstances(t *testing.T) {
	st := newTestStore(t)
	ctx := t.Context()
	const scope = "collector/heartbeat/cluster-a"
	for _, instance := range []string{"cluster-a", "cluster-b"} {
		c := store.Checkpoint{Source: model.SourceKelos, Instance: instance, Scope: scope, Value: `"x"`}
		if err := st.PutCheckpoint(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loopStore{st}.Checkpoints(ctx, "collector/")
	if err == nil || !strings.Contains(err.Error(), scope) {
		t.Errorf("Checkpoints error = %v, want one naming %s", err, scope)
	}
}

func TestLoopStoreRefusesRecordsWithoutARoot(t *testing.T) {
	// The store is never reached: the commit is refused first.
	err := loopStore{}.Commit(t.Context(), reconcile.Commit{Records: model.Records{Events: []model.Event{{}}}})
	if err == nil {
		t.Error("Commit accepted records without a root")
	}
}

// The fabricated task the loop tests read, and how long they wait for a
// cycle.
const (
	loopVikunjaHost = "vikunja.example.com"
	loopCluster     = "cluster-a"
	loopNamespace   = "agents"
	loopAgent       = "agent-opus"
	loopTaskID      = 42
	loopProjectID   = 3
	loopOwnerID     = 7
	loopServiceID   = 23
	loopCommentID   = 900
	loopTaskName    = "vikunja-widgets-agent-opus-42"
	loopPod         = loopTaskName + "-pod00"
	loopUID         = "uid-42"
	cycleTimeout    = 30 * time.Second
	cyclePoll       = 20 * time.Millisecond
	// cyclesMetric counts the loop's completed cycles.
	cyclesMetric = "sillage_collector_cycles_total"
)

// loopStart is when the fabricated task was created.
var loopStart = time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

func loopRoot() model.Root {
	return model.Root{Task: &model.TaskKey{Instance: loopVikunjaHost, ID: loopTaskID}}
}

// loopVikunja serves the task, its run announced, read now.
type loopVikunja struct{}

func (loopVikunja) Fetch(_ context.Context, id int64) (vikunja.Snapshot, error) {
	if id != loopTaskID {
		return vikunja.Snapshot{}, errors.New("no such task")
	}
	announced, now := loopStart.Add(time.Minute), time.Now()
	return vikunja.Snapshot{
		Task: vikunja.Task{
			ID: id, ProjectID: loopProjectID, Created: loopStart,
			CreatedBy: &vikunja.User{ID: loopOwnerID, Username: "owner"},
		},
		Comments: []vikunja.Comment{{
			ID: loopCommentID, Created: announced, Updated: announced,
			Author: vikunja.User{ID: loopServiceID, Username: vikunja.DefaultServiceAccount},
			Body: "<p>Agent <code>" + loopAgent + "</code> has started working on this task: Kelos Task <code>" +
				loopTaskName + "</code>, pod <code>" + loopPod + "</code>.</p>",
		}},
		ObservedAt: now, CommentsObservedAt: now,
	}, nil
}

// loopKelos lists the task's run, running.
type loopKelos struct{}

func (loopKelos) Read(context.Context) (kelos.Snapshot, error) {
	start := metav1.NewTime(loopStart.Add(time.Minute))
	return kelos.Snapshot{ObservedAt: time.Now(), Tasks: []kelos.Task{{
		ObjectMeta: metav1.ObjectMeta{
			Name: loopTaskName, UID: types.UID(loopUID), CreationTimestamp: start,
			Labels: map[string]string{
				loopVikunjaHost + "/project": "widgets", loopVikunjaHost + "/variant": loopAgent,
				loopVikunjaHost + "/task": strconv.Itoa(loopTaskID),
			},
		},
		Status: kelos.TaskStatus{Phase: kelos.PhaseRunning, PodName: loopPod, StartTime: &start},
	}}}, nil
}

type loopGitHub struct{}

func (loopGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	return nil, nil
}

type loopWiki struct{}

func (loopWiki) Fetch(context.Context, int64) (wiki.Snapshot, error) {
	return wiki.Snapshot{}, errors.New("no page")
}

func (loopWiki) Pages(context.Context) ([]wiki.Page, error) { return nil, nil }

type loopProjects struct{}

func (loopProjects) ChangedTasks(context.Context, int64, time.Time) ([]vikunja.TaskChange, error) {
	return nil, nil
}

// noLock lets every loop collect.
type noLock struct{}

func (noLock) WithLock(ctx context.Context, f func(context.Context) error) error { return f(ctx) }

// runOneCycle runs a new loop over st, as a sillage started anew, until
// it completes a cycle, then stops it.
func runOneCycle(t *testing.T, st *store.Store) {
	t.Helper()
	ccfg := collector.Config{
		VikunjaInstance: loopVikunjaHost, VikunjaServiceAccount: vikunja.DefaultServiceAccount,
		VikunjaAgentUsers: []string{loopAgent},
		Kelos: kelos.Config{
			Cluster: loopCluster, Namespace: loopNamespace, LabelDomain: loopVikunjaHost, VikunjaInstance: loopVikunjaHost,
		},
		GitHubInstance: "github.example.com", WikiInstance: "wiki.example.com",
		KelosTaskTTL: collector.DefaultKelosTaskTTL,
	}
	src := reconcile.Sources{
		Sources:  collector.Sources{Vikunja: loopVikunja{}, Kelos: loopKelos{}, GitHub: loopGitHub{}, Wiki: loopWiki{}},
		Projects: loopProjects{},
	}
	reg := prometheus.NewRegistry()
	loop, err := reconcile.New(reconcile.DefaultConfig(), ccfg, src, loopStore{st}, reconcile.NewMetrics(reg),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx, noLock{})
	}()
	defer func() {
		cancel()
		<-done
	}()
	for deadline := time.Now().Add(cycleTimeout); cycles(t, reg) == 0; time.Sleep(cyclePoll) {
		if time.Now().After(deadline) {
			t.Fatalf("no cycle completed within %s", cycleTimeout)
		}
	}
}

// cycles returns how many cycles the loop whose metrics reg holds
// completed.
func cycles(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == cyclesMetric {
			return f.GetMetric()[0].GetCounter().GetValue()
		}
	}
	return 0
}

// TestLoopOverTheStoreKeepsTheRestartGap runs the loop over PostgreSQL,
// stops it, and starts another: the story the API reads keeps the first
// read's records, unchanged, and shows the downtime between the two as a
// gap of each source the first read reached.
func TestLoopOverTheStoreKeepsTheRestartGap(t *testing.T) {
	st := newTestStore(t)
	runOneCycle(t, st)
	first, err := st.Story(t.Context(), loopRoot())
	if err != nil {
		t.Fatalf("the first cycle stored no story: %v", err)
	}
	if len(first.Runs) != 1 || len(first.Events) == 0 {
		t.Fatalf("first story = %+v, want the run and the task's events", first.Records)
	}
	runOneCycle(t, st)
	story, err := st.Story(t.Context(), loopRoot())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := recordsJSON(t, story.Records), recordsJSON(t, first.Records); got != want {
		t.Errorf("records after an unchanged read:\n%s\nwant the first read's:\n%s", got, want)
	}
	for _, source := range []model.Source{model.SourceVikunja, model.SourceKelos} {
		if !hasRestartGap(story, source) {
			t.Errorf("%s coverage = %+v, want the restart's gap", source, story.Coverage)
		}
	}
}

// recordsJSON is the JSON of recs but their coverage, which every read
// advances.
func recordsJSON(t *testing.T, recs model.Records) string {
	t.Helper()
	recs.Coverage = nil
	b, err := json.Marshal(recs)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// hasRestartGap reports whether the story's coverage of source has a gap
// a restart opened, closed by a later read.
func hasRestartGap(story model.Story, source model.Source) bool {
	for _, c := range story.Coverage {
		if c.Source != source || c.LastSuccessfulRead == nil {
			continue
		}
		for _, g := range c.Gaps {
			if strings.Contains(g.Reason, "sillage was not collecting") && !g.End.After(*c.LastSuccessfulRead) {
				return true
			}
		}
	}
	return false
}
