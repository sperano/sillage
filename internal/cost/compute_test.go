package cost

import (
	"slices"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	testCluster   = "cluster-a"
	testNamespace = "agents"
)

func testRun(t *testing.T, uid, name, pod, start, end string) model.Run {
	t.Helper()
	r := model.Run{Key: model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}, TaskName: name, Pod: pod,
		Role: model.RoleImplementation, Outcome: model.OutcomeSucceeded,
		Interval: model.Interval{Start: model.KnownAt(at(t, start)), End: model.KnownAt(at(t, end))}}
	return r
}

func testEvent(t *testing.T, pod, name, seq, when string, uncached int64) model.UsageEvent {
	t.Helper()
	return model.UsageEvent{Cluster: testCluster, Namespace: testNamespace, Pod: pod, PodUID: "uid-" + pod, TaskName: name,
		Runner: model.RunnerClaudeCode, Kind: model.UsageResponse, Event: "api_request", Stream: "s", Seq: seq,
		At: at(t, when), Model: "claude-opus-5-5", Tokens: &model.TokenCounts{
			UncachedInput: count(uncached), CacheRead: count(0), CacheWrite5m: count(0), Output: count(0)}}
}

func compute(t *testing.T, runs []model.Run, events []model.UsageEvent, feed Feed) TaskCost {
	t.Helper()
	return Compute(mustTable(t), Input{Story: model.Story{Runs: runs}, Events: events, Feed: feed})
}

func on(t *testing.T, readThrough string) Feed {
	t.Helper()
	rt := at(t, readThrough)
	return Feed{On: true, ReadThrough: &rt}
}

func TestComputeKeepsTheFirstDelivery(t *testing.T) {
	r := testRun(t, "u1", "task-a", "pod-a", "2026-11-23T10:00:00Z", "2026-11-23T11:00:00Z")
	first := testEvent(t, "pod-a", "task-a", "1", "2026-11-23T10:01:00Z", 1000000)
	again := first
	again.Tokens = &model.TokenCounts{UncachedInput: count(9000000), CacheRead: count(0), CacheWrite5m: count(0), Output: count(0)}
	again.At = first.At.Add(time.Second)
	tc := compute(t, []model.Run{r}, []model.UsageEvent{again, first}, on(t, "2026-11-24T00:00:00Z"))
	if got := tc.Total.EstimatedUSD; got != "4.000000" || tc.Total.Requests != 1 {
		t.Errorf("total = %q over %d requests, want the first delivery only: 4.000000 over 1", got, tc.Total.Requests)
	}
}

func TestComputeAttribution(t *testing.T) {
	a := testRun(t, "u1", "task-a", "pod-a", "2026-11-23T10:00:00Z", "2026-11-23T11:00:00Z")
	b := testRun(t, "u2", "task-a", "pod-b", "2026-11-23T10:30:00Z", "2026-11-23T12:00:00Z")
	feed := on(t, "2026-11-24T00:00:00Z")
	cases := []struct {
		name         string
		pods         map[model.RunKey][]string
		event        model.UsageEvent
		runA, runB   int
		unattributed bool
	}{
		{"by pod", nil, testEvent(t, "pod-b", "task-a", "1", "2026-11-23T10:40:00Z", 1), 0, 1, false},
		{"by a pod seen with the run", map[model.RunKey][]string{a.Key: {"pod-a2"}}, testEvent(t, "pod-a2", "task-a", "1", "2026-11-23T10:40:00Z", 1), 1, 0, false},
		{"one pod seen with two runs", map[model.RunKey][]string{a.Key: {"pod-b"}}, testEvent(t, "pod-b", "task-a", "1", "2026-11-23T10:40:00Z", 1), 0, 0, true},
		{"by the one run live then", nil, testEvent(t, "pod-x", "task-a", "1", "2026-11-23T10:10:00Z", 1), 1, 0, false},
		{"two runs live then", nil, testEvent(t, "pod-x", "task-a", "1", "2026-11-23T10:40:00Z", 1), 0, 0, true},
		{"no run live then", nil, testEvent(t, "pod-x", "task-a", "1", "2026-11-23T13:00:00Z", 1), 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := Compute(mustTable(t), Input{Story: model.Story{Runs: []model.Run{a, b}}, Events: []model.UsageEvent{c.event}, Pods: c.pods, Feed: feed})
			if tc.Runs[0].Requests != c.runA || tc.Runs[1].Requests != c.runB || (tc.Unattributed != nil) != c.unattributed {
				t.Errorf("requests by run = %d, %d, unattributed %v; want %d, %d, %v",
					tc.Runs[0].Requests, tc.Runs[1].Requests, tc.Unattributed != nil, c.runA, c.runB, c.unattributed)
			}
			if c.unattributed && !slices.Contains(tc.Total.Notes, NoteUnattributed) {
				t.Errorf("total notes = %v, want %s", tc.Total.Notes, NoteUnattributed)
			}
		})
	}
}

func TestComputeFinal(t *testing.T) {
	r := testRun(t, "u1", "task-a", "pod-a", "2026-11-23T10:00:00Z", "2026-11-23T11:00:00Z")
	ev := []model.UsageEvent{testEvent(t, "pod-a", "task-a", "1", "2026-11-23T10:01:00Z", 1)}
	end := at(t, "2026-11-23T11:00:00Z")
	enough, short := end.Add(FinalGrace), end.Add(FinalGrace-time.Nanosecond)
	if tc := compute(t, []model.Run{r}, ev, Feed{On: true, ReadThrough: &enough}); !tc.Final || !tc.Runs[0].Final {
		t.Errorf("read through end+FinalGrace: final %v, run final %v; want both", tc.Final, tc.Runs[0].Final)
	}
	if tc := compute(t, []model.Run{r}, ev, Feed{On: true, ReadThrough: &short}); tc.Final || tc.Runs[0].Final {
		t.Error("read through just short of end+FinalGrace: want not final")
	}
	if tc := compute(t, []model.Run{r}, ev, Feed{On: true}); tc.Final {
		t.Error("a feed that never read: want not final")
	}
	if tc := compute(t, nil, nil, Feed{On: true, ReadThrough: &enough}); tc.Final || tc.Total.Status != StatusUnknown {
		t.Errorf("no runs: final %v status %s, want not final, unknown", tc.Final, tc.Total.Status)
	}
}

func TestComputeRunNotes(t *testing.T) {
	since := at(t, "2026-11-23T10:30:00Z")
	feed := on(t, "2026-11-24T00:00:00Z")
	feed.Since = &since
	before := testRun(t, "u1", "task-a", "pod-a", "2026-11-23T10:00:00Z", "2026-11-23T11:00:00Z")
	unknownStart := testRun(t, "u2", "task-b", "pod-b", "2026-11-23T11:00:00Z", "2026-11-23T12:00:00Z")
	unknownStart.Interval.Start = model.UnknownBound()
	failed := testRun(t, "u3", "task-c", "pod-c", "2026-11-23T11:00:00Z", "2026-11-23T12:00:00Z")
	failed.Outcome = model.OutcomeFailed
	retried := testRun(t, "u4", "task-d", "pod-d", "2026-11-23T11:00:00Z", "2026-11-23T12:00:00Z")
	automation := testRun(t, "u5", "task-e", "pod-e", "2026-11-23T11:00:00Z", "2026-11-23T12:00:00Z")
	automation.Role = model.RoleAutomation
	runs := []model.Run{before, unknownStart, failed, retried, automation}
	events := []model.UsageEvent{
		testEvent(t, "pod-a", "task-a", "1", "2026-11-23T10:40:00Z", 1),
		testEvent(t, "pod-c", "task-c", "1", "2026-11-23T11:10:00Z", 1),
		testEvent(t, "pod-d", "task-d", "1", "2026-11-23T11:10:00Z", 1),
	}
	tc := Compute(mustTable(t), Input{Story: model.Story{Runs: runs}, Events: events, Feed: feed,
		Pods: map[model.RunKey][]string{retried.Key: {"pod-d0"}}})
	want := []struct {
		status Status
		notes  []string
	}{
		{StatusPartial, []string{NoteBeforeTelemetry}},
		{StatusUnknown, []string{NoteBeforeTelemetry, NoteNoUsageEvents}},
		{StatusPartial, []string{NoteMayLoseUnsent}},
		{StatusPartial, []string{NoteNoUsageEvents}},
		{StatusNone, nil},
	}
	for i, w := range want {
		if got := tc.Runs[i]; got.Status != w.status || !slices.Equal(got.Notes, w.notes) {
			t.Errorf("run %s: %s %v, want %s %v", got.Key.UID, got.Status, got.Notes, w.status, w.notes)
		}
	}
	if got := tc.Runs[3].Attempts; len(got) != 2 || got[0].Pod != "pod-d" || got[1].Pod != "pod-d0" || got[1].Status != StatusUnknown {
		t.Errorf("retried run's attempts = %+v, want pod-d priced and pod-d0 unknown", got)
	}
}
