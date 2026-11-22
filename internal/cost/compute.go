// Package cost estimates what a task's agent runs would have cost at the
// providers' public API prices, from the usage events their runners
// exported: by run, attempt, model, thread and category, with the
// runners' own reported cost kept apart. It is pure: a story's runs and
// usage events in, a TaskCost out.
//
// Every usage event counts once: events are deduplicated by their
// identity, and each is attributed to at most one run (by its pod, else by
// its Task name and time), so that every list of a TaskCost partitions its
// parent's total. Usage that is missing, or has no known price, makes an
// estimate partial or unknown, never zero.
package cost

import (
	"cmp"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// FinalGrace is how long after a run ends its usage events may still
// arrive: the exporters' batches and retries, the telemetry collector's
// batch, and the usage feed's delay and overlap. A run's cost is final
// once the feed has read that far past its end.
const FinalGrace = 20 * time.Minute

// Feed is what the usage feed has read.
type Feed struct {
	// On says that a usage feed reads events into the store.
	On bool `json:"on"`
	// Since is when the runners' usage telemetry started; zero when
	// unknown.
	Since *time.Time `json:"since,omitempty"`
	// ReadThrough: the feed has read every event before it; nil when it
	// never read.
	ReadThrough *time.Time `json:"readThrough,omitempty"`
}

// Input is what a task's cost is computed from.
type Input struct {
	Story  model.Story
	Events []model.UsageEvent
	// Pods are the pods each run keyed by Task UID was seen with.
	Pods map[model.RunKey][]string
	Feed Feed
}

// TaskCost is a task's estimated API-equivalent cost.
type TaskCost struct {
	Root        model.Root   `json:"root"`
	Prices      PricesRef    `json:"prices"`
	Assumptions []Assumption `json:"assumptions"`
	Feed        Feed         `json:"feed"`
	// Final says that every run has ended and the feed has read FinalGrace
	// past each one's end: no more usage is expected.
	Final bool      `json:"final"`
	Total Breakdown `json:"total"`
	// ByRole partitions the usage attributed to runs: with Unattributed,
	// it adds up to Total.
	ByRole  []RoleCost  `json:"byRole"`
	ByModel []ModelCost `json:"byModel"`
	Runs    []RunCost   `json:"runs"`
	// Unattributed is the usage of the task's Task names that no single
	// run could be joined to; counted in Total, absent when there is none.
	Unattributed *Breakdown `json:"unattributed,omitempty"`
}

// PricesRef names the price table an estimate used.
type PricesRef struct {
	Version    string   `json:"version"`
	VerifiedAt string   `json:"verifiedAt"`
	Sources    []Source `json:"sources"`
}

// RunCost is one run's estimate.
type RunCost struct {
	Key      model.RunKey  `json:"key"`
	TaskName string        `json:"taskName,omitempty"`
	Role     model.RunRole `json:"role"`
	Outcome  model.Outcome `json:"outcome"`
	// Model is the model the run was configured with, when known; the
	// models it used are in Models.
	Model string     `json:"model,omitempty"`
	Ended *time.Time `json:"ended,omitempty"`
	Final bool       `json:"final"`
	Breakdown
	Attempts []AttemptCost `json:"attempts"`
	Models   []ModelCost   `json:"models"`
	Threads  []ThreadCost  `json:"threads"`
}

// AttemptCost is the usage of one of a run's pods: a retried Job has one
// per attempt.
type AttemptCost struct {
	Pod string `json:"pod"`
	Breakdown
}

// ModelCost is the usage of one model at one tier.
type ModelCost struct {
	Runner model.Runner `json:"runner"`
	// Model is the model the runner reported, as the price table names
	// it; empty when not reported.
	Model string `json:"model"`
	Tier  Tier   `json:"tier,omitempty"`
	// Prices are the ids of the price entries that priced it.
	Prices []string `json:"prices"`
	Breakdown
}

// ThreadCost is the usage of one session or conversation, and within a
// Claude Code session, of its main loop or one subagent.
type ThreadCost struct {
	Runner model.Runner `json:"runner"`
	Stream string       `json:"stream"`
	// Agent is the subagent; empty for the main loop.
	Agent       string `json:"agent,omitempty"`
	ParentAgent string `json:"parentAgent,omitempty"`
	Breakdown
}

// RoleCost is the usage of the runs of one role.
type RoleCost struct {
	Role model.RunRole `json:"role"`
	Runs int           `json:"runs"`
	Breakdown
}

// Compute returns in's task cost under t.
func Compute(t *Table, in Input) TaskCost {
	runs := in.Story.Runs
	att := attribute(dedupe(in.Events), runs, in.Pods)
	total, unattributed := newAcc(), newAcc()
	models := newModelViews()
	out := TaskCost{
		Root: in.Story.Root, Prices: PricesRef{Version: t.Version, VerifiedAt: t.VerifiedAt, Sources: t.Sources},
		Assumptions: t.Assumptions, Feed: in.Feed, Final: len(runs) > 0, Runs: make([]RunCost, 0, len(runs)),
	}
	roles := map[model.RunRole]*acc{}
	roleRuns := map[model.RunRole]int{}
	for i, r := range runs {
		rc, ra := runCost(t, r, att.byRun[i], att.byName[i], in.Pods[r.Key], in.Feed, models)
		out.Runs = append(out.Runs, rc)
		out.Final = out.Final && rc.Final
		total.add(ra)
		if roles[r.Role] == nil {
			roles[r.Role] = newAcc()
		}
		roles[r.Role].add(ra)
		roleRuns[r.Role]++
	}
	for _, e := range att.none {
		p := priceEvent(t, e)
		unattributed.add(p.acc)
		models.add(e, p)
	}
	if len(att.none) > 0 {
		total.add(unattributed)
		total.note(NoteUnattributed)
		b := unattributed.breakdown()
		out.Unattributed = &b
	}
	if !in.Feed.On {
		total.note(NoteFeedOff)
	}
	out.Total = total.breakdown()
	out.ByRole = roleCosts(roles, roleRuns)
	out.ByModel = models.list()
	return out
}

// dedupe returns the events in time order, each identity once: the first
// of its deliveries.
func dedupe(events []model.UsageEvent) []model.UsageEvent {
	out := slices.Clone(events)
	slices.SortStableFunc(out, func(a, b model.UsageEvent) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.PodUID, b.PodUID), cmp.Compare(a.Event, b.Event),
			cmp.Compare(a.Stream, b.Stream), cmp.Compare(a.Seq, b.Seq))
	})
	seen := make(map[model.UsageKey]bool, len(out))
	kept := out[:0]
	for _, e := range out {
		if !seen[e.Key()] {
			seen[e.Key()] = true
			kept = append(kept, e)
		}
	}
	return kept
}

func roleCosts(roles map[model.RunRole]*acc, runs map[model.RunRole]int) []RoleCost {
	out := make([]RoleCost, 0, len(roles))
	for role, a := range roles {
		out = append(out, RoleCost{Role: role, Runs: runs[role], Breakdown: a.breakdown()})
	}
	slices.SortFunc(out, func(a, b RoleCost) int { return cmp.Compare(a.Role, b.Role) })
	return out
}
