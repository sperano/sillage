package github

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	// agentLabelPrefix starts the label an agent puts on the pull request
	// it opens (`gh pr create --label kelos:<agent>`); the rest names the
	// agent.
	agentLabelPrefix = "kelos:"
	// executorLabelWindow is how soon after the pull request's creation its
	// author must add an agent label for the label to name the executor.
	// `gh pr create --label` adds it in a second request right after; a
	// label added later, by anyone, only routes follow-ups and proves
	// nothing about who opened the pull request.
	executorLabelWindow = time.Minute
)

// timelineKinds maps the timeline events that become model events.
var timelineKinds = map[string]model.EventKind{
	timelineClosed:               model.KindPRClosed,
	timelineMerged:               model.KindPRMerged,
	timelineReopened:             model.KindPRReopened,
	timelineReviewRequested:      model.KindReviewRequested,
	timelineReviewRequestRemoved: model.KindReviewRequestRemoved,
}

// timeline adds the timeline's state changes and review requests. A close
// at or after the merge is the merge's own close and is dropped; a merge
// GitHub reports on the pull request but not on its timeline still gets
// its event.
func (n *normalizer) timeline() error {
	mergedAt, hasMergeEvent := n.mergeTime()
	for _, ev := range n.s.Timeline {
		kind, ok := timelineKinds[ev.Event]
		if !ok {
			continue
		}
		if ev.ID <= 0 {
			return fmt.Errorf("github: pull request %s: %s timeline event without an id", n.key, ev.Event)
		}
		if kind == model.KindPRClosed && mergedAt != nil && !ev.CreatedAt.Before(*mergedAt) {
			continue
		}
		n.add(n.timelineEvent(kind, ev))
	}
	if mergedAt != nil && !hasMergeEvent {
		n.add(n.mergedFromPR())
	}
	return nil
}

// mergeTime returns when the pull request was merged, the earlier of the
// timeline's merged event and the pull request's merged_at, or nil; and
// whether the timeline has a merged event.
func (n *normalizer) mergeTime() (*time.Time, bool) {
	at := n.s.PullRequest.MergedAt
	found := false
	for _, ev := range n.s.Timeline {
		if ev.Event != timelineMerged {
			continue
		}
		found = true
		if at == nil || ev.CreatedAt.Before(*at) {
			t := ev.CreatedAt
			at = &t
		}
	}
	return at, found
}

func (n *normalizer) timelineEvent(kind model.EventKind, ev TimelineEvent) model.Event {
	id := strconv.FormatInt(ev.ID, 10)
	e := n.event(kind, "issues/events/"+id, ev.CreatedAt, ev.Actor,
		timelinePath(n.s.Repository, n.s.PullRequest.Number), "event "+id+", "+ev.Event)
	switch {
	case ev.RequestedReviewer != nil && ev.RequestedReviewer.Login != "":
		e.Details = map[string]string{"reviewer": ev.RequestedReviewer.Login}
	case ev.RequestedTeam != nil && ev.RequestedTeam.Slug != "":
		e.Details = map[string]string{"team": ev.RequestedTeam.Slug}
	}
	return e
}

// mergedFromPR is the merge as the pull request alone reports it: GitHub
// gives it no event id and the list endpoint no merging account.
func (n *normalizer) mergedFromPR() model.Event {
	pr := n.s.PullRequest
	object := "pulls/" + strconv.FormatInt(pr.ID, 10)
	e := n.event(model.KindPRMerged, model.Fingerprint(*pr.MergedAt, object, timelineMerged, ""),
		*pr.MergedAt, nil, pullPath(n.s.Repository, pr.Number), "merged_at; the timeline has no merged event")
	e.IDKind = model.IDFingerprint
	return e
}

// agentLabel is the evidence that names a pull request's executor.
type agentLabel struct {
	agent   string
	label   string
	eventID int64
}

// executorOf returns the agent that opened pr, when the timeline shows
// pr's author adding exactly one agent's label within executorLabelWindow
// of its creation. Two agents' labels leave the executor unknown.
func executorOf(pr PullRequest, timeline []TimelineEvent) (agentLabel, bool) {
	if pr.User == nil {
		return agentLabel{}, false
	}
	found := make(map[string]agentLabel)
	for _, ev := range timeline {
		if ev.Event != timelineLabeled || ev.Label == nil || ev.Actor == nil ||
			!strings.EqualFold(ev.Actor.Login, pr.User.Login) {
			continue
		}
		agent, ok := strings.CutPrefix(ev.Label.Name, agentLabelPrefix)
		if !ok || agent == "" {
			continue
		}
		if d := ev.CreatedAt.Sub(pr.CreatedAt); d < 0 || d > executorLabelWindow {
			continue
		}
		if _, dup := found[agent]; !dup {
			found[agent] = agentLabel{agent: agent, label: ev.Label.Name, eventID: ev.ID}
		}
	}
	if len(found) != 1 {
		return agentLabel{}, false
	}
	for _, l := range found {
		return l, true
	}
	return agentLabel{}, false
}
