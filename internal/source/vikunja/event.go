package vikunja

import "time"

// Event is one normalized record read from Vikunja. It carries the fields
// the source-independent model needs (source instance and id, kind, source
// and observed times, actor, related keys, evidence) until internal/model
// defines its own Event; the conversion then lives here.
type Event struct {
	// Instance is the Vikunja base URL the record was read from.
	Instance string
	// SourceID is stable across reads of the same fact and distinct for
	// different facts: "task/<id>/created", "comment/<id>",
	// "task/<id>/done/<done_at>", "task/<id>/state/<observed at>".
	SourceID string
	Kind     Kind
	TaskID   int64
	// SourceTime is when Vikunja says the thing happened. The zero value
	// means Vikunja gives no time for it: unknown, never "now" or "epoch".
	SourceTime time.Time
	// ObservedAt is when sillage read the record.
	ObservedAt time.Time
	// Actor is the account Vikunja attributes the record to; nil when
	// Vikunja does not say who (a task being marked done, a state).
	Actor *User
	// Title is the task title, on KindTaskCreated only.
	Title string
	// CommentID is set on KindComment and KindRunAnnounced.
	CommentID int64
	// Run is set on KindRunAnnounced.
	Run *Announcement
	// State is set on KindStateObserved.
	State *State
	// Flags mark what a consumer must not take at face value.
	Flags    []Flag
	Evidence Evidence
}

// Kind names what an Event records.
type Kind string

const (
	// KindTaskCreated is the task's creation, by its creator.
	KindTaskCreated Kind = "vikunja.task.created"
	// KindComment is a comment, with its author and time.
	KindComment Kind = "vikunja.comment"
	// KindRunAnnounced is the service account's comment announcing that a
	// Kelos Task started on the task.
	KindRunAnnounced Kind = "vikunja.run.announced"
	// KindTaskDone is the task's current done mark. Vikunja keeps only the
	// latest one: a task done, reopened and done again shows the last time.
	KindTaskDone Kind = "vikunja.task.done"
	// KindStateObserved is a snapshot of the task's buckets, labels and
	// assignees as of ObservedAt. It says what held then, not when it
	// started to: Vikunja keeps no history of those changes.
	KindStateObserved Kind = "vikunja.task.state_observed"
)

// Flag marks a record a consumer must not take at face value.
type Flag string

const (
	// FlagUnparsedAnnouncement marks a service-account comment that reads
	// like a start announcement but whose Kelos Task could not be parsed.
	// It is kept as a plain KindComment.
	FlagUnparsedAnnouncement Flag = "unparsed_announcement"
	// FlagEdited marks a comment edited after it was posted: its body is
	// the latest version, not necessarily what was posted at SourceTime.
	FlagEdited Flag = "edited"
	// FlagDoneTimeUnknown marks a task Vikunja reports as done without a
	// done_at time.
	FlagDoneTimeUnknown Flag = "done_time_unknown"
)

// Role is the kind of Kelos run an announcement starts, as its wording says.
type Role string

const (
	// RoleImplementation is the first run on a task.
	RoleImplementation Role = "implementation"
	// RoleTaskFollowUp is a run started by a follow-up comment on the task.
	RoleTaskFollowUp Role = "task_follow_up"
)

// Announcement is what a Kelos start announcement says about its run. It
// names the Kelos Task, not its UID: a retry can reuse the name, so the
// Kelos source must resolve it (the pod name is per attempt) before it
// identifies a run.
type Announcement struct {
	Role Role
	// Agent and Model are empty when the announcement does not name them.
	Agent string
	Model string
	// Namespace is the Kelos Task's namespace; empty if not stated.
	Namespace string
	TaskName  string
	// Pod is empty when the announcement names no pod.
	Pod string
}

// State is a task's state as seen at one read.
type State struct {
	Done bool
	// Buckets holds one bucket per Kanban view; empty when Vikunja did not
	// report any.
	Buckets   []Bucket
	Labels    []string
	Assignees []User
}

// Evidence points at the API resource and field a record was read from.
type Evidence struct {
	// Path is the API path, without the base URL and query.
	Path string
	// Field names the JSON field(s) the record's time or content come from.
	Field string
}
