package github

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

var testCreated = time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

var (
	testOwner = &User{Login: "owner-gh", Type: "User"}
	testOther = &User{Login: "reviewer-gh", Type: "User"}
)

// testSnapshot is a minimal open pull request with the given timeline.
func testSnapshot(timeline ...TimelineEvent) Snapshot {
	return Snapshot{
		Instance:   testInstance,
		Repository: testRepo,
		PullRequest: PullRequest{
			ID: 7001, Number: 1, User: testOwner, CreatedAt: testCreated,
			Head: Ref{Ref: "kelos/vikunja-1", SHA: strings.Repeat("c", 40)},
		},
		Timeline:   timeline,
		ObservedAt: testObservedAt,
	}
}

func labeledEvent(id int64, actor *User, after time.Duration, name string) TimelineEvent {
	return TimelineEvent{ID: id, Event: timelineLabeled, Actor: actor, CreatedAt: testCreated.Add(after), Label: &Label{Name: name}}
}

func TestExecutorOf(t *testing.T) {
	tests := []struct {
		name     string
		timeline []TimelineEvent
		want     string
	}{
		{"label at creation", []TimelineEvent{labeledEvent(1, testOwner, time.Second, "kelos:agent-a")}, "agent-a"},
		{"at the window's end", []TimelineEvent{labeledEvent(1, testOwner, executorLabelWindow, "kelos:agent-a")}, "agent-a"},
		{"same label twice", []TimelineEvent{
			labeledEvent(1, testOwner, time.Second, "kelos:agent-a"),
			labeledEvent(2, testOwner, 2*time.Second, "kelos:agent-a"),
		}, "agent-a"},
		{"too late", []TimelineEvent{labeledEvent(1, testOwner, executorLabelWindow+time.Second, "kelos:agent-a")}, ""},
		{"before creation", []TimelineEvent{labeledEvent(1, testOwner, -time.Second, "kelos:agent-a")}, ""},
		{"added by another account", []TimelineEvent{labeledEvent(1, testOther, time.Second, "kelos:agent-a")}, ""},
		{"two agents", []TimelineEvent{
			labeledEvent(1, testOwner, time.Second, "kelos:agent-a"),
			labeledEvent(2, testOwner, time.Second, "kelos:agent-b"),
		}, ""},
		{"other label", []TimelineEvent{labeledEvent(1, testOwner, time.Second, "bug")}, ""},
		{"empty agent", []TimelineEvent{labeledEvent(1, testOwner, time.Second, "kelos:")}, ""},
		{"no label", []TimelineEvent{{ID: 1, Event: timelineLabeled, Actor: testOwner, CreatedAt: testCreated}}, ""},
	}
	for _, tt := range tests {
		got, ok := executorOf(testSnapshot().PullRequest, tt.timeline)
		if got.agent != tt.want || ok != (tt.want != "") {
			t.Errorf("%s: executorOf = %q, %v; want %q", tt.name, got.agent, ok, tt.want)
		}
	}
	pr := testSnapshot().PullRequest
	pr.User = nil
	if _, ok := executorOf(pr, []TimelineEvent{labeledEvent(1, testOwner, time.Second, "kelos:agent-a")}); ok {
		t.Error("executorOf named an executor for a pull request without an author")
	}
}

// A merge GitHub reports only on the pull request gets a fingerprinted
// event, and the close at the merge is still dropped.
func TestMergedFromPR(t *testing.T) {
	merged := testCreated.Add(time.Hour)
	s := testSnapshot(TimelineEvent{ID: 9, Event: timelineClosed, Actor: testOwner, CreatedAt: merged})
	s.PullRequest.MergedAt = &merged
	recs, err := Normalize(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventsOf(recs, 1); len(got) != 2 || got[1] != model.KindPRMerged {
		t.Fatalf("events = %v, want opened then merged", got)
	}
	e := recs.Events[1]
	if e.IDKind != model.IDFingerprint || e.Actor.Kind != model.ActorUnknown || !e.SourceTime.Equal(merged) {
		t.Errorf("merged event = %+v, want a fingerprint, an unknown actor and the merge time", e)
	}
}

// A close before the merge (closed, reopened, merged) stays a close.
func TestCloseBeforeMergeKept(t *testing.T) {
	merged := testCreated.Add(time.Hour)
	s := testSnapshot(
		TimelineEvent{ID: 1, Event: timelineClosed, Actor: testOwner, CreatedAt: testCreated.Add(time.Minute)},
		TimelineEvent{ID: 2, Event: timelineReopened, Actor: testOwner, CreatedAt: testCreated.Add(2 * time.Minute)},
		TimelineEvent{ID: 3, Event: timelineMerged, Actor: testOwner, CreatedAt: merged},
		TimelineEvent{ID: 4, Event: timelineClosed, Actor: testOwner, CreatedAt: merged},
	)
	s.PullRequest.MergedAt = &merged
	recs, err := Normalize(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.EventKind{model.KindPROpened, model.KindPRClosed, model.KindPRReopened, model.KindPRMerged}
	if got := eventsOf(recs, 1); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestTimelineEventWithoutID(t *testing.T) {
	_, err := Normalize(testSnapshot(TimelineEvent{Event: timelineClosed, Actor: testOwner, CreatedAt: testCreated}), nil)
	if err == nil || !strings.Contains(err.Error(), "without an id") {
		t.Errorf("Normalize error = %v, want a missing id", err)
	}
}

// Timeline events sillage does not read are skipped, even without an id.
func TestTimelineIgnoresOtherEvents(t *testing.T) {
	recs, err := Normalize(testSnapshot(
		TimelineEvent{Event: timelineCommitted, SHA: "c"},
		TimelineEvent{ID: 5, Event: "head_ref_deleted", Actor: testOwner, CreatedAt: testCreated},
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventsOf(recs, 1); len(got) != 1 {
		t.Errorf("events = %v, want only the opening", got)
	}
}

// A request for a deleted account's review names nobody.
func TestReviewRequestForDeletedReviewer(t *testing.T) {
	recs, err := Normalize(testSnapshot(TimelineEvent{
		ID: 6, Event: timelineReviewRequested, Actor: testOwner, CreatedAt: testCreated, RequestedReviewer: &User{},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := recs.Events[1].Details; d != nil {
		t.Errorf("details = %v, want none", d)
	}
}
