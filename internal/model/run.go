package model

import (
	"errors"
	"fmt"
)

// RunRole is why a run was started.
type RunRole string

// The roles of a run.
const (
	// RoleImplementation: the first run on a Vikunja task, expected to end
	// in a pull request.
	RoleImplementation RunRole = "implementation"
	// RoleDiscussion: the first run on a Vikunja task labelled discuss.
	// Its deliverable is a comment or a wiki page, never a pull request.
	RoleDiscussion RunRole = "discussion"
	// RoleTaskFollowUp: started by a comment on a Vikunja task.
	RoleTaskFollowUp RunRole = "task-follow-up"
	// RolePRFollowUp: started by a comment or review on a pull request.
	RolePRFollowUp RunRole = "pr-follow-up"
	// RoleWikiFollowUp: started by a comment on a wiki page.
	RoleWikiFollowUp RunRole = "wiki-follow-up"
	// RoleConflictFix: started to resolve a pull request's merge conflict.
	RoleConflictFix RunRole = "conflict-fix"
	// RoleUnknown: no evidence of the role.
	RoleUnknown RunRole = "unknown"
)

func (r RunRole) valid() bool {
	switch r {
	case RoleImplementation, RoleDiscussion, RoleTaskFollowUp, RolePRFollowUp,
		RoleWikiFollowUp, RoleConflictFix, RoleUnknown:
		return true
	}
	return false
}

// Outcome is how a run ended, as far as the evidence says.
type Outcome string

// The outcomes of a run.
const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	// OutcomeRunning: not finished at the last successful read. The run's
	// end is open.
	OutcomeRunning Outcome = "running"
	// OutcomeUnknown: no evidence of how, or whether, it ended.
	OutcomeUnknown Outcome = "unknown"
)

func (o Outcome) valid() bool {
	switch o {
	case OutcomeSucceeded, OutcomeFailed, OutcomeRunning, OutcomeUnknown:
		return true
	}
	return false
}

// Run is one Kelos Task attempt. Its interval is the controller's
// lifecycle (start to completion), not a measure of effort.
type Run struct {
	Key RunKey `json:"key"`
	// TaskName is the Kelos Task name. A retry reuses it, so it is a
	// display label and a correlation hint, never an identity.
	TaskName string `json:"taskName,omitempty"`
	// Pod is the run's pod, when known; announcements name it.
	Pod string `json:"pod,omitempty"`
	// Agent is the agent profile (variant) that ran, when known. Several
	// runs of one profile can run at the same time.
	Agent    string     `json:"agent,omitempty"`
	Role     RunRole    `json:"role"`
	Interval Interval   `json:"interval"`
	Outcome  Outcome    `json:"outcome"`
	Evidence []Evidence `json:"evidence"`
}

// Validate reports whether the run is complete and self-consistent.
func (r Run) Validate() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("run %s: %w", r.Key, err)
	}
	return nil
}

func (r Run) validate() error {
	if err := r.Key.Validate(); err != nil {
		return err
	}
	if r.Key.Pod != "" && r.Pod != r.Key.Pod {
		return errors.New("a run keyed by pod must name the same pod")
	}
	if !r.Role.valid() {
		return fmt.Errorf("unknown role %q", r.Role)
	}
	if !r.Outcome.valid() {
		return fmt.Errorf("unknown outcome %q", r.Outcome)
	}
	if err := r.Interval.Validate(); err != nil {
		return err
	}
	if (r.Outcome == OutcomeRunning) != (r.Interval.End.State == BoundOpen) {
		return errors.New("a running run, and only a running run, has an open end")
	}
	return validateEvidence(r.Evidence)
}

func (r *Run) normalize() {
	r.Interval.normalize()
	sortEvidence(r.Evidence)
}
