package kelos

import (
	"context"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// relist lists both resources into a new state and commits every run and
// an observation of every object listed. After an expired watch (prev not
// nil), it keeps prev's Tasks the list no longer has as deleted at an
// unknown time, and records a gap from where prev's slower watch stood to
// the relist: both watches miss what came between.
func (w *Watcher) relist(ctx context.Context, prev *state) (*state, error) {
	s, err := readSnapshot(ctx, w.client, w.cfg, w.now)
	if err != nil {
		return nil, err
	}
	if s.TasksVersion == "" || s.RecordsVersion == "" {
		return nil, errors.New("kelos: a list has no resourceVersion to watch from")
	}
	changed, err := pair(w.cfg, s)
	if err != nil {
		return nil, err
	}
	st := &state{attempts: make(map[types.UID]*attempt, len(changed)),
		tasksVersion: s.TasksVersion, recordsVersion: s.RecordsVersion,
		tasksSeenAt: s.ObservedAt, recordsSeenAt: s.ObservedAt}
	for _, a := range changed {
		st.attempts[a.uid] = a
	}
	var gaps []model.Gap
	if prev != nil {
		changed = append(changed, st.carryOver(prev, s.ObservedAt)...)
		gaps = expiredGap(prev.knownUntil(), s.ObservedAt)
	}
	return st, w.commit(ctx, st, changed, w.cfg.listed(s), gaps)
}

// carryOver keeps what prev held that is missing from the relisted state.
// A TaskRecord deleted meanwhile keeps its values in its run, as a watched
// deletion leaves them. A Task was deleted at an unknown time, so it keeps
// its run with its last observed values. It returns the attempts newly
// found deleted.
func (s *state) carryOver(prev *state, at time.Time) []*attempt {
	var changed []*attempt
	for uid, old := range prev.attempts {
		cur := s.attempts[uid]
		if cur != nil && cur.task != nil {
			if cur.record == nil {
				cur.record = old.record
			}
			continue // still listed
		}
		if old.task == nil {
			continue
		}
		a := s.attempt(uid)
		if a.record == nil {
			a.record = old.record
		}
		a.task, a.deleted = old.task, old.deleted
		if a.deleted == nil {
			a.deleted = &Deletion{At: at}
			changed = append(changed, a)
		}
	}
	return changed
}

// expiredGap is the span a relist cannot recover, when it has a length.
func expiredGap(lastEvent, relist time.Time) []model.Gap {
	if !relist.After(lastEvent) {
		return nil
	}
	return []model.Gap{{Start: lastEvent, End: relist, Reason: GapWatchExpired}}
}
