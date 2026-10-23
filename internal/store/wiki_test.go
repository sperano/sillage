package store

import (
	"strconv"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

const testPageObject = "pages/57"

// pageEvent is a save of page 57 at minute, read at minute+1.
func pageEvent(minute int) model.Event {
	return model.Event{
		Source: model.SourceWiki, Instance: testWiki, Kind: model.KindPageEdited,
		SourceTime: ptr(at(minute)), ObservedAt: at(minute + 1),
		Actor:    model.Account{Login: "agent", Kind: model.ActorAgent},
		Related:  model.Related{Page: ptr(pageKey(57))},
		Evidence: model.Evidence{Source: model.SourceWiki, Instance: testWiki, Ref: "pages.single(id: 57)"},
	}
}

// liveEdit is the page's live version saved at minute.
func liveEdit(minute int) model.Event {
	e := pageEvent(minute)
	e.ID, e.IDKind = model.Fingerprint(at(minute), testPageObject, "edited", ""), model.IDFingerprint
	e.Details = map[string]string{detailVersion: versionLive}
	return e
}

// archived is history version id of the save at minute, which the live
// edit read before it was archived superseded.
func archived(id, minute int) model.Event {
	e := pageEvent(minute)
	e.ID, e.IDKind = testPageObject+"/versions/"+strconv.Itoa(id), model.IDSource
	e.Details = map[string]string{
		detailVersion:    strconv.Itoa(id),
		detailSupersedes: model.Fingerprint(at(minute), testPageObject, "edited", ""),
	}
	return e
}

func eventIDs(story model.Story) []string {
	var ids []string
	for _, e := range story.Events {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestArchivedVersionSupersedesLiveEdit(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := pageRoot(57)
	live := liveEdit(0)
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{live},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(live.Ref(), run.Key.Ref())},
	})
	version, newer := archived(121, 0), liveEdit(10)
	mustSave(t, s, root, model.Records{Events: []model.Event{version, newer}})
	// A late read that still saw the first save as live changes nothing.
	mustSave(t, s, root, model.Records{Events: []model.Event{live}})

	story := mustStory(t, s, root)
	want := []string{version.ID, newer.ID}
	if got := eventIDs(story); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("events = %v, want %v", got, want)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].From != version.Ref() {
		t.Errorf("correlations = %+v, want the run linked to version 121", story.Correlations)
	}
}

func TestSupersededLiveEditInSameBatch(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := pageRoot(57)
	live, version := liveEdit(0), archived(121, 0)
	mustSave(t, s, root, model.Records{Events: []model.Event{live, version}})
	if got := eventIDs(mustStory(t, s, root)); len(got) != 1 || got[0] != version.ID {
		t.Errorf("events = %v, want only version 121", got)
	}
}

func TestNewestLiveEditKept(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := pageRoot(57)
	older, newer, newest := liveEdit(0), liveEdit(10), liveEdit(20)
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{Events: []model.Event{newer}})
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{older},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(older.Ref(), run.Key.Ref())},
	})
	story := mustStory(t, s, root)
	if got := eventIDs(story); len(got) != 1 || got[0] != newer.ID {
		t.Errorf("after an older live edit: events = %v, want only %s", got, newer.ID)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].From != newer.Ref() {
		t.Errorf("correlations = %+v, want the same account's newer edit linked", story.Correlations)
	}
	mustSave(t, s, root, model.Records{Events: []model.Event{newest}})
	story = mustStory(t, s, root)
	if got := eventIDs(story); len(got) != 1 || got[0] != newest.ID {
		t.Errorf("after a newer live edit: events = %v, want only %s", got, newest.ID)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].From != newest.Ref() {
		t.Errorf("correlations = %+v, want the newest edit linked", story.Correlations)
	}
}

// TestLiveEditOfAnotherAccount replaces a live edit with another
// account's: the story holds the newer edit, without the older's links.
func TestLiveEditOfAnotherAccount(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := pageRoot(57)
	older, newer := liveEdit(0), liveEdit(10)
	newer.Actor = model.Account{Login: "owner", Kind: model.ActorPerson}
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, root, model.Records{
		Events:       []model.Event{older},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(older.Ref(), run.Key.Ref())},
	})
	mustSave(t, s, root, model.Records{Events: []model.Event{newer}})
	story := mustStory(t, s, root)
	if got := eventIDs(story); len(got) != 1 || got[0] != newer.ID {
		t.Errorf("events = %v, want only %s", got, newer.ID)
	}
	if len(story.Correlations) != 0 {
		t.Errorf("correlations = %+v, want the agent's link not given to the owner's edit", story.Correlations)
	}
}

// TestLiveEditReplacedInEveryStory stores a live edit in a task's story,
// then its re-render in the page's: the task's story holds the re-render,
// then the version that archives it.
func TestLiveEditReplacedInEveryStory(t *testing.T) {
	s := newTestStore(t, Retention{})
	task, page := taskRoot(7), pageRoot(57)
	first, rerender := liveEdit(0), liveEdit(1)
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, task, model.Records{
		Events:       []model.Event{first},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(first.Ref(), run.Key.Ref())},
	})
	mustSave(t, s, page, model.Records{Events: []model.Event{rerender}})
	story := mustStory(t, s, task)
	if got := eventIDs(story); len(got) != 1 || got[0] != rerender.ID {
		t.Errorf("task events = %v, want the re-render %s", got, rerender.ID)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].From != rerender.Ref() {
		t.Errorf("task correlations = %+v, want the run linked to the re-render", story.Correlations)
	}
	version := archived(121, 1)
	mustSave(t, s, page, model.Records{Events: []model.Event{version}})
	if got := eventIDs(mustStory(t, s, task)); len(got) != 1 || got[0] != version.ID {
		t.Errorf("task events after the archive = %v, want version 121", got)
	}
}

// TestSupersededLiveEditJoinsStoryAsVersion saves, under a task, a live
// edit read before the page's story stored the version archiving it.
func TestSupersededLiveEditJoinsStoryAsVersion(t *testing.T) {
	s := newTestStore(t, Retention{})
	task, page := taskRoot(7), pageRoot(57)
	live, version := liveEdit(0), archived(121, 0)
	mustSave(t, s, page, model.Records{Events: []model.Event{version}})
	run := finishedRun("uid-1", 0, 5, model.OutcomeSucceeded)
	mustSave(t, s, task, model.Records{
		Events:       []model.Event{live},
		Runs:         []model.Run{run},
		Correlations: []model.Correlation{link(live.Ref(), run.Key.Ref())},
	})
	story := mustStory(t, s, task)
	if got := eventIDs(story); len(got) != 1 || got[0] != version.ID {
		t.Errorf("events = %v, want version 121", got)
	}
	if len(story.Correlations) != 1 || story.Correlations[0].From != version.Ref() {
		t.Errorf("correlations = %+v, want the run linked to version 121", story.Correlations)
	}
}
