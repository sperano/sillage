package vikunja

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelostext"
)

var update = flag.Bool("update", false, "rewrite the Vikunja fixtures' golden files")

const (
	serviceAccount = "kelos"
	goldenFile     = "records.golden.json"
	goldenPerm     = 0o644
)

var (
	fixtureOwner = User{ID: 7, Username: "owner", Name: "Robin Owner"}
	fixtureAgent = User{ID: 21, Username: "agent-opus", Name: "Agent Opus"}
	fixtureKelos = User{ID: 23, Username: "kelos", Name: "Kelos"}
	fixtureZone  = time.FixedZone("", -5*60*60)
)

func at(month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(2026, month, day, hour, minute, second, 0, fixtureZone)
}

func storyRecords(t *testing.T, story string, taskID int64) model.Records {
	t.Helper()
	snapshot, _ := fetchStory(t, story, taskID)
	records, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return records
}

func fixtureStories(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	var stories []string
	for _, entry := range entries {
		if entry.IsDir() {
			stories = append(stories, entry.Name())
		}
	}
	return stories
}

func fixtureTaskID(story string) int64 {
	switch story {
	case "two-pages":
		return 4201
	case "one-full-page":
		return 4202
	case "no-comments":
		return 4203
	case "unparseable-announcement":
		return 4204
	case "gordon-voice":
		return 4220
	default:
		return 0
	}
}

func TestNormalizeGolden(t *testing.T) {
	for _, story := range fixtureStories(t) {
		t.Run(story, func(t *testing.T) {
			records := storyRecords(t, story, fixtureTaskID(story))
			got, err := json.MarshalIndent(map[string][]model.Event{"events": records.Events}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(fixtureDir, story, goldenFile)
			if *update {
				if err := os.WriteFile(path, got, goldenPerm); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the normalized records; run with -update and review the diff", path)
			}
		})
	}
}

func TestNormalizedRecordsBuildAStory(t *testing.T) {
	for _, story := range fixtureStories(t) {
		taskID := fixtureTaskID(story)
		root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: taskID}}
		if _, err := model.BuildStory(root, storyRecords(t, story, taskID)); err != nil {
			t.Errorf("%s: %v", story, err)
		}
	}
}

func kinds(events []model.Event) []model.EventKind {
	out := make([]model.EventKind, len(events))
	for i, event := range events {
		out[i] = event.Kind
	}
	return out
}

func eventByKind(t *testing.T, events []model.Event, kind model.EventKind) model.Event {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind {
			return event
		}
	}
	t.Fatalf("no event of kind %s", kind)
	return model.Event{}
}

func TestNormalizeTwoPages(t *testing.T) {
	events := storyRecords(t, "two-pages", 4201).Events
	wantKinds := []model.EventKind{
		model.KindTaskCreated,
		model.KindComment,
		model.KindRunAnnounced,
		model.KindComment,
		model.KindTaskDone,
		model.KindTaskBucketObserved,
		model.KindTaskLabelObserved,
		model.KindTaskAssigneeObserved,
	}
	if got := kinds(events); !slices.Equal(got, wantKinds) {
		t.Fatalf("kinds = %v, want %v", got, wantKinds)
	}
	created := events[0]
	if created.ID != "tasks/4201" ||
		created.Actor != (model.Account{Login: fixtureOwner.Username, Kind: model.ActorPerson}) ||
		created.SourceTime == nil || !created.SourceTime.Equal(at(time.March, 2, 9, 0, 0)) {
		t.Errorf("created = %+v", created)
	}
	announced := eventByKind(t, events, model.KindRunAnnounced)
	if announced.ID != "comments/9002" || announced.Actor.Kind != model.ActorService ||
		announced.Details["taskName"] != "vikunja-demo-agent-opus-4201" ||
		announced.Details["pod"] != "vikunja-demo-agent-opus-4201-x7k2p" ||
		announced.Executor == nil || announced.Executor.Name != "agent-opus" {
		t.Errorf("announcement = %+v", announced)
	}
	edited := events[3]
	if !strings.Contains(edited.Evidence.Note, "keeps no prior body") {
		t.Errorf("edited comment evidence = %q", edited.Evidence.Note)
	}
	done := eventByKind(t, events, model.KindTaskDone)
	if done.IDKind != model.IDFingerprint || done.SourceTime == nil ||
		!done.SourceTime.Equal(at(time.March, 2, 11, 30, 0)) {
		t.Errorf("done = %+v", done)
	}
	assertObservation(t, events[5], "bucket", "Done")
	assertObservation(t, events[6], "label", "backend")
	assertObservation(t, events[7], "assignee", fixtureAgent.Username)
}

func assertObservation(t *testing.T, event model.Event, detail, value string) {
	t.Helper()
	if event.SourceTime != nil || event.Actor.Kind != model.ActorUnknown ||
		event.IDKind != model.IDFingerprint || event.Details[detail] != value {
		t.Errorf("%s observation = %+v", detail, event)
	}
}

func TestNormalizeNoComments(t *testing.T) {
	events := storyRecords(t, "no-comments", 4203).Events
	if got, want := kinds(events), []model.EventKind{model.KindTaskCreated}; !slices.Equal(got, want) {
		t.Errorf("kinds = %v, want %v", got, want)
	}
}

func TestNormalizeUnparseableAnnouncement(t *testing.T) {
	events := storyRecords(t, "unparseable-announcement", 4204).Events
	comment := events[1]
	if comment.Kind != model.KindComment ||
		!strings.Contains(comment.Evidence.Note, "Kelos Task could not be parsed") {
		t.Errorf("comment = %+v", comment)
	}
}

func announcementComment(id int64, author User, created time.Time) Comment {
	return Comment{
		ID: id, Author: author, Created: created, Updated: created,
		Body: "<p>The agent has started working on this task: Kelos Task <code>vikunja-demo-4205</code>.</p>",
	}
}

func TestNormalizeAnnouncementNeedsServiceAccount(t *testing.T) {
	created := at(time.March, 7, 9, 0, 0)
	snapshot := Snapshot{
		Instance: "vikunja.example.com",
		Task:     Task{ID: 4205, Created: created},
		Comments: []Comment{
			announcementComment(9030, fixtureOwner, created.Add(time.Minute)),
			announcementComment(9031, fixtureKelos, created.Add(2*time.Minute)),
		},
		ObservedAt: testObservedAt,
	}
	records, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.EventKind{model.KindTaskCreated, model.KindComment, model.KindRunAnnounced}
	if got := kinds(records.Events); !slices.Equal(got, want) {
		t.Errorf("with service account: kinds = %v, want %v", got, want)
	}
	records, err = Normalize(snapshot, "")
	if err != nil {
		t.Fatal(err)
	}
	want = []model.EventKind{model.KindTaskCreated, model.KindComment, model.KindComment}
	if got := kinds(records.Events); !slices.Equal(got, want) {
		t.Errorf("without service account: kinds = %v, want %v", got, want)
	}
}

func TestNormalizeRequests(t *testing.T) {
	created := at(time.March, 7, 9, 0, 0)
	tests := []struct {
		body    string
		command string
		agent   string
		kind    model.EventKind
	}{
		{"/gordon @agent-sol, please retry", kelostext.CommandGordon, "agent-sol", model.KindRequest},
		{"  /kelos retry", kelostext.CommandKelos, "", model.KindRequest},
		{"/gordonian retry", "", "", model.KindComment},
		{"please /gordon retry", "", "", model.KindComment},
	}
	for _, test := range tests {
		snapshot := Snapshot{
			Instance: "vikunja.example.com",
			Task:     Task{ID: 4205, Created: created},
			Comments: []Comment{{
				ID: 1, Body: "<p>" + test.body + "</p>", Author: fixtureOwner, Created: created,
			}},
			ObservedAt: testObservedAt,
		}
		records, err := Normalize(snapshot, serviceAccount)
		if err != nil {
			t.Fatal(err)
		}
		event := records.Events[1]
		if event.Kind != test.kind ||
			event.Details["command"] != test.command ||
			event.Details["agent"] != test.agent {
			t.Errorf("%q = %+v", test.body, event)
		}
	}
}

func TestNormalizeCommentsObservedFallback(t *testing.T) {
	created := at(time.March, 9, 9, 0, 0)
	snapshot := Snapshot{
		Instance:   "vikunja.example.com",
		Task:       Task{ID: 4207, Created: created},
		Comments:   []Comment{{ID: 9050, Author: fixtureOwner, Created: created.Add(time.Minute)}},
		ObservedAt: testObservedAt,
	}
	records, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatal(err)
	}
	if got := records.Events[1].ObservedAt; !got.Equal(testObservedAt) {
		t.Errorf("comment observed at %v, want %v", got, testObservedAt)
	}
}

func TestNormalizeDoneWithoutTime(t *testing.T) {
	created := at(time.March, 8, 9, 0, 0)
	snapshot := Snapshot{
		Instance:   "vikunja.example.com",
		Task:       Task{ID: 4206, Created: created, Done: true, BucketID: 31},
		ObservedAt: testObservedAt,
	}
	records, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatal(err)
	}
	done := eventByKind(t, records.Events, model.KindTaskDone)
	if done.SourceTime != nil || !strings.Contains(done.Evidence.Note, "source time is unknown") {
		t.Errorf("done = %+v", done)
	}
	assertObservation(t, records.Events[len(records.Events)-1], "bucket", "31")
}

func TestNormalizeObservationIDsChangeAcrossReads(t *testing.T) {
	snapshot, _ := fetchStory(t, "two-pages", 4201)
	first, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Hour)
	second, err := Normalize(snapshot, serviceAccount)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range first.Events {
		if event.SourceTime != nil && event.ID != second.Events[i].ID {
			t.Errorf("source record id changed from %q to %q", event.ID, second.Events[i].ID)
		}
		if event.SourceTime == nil && event.ID == second.Events[i].ID {
			t.Errorf("observation id did not change from %q", event.ID)
		}
	}
}

func TestNormalizeRejectsInvalidTask(t *testing.T) {
	_, err := Normalize(Snapshot{Task: Task{ID: 4201}, ObservedAt: testObservedAt}, serviceAccount)
	if err == nil || !strings.Contains(err.Error(), "source, instance and id") {
		t.Errorf("error = %v, want invalid source instance", err)
	}
}
