package vikunja

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/htmltext"
	"github.com/sperano/sillage/internal/source/kelostext"
)

const (
	actionDone               = "done"
	actionBucketObserved     = "bucket_observed"
	actionLabelObserved      = "label_observed"
	actionAssigneeObserved   = "assignee_observed"
	actionCompletionObserved = "completion_observed"
)

// Normalize turns one read of a task into source-independent model records:
// creation, comments, the current done mark, what the read saw of the
// task's completion, and one observation per current bucket, label and
// assignee. Every event is validated.
func Normalize(s Snapshot, serviceAccount string) (model.Records, error) {
	n := normalizer{
		s:              s,
		serviceAccount: serviceAccount,
		key:            model.TaskKey{Instance: s.Instance, ID: s.Task.ID},
	}
	n.events = append(n.events, n.taskCreated())
	for _, comment := range s.Comments {
		n.events = append(n.events, n.comment(comment))
	}
	if s.Task.Done {
		n.events = append(n.events, n.taskDone())
	}
	slices.SortStableFunc(n.events, func(a, b model.Event) int { return a.Time().Compare(b.Time()) })
	n.observeState()

	errs := make([]error, 0, len(n.events))
	for _, event := range n.events {
		errs = append(errs, event.Validate())
	}
	if err := errors.Join(errs...); err != nil {
		return model.Records{}, fmt.Errorf("vikunja: task %s: %w", n.key, err)
	}
	return model.Records{Events: n.events}, nil
}

type normalizer struct {
	s              Snapshot
	serviceAccount string
	key            model.TaskKey
	events         []model.Event
}

func (n *normalizer) event(kind model.EventKind, id string, idKind model.IDKind, at *time.Time) model.Event {
	key := n.key
	return model.Event{
		Source:     model.SourceVikunja,
		Instance:   n.s.Instance,
		ID:         id,
		IDKind:     idKind,
		Kind:       kind,
		SourceTime: at,
		ObservedAt: n.s.ObservedAt,
		Actor:      model.Account{Kind: model.ActorUnknown},
		Related:    model.Related{Task: &key},
		Evidence: model.Evidence{
			Source:   model.SourceVikunja,
			Instance: n.s.Instance,
		},
	}
}

func (n *normalizer) object() string {
	return "tasks/" + strconv.FormatInt(n.s.Task.ID, 10)
}

func (n *normalizer) taskRef() string {
	return "GET " + taskPath(n.s.Task.ID) + "?expand=" + expandBuckets
}

func (n *normalizer) commentsRef() string {
	return "GET " + taskPath(n.s.Task.ID) + "/comments"
}

func (n *normalizer) taskCreated() model.Event {
	e := n.event(model.KindTaskCreated, n.object(), model.IDSource, timePointer(n.s.Task.Created))
	e.Actor = n.account(n.s.Task.CreatedBy)
	e.Evidence.Ref = n.taskRef()
	e.Evidence.Note = "created, created_by"
	return e
}

func (n *normalizer) comment(c Comment) model.Event {
	id := strconv.FormatInt(c.ID, 10)
	e := n.event(model.KindComment, "comments/"+id, model.IDSource, timePointer(c.Created))
	if !n.s.CommentsObservedAt.IsZero() {
		e.ObservedAt = n.s.CommentsObservedAt
	}
	e.Actor = n.account(&c.Author)
	e.Evidence.Ref = n.commentsRef()
	e.Evidence.Note = "comment " + id + ": created, author"
	if !c.Created.IsZero() && c.Updated.After(c.Created) {
		e.Evidence.Note += "; edited after creation, and Vikunja keeps no prior body"
	}

	text := htmltext.Plain(c.Body)
	if request, ok := kelostext.ParseRequest(text); ok {
		e.Kind = model.KindRequest
		setDetail(&e, "command", request.Command)
		if request.Agent != "" {
			setDetail(&e, "agent", request.Agent)
		}
		return e
	}
	if n.serviceAccount == "" || c.Author.Username != n.serviceAccount {
		return e
	}
	a, err := parseAnnouncement(text)
	switch {
	case err == nil:
		n.announce(&e, a)
	case errors.Is(err, kelostext.ErrUnparsedAnnouncement):
		e.Evidence.Note += "; reads like a start announcement but its Kelos Task could not be parsed"
	}
	return e
}

func (n *normalizer) announce(e *model.Event, a announcement) {
	e.Kind = model.KindRunAnnounced
	e.Evidence.Note += ", and the Kelos Task" + namedAlong(a) + " it names"
	if a.agent != "" {
		e.Executor = &model.Executor{Name: a.agent, Kind: model.ActorAgent}
	}
	for _, detail := range [][2]string{
		{"taskName", a.taskName},
		{"pod", a.pod},
		{"namespace", a.namespace},
		{"model", a.model},
		{"role", string(a.role)},
	} {
		if detail[1] != "" {
			setDetail(e, detail[0], detail[1])
		}
	}
}

func namedAlong(a announcement) string {
	switch {
	case a.pod != "" && a.namespace != "":
		return ", pod and namespace"
	case a.pod != "":
		return " and pod"
	case a.namespace != "":
		return " and namespace"
	}
	return ""
}

func (n *normalizer) taskDone() model.Event {
	at := n.s.Task.DoneAt
	sourceTime := timePointer(at)
	if at.IsZero() {
		at = n.s.ObservedAt
	}
	e := n.event(model.KindTaskDone,
		model.Fingerprint(at, n.object(), actionDone, ""), model.IDFingerprint, sourceTime)
	e.Evidence.Ref = n.taskRef()
	e.Evidence.Note = "done is true; done_at is the latest completion time; Vikunja does not retain earlier completion or reopen times"
	if sourceTime == nil {
		e.Evidence.Note = "done is true; done_at is unset, so the source time is unknown"
	}
	return e
}

func (n *normalizer) observeState() {
	// Every read records one, done or not: a read that saw the task not
	// done after one that saw it done is the only trace of a reopening.
	done := model.DoneFalse
	if n.s.Task.Done {
		done = model.DoneTrue
	}
	n.events = append(n.events, n.observation(
		model.KindTaskCompletionObserved, actionCompletionObserved, done, model.DetailDone, done, "done"))
	for _, bucket := range buckets(n.s.Task) {
		value := bucket.Title
		if value == "" {
			value = strconv.FormatInt(bucket.ID, 10)
		}
		n.events = append(n.events, n.observation(
			model.KindTaskBucketObserved, actionBucketObserved, bucketIdentity(bucket), "bucket", value,
			"buckets, bucket_id"))
	}
	for _, label := range n.s.Task.Labels {
		n.events = append(n.events, n.observation(
			model.KindTaskLabelObserved, actionLabelObserved, objectIdentity(label.ID, label.Title),
			"label", label.Title, "labels"))
	}
	for _, assignee := range n.s.Task.Assignees {
		e := n.observation(model.KindTaskAssigneeObserved, actionAssigneeObserved,
			objectIdentity(assignee.ID, assignee.Username), "assignee", assignee.Username, "assignees")
		if assignee.ID > 0 {
			setDetail(&e, "assigneeId", strconv.FormatInt(assignee.ID, 10))
		}
		n.events = append(n.events, e)
	}
}

func bucketIdentity(bucket Bucket) string {
	if bucket.ID <= 0 {
		return bucket.Title
	}
	return strconv.FormatInt(bucket.ProjectViewID, 10) + "/" + strconv.FormatInt(bucket.ID, 10)
}

func objectIdentity(id int64, fallback string) string {
	if id > 0 {
		return strconv.FormatInt(id, 10)
	}
	return fallback
}

func (n *normalizer) observation(
	kind model.EventKind,
	action, content, detail, value, note string,
) model.Event {
	e := n.event(kind, model.Fingerprint(n.s.ObservedAt, n.object(), action, content),
		model.IDFingerprint, nil)
	e.Details = map[string]string{detail: value}
	e.Evidence.Ref = n.taskRef()
	e.Evidence.Note = note
	return e
}

func (n *normalizer) account(user *User) model.Account {
	if user == nil || user.Username == "" {
		return model.Account{Kind: model.ActorUnknown}
	}
	kind := model.ActorPerson
	if user.Username == n.serviceAccount {
		kind = model.ActorService
	}
	return model.Account{Login: user.Username, Kind: kind}
}

func setDetail(e *model.Event, key, value string) {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	e.Details[key] = value
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// buckets prefers the expanded buckets and falls back to the bare
// bucket_id of Vikunja versions without project views.
func buckets(task Task) []Bucket {
	if len(task.Buckets) > 0 {
		return slices.Clone(task.Buckets)
	}
	if task.BucketID != 0 {
		return []Bucket{{ID: task.BucketID}}
	}
	return nil
}
