package collector

import (
	"slices"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// The Vikunja source still returns its own Event type. Until it emits
// model records itself, vikunjaEvents converts them here, with the mapping
// that change is to adopt: this file then shrinks to a call.

// Notes for the flags the Vikunja source sets on a record.
var vikunjaFlagNotes = map[vikunja.Flag]string{
	vikunja.FlagEdited:               "edited after it was posted: the text read is the latest",
	vikunja.FlagUnparsedAnnouncement: "reads like a start announcement but names no Kelos Task in the expected form",
	vikunja.FlagDoneTimeUnknown:      "done, but Vikunja gives no done_at",
}

// vikunjaRoles maps the role an announcement's wording names to the run's.
var vikunjaRoles = map[vikunja.Role]model.RunRole{
	vikunja.RoleImplementation: model.RoleImplementation,
	vikunja.RoleTaskFollowUp:   model.RoleTaskFollowUp,
}

// vikunjaEvents turns one read of a task into model events.
func (a *assembly) vikunjaEvents(s vikunja.Snapshot) []model.Event {
	key := model.TaskKey{Instance: a.cfg.VikunjaInstance, ID: s.Task.ID}
	var out []model.Event
	for _, e := range vikunja.Normalize(s, a.cfg.VikunjaServiceAccount) {
		switch e.Kind {
		case vikunja.KindTaskCreated:
			out = append(out, a.vikunjaEvent(key, e, model.KindTaskCreated, taskObject(key), model.IDSource))
		case vikunja.KindComment:
			out = append(out, a.vikunjaEvent(key, e, model.KindComment, commentObject(e.CommentID), model.IDSource))
		case vikunja.KindRunAnnounced:
			out = append(out, a.announcement(key, e))
		case vikunja.KindTaskDone:
			at := e.SourceTime
			if at.IsZero() {
				at = e.ObservedAt
			}
			id := model.Fingerprint(at, taskObject(key), "done", "")
			out = append(out, a.vikunjaEvent(key, e, model.KindTaskDone, id, model.IDFingerprint))
		case vikunja.KindStateObserved:
			out = append(out, a.observed(key, e)...)
		}
	}
	return out
}

func taskObject(key model.TaskKey) string { return "tasks/" + strconv.FormatInt(key.ID, 10) }

func viewObject(id string) string { return "views/" + id }

func commentObject(id int64) string { return "comments/" + strconv.FormatInt(id, 10) }

func (a *assembly) vikunjaEvent(key model.TaskKey, e vikunja.Event, kind model.EventKind, id string,
	idKind model.IDKind) model.Event {
	out := model.Event{
		Source:     model.SourceVikunja,
		Instance:   a.cfg.VikunjaInstance,
		ID:         id,
		IDKind:     idKind,
		Kind:       kind,
		ObservedAt: e.ObservedAt,
		Actor:      a.vikunjaAccount(e.Actor),
		Related:    model.Related{Task: &key},
		Evidence: model.Evidence{
			Source: model.SourceVikunja, Instance: a.cfg.VikunjaInstance,
			Ref: "GET " + e.Evidence.Path, Note: e.Evidence.Field,
		},
	}
	if !e.SourceTime.IsZero() {
		at := e.SourceTime
		out.SourceTime = &at
	}
	for _, f := range e.Flags {
		if note := vikunjaFlagNotes[f]; note != "" {
			out.Evidence.Note += "; " + note
		}
	}
	return out
}

// vikunjaAccount is the account of a Vikunja user: the service account's,
// an agent's, or a person's; unknown without a user.
func (a *assembly) vikunjaAccount(u *vikunja.User) model.Account {
	switch {
	case u == nil || u.Username == "":
		return model.Account{Kind: model.ActorUnknown}
	case u.Username == a.cfg.VikunjaServiceAccount:
		return model.Account{Login: u.Username, Kind: model.ActorService}
	case slices.Contains(a.cfg.VikunjaAgentUsers, u.Username):
		return model.Account{Login: u.Username, Kind: model.ActorAgent}
	}
	return model.Account{Login: u.Username, Kind: model.ActorPerson}
}

// announcement is a start announcement, with the Kelos Task and pod it
// names and the role its wording names as details, and the agent it names
// as executor.
func (a *assembly) announcement(key model.TaskKey, e vikunja.Event) model.Event {
	out := a.vikunjaEvent(key, e, model.KindRunAnnounced, commentObject(e.CommentID), model.IDSource)
	r := e.Run
	if r.Agent != "" {
		out.Executor = &model.Executor{Name: r.Agent, Kind: model.ActorAgent}
	}
	details := map[string]string{
		detailTaskName: r.TaskName, detailPod: r.Pod, detailNamespace: r.Namespace, detailModel: r.Model,
		detailRole: string(vikunjaRoles[r.Role]),
	}
	out.Details = make(map[string]string)
	for k, v := range details {
		if v != "" {
			out.Details[k] = v
		}
	}
	return out
}

// observed turns a state snapshot into one event per bucket and per
// label: what was seen, with no source time and no known actor. A bucket
// is identified by its Kanban view as well as its title: two views may
// each have a bucket of the same title.
func (a *assembly) observed(key model.TaskKey, e vikunja.Event) []model.Event {
	var out []model.Event
	add := func(kind model.EventKind, action, value string, details map[string]string) {
		ev := a.vikunjaEvent(key, e, kind, model.Fingerprint(e.ObservedAt, taskObject(key), action, value),
			model.IDFingerprint)
		ev.Actor, ev.SourceTime = model.Account{Kind: model.ActorUnknown}, nil
		ev.Details = details
		out = append(out, ev)
	}
	for _, b := range e.State.Buckets {
		if b.Title == "" {
			continue
		}
		value, details := b.Title, map[string]string{detailBucket: b.Title}
		if b.ProjectViewID != 0 {
			view := strconv.FormatInt(b.ProjectViewID, 10)
			details[detailView] = view
			value = viewObject(view) + "/" + b.Title
		}
		add(model.KindTaskBucketObserved, "bucket_observed", value, details)
	}
	for _, l := range e.State.Labels {
		add(model.KindTaskLabelObserved, "label_observed", l, map[string]string{detailLabel: l})
	}
	return out
}
