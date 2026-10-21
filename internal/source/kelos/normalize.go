package kelos

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// attempt is one Task UID's objects: its Task, its TaskRecord, or both.
type attempt struct {
	uid    types.UID
	task   *Task
	record *TaskRecord
}

// Normalize turns one read into model records: one run per Task UID, with
// a Task and its TaskRecord merged into it, and a correlation from each run
// to the Vikunja task its task label names. Two attempts that share a Task
// name stay two runs: nothing is joined by name. Every record is
// validated; an invalid object fails the whole read, naming the object.
func Normalize(cfg Config, s Snapshot) (model.Records, error) {
	attempts, err := pair(cfg, s)
	if err != nil {
		return model.Records{}, err
	}
	recs := model.Records{Runs: []model.Run{}, Correlations: []model.Correlation{}}
	var errs []error
	for _, a := range attempts {
		run, link := cfg.build(a)
		errs = append(errs, run.Validate())
		recs.Runs = append(recs.Runs, run)
		if link != nil {
			errs = append(errs, link.Validate())
			recs.Correlations = append(recs.Correlations, *link)
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
	get := func(uid types.UID) *attempt {
		a, ok := byUID[uid]
		if !ok {
			a = &attempt{uid: uid}
			byUID[uid] = a
		}
		return a
	}
	var errs []error
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if a := get(t.UID); t.UID == "" || a.task != nil {
			errs = append(errs, fmt.Errorf("task %s: no uid, or one seen twice", cfg.taskRef(t.Name)))
		} else {
			a.task = t
		}
	}
	for i := range s.Records {
		r := &s.Records[i]
		if a := get(r.Spec.TaskRef.UID); r.Spec.TaskRef.UID == "" || a.record != nil {
			errs = append(errs, fmt.Errorf("taskrecord %s: no spec.taskRef.uid, or one seen twice", cfg.recordRef(r.Name)))
		} else {
			a.record = r
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("kelos: %w", err)
	}
	out := make([]*attempt, 0, len(byUID))
	for _, a := range byUID {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b *attempt) int { return cmp.Compare(a.uid, b.uid) })
	return out, nil
}

// taskRef locates a Task in evidence.
func (c Config) taskRef(name string) string { return tasksResource + "/" + c.Namespace + "/" + name }

// recordRef locates a TaskRecord in evidence.
func (c Config) recordRef(name string) string {
	return recordsResource + "/" + c.Namespace + "/" + name
}
