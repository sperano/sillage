package collector

import (
	"slices"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// vikunjaEvents turns one read of a task into model events. The source
// normalizes the read; the collector adds only what it alone knows: the
// instance its task keys name, and which accounts are agents
// (EnvVikunjaAgentUsers), which the source reads as persons.
func (a *assembly) vikunjaEvents(s vikunja.Snapshot) ([]model.Event, error) {
	s.Instance = a.cfg.VikunjaInstance
	recs, err := vikunja.Normalize(s, a.cfg.VikunjaServiceAccount)
	if err != nil {
		return nil, err
	}
	for i, e := range recs.Events {
		if e.Actor.Kind == model.ActorPerson && a.vikunjaAgent(e.Actor.Login) {
			recs.Events[i].Actor.Kind = model.ActorAgent
		}
	}
	return recs.Events, nil
}

func commentObject(id int64) string { return "comments/" + strconv.FormatInt(id, 10) }

// vikunjaAgent reports whether login is one of the configured agents'.
func (a *assembly) vikunjaAgent(login string) bool {
	return slices.Contains(a.cfg.VikunjaAgentUsers, login)
}
