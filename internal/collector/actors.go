package collector

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// actorKinds are the events an agent's account makes while it works: the
// run that made one is guessed from who made it and when.
var actorKinds = []model.EventKind{model.KindPageCreated, model.KindPageEdited, model.KindPROpened, model.KindComment}

// actorRuns joins each event an agent made to the runs of that agent
// running at the time, from the whole namespace: a heuristic, ambiguous
// when several were running. The agent is the event's executor, else its
// agent account's login and the agents its own start announcements name.
func (a *assembly) actorRuns() {
	if a.kelos == nil {
		return
	}
	announced := a.announcingAccounts()
	for _, kind := range actorKinds {
		for _, e := range a.eventsOf(kind) {
			agents := agentsOf(e, announced)
			if len(agents) == 0 || e.SourceTime == nil {
				continue
			}
			at := *e.SourceTime
			candidates := a.runsWhere(func(r model.Run) bool { return slices.Contains(agents, r.Agent) && during(r, at) })
			for _, r := range candidates {
				a.addRun(r)
				a.links.heuristic(e.Ref(), r.Key.Ref(), len(candidates) > 1, model.Evidence{
					Source: e.Source, Instance: e.Instance, Ref: e.Evidence.Ref,
					Note: actorNote(e, r.Agent, len(candidates)),
				})
			}
		}
	}
}

func actorNote(e model.Event, agent string, candidates int) string {
	who := e.Actor.Login
	if e.Executor != nil {
		who = e.Executor.Name
	}
	note := e.ID + " by " + who + " inside the run's interval; "
	if candidates > 1 {
		return note + strconv.Itoa(candidates) + " runs of " + agent + " were running"
	}
	return note + "no other run of " + agent + " with a known start was running"
}

// accountKey names an account of one source instance.
func accountKey(e model.Event) string {
	return string(e.Source) + "/" + e.Instance + "/" + e.Actor.Login
}

// announcingAccounts maps each agent account that posted a start
// announcement itself (a wiki follow-up's) to the agents they name.
func (a *assembly) announcingAccounts() map[string][]string {
	out := make(map[string][]string)
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		if e.Actor.Kind == model.ActorAgent && e.Executor != nil {
			key := accountKey(e)
			if !slices.Contains(out[key], e.Executor.Name) {
				out[key] = append(out[key], e.Executor.Name)
			}
		}
	}
	return out
}

// agentsOf returns the agents that may have made e.
func agentsOf(e model.Event, announced map[string][]string) []string {
	if e.Executor != nil && e.Executor.Kind == model.ActorAgent {
		return []string{e.Executor.Name}
	}
	if e.Actor.Kind != model.ActorAgent {
		return nil
	}
	agents := slices.Clone(announced[accountKey(e)])
	if login := strings.TrimSpace(e.Actor.Login); !slices.Contains(agents, login) {
		agents = append(agents, login)
	}
	return agents
}

// during reports whether r had started by at and not ended before it. A
// run with an unknown start is never a candidate: nothing places it.
func during(r model.Run, at time.Time) bool {
	start, ok := r.Interval.Start.Time()
	if !ok || at.Before(start) {
		return false
	}
	if r.Interval.End.State == model.BoundOpen {
		return true
	}
	end, ok := r.Interval.End.Time()
	return ok && !at.After(end)
}
