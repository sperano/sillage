package overview

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

var testPR = model.PRKey{Instance: "github.example.com", Repository: "example-org/widgets", Number: 9}

// prEvent is a GitHub event of testPR at minute min past 14:00 on the
// fixture afternoon.
func prEvent(kind model.EventKind, min int, actor string, details map[string]string) model.Event {
	at := afternoon.From.Add(time.Duration(min) * time.Minute)
	return model.Event{
		Source: model.SourceGitHub, Instance: testPR.Instance, ID: string(kind) + "/" + at.Format("1504"),
		IDKind: model.IDSource, Kind: kind, SourceTime: &at, ObservedAt: at,
		Actor: model.Account{Login: actor, Kind: model.ActorPerson}, Related: model.Related{PR: &testPR},
		Details:  details,
		Evidence: model.Evidence{Source: model.SourceGitHub, Instance: testPR.Instance, Ref: "GET /timeline"},
	}
}

func minute(min int) time.Time { return afternoon.From.Add(time.Duration(min) * time.Minute) }

func TestPeriodsCloseReopenMerge(t *testing.T) {
	events := []model.Event{
		prEvent(model.KindPROpened, 5, "owner-gh", nil),
		prEvent(model.KindPRClosed, 10, "owner-gh", nil),
		prEvent(model.KindPRReopened, 20, "owner-gh", nil),
		prEvent(model.KindPRMerged, 30, "owner-gh", nil),
	}
	got := periods(events)
	want := []model.Interval{
		{Start: model.KnownAt(minute(5)), End: model.KnownAt(minute(10))},
		{Start: model.KnownAt(minute(20)), End: model.KnownAt(minute(30))},
	}
	if len(got) != len(want) {
		t.Fatalf("periods = %+v, want %+v", got, want)
	}
	for i := range want {
		if !sameInterval(got[i], want[i]) {
			t.Errorf("period %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, c := range []struct {
		to   int
		want PRState
	}{{1, PRUnknown}, {6, PROpen}, {15, PRClosed}, {25, PROpen}, {31, PRMerged}} {
		if got := stateAt(Window{From: afternoon.From, To: minute(c.to)}, events); got != c.want {
			t.Errorf("state before 14:%02d = %s, want %s", c.to, got, c.want)
		}
	}
}

// A merge with no recorded opening ends a period whose start is unknown;
// an opening with no end is open.
func TestPeriodsUnknownStartAndOpenEnd(t *testing.T) {
	merged := periods([]model.Event{prEvent(model.KindPRMerged, 30, "owner-gh", nil)})
	if len(merged) != 1 || merged[0].Start.State != model.BoundUnknown || merged[0].End.State != model.BoundKnown {
		t.Errorf("merge only: %+v, want one period, start unknown", merged)
	}
	open := periods([]model.Event{prEvent(model.KindPROpened, 5, "owner-gh", nil)})
	if len(open) != 1 || open[0].End.State != model.BoundOpen {
		t.Errorf("opening only: %+v, want one open period", open)
	}
}

func TestReviewWaitEnds(t *testing.T) {
	reviewer := map[string]string{"reviewer": "reviewer-gh"}
	team := map[string]string{"team": "core"}
	request := prEvent(model.KindReviewRequested, 5, "owner-gh", reviewer)
	const unclosed = -1
	for _, c := range []struct {
		name  string
		later []model.Event
		want  model.BoundState
		// closed is the minute past 14:00 the wait's Closed names, or unclosed.
		closed int
	}{
		{"the reviewer's review", []model.Event{prEvent(model.KindReviewSubmitted, 20, "reviewer-gh", nil)}, model.BoundKnown, unclosed},
		{"another account's review", []model.Event{prEvent(model.KindReviewSubmitted, 20, "other-gh", nil)}, model.BoundOpen, unclosed},
		{"the request's removal", []model.Event{prEvent(model.KindReviewRequestRemoved, 20, "owner-gh", reviewer)}, model.BoundKnown, unclosed},
		{"merged unanswered", []model.Event{prEvent(model.KindPRMerged, 20, "owner-gh", nil)}, model.BoundUnknown, 20},
		{"closed unanswered", []model.Event{prEvent(model.KindPRClosed, 20, "owner-gh", nil)}, model.BoundUnknown, 20},
		{"closed, then reopened", []model.Event{
			prEvent(model.KindPRClosed, 20, "owner-gh", nil),
			prEvent(model.KindPRReopened, 25, "owner-gh", nil),
		}, model.BoundOpen, unclosed},
		{"closed, reopened, closed again", []model.Event{
			prEvent(model.KindPRClosed, 20, "owner-gh", nil),
			prEvent(model.KindPRReopened, 25, "owner-gh", nil),
			prEvent(model.KindPRClosed, 40, "owner-gh", nil),
		}, model.BoundUnknown, 40},
		{"answered, then merged", []model.Event{
			prEvent(model.KindReviewSubmitted, 20, "reviewer-gh", nil),
			prEvent(model.KindPRMerged, 30, "owner-gh", nil),
		}, model.BoundKnown, unclosed},
	} {
		events := append([]model.Event{request}, c.later...)
		waits := reviewWaits(afternoon, testPR, events)
		if len(waits) != 1 || waits[0].Interval.End.State != c.want {
			t.Errorf("%s: waits = %+v, want one ending %s", c.name, waits, c.want)
			continue
		}
		switch closed := waits[0].Closed; {
		case c.closed == unclosed && closed != nil:
			t.Errorf("%s: closed at %s, want unset", c.name, closed)
		case c.closed != unclosed && (closed == nil || !closed.Equal(minute(c.closed))):
			t.Errorf("%s: closed at %v, want %s", c.name, closed, minute(c.closed))
		}
	}
	teamRequest := prEvent(model.KindReviewRequested, 5, "owner-gh", team)
	events := []model.Event{teamRequest, prEvent(model.KindReviewSubmitted, 20, "core-member", nil)}
	if waits := reviewWaits(afternoon, testPR, events); waits[0].Team != "core" || waits[0].Interval.End.State != model.BoundOpen {
		t.Errorf("a team's request answered by a review: %+v, want team core, open", waits)
	}
}

func sameInterval(a, b model.Interval) bool {
	return sameBound(a.Start, b.Start) && sameBound(a.End, b.End)
}

func sameBound(a, b model.Bound) bool {
	ta, oka := a.Time()
	tb, okb := b.Time()
	return a.State == b.State && oka == okb && ta.Equal(tb)
}
