package kelos

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// attempt is one Task UID's objects: its Task, its TaskRecord, or both.
type attempt struct {
	uid    types.UID
	task   *Task
	record *TaskRecord
	// deleted is set once a watch found the Task gone; task then holds its
	// last observed state.
	deleted *Deletion
	// awaitingTaskSince is set when a watch received the record before
	// any state of its Task: the Task may still arrive.
	awaitingTaskSince time.Time
	// remembered is the Origin a reader kept of the Task, for an attempt
	// read from its TaskRecord alone.
	remembered *Origin
}

// Normalize turns one read into model records: one run per Task UID, with
// a Task and its TaskRecord merged into it, and the identifier
// correlations from each run to the Vikunja task its task label names and
// to the pull request its Origin names (the Task's
// correlation annotations, else s.Remembered). Two attempts that share a
// Task name stay two runs: nothing is joined by name. Every record is
// validated; an invalid object fails the whole read, naming the object.
func Normalize(cfg Config, s Snapshot) (model.Records, error) {
	if err := cfg.Validate(); err != nil {
		return model.Records{}, err
	}
	attempts, err := pair(cfg, s)
	if err != nil {
		return model.Records{}, err
	}
	return cfg.records(attempts)
}

// records builds and validates the run, and its correlations, of each
// attempt.
func (c Config) records(attempts []*attempt) (model.Records, error) {
	recs := model.Records{Runs: []model.Run{}, Correlations: []model.Correlation{}}
	var errs []error
	for _, a := range attempts {
		run, links := c.build(a)
		errs = append(errs, run.Validate())
		recs.Runs = append(recs.Runs, run)
		for _, link := range links {
			errs = append(errs, link.Validate())
			recs.Correlations = append(recs.Correlations, link)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return model.Records{}, fmt.Errorf("kelos: %w", err)
	}
	return recs, nil
}

// pair groups the Tasks and TaskRecords by Task UID, in UID order. A
// TaskRecord names its Task's UID in spec.taskRef.uid.
func pair(cfg Config, s Snapshot) ([]*attempt, error) {
	byUID := make(map[types.UID]*attempt)
	var errs []error
	for i := range s.Tasks {
		t := &s.Tasks[i]
		switch a := byUID[t.UID]; {
		case t.UID == "":
			errs = append(errs, fmt.Errorf("task %s: no uid", cfg.taskRef(t.Name)))
		case a == nil:
			byUID[t.UID] = &attempt{uid: t.UID, task: t}
		default:
			errs = append(errs, fmt.Errorf("task %s: uid %s seen twice", cfg.taskRef(t.Name), t.UID))
		}
	}
	for i := range s.Records {
		r := &s.Records[i]
		uid := r.Spec.TaskRef.UID
		switch a := byUID[uid]; {
		case uid == "":
			errs = append(errs, fmt.Errorf("taskrecord %s: no spec.taskRef.uid", cfg.recordRef(r.Name)))
		case a == nil:
			byUID[uid] = &attempt{uid: uid, record: r, remembered: remembered(s, uid)}
		case a.record == nil:
			a.record = r
		default:
			errs = append(errs, fmt.Errorf("taskrecord %s: task uid %s seen twice", cfg.recordRef(r.Name), uid))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("kelos: %w", err)
	}
	out := slices.Collect(maps.Values(byUID))
	slices.SortFunc(out, func(a, b *attempt) int { return cmp.Compare(a.uid, b.uid) })
	return out, nil
}

// remembered returns the Origin s.Remembered holds for uid, or nil.
func remembered(s Snapshot, uid types.UID) *Origin {
	if o, ok := s.Remembered[uid]; ok {
		return &o
	}
	return nil
}

// taskRef locates a Task in evidence.
func (c Config) taskRef(name string) string { return tasksResource + "/" + c.Namespace + "/" + name }

// recordRef locates a TaskRecord in evidence.
func (c Config) recordRef(name string) string {
	return recordsResource + "/" + c.Namespace + "/" + name
}
