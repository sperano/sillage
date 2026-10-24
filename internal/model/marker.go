package model

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// MarkerVersion is the version of the phase-marker wire format a
// MarkerReport holds. A new version is a new contract: the old one stays
// accepted until no emitter sends it.
const MarkerVersion = 1

// MarkerEmitter is the wrapper that sent a phase marker.
type MarkerEmitter string

// The emitters of phase markers.
const (
	EmitterWaitForBlockers MarkerEmitter = "wait_for_blockers"
	EmitterWikiGQL         MarkerEmitter = "wiki_gql"
	EmitterEntrypoint      MarkerEmitter = "entrypoint"
)

// MarkerKind is what part of a run a phase marker's span covers.
type MarkerKind string

// The kinds of span. There is no per-call wiki request: a run makes
// dozens, and a login is the one call that makes a run look stalled.
const (
	// MarkerBlockerWait: wait-for-blockers holding the run until the tasks
	// it is blocked by are done. Kelos counts it as running.
	MarkerBlockerWait MarkerKind = "blocker-wait"
	// MarkerWikiLogin: a wiki login, with its retries.
	MarkerWikiLogin MarkerKind = "wiki-login"
	// MarkerAgent: the agent process.
	MarkerAgent MarkerKind = "agent"
)

// MarkerEdge is which end of its span a marker records.
type MarkerEdge string

// The edges of a span. Version 1 has no point markers.
const (
	EdgeStart MarkerEdge = "start"
	EdgeEnd   MarkerEdge = "end"
)

// MarkerOutcome is how a span ended, on its end edge only.
type MarkerOutcome string

// The outcomes of a span, each allowed for one kind (markerKinds).
const (
	MarkerUnblocked   MarkerOutcome = "unblocked"
	MarkerTaskMissing MarkerOutcome = "task-missing"
	MarkerTimedOut    MarkerOutcome = "timed-out"
	MarkerFailed      MarkerOutcome = "failed"

	MarkerLoginOK          MarkerOutcome = "ok"
	MarkerLoginRejected    MarkerOutcome = "rejected"
	MarkerLoginUnavailable MarkerOutcome = "unavailable"

	MarkerSucceeded  MarkerOutcome = "succeeded"
	MarkerLimitHit   MarkerOutcome = "limit-hit"
	MarkerTerminated MarkerOutcome = "terminated"
)

// markerKind is what one kind allows.
type markerKind struct {
	emitter  MarkerEmitter
	outcomes []MarkerOutcome
	// attempts is set for the kind whose end counts its attempts.
	attempts bool
}

// markerKinds are the kinds, each with its one emitter and its outcomes.
var markerKinds = map[MarkerKind]markerKind{
	MarkerBlockerWait: {emitter: EmitterWaitForBlockers,
		outcomes: []MarkerOutcome{MarkerUnblocked, MarkerTaskMissing, MarkerTimedOut, MarkerFailed}},
	MarkerWikiLogin: {emitter: EmitterWikiGQL, attempts: true,
		outcomes: []MarkerOutcome{MarkerLoginOK, MarkerLoginRejected, MarkerLoginUnavailable}},
	MarkerAgent: {emitter: EmitterEntrypoint,
		outcomes: []MarkerOutcome{MarkerSucceeded, MarkerFailed, MarkerLimitHit, MarkerTerminated}},
}

// spanPattern is a span id: 128 random bits in lowercase hex.
var spanPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// objectNamePattern is a Kubernetes object name (a DNS-1123 subdomain),
// at most maxObjectNameLength long: what a Kelos Task is named.
var objectNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// maxObjectNameLength bounds a Kubernetes object name.
const maxObjectNameLength = 253

// MarkerReport is one phase marker as a pod reports it: one edge of one
// span, in wire format version 1. It never holds content (no prompt,
// argument, page, query, error text or host name), and nothing in it
// identifies the pod or the run: what sent it is the token's, which
// MarkerSender holds.
type MarkerReport struct {
	Version int           `json:"version"`
	Emitter MarkerEmitter `json:"emitter"`
	Kind    MarkerKind    `json:"kind"`
	Edge    MarkerEdge    `json:"edge"`
	// Span is the span's id, generated at its start and repeated at its
	// end.
	Span string `json:"span"`
	// At is when the edge happened, by the pod's clock.
	At time.Time `json:"at"`
	// TaskName is the Kelos Task name the pod was given: a hint for the
	// join by name, never an identity.
	TaskName string `json:"taskName,omitempty"`
	// Outcome is how the span ended, on an end edge only, where it is
	// required.
	Outcome MarkerOutcome `json:"outcome,omitempty"`
	// Attempts is how many attempts a wiki login took, on its end edge
	// only, where it is required.
	Attempts *int `json:"attempts,omitempty"`
}

// Validate reports whether the report is a version 1 marker whose every
// value is in its closed list, with what its kind and edge require.
func (r MarkerReport) Validate() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("marker %s %s %s: %w", r.Kind, r.Edge, r.Span, err)
	}
	return nil
}

func (r MarkerReport) validate() error {
	if r.Version != MarkerVersion {
		return fmt.Errorf("unknown version %d", r.Version)
	}
	kind, ok := markerKinds[r.Kind]
	switch {
	case !ok:
		return fmt.Errorf("unknown kind %q", r.Kind)
	case r.Emitter != kind.emitter:
		return fmt.Errorf("kind %q is emitted by %q, not %q", r.Kind, kind.emitter, r.Emitter)
	case !spanPattern.MatchString(r.Span):
		return errors.New("a span is 32 lowercase hexadecimal digits")
	case r.At.IsZero():
		return errors.New("its time is required")
	}
	if r.TaskName != "" && (len(r.TaskName) > maxObjectNameLength || !objectNamePattern.MatchString(r.TaskName)) {
		return fmt.Errorf("task name %q is no object name", r.TaskName)
	}
	switch r.Edge {
	case EdgeStart:
		if r.Outcome != "" || r.Attempts != nil {
			return errors.New("a start has no outcome and no attempts")
		}
		return nil
	case EdgeEnd:
		return kind.validateEnd(r)
	}
	return fmt.Errorf("unknown edge %q", r.Edge)
}

// validateEnd checks an end's outcome, and its attempts where the kind
// counts them.
func (k markerKind) validateEnd(r MarkerReport) error {
	if !slices.Contains(k.outcomes, r.Outcome) {
		return fmt.Errorf("outcome %q is not one of kind %q's", r.Outcome, r.Kind)
	}
	switch {
	case k.attempts && (r.Attempts == nil || *r.Attempts < 1):
		return fmt.Errorf("the end of kind %q counts its attempts, at least 1", r.Kind)
	case !k.attempts && r.Attempts != nil:
		return fmt.Errorf("kind %q counts no attempts", r.Kind)
	}
	return nil
}

// Equal reports whether r and o report the same edge with the same
// values, whatever the time zone of At.
func (r MarkerReport) Equal(o MarkerReport) bool {
	sameAttempts := (r.Attempts == nil) == (o.Attempts == nil) &&
		(r.Attempts == nil || *r.Attempts == *o.Attempts)
	return r.Version == o.Version && r.Emitter == o.Emitter && r.Kind == o.Kind && r.Edge == o.Edge &&
		r.Span == o.Span && r.At.Equal(o.At) && r.TaskName == o.TaskName && r.Outcome == o.Outcome &&
		sameAttempts
}

// MarkerSender is what sillage verified of the pod that sent a marker:
// a TokenReview of the pod's projected ServiceAccount token, never the
// body.
type MarkerSender struct {
	// Cluster is the name sillage gives the cluster it reviewed the token
	// in.
	Cluster        string `json:"cluster"`
	Namespace      string `json:"namespace"`
	ServiceAccount string `json:"serviceAccount"`
	Pod            string `json:"pod"`
	PodUID         string `json:"podUID"`
}

func (s MarkerSender) validate() error {
	if s.Cluster == "" || s.Namespace == "" || s.ServiceAccount == "" || s.Pod == "" || s.PodUID == "" {
		return errors.New("sender: cluster, namespace, ServiceAccount, pod and pod UID are required")
	}
	return nil
}

// Marker is a phase marker sillage received: what the pod reported, what
// sillage verified of the pod, and when it received it. It is identified
// by its pod UID, span and edge, so that a re-sent marker is one marker.
type Marker struct {
	Report     MarkerReport `json:"report"`
	Sender     MarkerSender `json:"sender"`
	ReceivedAt time.Time    `json:"receivedAt"`
	// Join is the run the marker was joined to; nil while it is
	// unattached.
	Join *MarkerJoin `json:"join,omitempty"`
}

// Validate reports whether the marker is complete and self-consistent.
func (m Marker) Validate() error {
	errs := []error{m.Report.Validate(), m.Sender.validate()}
	if m.ReceivedAt.IsZero() {
		errs = append(errs, errors.New("marker: the time it was received is required"))
	}
	if m.Join != nil {
		errs = append(errs, m.Join.validate())
	}
	return errors.Join(errs...)
}

// MarkerJoin is the run a marker was joined to, how strongly the evidence
// establishes it (ConfidenceIdentifier for its pod, ConfidenceHeuristic
// for its Task name and time) and that evidence.
type MarkerJoin struct {
	Run        RunKey     `json:"run"`
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
}

func (j MarkerJoin) validate() error {
	if err := j.Run.Validate(); err != nil {
		return fmt.Errorf("marker join: %w", err)
	}
	if j.Run.UID == "" {
		return errors.New("marker join: a marker joins a run by Task UID")
	}
	switch j.Confidence {
	case ConfidenceIdentifier, ConfidenceHeuristic:
	default:
		return fmt.Errorf("marker join: unknown confidence %q", j.Confidence)
	}
	return validateEvidence(j.Evidence)
}
