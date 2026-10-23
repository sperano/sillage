package collector

import (
	"slices"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// vikunjaEvents turns one read of a task into model events through the
// Vikunja source's own normalization, on the configured instance, with
// the agents' accounts marked as theirs: the source knows only the
// service account.
func (a *assembly) vikunjaEvents(s vikunja.Snapshot) ([]model.Event, error) {
	s.Instance = a.cfg.VikunjaInstance
	recs, err := vikunja.Normalize(s, a.cfg.VikunjaServiceAccount)
	if err != nil {
		return nil, err
	}
	for i := range recs.Events {
		if act := &recs.Events[i].Actor; act.Kind == model.ActorPerson {
			act.Kind = a.vikunjaAccount(&vikunja.User{Username: act.Login}).Kind
		}
	}
	return recs.Events, nil
}

// vikunjaAccount is the account of a Vikunja user: the service account's,
// an agent's, or a person's; unknown without a user.
func (a *assembly) vikunjaAccount(u *vikunja.User) model.Account {
	switch {
	case u == nil || u.Username == "":
		return model.Account{Kind: model.ActorUnknown}
	case u.Username == a.cfg.VikunjaServiceAccount:
		return model.Account{Login: u.Username, Kind: model.ActorService}
	case slices.Contains(a.cfg.VikunjaAgentUsers, u.Username):
		return model.Account{Login: u.Username, Kind: model.ActorAgent}
	}
	return model.Account{Login: u.Username, Kind: model.ActorPerson}
}

func taskObject(key model.TaskKey) string { return "tasks/" + strconv.FormatInt(key.ID, 10) }

func commentObject(id int64) string { return "comments/" + strconv.FormatInt(id, 10) }
