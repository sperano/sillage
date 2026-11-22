package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func usageEvent(pod, podUID, seq string, minute int) model.UsageEvent {
	return model.UsageEvent{
		Cluster: testCluster, Namespace: testNamespace, Pod: pod, PodUID: podUID, TaskName: testTaskName,
		Runner: model.RunnerClaudeCode, Kind: model.UsageResponse, Event: "api_request", Stream: "session-1",
		Seq: seq, At: at(minute), Model: "claude-example-1",
		Tokens: &model.TokenCounts{UncachedInput: ptr(int64(10)), Output: ptr(int64(5))},
	}
}

func countUsage(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM usage_events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSaveUsageIsImmutableAndCountsNewEvents(t *testing.T) {
	s := newTestStore(t, Retention{})
	first := usageEvent("pod-a", "uid-a", "1", 1)
	n, err := s.SaveUsage(t.Context(), []model.UsageEvent{first, usageEvent("pod-a", "uid-a", "2", 2)})
	if err != nil || n != 2 {
		t.Fatalf("SaveUsage = %d, %v; want 2, nil", n, err)
	}
	changed := first
	changed.Tokens = &model.TokenCounts{Output: ptr(int64(999))}
	n, err = s.SaveUsage(t.Context(), []model.UsageEvent{changed, usageEvent("pod-a", "uid-a", "3", 3)})
	if err != nil || n != 1 {
		t.Fatalf("second SaveUsage = %d, %v; want 1, nil", n, err)
	}
	got, err := s.UsageOf(t.Context(), usageStory(model.Run{Key: uidKey("run-1"), Pod: "pod-a"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 3 || *got.Events[0].Tokens.Output != 5 || got.Events[0].Tokens.UncachedInput == nil {
		t.Errorf("events = %+v; want 3, the first unchanged", got.Events)
	}
}

func TestSaveUsageRejectsInvalidEventWholesale(t *testing.T) {
	s := newTestStore(t, Retention{})
	bad := usageEvent("pod-a", "uid-a", "2", 2)
	bad.PodUID = ""
	n, err := s.SaveUsage(t.Context(), []model.UsageEvent{usageEvent("pod-a", "uid-a", "1", 1), bad})
	if err == nil || !strings.Contains(err.Error(), "pod UID") || n != 0 {
		t.Fatalf("SaveUsage = %d, %v; want 0 and a pod UID error", n, err)
	}
	if got := countUsage(t, s); got != 0 {
		t.Errorf("%d events saved, want none", got)
	}
}

func TestSaveUsageKeepsEventsOfAKeyApart(t *testing.T) {
	s := newTestStore(t, Retention{})
	a := usageEvent("pod-a", "uid-a", "1", 1)
	b := a
	b.Event = "api_error"
	b.Kind, b.Tokens = model.UsageFailedRequest, nil
	c := a
	c.PodUID = "uid-b"
	if n, err := s.SaveUsage(t.Context(), []model.UsageEvent{a, b, c}); err != nil || n != 3 {
		t.Fatalf("SaveUsage = %d, %v; want 3, nil", n, err)
	}
}

func TestUsageFeedNeverMovesBackwards(t *testing.T) {
	s := newTestStore(t, Retention{})
	if got, err := s.UsageFeed(t.Context()); err != nil || got != nil {
		t.Fatalf("UsageFeed = %v, %v; want nil, nil", got, err)
	}
	for _, minute := range []int{5, 3, 9} {
		if err := s.SetUsageFeed(t.Context(), at(minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.UsageFeed(t.Context())
	if err != nil || got == nil || !got.Equal(at(9)) || got.Location() != time.UTC {
		t.Fatalf("UsageFeed = %v, %v; want %v in UTC", got, err, at(9))
	}
	if err := s.SetUsageFeed(t.Context(), at(4)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UsageFeed(t.Context()); !got.Equal(at(9)) {
		t.Errorf("UsageFeed = %v after an earlier set, want %v", got, at(9))
	}
	if err := s.SetUsageFeed(t.Context(), time.Time{}); err == nil {
		t.Error("SetUsageFeed accepted a zero time")
	}
}

func usageStory(runs ...model.Run) model.Story {
	return model.Story{Records: model.Records{Runs: runs}}
}

func TestUsageOfJoinsByPodsAndTaskNames(t *testing.T) {
	s := newTestStore(t, Retention{})
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO run_pods (cluster, namespace, uid, pod, ref) VALUES ($1, $2, 'run-1', 'pod-old', 'tasks/x')`,
		testCluster, testNamespace)
	named := usageEvent("pod-other", "uid-other", "1", 4)
	named.TaskName = "task-by-name"
	otherNS := usageEvent("pod-old", "uid-old-ns", "1", 1)
	otherNS.Namespace = "elsewhere"
	unrelated := usageEvent("pod-none", "uid-none", "1", 2)
	unrelated.TaskName = "other-task"
	events := []model.UsageEvent{
		usageEvent("pod-new", "uid-new", "1", 3), usageEvent("pod-old", "uid-old", "2", 2),
		usageEvent("pod-old", "uid-old", "1", 2), named, otherNS, unrelated,
	}
	for i := range events {
		if events[i].TaskName == testTaskName {
			events[i].TaskName = ""
		}
	}
	if _, err := s.SaveUsage(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUsageFeed(t.Context(), at(30)); err != nil {
		t.Fatal(err)
	}
	story := usageStory(
		model.Run{Key: uidKey("run-1"), Pod: "pod-new"},
		model.Run{Key: uidKey("run-2"), TaskName: "task-by-name"},
		model.Run{Key: podRunKey("pod-placeholder")},
	)
	got, err := s.UsageOf(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range got.Events {
		keys = append(keys, e.PodUID+"/"+e.Seq)
	}
	if want := []string{"uid-old/1", "uid-old/2", "uid-new/1", "uid-other/1"}; !slices.Equal(keys, want) {
		t.Errorf("events = %v, want %v", keys, want)
	}
	if want := []string{"pod-new", "pod-old"}; !slices.Equal(got.Pods[uidKey("run-1")], want) {
		t.Errorf("pods of run-1 = %v, want %v", got.Pods[uidKey("run-1")], want)
	}
	if pods, ok := got.Pods[uidKey("run-2")]; !ok || len(pods) != 0 {
		t.Errorf("pods of run-2 = %v, %v; want present and empty", pods, ok)
	}
	if len(got.Pods) != 2 {
		t.Errorf("Pods = %v, want the two runs keyed by Task UID", got.Pods)
	}
	if got.ReadThrough == nil || !got.ReadThrough.Equal(at(30)) {
		t.Errorf("ReadThrough = %v, want %v", got.ReadThrough, at(30))
	}
}

func TestUsageOfEmptyStory(t *testing.T) {
	s := newTestStore(t, Retention{})
	got, err := s.UsageOf(t.Context(), model.Story{})
	if err != nil || len(got.Events) != 0 || got.ReadThrough != nil {
		t.Errorf("UsageOf = %+v, %v; want nothing", got, err)
	}
}

func TestPruneDeletesUsageByEventTime(t *testing.T) {
	s := newTestStore(t, Retention{keep: 24 * time.Hour})
	old, recent := usageEvent("pod-a", "uid-a", "1", -3*24*60), usageEvent("pod-a", "uid-a", "2", -60)
	if _, err := s.SaveUsage(t.Context(), []model.UsageEvent{old, recent}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(t.Context(), testBase); err != nil {
		t.Fatal(err)
	}
	if got := countUsage(t, s); got != 1 {
		t.Errorf("%d usage events after pruning, want only the recent one", got)
	}
}

// usageEventsMigrationVersion is 0011_usage_events.sql's version.
const usageEventsMigrationVersion = 11

// TestUsageEventsMigrationUpgradesAStore applies the usage migration to a
// store that holds runs' pods already: they are kept, history has no usage
// (nothing is back-filled), and the identity keeps an event once.
func TestUsageEventsMigrationUpgradesAStore(t *testing.T) {
	ctx := t.Context()
	pool := migratedBefore(t, usageEventsMigrationVersion)
	if _, err := pool.Exec(ctx, `INSERT INTO run_pods (cluster, namespace, uid, pod, ref)
		VALUES ('cluster-a', 'agents', 'run-1', 'pod-1', 'tasks/agents/task-1')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	wantCount(t, pool, "SELECT count(*) FROM run_pods", 1)
	wantCount(t, pool, "SELECT count(*) FROM usage_events", 0)
	wantCount(t, pool, "SELECT count(*) FROM usage_feed", 0)
	const insert = `INSERT INTO usage_events (cluster, namespace, pod, pod_uid, runner, kind, event, stream, seq, at, body)
		VALUES ('cluster-a', 'agents', 'pod-1', 'uid-1', 'claude-code', 'response', 'api_request', 's', '1', now(), '{}')
		ON CONFLICT DO NOTHING`
	for range 2 {
		if _, err := pool.Exec(ctx, insert); err != nil {
			t.Fatal(err)
		}
	}
	wantCount(t, pool, "SELECT count(*) FROM usage_events", 1)
}
