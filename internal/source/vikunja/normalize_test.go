package vikunja

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// serviceAccount is the fixtures' Kelos account.
const serviceAccount = "kelos"

var (
	fixtureOwner = User{ID: 7, Username: "owner", Name: "Robin Owner"}
	fixtureAgent = User{ID: 21, Username: "agent-opus", Name: "Agent Opus"}
	fixtureKelos = User{ID: 23, Username: "kelos", Name: "Kelos"}
	// fixtureZone is the fixtures' UTC offset.
	fixtureZone = time.FixedZone("", -5*60*60)
)

func at(month time.Month, day, hour, minute, sec int) time.Time {
	return time.Date(2026, month, day, hour, minute, sec, 0, fixtureZone)
}

func kinds(events []Event) []Kind {
	out := make([]Kind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

func sourceIDs(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.SourceID
	}
	return out
}

func TestNormalizeTwoPages(t *testing.T) {
	snap, _ := fetchStory(t, "two-pages", 4201)
	events := Normalize(snap, serviceAccount)

	wantIDs := []string{
		"task/4201/created", "comment/9001", "comment/9002", "comment/9005",
		"task/4201/done/2026-03-02T16:30:00Z", "task/4201/state/2026-03-06T12:00:00Z",
	}
	if got := sourceIDs(events); !slices.Equal(got, wantIDs) {
		t.Fatalf("source ids = %v, want %v", got, wantIDs)
	}
	wantKinds := []Kind{KindTaskCreated, KindComment, KindRunAnnounced, KindComment, KindTaskDone, KindStateObserved}
	if got := kinds(events); !slices.Equal(got, wantKinds) {
		t.Errorf("kinds = %v, want %v", got, wantKinds)
	}
	for _, e := range events {
		wantObserved := testObservedAt
		if e.CommentID != 0 {
			wantObserved = testCommentsObservedAt
		}
		if e.Instance != snap.Instance || e.TaskID != 4201 || !e.ObservedAt.Equal(wantObserved) {
			t.Errorf("%s: instance %q, task %d, observed %v", e.SourceID, e.Instance, e.TaskID, e.ObservedAt)
		}
	}

	created := events[0]
	if created.Title != "Add a fabricated widget" || !reflect.DeepEqual(created.Actor, &fixtureOwner) ||
		!created.SourceTime.Equal(at(time.March, 2, 9, 0, 0)) {
		t.Errorf("created = %+v", created)
	}

	announced := events[2]
	wantRun := Announcement{
		Role: RoleImplementation, Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
		TaskName: "vikunja-demo-agent-opus-4201", Pod: "vikunja-demo-agent-opus-4201-x7k2p",
	}
	if announced.Run == nil || *announced.Run != wantRun {
		t.Errorf("run = %+v, want %+v", announced.Run, wantRun)
	}
	if announced.Evidence.Path != "/api/v1/tasks/4201/comments/9002" {
		t.Errorf("announcement evidence = %+v", announced.Evidence)
	}
	if announced.CommentID != 9002 || !reflect.DeepEqual(announced.Actor, &fixtureKelos) || len(announced.Flags) != 0 {
		t.Errorf("announcement = %+v", announced)
	}

	edited := events[3]
	if !slices.Equal(edited.Flags, []Flag{FlagEdited}) || !reflect.DeepEqual(edited.Actor, &fixtureAgent) {
		t.Errorf("edited comment = %+v", edited)
	}

	done := events[4]
	if !done.SourceTime.Equal(at(time.March, 2, 11, 30, 0)) || done.Actor != nil || len(done.Flags) != 0 {
		t.Errorf("done = %+v", done)
	}

	wantState := State{
		Done:      true,
		Buckets:   []Bucket{{ID: 14, Title: "Done", ProjectViewID: 4}},
		Labels:    []string{"backend"},
		Assignees: []User{fixtureAgent},
	}
	assertState(t, events[5], wantState)
}

// assertState checks a state snapshot: its contents, and that it claims no
// source time and no actor, since Vikunja records neither.
func assertState(t *testing.T, e Event, want State) {
	t.Helper()
	if e.Kind != KindStateObserved || e.State == nil {
		t.Fatalf("not a state snapshot: %+v", e)
	}
	if !reflect.DeepEqual(*e.State, want) {
		t.Errorf("state = %+v, want %+v", *e.State, want)
	}
	if !e.SourceTime.IsZero() || e.Actor != nil {
		t.Errorf("state claims source time %v, actor %v", e.SourceTime, e.Actor)
	}
}

func TestNormalizeOneFullPage(t *testing.T) {
	snap, _ := fetchStory(t, "one-full-page", 4202)
	events := Normalize(snap, serviceAccount)
	want := []Kind{KindTaskCreated, KindRunAnnounced, KindComment, KindStateObserved}
	if got := kinds(events); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	assertState(t, events[3], State{
		Buckets:   []Bucket{{ID: 12, Title: "Doing", ProjectViewID: 4}},
		Labels:    []string{"in progress"},
		Assignees: []User{fixtureAgent},
	})
}

func TestNormalizeNoComments(t *testing.T) {
	snap, _ := fetchStory(t, "no-comments", 4203)
	events := Normalize(snap, serviceAccount)
	want := []Kind{KindTaskCreated, KindStateObserved}
	if got := kinds(events); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	assertState(t, events[1], State{})
}

func TestNormalizeUnparseableAnnouncement(t *testing.T) {
	snap, _ := fetchStory(t, "unparseable-announcement", 4204)
	events := Normalize(snap, serviceAccount)
	want := []Kind{KindTaskCreated, KindComment, KindStateObserved}
	if got := kinds(events); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	c := events[1]
	if c.Run != nil || c.CommentID != 9020 || !slices.Equal(c.Flags, []Flag{FlagUnparsedAnnouncement}) {
		t.Errorf("comment = %+v", c)
	}
}

// announcementComment is a well-formed announcement posted by author.
func announcementComment(id int64, author User, created time.Time) Comment {
	return Comment{
		ID: id, Author: author, Created: created, Updated: created,
		Body: "<p>The agent has started working on this task: Kelos Task <code>vikunja-demo-4205</code>.</p>",
	}
}

// Only the service account's comments announce runs: anyone else quoting
// one, or a client configured without a service account, gets a comment.
func TestNormalizeAnnouncementNeedsServiceAccount(t *testing.T) {
	created := at(time.March, 7, 9, 0, 0)
	snap := Snapshot{
		Task: Task{ID: 4205, Created: created},
		Comments: []Comment{
			announcementComment(9030, fixtureOwner, created.Add(time.Minute)),
			announcementComment(9031, fixtureKelos, created.Add(2*time.Minute)),
		},
		ObservedAt: testObservedAt,
	}
	got := kinds(Normalize(snap, serviceAccount))
	if want := []Kind{KindTaskCreated, KindComment, KindRunAnnounced, KindStateObserved}; !slices.Equal(got, want) {
		t.Errorf("with service account: kinds = %v, want %v", got, want)
	}
	got = kinds(Normalize(snap, ""))
	if want := []Kind{KindTaskCreated, KindComment, KindComment, KindStateObserved}; !slices.Equal(got, want) {
		t.Errorf("without service account: kinds = %v, want %v", got, want)
	}
}

// A done task without done_at keeps its done event, with an unknown time,
// flagged, after every event whose time is known.
func TestNormalizeDoneWithoutTime(t *testing.T) {
	created := at(time.March, 8, 9, 0, 0)
	snap := Snapshot{
		Task:       Task{ID: 4206, Created: created, Done: true, BucketID: 31},
		Comments:   []Comment{{ID: 9040, Author: fixtureOwner, Created: created.Add(time.Hour)}},
		ObservedAt: testObservedAt,
	}
	events := Normalize(snap, serviceAccount)
	want := []string{"task/4206/created", "comment/9040", "task/4206/done", "task/4206/state/2026-03-06T12:00:00Z"}
	if got := sourceIDs(events); !slices.Equal(got, want) {
		t.Fatalf("source ids = %v, want %v", got, want)
	}
	done := events[2]
	if !done.SourceTime.IsZero() || !slices.Equal(done.Flags, []Flag{FlagDoneTimeUnknown}) {
		t.Errorf("done = %+v", done)
	}
	assertState(t, events[3], State{Done: true, Buckets: []Bucket{{ID: 31}}})
}

// Reading the same task again changes only the state snapshot's id.
func TestNormalizeSourceIDsStableAcrossReads(t *testing.T) {
	snap, _ := fetchStory(t, "two-pages", 4201)
	first := sourceIDs(Normalize(snap, serviceAccount))
	snap.ObservedAt = snap.ObservedAt.Add(time.Hour)
	second := sourceIDs(Normalize(snap, serviceAccount))
	last := len(first) - 1
	if !slices.Equal(first[:last], second[:last]) {
		t.Errorf("ids changed between reads: %v, then %v", first, second)
	}
	if first[last] == second[last] {
		t.Errorf("two observations share the id %q", first[last])
	}
}
