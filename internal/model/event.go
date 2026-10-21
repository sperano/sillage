package model

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// EventKind is what happened. Each kind belongs to one source.
type EventKind string

// The kinds of event.
const (
	KindTaskCreated EventKind = "task.created"
	// KindTaskBucketObserved: the card was seen in a bucket (details:
	// "bucket"). Polling tells when a state was seen, not when it changed,
	// so it has no source time.
	KindTaskBucketObserved EventKind = "task.bucket_observed"
	// KindTaskLabelObserved: the task was seen carrying a label (details:
	// "label").
	KindTaskLabelObserved EventKind = "task.label_observed"
	// KindTaskDone: the task was marked done. A pull request's merge is a
	// separate event, never this one.
	KindTaskDone EventKind = "task.done"

	KindPageCreated EventKind = "page.created"
	KindPageEdited  EventKind = "page.edited"

	// KindComment: a comment on a task, a page or a pull request.
	KindComment EventKind = "comment.created"
	// KindRequest: a comment asking an agent to act (/kelos).
	KindRequest EventKind = "request.created"
	// KindRunAnnounced: the service account announced a run (details:
	// "taskName", "pod").
	KindRunAnnounced EventKind = "run.announced"

	KindPROpened        EventKind = "pr.opened"
	KindReviewSubmitted EventKind = "pr.review_submitted"
	KindPRMerged        EventKind = "pr.merged"
	// KindPRClosed: closed without being merged.
	KindPRClosed EventKind = "pr.closed"
)

// kindSources lists the sources each kind can come from.
var kindSources = map[EventKind][]Source{
	KindTaskCreated:        {SourceVikunja},
	KindTaskBucketObserved: {SourceVikunja},
	KindTaskLabelObserved:  {SourceVikunja},
	KindTaskDone:           {SourceVikunja},
	KindPageCreated:        {SourceWiki},
	KindPageEdited:         {SourceWiki},
	KindComment:            {SourceVikunja, SourceWiki, SourceGitHub},
	KindRequest:            {SourceVikunja, SourceWiki, SourceGitHub},
	KindRunAnnounced:       {SourceVikunja, SourceWiki, SourceGitHub},
	KindPROpened:           {SourceGitHub},
	KindReviewSubmitted:    {SourceGitHub},
	KindPRMerged:           {SourceGitHub},
	KindPRClosed:           {SourceGitHub},
}

// IDKind says where an event id comes from.
type IDKind string

// The kinds of event id.
const (
	// IDSource: the source's own stable id for the record (a comment id,
	// a review id, a history version id).
	IDSource IDKind = "source"
	// IDFingerprint: the source has no id for it; see Fingerprint.
	IDFingerprint IDKind = "fingerprint"
)

// ActorKind is what kind of party an account or executor is.
type ActorKind string

// The kinds of actor.
const (
	ActorPerson  ActorKind = "person"
	ActorAgent   ActorKind = "agent"
	ActorService ActorKind = "service"
	ActorUnknown ActorKind = "unknown"
)

func (k ActorKind) valid() bool {
	switch k {
	case ActorPerson, ActorAgent, ActorService, ActorUnknown:
		return true
	}
	return false
}

// Account is the account a source says acted. A state seen by polling
// has no known actor: its Kind is ActorUnknown and its Login empty.
type Account struct {
	Login string    `json:"login,omitempty"`
	Kind  ActorKind `json:"kind"`
}

func (a Account) validate() error {
	if !a.Kind.valid() || (a.Login == "") != (a.Kind == ActorUnknown) {
		return errors.New("actor: a login and a kind, or kind unknown without a login, are required")
	}
	return nil
}

// Executor is who or what actually acted, when known and different from
// what the account suggests: an agent can post as the owner's GitHub user.
type Executor struct {
	Name string    `json:"name"`
	Kind ActorKind `json:"kind"`
}

// Related holds the keys the source record itself names: the task a
// comment is on, the pull request a review belongs to. A link the
// collector derives by joining records is a Correlation instead.
type Related struct {
	Task *TaskKey `json:"task,omitempty"`
	Page *PageKey `json:"page,omitempty"`
	Run  *RunKey  `json:"run,omitempty"`
	PR   *PRKey   `json:"pr,omitempty"`
}

// Evidence points to the source record behind a fact.
type Evidence struct {
	Source   Source `json:"source"`
	Instance string `json:"instance"`
	// Ref locates the record in the source: an API path, a GraphQL field,
	// a Kubernetes object.
	Ref string `json:"ref"`
	// Note says what the record shows, when Ref alone does not.
	Note string `json:"note,omitempty"`
}

// Event is one thing that happened, as one source recorded it.
type Event struct {
	Source   Source `json:"source"`
	Instance string `json:"instance"`
	ID       string `json:"id"`
	IDKind   IDKind `json:"idKind"`
	// Kind is what happened.
	Kind EventKind `json:"kind"`
	// SourceTime is when the source says it happened; nil for a state seen
	// by polling.
	SourceTime *time.Time `json:"sourceTime,omitempty"`
	// ObservedAt is when the collector read it.
	ObservedAt time.Time `json:"observedAt"`
	Actor      Account   `json:"actor"`
	Executor   *Executor `json:"executor,omitempty"`
	Related    Related   `json:"related"`
	// Details holds safe, short attributes: never content, prompts or
	// secrets.
	Details  map[string]string `json:"details,omitempty"`
	Evidence Evidence          `json:"evidence"`
}

// Key returns the event's identity: source, instance and id.
func (e Event) Key() string {
	return string(e.Source) + "/" + e.Instance + "/" + e.ID
}

// Ref returns a reference to the event.
func (e Event) Ref() Ref { return Ref{Kind: RefEvent, ID: e.Key()} }

// Time is the event's place on a timeline: its source time when it has
// one, else when it was observed.
func (e Event) Time() time.Time {
	if e.SourceTime != nil {
		return *e.SourceTime
	}
	return e.ObservedAt
}

// Validate reports whether the event is complete and self-consistent.
func (e Event) Validate() error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("event %s: %w", e.Key(), err)
	}
	return nil
}

func (e Event) validate() error {
	if !e.Source.valid() || e.Instance == "" || e.ID == "" {
		return errors.New("source, instance and id are required")
	}
	if err := e.validateID(); err != nil {
		return err
	}
	if _, ok := kindSources[e.Kind]; !ok {
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	if !kindFrom(e.Kind, e.Source) {
		return fmt.Errorf("kind %q cannot come from %s", e.Kind, e.Source)
	}
	if e.ObservedAt.IsZero() {
		return errors.New("observed time is required")
	}
	if err := e.Actor.validate(); err != nil {
		return err
	}
	if e.Executor != nil && (e.Executor.Name == "" || !e.Executor.Kind.valid()) {
		return errors.New("executor name and kind are required")
	}
	if err := e.Related.validate(); err != nil {
		return err
	}
	return validateEvidence([]Evidence{e.Evidence})
}

func (e Event) validateID() error {
	switch e.IDKind {
	case IDSource:
		return nil
	case IDFingerprint:
		if !isFingerprint(e.ID) {
			return fmt.Errorf("id %q is not a fingerprint", e.ID)
		}
		return nil
	}
	return fmt.Errorf("unknown id kind %q", e.IDKind)
}

func kindFrom(kind EventKind, source Source) bool {
	return slices.Contains(kindSources[kind], source)
}

func (r Related) validate() error {
	var errs []error
	if r.Task != nil {
		errs = append(errs, r.Task.Validate())
	}
	if r.Page != nil {
		errs = append(errs, r.Page.Validate())
	}
	if r.Run != nil {
		errs = append(errs, r.Run.Validate())
	}
	if r.PR != nil {
		errs = append(errs, r.PR.Validate())
	}
	return errors.Join(errs...)
}

func (e *Event) normalize() {
	if e.SourceTime != nil {
		t := e.SourceTime.UTC()
		e.SourceTime = &t
	}
	e.ObservedAt = e.ObservedAt.UTC()
}

func validateEvidence(evidence []Evidence) error {
	if len(evidence) == 0 {
		return errors.New("evidence is required")
	}
	for _, ev := range evidence {
		if !ev.Source.valid() || ev.Instance == "" || ev.Ref == "" {
			return errors.New("evidence needs a source, an instance and a ref")
		}
	}
	return nil
}
