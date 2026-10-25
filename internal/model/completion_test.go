package model

import "testing"

// testDone is the task's completion at minute done, read at minute read.
func testDone(id string, done, read int) Event {
	e := testEvent(id)
	e.Kind, e.SourceTime, e.ObservedAt = KindTaskDone, timePtr(at(done)), at(read)
	e.Actor = Account{Kind: ActorUnknown}
	return e
}

// testSeen is a read at minute first, and again at minute last when it is
// later, that saw the task done or not.
func testSeen(id string, done bool, first, last int) Event {
	e := testEvent(id)
	e.Kind, e.SourceTime, e.ObservedAt = KindTaskCompletionObserved, nil, at(first)
	e.Actor = Account{Kind: ActorUnknown}
	e.Details = map[string]string{DetailDone: DoneFalse}
	if done {
		e.Details[DetailDone] = DoneTrue
	}
	if last > first {
		e.LastObservedAt = timePtr(at(last))
	}
	return e
}

func completionStory(t *testing.T, events ...Event) Story {
	t.Helper()
	s, err := BuildStory(testRoot(), Records{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCompletionAt(t *testing.T) {
	done := testDone("done", 10, 12)
	tests := []struct {
		name   string
		events []Event
		at     int
		want   CompletionState
		after  int
		by     int
	}{
		{"done, read once", []Event{done, testSeen("o1", true, 12, 0)}, 60, CompletionDone, 0, 0},
		{"done and still done", []Event{done, testSeen("o1", true, 12, 50)}, 60, CompletionDone, 0, 0},
		{"reopened", []Event{
			testSeen("o0", false, 2, 8), done, testSeen("o1", true, 12, 20), testSeen("o2", false, 30, 50),
		}, 60, CompletionReopened, 20, 30},
		{"reopened after the instant", []Event{
			done, testSeen("o1", true, 12, 20), testSeen("o2", false, 30, 0),
		}, 30, CompletionDone, 0, 0},
		{"seen not done only before the completion", []Event{
			testSeen("o0", false, 2, 8), done,
		}, 60, CompletionDone, 0, 0},
		{"reopened, then done again", []Event{
			done, testSeen("o1", true, 12, 0), testSeen("o2", false, 30, 0), testDone("again", 40, 42),
			testSeen("o3", true, 42, 0),
		}, 60, CompletionDone, 0, 0},
		{"reopened twice: the first read after the second completion", []Event{
			done, testSeen("o2", false, 30, 0), testDone("again", 40, 42),
			testSeen("o3", true, 42, 45), testSeen("o4", false, 50, 55),
		}, 60, CompletionReopened, 45, 50},
		{"reopened, the completion's read the last seen done", []Event{
			done, testSeen("o2", false, 30, 0),
		}, 60, CompletionReopened, 12, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := completionStory(t, tt.events...).CompletionAt(at(tt.at))
			if !ok {
				t.Fatal("no completion")
			}
			if c.State != tt.want {
				t.Fatalf("state = %s, want %s", c.State, tt.want)
			}
			if tt.want == CompletionDone {
				if c.Reopened != nil {
					t.Errorf("reopened = %+v on a task still done", c.Reopened)
				}
				return
			}
			if c.Reopened == nil || !c.Reopened.After.Equal(at(tt.after)) || !c.Reopened.By.Equal(at(tt.by)) {
				t.Errorf("reopened = %+v, want after minute %d, by minute %d", c.Reopened, tt.after, tt.by)
			}
		})
	}
}

func TestCompletionAtNamesItsRecords(t *testing.T) {
	done, open := testDone("done", 10, 12), testSeen("o2", false, 30, 0)
	c, _ := completionStory(t, done, open).CompletionAt(at(60))
	if c.Done != done.Key() || !c.DoneAt.Equal(at(10)) || c.Reopened == nil || c.Reopened.Observation != open.Key() {
		t.Errorf("completion = %+v, want %s at minute 10, reopened by %s", c, done.Key(), open.Key())
	}
}

func TestCompletionAtWithoutCompletion(t *testing.T) {
	tests := []struct {
		name  string
		story Story
	}{
		{"never done", completionStory(t, testSeen("o1", false, 2, 50))},
		{"done after the instant", completionStory(t, testDone("done", 70, 72))},
		{"done at the instant", completionStory(t, testDone("done", 60, 62))},
		{"a page's story", Story{Root: Root{Page: &PageKey{Instance: testWikiHost, ID: testPageID, Locale: testLocale}},
			Records: Records{Events: []Event{testDone("done", 10, 12)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if c, ok := tt.story.CompletionAt(at(60)); ok {
				t.Errorf("completion = %+v, want none", c)
			}
		})
	}
}

func TestCompletionAtOtherTask(t *testing.T) {
	other := TaskKey{Instance: testVikunjaHost, ID: testTaskID + 1}
	open := testSeen("o2", false, 30, 0)
	open.Related.Task = &other
	c, _ := completionStory(t, testDone("done", 10, 12), open).CompletionAt(at(60))
	if c.State != CompletionDone {
		t.Errorf("state = %s: another task's read reopened this one", c.State)
	}
}

func TestEventValidateCompletion(t *testing.T) {
	for _, value := range []string{"", "yes", "1"} {
		e := testSeen("o1", true, 12, 0)
		e.Details[DetailDone] = value
		checkErr(t, e.Validate(), `"done" detail`)
	}
	checkErr(t, testSeen("o1", false, 12, 0).Validate(), "")
}
