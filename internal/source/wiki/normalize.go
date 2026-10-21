package wiki

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	// branchPrefix starts the branch Kelos pushes a page's follow-ups to:
	// one branch per page, so a later request adds to the pull request an
	// earlier one opened.
	branchPrefix = "kelos/wiki-"
	// actionEdited and actionMoved are the fingerprinted actions on a page.
	actionEdited = "edited"
	actionMoved  = "moved"
	// versionLive is the "version" detail of the live version, which has
	// no history version id yet.
	versionLive = "live"
	// noteArchived explains why the live version is read from the page.
	noteArchived = "Wiki.js 2 archives a version only when the next save replaces it"
)

// Branch returns the branch Kelos works on for follow-ups requested on
// page pageID. It is what links the page to its pull requests: hand it to
// the GitHub source, with the repository of the workspace a start
// announcement names.
func Branch(pageID int64) string {
	return branchPrefix + strconv.FormatInt(pageID, 10)
}

// Normalize turns one read of a page into model events, in time order:
// the page's creation; one event per later state of the page, the live
// one included, each an edit by its author or a move by an unknown actor;
// and each comment, as a /kelos request, a start announcement posted by
// one of agentUsers, or a plain comment (a reply). Only what Wiki.js
// records is claimed: no comment edits, no reading.
//
// A trail version's id is its history version id. The live version has
// none yet: its id is a fingerprint of its time and action. A page has at
// most one live record: whatever a newer read of the page returns
// replaces it, since its updatedAt can also move without a new version
// (the render a few seconds after a save, a re-render). Once a later save
// archives the live version, the version's event also names that
// fingerprint in its "supersedes" detail. Every event is validated.
func Normalize(s Snapshot, agentUsers []int64) (model.Records, error) {
	n := normalizer{
		s:      s,
		agents: agentUsers,
		key:    model.PageKey{Instance: s.Instance, ID: s.Page.ID, Locale: s.Page.Locale},
		seen:   make(map[string]bool),
	}
	n.pageEvents()
	for _, c := range s.Comments {
		n.add(n.comment(c))
	}
	slices.SortStableFunc(n.events, func(a, b model.Event) int { return a.Time().Compare(b.Time()) })
	errs := make([]error, 0, len(n.events))
	for _, e := range n.events {
		errs = append(errs, e.Validate())
	}
	if err := errors.Join(errs...); err != nil {
		return model.Records{}, fmt.Errorf("wiki: page %s: %w", n.key, err)
	}
	return model.Records{Events: n.events}, nil
}

// normalizer accumulates the events of one snapshot, each id once.
type normalizer struct {
	s      Snapshot
	agents []int64
	key    model.PageKey
	events []model.Event
	seen   map[string]bool
}

func (n *normalizer) add(e model.Event) {
	if !n.seen[e.ID] {
		n.seen[e.ID] = true
		n.events = append(n.events, e)
	}
}

// user is an account a record names; zero when unknown.
type user struct {
	ID   int64
	Name string
}

// state is one state of the page: a trail version, or the live page
// (versionID 0). Its time and author are those of the write that produced
// it; from and to are set when that write moved the page.
type state struct {
	versionID int64
	at        time.Time
	author    user
	from, to  string
}

// states lists the page's states, oldest first, the live one last. The
// live page is left out when the newest trail version is not older: a
// save during the read archived the state the live page showed.
func (n *normalizer) states() []state {
	out := make([]state, 0, len(n.s.Trail)+1)
	for _, v := range n.s.Trail {
		st := state{versionID: v.VersionID, at: v.VersionDate, author: user{v.AuthorID, v.AuthorName}}
		// The oldest version of a trail whose length is a multiple of the
		// page size has nothing older to compare with, and Wiki.js calls
		// it a move from "": that is no move.
		if v.ActionType == trailActionMove && v.ValueBefore != nil && v.ValueAfter != nil && *v.ValueBefore != "" {
			st.from, st.to = *v.ValueBefore, *v.ValueAfter
		}
		out = append(out, st)
	}
	if k := len(n.s.Trail); k > 0 && !n.s.Trail[k-1].VersionDate.Before(n.s.Page.UpdatedAt) {
		return out
	}
	live := state{at: n.s.Page.UpdatedAt}
	if !n.s.Restricted {
		live.author = user{n.s.Page.AuthorID, n.s.Page.AuthorName}
	}
	if n.s.NewestPath != "" && n.s.NewestPath != n.s.Page.Path {
		live.from, live.to = n.s.NewestPath, n.s.Page.Path
	}
	return append(out, live)
}

// pageEvents adds the creation and an event per later state. The oldest
// state is the creation's own (its time is the render after the save,
// seconds after createdAt), not an edit, unless it is a move or another
// author's: then the history before it was purged.
func (n *normalizer) pageEvents() {
	states := n.states()
	first := states[0]
	creator := user{n.s.Page.CreatorID, n.s.Page.CreatorName}
	isCreation := first.from == ""
	switch {
	case n.s.Restricted && isCreation:
		creator = first.author
	case n.s.Restricted:
		creator = user{}
	default:
		isCreation = isCreation && first.author.ID == creator.ID
	}
	n.add(n.created(creator, first, isCreation))
	if isCreation {
		states = states[1:]
	}
	for _, st := range states {
		n.add(n.stateEvent(st))
	}
}

// event fills the fields every event of the snapshot shares.
func (n *normalizer) event(kind model.EventKind, id string, idKind model.IDKind, at time.Time) model.Event {
	key := n.key
	return model.Event{
		Source:     model.SourceWiki,
		Instance:   n.s.Instance,
		ID:         id,
		IDKind:     idKind,
		Kind:       kind,
		SourceTime: &at,
		ObservedAt: n.s.ObservedAt,
		Actor:      model.Account{Kind: model.ActorUnknown},
		Related:    model.Related{Page: &key},
		Evidence:   model.Evidence{Source: model.SourceWiki, Instance: n.s.Instance},
	}
}

// object names the page in ids and fingerprints.
func (n *normalizer) object() string {
	return "pages/" + strconv.FormatInt(n.s.Page.ID, 10)
}

// pageRef names the query the live page was read with.
func (n *normalizer) pageRef() string {
	if n.s.Restricted {
		return "pages.list"
	}
	return "pages.single(id: " + strconv.FormatInt(n.s.Page.ID, 10) + ")"
}

func (n *normalizer) created(creator user, first state, isCreation bool) model.Event {
	e := n.event(model.KindPageCreated, n.object(), model.IDSource, n.s.Page.CreatedAt)
	n.setAuthor(&e, creator)
	e.Evidence.Ref = n.pageRef()
	switch {
	case !n.s.Restricted:
		e.Evidence.Note = "createdAt, creatorId, creatorName"
	case isCreation && first.versionID != 0:
		e.Evidence.Note = "createdAt; creator: author of history version " + strconv.FormatInt(first.versionID, 10)
	default:
		e.Evidence.Note = "createdAt; the token may not read the creator"
	}
	if !isCreation {
		e.Evidence.Note += "; the creation's own version is not in the history trail"
	}
	return e
}

func (n *normalizer) stateEvent(st state) model.Event {
	kind, action := model.KindPageEdited, actionEdited
	if st.from != "" {
		kind, action = model.KindPageMoved, actionMoved
	}
	fingerprint := model.Fingerprint(st.at, n.object(), action, "")
	e := n.event(kind, fingerprint, model.IDFingerprint, st.at)
	if st.versionID == 0 {
		e.Details = map[string]string{"version": versionLive}
		e.Evidence.Ref = n.pageRef()
		e.Evidence.Note = n.liveNote(st.from != "")
	} else {
		version := strconv.FormatInt(st.versionID, 10)
		e.ID, e.IDKind = n.object()+"/versions/"+version, model.IDSource
		e.Details = map[string]string{"version": version, "supersedes": fingerprint}
		e.Evidence.Ref = "pages.history(id: " + strconv.FormatInt(n.s.Page.ID, 10) + ")"
		e.Evidence.Note = "version " + version + ": versionDate, authorId, authorName, actionType"
		if st.from != "" {
			e.Evidence.Note = "version " + version + ": versionDate, actionType, valueBefore, valueAfter"
		}
	}
	if st.from != "" {
		e.Details["from"], e.Details["to"] = st.from, st.to
		e.Evidence.Note += "; Wiki.js does not record who moved a page"
		return e
	}
	n.setAuthor(&e, st.author)
	return e
}

func (n *normalizer) liveNote(moved bool) string {
	switch {
	case moved:
		return "updatedAt and path of the live page, path of the newest history version; " + noteArchived
	case n.s.Restricted:
		return "updatedAt of the live page; the token may not read its author (pages.single needs manage:pages " +
			"or delete:pages, its author write:pages); " + noteArchived
	}
	return "updatedAt, authorId, authorName of the live page; " + noteArchived
}

// setAuthor sets the event's actor to u, and its "authorId" detail when
// u is known. An agent user's account is an agent's, any other a
// person's.
func (n *normalizer) setAuthor(e *model.Event, u user) {
	if u.ID <= 0 || u.Name == "" {
		e.Actor = model.Account{Kind: model.ActorUnknown}
		return
	}
	kind := model.ActorPerson
	if slices.Contains(n.agents, u.ID) {
		kind = model.ActorAgent
	}
	e.Actor = model.Account{Login: u.Name, Kind: kind}
	setDetail(e, "authorId", strconv.FormatInt(u.ID, 10))
}

func setDetail(e *model.Event, key, value string) {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	e.Details[key] = value
}
