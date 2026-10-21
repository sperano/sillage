package model

import (
	"errors"
	"fmt"
)

// CheckConclusion is a check run's result.
type CheckConclusion string

// The conclusions of a check run, as GitHub names them, plus pending for
// one not completed.
const (
	CheckPending        CheckConclusion = "pending"
	CheckSuccess        CheckConclusion = "success"
	CheckFailure        CheckConclusion = "failure"
	CheckCancelled      CheckConclusion = "cancelled"
	CheckTimedOut       CheckConclusion = "timed_out"
	CheckNeutral        CheckConclusion = "neutral"
	CheckSkipped        CheckConclusion = "skipped"
	CheckActionRequired CheckConclusion = "action_required"
	// CheckStale: GitHub marked the check stale after it stayed incomplete
	// too long.
	CheckStale CheckConclusion = "stale"
)

func (c CheckConclusion) valid() bool {
	switch c {
	case CheckPending, CheckSuccess, CheckFailure, CheckCancelled, CheckTimedOut,
		CheckNeutral, CheckSkipped, CheckActionRequired, CheckStale:
		return true
	}
	return false
}

// CheckRun is one check on one commit. A later commit's checks never
// replace an earlier commit's: each SHA keeps its own results.
type CheckRun struct {
	// ID is GitHub's check run id.
	ID         int64           `json:"id"`
	SHA        string          `json:"sha"`
	Name       string          `json:"name"`
	Interval   Interval        `json:"interval"`
	Conclusion CheckConclusion `json:"conclusion"`
}

func (c CheckRun) validate() error {
	if c.ID <= 0 || c.SHA == "" || c.Name == "" {
		return errors.New("a positive id, a sha and a name are required")
	}
	if !c.Conclusion.valid() {
		return fmt.Errorf("unknown conclusion %q", c.Conclusion)
	}
	if err := c.Interval.Validate(); err != nil {
		return err
	}
	if (c.Conclusion == CheckPending) != (c.Interval.End.State == BoundOpen) {
		return errors.New("a pending check, and only a pending check, has an open end")
	}
	return nil
}

// PullRequest is a GitHub pull request and its checks per commit. Its
// opening, reviews, merge and closing are events.
type PullRequest struct {
	Key    PRKey      `json:"key"`
	Branch string     `json:"branch"`
	Checks []CheckRun `json:"checks"`
}

// Validate reports whether the pull request and its checks are complete.
func (p PullRequest) Validate() error {
	if err := p.Key.Validate(); err != nil {
		return err
	}
	if p.Branch == "" {
		return fmt.Errorf("pull request %s: branch is required", p.Key)
	}
	seen := make(map[int64]bool, len(p.Checks))
	for _, c := range p.Checks {
		if err := c.validate(); err != nil {
			return fmt.Errorf("pull request %s: check %d: %w", p.Key, c.ID, err)
		}
		if seen[c.ID] {
			return fmt.Errorf("pull request %s: duplicate check %d", p.Key, c.ID)
		}
		seen[c.ID] = true
	}
	return nil
}

// SHAs returns the commits that have checks, in the order of their checks.
func (p PullRequest) SHAs() []string {
	var shas []string
	seen := make(map[string]bool)
	for _, c := range p.Checks {
		if !seen[c.SHA] {
			seen[c.SHA] = true
			shas = append(shas, c.SHA)
		}
	}
	return shas
}

func (p *PullRequest) normalize() {
	for i := range p.Checks {
		p.Checks[i].Interval.normalize()
	}
	sortChecks(p.Checks)
}
