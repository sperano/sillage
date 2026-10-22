package wiki

import (
	"errors"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/htmltext"
)

// comment is a /kelos request, a start announcement when an agent user
// posted one, or else a plain comment: a reply, whoever wrote it. Which
// request a reply answers is not recorded, so it is not claimed.
func (n *normalizer) comment(c Comment) model.Event {
	id := strconv.FormatInt(c.ID, 10)
	e := n.event(model.KindComment, "comments/"+id, model.IDSource, c.CreatedAt)
	n.setAuthor(&e, user{c.AuthorID, c.AuthorName})
	e.Evidence.Ref = "comments.list(locale: " + n.s.Page.Locale + ", path: " + n.s.Page.Path + ")"
	e.Evidence.Note = "comment " + id + ": createdAt, authorId, authorName"
	text := htmltext.Plain(c.Render)
	if agent, ok := parseRequest(text); ok {
		e.Kind = model.KindRequest
		setDetail(&e, "command", requestPrefix)
		if agent != "" {
			setDetail(&e, "agent", agent)
		}
		return e
	}
	if e.Actor.Kind != model.ActorAgent {
		return e
	}
	a, err := parseAnnouncement(text, n.s.Page.ID)
	switch {
	case err == nil:
		announced(&e, a)
	case errors.Is(err, errUnparsedAnnouncement):
		e.Evidence.Note += "; reads like a start announcement but names no Kelos Task of this page"
	}
	return e
}

// announced makes e the announcement a. The agent it names is the run's
// executor.
func announced(e *model.Event, a Announcement) {
	e.Kind = model.KindRunAnnounced
	e.Evidence.Note += ", and the Kelos Task" + namedAlong(a) + " it names"
	if a.Agent != "" {
		e.Executor = &model.Executor{Name: a.Agent, Kind: model.ActorAgent}
	}
	details := [][2]string{
		{"taskName", a.TaskName},
		{"pod", a.Pod},
		{"namespace", a.Namespace},
		{"model", a.Model},
		{"workspace", a.Workspace},
		{"requestComment", strconv.FormatInt(a.RequestComment, 10)},
	}
	for _, d := range details {
		if d[1] != "" {
			setDetail(e, d[0], d[1])
		}
	}
}

// namedAlong lists what a names besides its Kelos Task, for an evidence
// note: the pod and namespace, when stated.
func namedAlong(a Announcement) string {
	switch {
	case a.Pod != "" && a.Namespace != "":
		return ", pod and namespace"
	case a.Pod != "":
		return " and pod"
	case a.Namespace != "":
		return " and namespace"
	}
	return ""
}
