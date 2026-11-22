package cost

import (
	"cmp"
	"maps"
	"slices"

	"github.com/sperano/sillage/internal/model"
)

// modelKey is one model at one tier.
type modelKey struct {
	runner model.Runner
	model  string
	tier   Tier
}

// modelViews accumulates usage by model.
type modelViews struct {
	accs    map[modelKey]*acc
	entries map[modelKey]map[string]bool
}

func newModelViews() *modelViews {
	return &modelViews{accs: map[modelKey]*acc{}, entries: map[modelKey]map[string]bool{}}
}

// add adds e's contribution p. A reported cost without a model belongs to
// its runner's unnamed model.
func (v *modelViews) add(e model.UsageEvent, p priced) {
	k := modelKey{runner: e.Runner, model: modelID(e.Model), tier: p.tier}
	if v.accs[k] == nil {
		v.accs[k] = newAcc()
		v.entries[k] = map[string]bool{}
	}
	v.accs[k].add(p.acc)
	if p.entry != "" {
		v.entries[k][p.entry] = true
	}
}

func (v *modelViews) list() []ModelCost {
	out := make([]ModelCost, 0, len(v.accs))
	for k, a := range v.accs {
		out = append(out, ModelCost{Runner: k.runner, Model: k.model, Tier: k.tier,
			Prices: slices.Sorted(maps.Keys(v.entries[k])), Breakdown: a.breakdown()})
	}
	slices.SortFunc(out, func(a, b ModelCost) int {
		return cmp.Or(cmp.Compare(a.Runner, b.Runner), cmp.Compare(a.Model, b.Model), cmp.Compare(a.Tier, b.Tier))
	})
	return out
}

// threadKey is one session or conversation's main loop or subagent.
type threadKey struct {
	runner        model.Runner
	stream, agent string
}

// threadViews accumulates usage by thread.
type threadViews struct {
	accs    map[threadKey]*acc
	parents map[threadKey]string
}

func newThreadViews() *threadViews {
	return &threadViews{accs: map[threadKey]*acc{}, parents: map[threadKey]string{}}
}

func (v *threadViews) add(e model.UsageEvent, p priced) {
	k := threadKey{runner: e.Runner, stream: e.Stream, agent: e.Agent}
	if v.accs[k] == nil {
		v.accs[k] = newAcc()
	}
	v.accs[k].add(p.acc)
	if e.ParentAgent != "" {
		v.parents[k] = e.ParentAgent
	}
}

func (v *threadViews) list() []ThreadCost {
	out := make([]ThreadCost, 0, len(v.accs))
	for k, a := range v.accs {
		out = append(out, ThreadCost{Runner: k.runner, Stream: k.stream, Agent: k.agent,
			ParentAgent: v.parents[k], Breakdown: a.breakdown()})
	}
	slices.SortFunc(out, func(a, b ThreadCost) int {
		return cmp.Or(cmp.Compare(a.Runner, b.Runner), cmp.Compare(a.Stream, b.Stream), cmp.Compare(a.Agent, b.Agent))
	})
	return out
}
