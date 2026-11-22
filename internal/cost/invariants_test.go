package cost_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/sperano/sillage/internal/cost"
)

// sum is what a set of breakdowns adds up to.
type sum struct {
	estimated, reported big.Rat
	requests, failed    int
	tokens              map[cost.Category]int64
}

func (s *sum) String() string {
	return fmt.Sprintf("estimated %s reported %s requests %d failed %d tokens %v",
		s.estimated.FloatString(6), s.reported.FloatString(6), s.requests, s.failed, s.tokens)
}

func addUSD(t *testing.T, r *big.Rat, s string) {
	t.Helper()
	if s == "" {
		return
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("amount %q is not a decimal", s)
	}
	r.Add(r, v)
}

func sumOf(t *testing.T, bs ...cost.Breakdown) *sum {
	t.Helper()
	s := &sum{tokens: map[cost.Category]int64{}}
	for _, b := range bs {
		addUSD(t, &s.estimated, b.EstimatedUSD)
		addUSD(t, &s.reported, b.ReportedUSD)
		s.requests += b.Requests
		s.failed += b.FailedRequests
		for _, c := range b.Categories {
			if c.Tokens != 0 {
				s.tokens[c.Category] += c.Tokens
			}
		}
	}
	return s
}

func samePartition(t *testing.T, what string, whole cost.Breakdown, parts []cost.Breakdown) {
	t.Helper()
	w, p := sumOf(t, whole), sumOf(t, parts...)
	if w.String() != p.String() {
		t.Errorf("%s: the parts add up to %v, not the whole's %v", what, p, w)
	}
}

func breakdowns[T any](xs []T, of func(T) cost.Breakdown) []cost.Breakdown {
	out := make([]cost.Breakdown, 0, len(xs))
	for _, x := range xs {
		out = append(out, of(x))
	}
	return out
}

// TestEveryEventCountsOnce checks, of every fixture, that each list of a
// task's cost partitions its parent: no usage is counted twice or lost.
func TestEveryEventCountsOnce(t *testing.T) {
	tab := defaultTable(t)
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			tc := cost.Compute(tab, loadInput(t, name))
			runs := breakdowns(tc.Runs, func(r cost.RunCost) cost.Breakdown { return r.Breakdown })
			if tc.Unattributed != nil {
				runs = append(runs, *tc.Unattributed)
			}
			samePartition(t, "runs", tc.Total, runs)
			samePartition(t, "models", tc.Total, breakdowns(tc.ByModel, func(m cost.ModelCost) cost.Breakdown { return m.Breakdown }))
			roles := breakdowns(tc.ByRole, func(r cost.RoleCost) cost.Breakdown { return r.Breakdown })
			if tc.Unattributed != nil {
				roles = append(roles, *tc.Unattributed)
			}
			samePartition(t, "roles", tc.Total, roles)
			for _, r := range tc.Runs {
				samePartition(t, r.Key.UID+" attempts", r.Breakdown, breakdowns(r.Attempts, func(a cost.AttemptCost) cost.Breakdown { return a.Breakdown }))
				samePartition(t, r.Key.UID+" models", r.Breakdown, breakdowns(r.Models, func(m cost.ModelCost) cost.Breakdown { return m.Breakdown }))
				samePartition(t, r.Key.UID+" threads", r.Breakdown, breakdowns(r.Threads, func(th cost.ThreadCost) cost.Breakdown { return th.Breakdown }))
			}
			catSum := new(big.Rat)
			for _, c := range tc.Total.Categories {
				addUSD(t, catSum, c.EstimatedUSD)
			}
			if total := sumOf(t, tc.Total); catSum.Cmp(&total.estimated) != 0 {
				t.Errorf("categories add up to %s, not the total's %s", catSum.FloatString(6), total.estimated.FloatString(6))
			}
		})
	}
}
