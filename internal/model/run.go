package model

import (
	"errors"
	"fmt"
	"math/big"
	"time"
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
	// RoleAutomation: bookkeeping started by automation (labelling an
	// opened pull request, closing the task of a merged one, reconciling
	// conflicts), not an agent asked to work on the task.
	RoleAutomation RunRole = "automation"
	// RoleUnknown: no evidence of the role.
	RoleUnknown RunRole = "unknown"
)

func (r RunRole) valid() bool {
	switch r {
	case RoleImplementation, RoleDiscussion, RoleTaskFollowUp, RolePRFollowUp,
		RoleWikiFollowUp, RoleConflictFix, RoleAutomation, RoleUnknown:
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

// Run is one Kelos Task attempt. Its times (Created and Interval) are the
// Kubernetes API server's and the Kelos controller's lifecycle times: when
// the Task object was created, and when the controller saw it start and
// complete. They are not CPU time, nor the timing of any phase of the
// agent's work, nor a measure of effort.
type Run struct {
	Key RunKey `json:"key"`
	// TaskName is the Kelos Task name. A retry reuses it, so it is a
	// display label and a correlation hint, never an identity.
	TaskName string `json:"taskName,omitempty"`
	// Pod is the run's pod, when known; announcements name it.
	Pod string `json:"pod,omitempty"`
	// Job is the Kubernetes Job the controller created for the run, when
	// known.
	Job string `json:"job,omitempty"`
	// Agent is the agent profile (variant) that ran, when known. Several
	// runs of one profile can run at the same time.
	Agent string `json:"agent,omitempty"`
	// Project is the project the run was started for, as its spawner
	// labelled it, when known.
	Project string  `json:"project,omitempty"`
	Role    RunRole `json:"role"`
	// Created is when the Task object was created, when known. A run can
	// wait (for a dependency, a branch lock, a budget) between its creation
	// and its start.
	Created *time.Time `json:"created,omitempty"`
	// Interval runs from the controller's start time to its completion
	// time.
	Interval Interval `json:"interval"`
	Outcome  Outcome  `json:"outcome"`
	// Message is the controller's last status message, when known.
	Message string `json:"message,omitempty"`
	// Model is the effective model the agent ran with, when known.
	Model string `json:"model,omitempty"`
	// Usage is what the run reported using, when it reported anything.
	Usage    *Usage     `json:"usage,omitempty"`
	Evidence []Evidence `json:"evidence"`
}

// Usage is the cost and token usage a run reported. Each field is unset
// when the run did not report it.
type Usage struct {
	// CostUSD is the reported cost in US dollars as a decimal string
	// ("0.42"), kept exact: it is never converted to a float.
	CostUSD      string `json:"costUSD,omitempty"`
	InputTokens  *int64 `json:"inputTokens,omitempty"`
	OutputTokens *int64 `json:"outputTokens,omitempty"`
}

func (u Usage) validate() error {
	if u.CostUSD != "" {
		cost, ok := new(big.Rat).SetString(u.CostUSD)
		if !ok || cost.Sign() < 0 {
			return fmt.Errorf("usage: cost %q is not a non-negative decimal", u.CostUSD)
		}
	}
	if (u.InputTokens != nil && *u.InputTokens < 0) || (u.OutputTokens != nil && *u.OutputTokens < 0) {
		return errors.New("usage: a token count is negative")
	}
	return nil
}

func (u *Usage) clone() *Usage {
	if u == nil {
		return nil
	}
	c := *u
	c.InputTokens = cloneInt(u.InputTokens)
	c.OutputTokens = cloneInt(u.OutputTokens)
	return &c
}

func cloneInt(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
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
	if r.Usage != nil {
		if err := r.Usage.validate(); err != nil {
			return err
		}
	}
	return validateEvidence(r.Evidence)
}

func (r *Run) normalize() {
	if r.Created != nil {
		t := r.Created.UTC()
		r.Created = &t
	}
	r.Interval.normalize()
	sortEvidence(r.Evidence)
}
