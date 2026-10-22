package wiki

import (
	"bytes"
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// update rewrites every records.golden.json from its recorded replies:
// go test ./internal/source/wiki -run TestNormalizeGolden -update
var update = flag.Bool("update", false, "rewrite the wiki fixtures' golden files")

const (
	goldenFile = "records.golden.json"
	// goldenPerm is the mode of a rewritten golden file.
	goldenPerm = 0o644
)

// storyEvents reads a story as the collector would and normalizes it.
func storyEvents(t *testing.T, story string) []model.Event {
	t.Helper()
	snap, _ := fetchStory(t, story)
	recs, err := Normalize(snap, testAgents)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return recs.Events
}

func TestNormalizeGolden(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		story := entry.Name()
		t.Run(story, func(t *testing.T) {
			got, err := json.MarshalIndent(storyEvents(t, story), "", "  ")
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
				t.Errorf("events differ from %s; rerun with -update and review the diff", path)
			}
		})
	}
}

// summary is what the tests below check of an event: kind, id, actor.
type summary struct {
	Kind  model.EventKind
	ID    string
	Actor string
}

func summarize(events []model.Event) []summary {
	out := make([]summary, len(events))
	for i, e := range events {
		out[i] = summary{e.Kind, e.ID, e.Actor.Login + "/" + string(e.Actor.Kind)}
	}
	return out
}

func liveFingerprint(t *testing.T, at, action, pageID string) string {
	t.Helper()
	when, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	return model.Fingerprint(when, "pages/"+pageID, action, "")
}

// The newest save is only in the live page: it must be an edit, after the
// trail's, with the live page's author and a fingerprint id.
func TestNormalizeAddsLiveEdit(t *testing.T) {
	events := storyEvents(t, "live-edit-not-in-trail")
	live := liveFingerprint(t, "2026-03-04T15:16:45.900Z", actionEdited, "57")
	want := []summary{
		{model.KindPageCreated, "pages/57", "Robin Owner/person"},
		{model.KindRequest, "comments/880", "Robin Owner/person"},
		{model.KindRunAnnounced, "comments/881", "Agent Opus/agent"},
		{model.KindPageEdited, "pages/57/versions/121", "Agent Opus/agent"},
		{model.KindPageEdited, live, "Agent Opus/agent"},
		{model.KindComment, "comments/882", "Agent Opus/agent"},
	}
	if got := summarize(events); !slices.Equal(got, want) {
		t.Fatalf("events =\n%v\nwant\n%v", got, want)
	}
	if e := events[4]; e.IDKind != model.IDFingerprint || e.Details["version"] != versionLive ||
		e.Evidence.Ref != "pages.single(id: 57)" {
		t.Errorf("live edit = %+v", e)
	}
	// The trail's first version is the creation's render, not an edit.
	if created := events[0]; !created.SourceTime.Equal(time.Date(2026, 3, 4, 10, 0, 0, 120e6, time.UTC)) {
		t.Errorf("created at %v", created.SourceTime)
	}
}

// Two agents edit one page, interleaved, and the owner moves it: each edit
// keeps its own author, and the move claims no actor.
func TestNormalizeTwoAgents(t *testing.T) {
	events := storyEvents(t, "two-agents")
	var got []summary
	for _, s := range summarize(events) {
		if s.Kind == model.KindPageEdited || s.Kind == model.KindPageMoved {
			got = append(got, s)
		}
	}
	want := []summary{
		{model.KindPageEdited, "pages/58/versions/201", "Agent Sol/agent"},
		{model.KindPageEdited, "pages/58/versions/202", "Agent Opus/agent"},
		{model.KindPageEdited, "pages/58/versions/203", "Agent Sol/agent"},
		{model.KindPageMoved, "pages/58/versions/204", "/unknown"},
		{model.KindPageEdited, "pages/58/versions/205", "Agent Opus/agent"},
		{model.KindPageEdited, liveFingerprint(t, "2026-03-05T11:44:00.700Z", actionEdited, "58"), "Agent Opus/agent"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("page events =\n%v\nwant\n%v", got, want)
	}
	executors := map[string]string{}
	for _, e := range events {
		if e.Kind == model.KindRunAnnounced && e.Executor != nil {
			executors[e.ID] = e.Executor.Name + " " + e.Details["requestComment"]
		}
	}
	if want := map[string]string{"comments/901": "agent-sol 900", "comments/903": "agent-opus 902"}; !maps.Equal(executors, want) {
		t.Errorf("announcements = %v, want %v", executors, want)
	}
}

// An announcement nobody replied to stays one: no reply is invented. Read
// with a token that may not read pages.single, the live edit is kept with
// an unknown author and the creator comes from the trail.
func TestNormalizeAnnouncementWithoutReply(t *testing.T) {
	events := storyEvents(t, "announcement-no-reply")
	want := []summary{
		{model.KindPageCreated, "pages/59", "Robin Owner/person"},
		{model.KindRequest, "comments/950", "Robin Owner/person"},
		{model.KindRunAnnounced, "comments/951", "Agent Opus/agent"},
		{model.KindComment, "comments/952", "Robin Owner/person"},
		{model.KindComment, "comments/953", "Agent Opus/agent"},
		{model.KindPageEdited, liveFingerprint(t, "2026-03-06T09:30:00.800Z", actionEdited, "59"), "/unknown"},
	}
	if got := summarize(events); !slices.Equal(got, want) {
		t.Fatalf("events =\n%v\nwant\n%v", got, want)
	}
	if ref := events[5].Evidence.Ref; ref != "pages.list" {
		t.Errorf("live edit evidence = %q, want pages.list", ref)
	}
}

func TestBranch(t *testing.T) {
	if got, want := Branch(57), "kelos/wiki-57"; got != want {
		t.Errorf("Branch(57) = %q, want %q", got, want)
	}
}

var (
	testOwner = user{7, "Robin Owner"}
	testOpus  = user{21, "Agent Opus"}
	// t0 is when the hand-built pages below were created.
	t0 = time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)
	// renderDelay is how long after a save Wiki.js writes its render.
	renderDelay = 2 * time.Second
)

// handPage is page 60, created by the owner at t0, last written at
// updated by author.
func handPage(updated time.Time, author user) Page {
	return Page{
		ID: 60, Path: "widgets/hand", Locale: "en", CreatedAt: t0, UpdatedAt: updated,
		AuthorID: author.ID, AuthorName: author.Name, CreatorID: testOwner.ID, CreatorName: testOwner.Name,
	}
}

func handVersion(id int64, at time.Time, author user) Version {
	return Version{VersionID: id, VersionDate: at, AuthorID: author.ID, AuthorName: author.Name, ActionType: "edit"}
}

func normalizeHand(t *testing.T, s Snapshot) []model.Event {
	t.Helper()
	s.Instance, s.ObservedAt = testInstance, testObservedAt
	recs, err := Normalize(s, testAgents)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return recs.Events
}

func eventByVersion(t *testing.T, events []model.Event, version string) model.Event {
	t.Helper()
	for _, e := range events {
		if e.Details["version"] == version {
			return e
		}
	}
	t.Fatalf("no event of version %s in %v", version, summarize(events))
	return model.Event{}
}

// An edit or a move read first as the live version, then, after the next
// write, as a trail version, names its earlier id, so it is never kept
// twice. Three reads: after an edit, after a move, after another edit.
func TestNormalizeArchivedVersionSupersedesLive(t *testing.T) {
	const oldPath, newPath = "widgets/hand", "widgets/moved"
	edit, moved := t0.Add(time.Hour), t0.Add(2*time.Hour)
	created := handVersion(1, t0.Add(renderDelay), testOwner)
	edited := handVersion(2, edit, testOpus)
	move := handVersion(3, moved, testOpus)
	move.ActionType, move.ValueBefore, move.ValueAfter = trailActionMove, ptr(oldPath), ptr(newPath)

	afterEdit := normalizeHand(t, Snapshot{Page: handPage(edit, testOpus), Trail: []Version{created}})
	movedPage := handPage(moved, testOpus)
	movedPage.Path = newPath
	afterMove := normalizeHand(t, Snapshot{
		Page: movedPage, Trail: []Version{created, edited}, NewestPath: oldPath,
	})
	laterPage := handPage(moved.Add(time.Hour), testOpus)
	laterPage.Path = newPath
	afterNextEdit := normalizeHand(t, Snapshot{
		Page: laterPage, Trail: []Version{created, edited, move}, NewestPath: newPath,
	})

	liveEdit := eventByVersion(t, afterEdit, versionLive)
	if got := eventByVersion(t, afterMove, "2"); got.Details["supersedes"] != liveEdit.ID {
		t.Errorf("version 2 supersedes %q, want the live edit's id %q", got.Details["supersedes"], liveEdit.ID)
	}
	liveMove := eventByVersion(t, afterMove, versionLive)
	if liveMove.Kind != model.KindPageMoved || liveMove.Actor.Kind != model.ActorUnknown ||
		liveMove.Details["from"] != oldPath || liveMove.Details["to"] != newPath {
		t.Errorf("live move = %+v", liveMove)
	}
	if got := eventByVersion(t, afterNextEdit, "3"); got.Details["supersedes"] != liveMove.ID || got.Kind != model.KindPageMoved {
		t.Errorf("version 3 = %+v, want a move superseding %q", got, liveMove.ID)
	}
}

func ptr(s string) *string { return &s }

// A page never edited has no trail: its live version is the creation's,
// not an edit, whether or not its author can be read.
func TestNormalizeNeverEdited(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		events := normalizeHand(t, Snapshot{Page: handPage(t0.Add(renderDelay), testOwner), Restricted: restricted})
		want := "Robin Owner/person"
		if restricted {
			want = "/unknown"
		}
		if got := summarize(events); !slices.Equal(got, []summary{{model.KindPageCreated, "pages/60", want}}) {
			t.Errorf("restricted %v: events = %v", restricted, got)
		}
	}
}

// When the oldest version is not the creator's, the history before it was
// purged: it is an edit, and the creation is still the page's.
func TestNormalizePurgedTrail(t *testing.T) {
	events := normalizeHand(t, Snapshot{
		Page:  handPage(t0.Add(2*time.Hour), testOpus),
		Trail: []Version{handVersion(9, t0.Add(time.Hour), testOpus)},
	})
	want := []summary{
		{model.KindPageCreated, "pages/60", "Robin Owner/person"},
		{model.KindPageEdited, "pages/60/versions/9", "Agent Opus/agent"},
		{model.KindPageEdited, liveFingerprint(t, t0.Add(2*time.Hour).Format(time.RFC3339Nano), actionEdited, "60"),
			"Agent Opus/agent"},
	}
	if got := summarize(events); !slices.Equal(got, want) {
		t.Fatalf("events =\n%v\nwant\n%v", got, want)
	}
}

// When the creator cannot be read, the oldest version's author is the
// creator only if that version is dated as the creation's render; a later
// one may follow a purge, so neither its author nor an edit is claimed.
func TestNormalizeRestrictedCreator(t *testing.T) {
	live := t0.Add(2 * time.Hour)
	liveEdit := summary{model.KindPageEdited, liveFingerprint(t, live.Format(time.RFC3339Nano), actionEdited, "60"),
		"/unknown"}
	tests := []struct {
		name    string
		oldest  Version
		creator string
	}{
		{"creation's render", handVersion(1, t0.Add(renderDelay), testOwner), "Robin Owner/person"},
		{"window's end", handVersion(1, t0.Add(creationRenderWindow), testOwner), "Robin Owner/person"},
		{"after the window", handVersion(9, t0.Add(creationRenderWindow+time.Millisecond), testOpus), "/unknown"},
	}
	for _, tt := range tests {
		events := normalizeHand(t, Snapshot{Page: handPage(live, testOpus), Restricted: true, Trail: []Version{tt.oldest}})
		want := []summary{{model.KindPageCreated, "pages/60", tt.creator}, liveEdit}
		if got := summarize(events); !slices.Equal(got, want) {
			t.Errorf("%s: events =\n%v\nwant\n%v", tt.name, got, want)
		}
	}
}

// An author Wiki.js does not name (a deleted account) is unknown, never
// guessed.
func TestNormalizeUnknownAuthor(t *testing.T) {
	events := normalizeHand(t, Snapshot{
		Page:     handPage(t0.Add(renderDelay), testOwner),
		Comments: []Comment{{ID: 70, Render: "<p>hello</p>", CreatedAt: t0.Add(time.Hour)}},
	})
	if got := events[1]; got.Actor.Kind != model.ActorUnknown || got.Details != nil {
		t.Errorf("comment = %+v", got)
	}
}

func TestNormalizeRejectsInvalidPage(t *testing.T) {
	page := handPage(t0.Add(renderDelay), testOwner)
	page.Locale = ""
	if _, err := Normalize(Snapshot{Instance: testInstance, Page: page, ObservedAt: testObservedAt}, testAgents); err == nil {
		t.Error("Normalize accepted a page without a locale")
	}
}

// A save between the live page's read and the trail's archives the state
// the live page showed: it is kept once, as the trail version.
func TestNormalizeSaveDuringRead(t *testing.T) {
	edit := t0.Add(time.Hour)
	events := normalizeHand(t, Snapshot{
		Page:  handPage(edit, testOpus),
		Trail: []Version{handVersion(1, t0.Add(renderDelay), testOwner), handVersion(2, edit, testOpus)},
	})
	want := []summary{
		{model.KindPageCreated, "pages/60", "Robin Owner/person"},
		{model.KindPageEdited, "pages/60/versions/2", "Agent Opus/agent"},
	}
	if got := summarize(events); !slices.Equal(got, want) {
		t.Fatalf("events =\n%v\nwant\n%v", got, want)
	}
}
