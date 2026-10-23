package vikunja

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/source/htmltext"
	"github.com/sperano/sillage/internal/source/kelostext"
)

// sourceTimeLayout formats times inside SourceIDs.
const sourceTimeLayout = time.RFC3339Nano

// Normalize turns one read of a task into events: its creation, each
// comment (a request when it starts with one of kelostext.Commands, a
// parsed start announcement when serviceAccount posted one), its done mark
// when it is done, and a snapshot of its current state. Events with a
// source time come in time order; the state snapshot comes last.
func Normalize(s Snapshot, serviceAccount string) []Event {
	events := []Event{taskCreated(s)}
	for _, c := range s.Comments {
		events = append(events, commentEvent(s, c, serviceAccount))
	}
	if s.Task.Done {
		events = append(events, taskDone(s))
	}
	slices.SortStableFunc(events, bySourceTime)
	return append(events, stateObserved(s))
}

// bySourceTime orders events by source time, those of unknown time last.
func bySourceTime(a, b Event) int {
	switch az, bz := a.SourceTime.IsZero(), b.SourceTime.IsZero(); {
	case az && bz:
		return 0
	case az:
		return 1
	case bz:
		return -1
	}
	return a.SourceTime.Compare(b.SourceTime)
}

// base fills the fields every event of s shares.
func base(s Snapshot, kind Kind, sourceID string) Event {
	return Event{
		Instance:   s.Instance,
		SourceID:   sourceID,
		Kind:       kind,
		TaskID:     s.Task.ID,
		ObservedAt: s.ObservedAt,
	}
}

func taskCreated(s Snapshot) Event {
	e := base(s, KindTaskCreated, taskKey(s.Task.ID)+"/created")
	e.SourceTime = s.Task.Created
	if by := s.Task.CreatedBy; by != nil {
		creator := *by
		e.Actor = &creator
	}
	e.Title = s.Task.Title
	e.Evidence = Evidence{Path: taskPath(s.Task.ID), Field: "created, created_by"}
	return e
}

func commentEvent(s Snapshot, c Comment, serviceAccount string) Event {
	commentID := strconv.FormatInt(c.ID, 10)
	e := base(s, KindComment, "comment/"+commentID)
	if !s.CommentsObservedAt.IsZero() {
		e.ObservedAt = s.CommentsObservedAt
	}
	e.SourceTime = c.Created
	author := c.Author
	e.Actor = &author
	e.CommentID = c.ID
	e.Evidence = Evidence{Path: taskPath(s.Task.ID) + "/comments/" + commentID, Field: "created, author"}
	if !c.Created.IsZero() && c.Updated.After(c.Created) {
		e.Flags = append(e.Flags, FlagEdited)
	}
	text := htmltext.Plain(c.Body)
	if r, ok := kelostext.ParseRequest(text); ok {
		e.Kind = KindRequest
		e.Request = &r
		e.Evidence.Field = "created, author, comment"
		return e
	}
	if serviceAccount == "" || c.Author.Username != serviceAccount {
		return e
	}
	a, err := parseAnnouncement(text)
	switch {
	case err == nil:
		e.Kind = KindRunAnnounced
		e.Run = &a
		e.Evidence.Field = "created, author, comment"
	case errors.Is(err, kelostext.ErrUnparsedAnnouncement):
		e.Flags = append(e.Flags, FlagUnparsedAnnouncement)
	}
	return e
}

func taskDone(s Snapshot) Event {
	id := taskKey(s.Task.ID) + "/done"
	if !s.Task.DoneAt.IsZero() {
		id += "/" + s.Task.DoneAt.UTC().Format(sourceTimeLayout)
	}
	e := base(s, KindTaskDone, id)
	e.SourceTime = s.Task.DoneAt
	e.Evidence = Evidence{Path: taskPath(s.Task.ID), Field: "done, done_at"}
	if s.Task.DoneAt.IsZero() {
		e.Flags = append(e.Flags, FlagDoneTimeUnknown)
	}
	return e
}

func stateObserved(s Snapshot) Event {
	e := base(s, KindStateObserved,
		taskKey(s.Task.ID)+"/state/"+s.ObservedAt.UTC().Format(sourceTimeLayout))
	e.State = &State{
		Done:      s.Task.Done,
		Buckets:   buckets(s.Task),
		Labels:    labelTitles(s.Task.Labels),
		Assignees: slices.Clone(s.Task.Assignees),
	}
	e.Evidence = Evidence{Path: taskPath(s.Task.ID), Field: "done, buckets, bucket_id, labels, assignees"}
	return e
}

// buckets prefers the expanded buckets and falls back to the bare
// bucket_id of Vikunja versions without project views.
func buckets(t Task) []Bucket {
	if len(t.Buckets) > 0 {
		return slices.Clone(t.Buckets)
	}
	if t.BucketID != 0 {
		return []Bucket{{ID: t.BucketID}}
	}
	return nil
}

func labelTitles(labels []Label) []string {
	if len(labels) == 0 {
		return nil
	}
	titles := make([]string, len(labels))
	for i, l := range labels {
		titles[i] = l.Title
	}
	return titles
}

func taskKey(id int64) string {
	return "task/" + strconv.FormatInt(id, 10)
}
