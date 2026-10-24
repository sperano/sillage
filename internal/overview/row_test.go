package overview

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// The fixture runs' cluster, namespace and Task name.
const (
	testCluster   = "cluster-a"
	testNamespace = "agents"
	testTaskName  = "vikunja-widgets-agent-cedar-301"
)

func testRun(uid string, start, end model.Bound, outcome model.Outcome) model.Run {
	return model.Run{
		Key:      model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid},
		TaskName: testTaskName,
		Pod:      testTaskName + "-" + uid,
		Interval: model.Interval{Start: start, End: end},
		Outcome:  outcome,
	}
}

func announcementAt(min int, details map[string]string) model.Event {
	t := minute(min)
	return model.Event{Kind: model.KindRunAnnounced, SourceTime: &t, Details: details}
}

func runUIDs(runs []model.Run) []string {
	out := []string{}
	for _, r := range runs {
		out = append(out, r.Key.UID)
	}
	return out
}

// A failure is kept for a retry of the same Kelos Task only: a Task name is
// unique in its cluster and namespace, not across them.
func TestRetryOfTheSameTaskOnly(t *testing.T) {
	failed := testRun("failed", model.KnownAt(minute(-50)), model.KnownAt(minute(-40)), model.OutcomeFailed)
	for _, c := range []struct {
		name  string
		retry func(*model.Run)
		want  []string
	}{
		{"same namespace", func(*model.Run) {}, []string{"failed", "retry"}},
		{"another namespace", func(r *model.Run) { r.Key.Namespace = "others" }, []string{"retry"}},
		{"another cluster", func(r *model.Run) { r.Key.Cluster = "cluster-b" }, []string{"retry"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			retry := testRun("retry", model.KnownAt(minute(10)), model.OpenEnd(), model.OutcomeRunning)
			c.retry(&retry)
			got := runUIDs(runsIn(afternoon, model.Story{Runs: []model.Run{failed, retry}}))
			if !slices.Equal(got, c.want) {
				t.Errorf("runs = %v, want %v", got, c.want)
			}
		})
	}
}

// An announcement naming no pod brings in every attempt of its Task name
// whose recorded times leave open that it ran then, ambiguous as it is.
func TestPodlessAnnouncement(t *testing.T) {
	runs := []model.Run{
		testRun("unknown", model.UnknownBound(), model.UnknownBound(), model.OutcomeUnknown),
		testRun("ended-before", model.KnownAt(minute(-90)), model.KnownAt(minute(-70)), model.OutcomeSucceeded),
		testRun("across", model.KnownAt(minute(-30)), model.UnknownBound(), model.OutcomeUnknown),
		testRun("later", model.KnownAt(minute(30)), model.KnownAt(minute(40)), model.OutcomeSucceeded),
	}
	for _, c := range []struct {
		name    string
		details map[string]string
		want    []string
	}{
		{"by Task name", map[string]string{detailTaskName: testTaskName}, []string{"unknown", "across", "later"}},
		{"in its namespace", map[string]string{detailTaskName: testTaskName, detailNamespace: testNamespace},
			[]string{"unknown", "across", "later"}},
		{"in another namespace", map[string]string{detailTaskName: testTaskName, detailNamespace: "others"},
			[]string{"later"}},
		{"of another Task", map[string]string{detailTaskName: "other-task"}, []string{"later"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := model.Story{Runs: runs, Events: []model.Event{announcementAt(5, c.details)}}
			if got := runUIDs(runsIn(afternoon, s)); !slices.Equal(got, c.want) {
				t.Errorf("runs = %v, want %v", got, c.want)
			}
		})
	}
}

// A run created before the window that starts in it shows from its start,
// even with its end unknown, and is active then.
func TestRunStartingInTheWindowAfterAnEarlierCreation(t *testing.T) {
	created := minute(-20)
	r := testRun("late-start", model.KnownAt(minute(10)), model.UnknownBound(), model.OutcomeUnknown)
	r.Created = &created
	row, ok := buildRow(afternoon, "", model.Story{Runs: []model.Run{r}})
	if !ok || len(row.Runs) != 1 {
		t.Fatalf("row = %+v %v, want the run", row, ok)
	}
	if want := minute(10); !row.LastActivity.Equal(want) {
		t.Errorf("last activity = %s, want its start, %s", row.LastActivity, want)
	}
}

// A mark's source counts toward the row's sources: a mark read from a
// source with no coverage leaves its history missing.
func TestMarkSourceWithoutCoverage(t *testing.T) {
	done := minute(10)
	e := model.Event{
		Source: model.SourceVikunja, Instance: "vikunja.example.com", ID: "done",
		Kind: model.KindTaskDone, SourceTime: &done,
	}
	row, ok := buildRow(afternoon, "", model.Story{Events: []model.Event{e}})
	if !ok {
		t.Fatal("no row for a completion in the window")
	}
	if m := row.Marks[0]; m.Source != model.SourceVikunja || m.Instance != "vikunja.example.com" {
		t.Errorf("mark = %+v, want its source and instance", m)
	}
	if !row.MissingHistory {
		t.Error("a completion from a source with no coverage leaves the history complete")
	}
}
