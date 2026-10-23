package kelos

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// deletedTaskRetention is how long a watch keeps a deleted Task that has
// no TaskRecord, so that a record arriving after the deletion still joins
// it by UID. Kelos writes the record when the Task completes, long before
// the Task's TTL deletes it, so a record arrives later only when the two
// watches deliver out of order or the controller lags; a day covers both.
// Past it, a record joins nothing here, and the store's run keeps the
// Task's fields.
const deletedTaskRetention = 24 * time.Hour

// Sink stores what a Watcher reads: the store implements it.
type Sink interface {
	// Checkpoint returns the last committed checkpoint of the namespace,
	// with ok false when none was ever committed.
	Checkpoint(ctx context.Context, cluster, namespace string) (cp Checkpoint, ok bool, err error)
	// Commit stores a batch atomically: its records and the checkpoint
	// they bring the watch to. A batch whose commit fails is read again
	// from the previous checkpoint.
	Commit(ctx context.Context, b Batch) error
}

// Batch is what one list or watch event changed.
type Batch struct {
	// Records holds the runs and correlations the batch changed, one
	// observation event per object it saw, and the Kelos coverage: read up
	// to Checkpoint.LastEventAt, with a gap when the watch expired.
	Records model.Records
	// Checkpoint is where the watch stands once the batch is stored.
	Checkpoint Checkpoint
}

// Checkpoint is where a watch of one namespace stands, persisted as one
// JSON value so that a restarted Watcher resumes the same watches with the
// objects it held. It holds the live Tasks, the deleted Tasks still
// awaiting a TaskRecord, and the TaskRecords of those Tasks: a run whose
// Task is gone and whose record was read is complete, and leaves it.
type Checkpoint struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	// TasksVersion and RecordsVersion are the resourceVersions the two
	// watches resume from.
	TasksVersion   string `json:"tasksVersion"`
	RecordsVersion string `json:"recordsVersion"`
	// LastEventAt is when the last event or bookmark was received, or the
	// last list started: the namespace is known up to it.
	LastEventAt time.Time     `json:"lastEventAt"`
	Tasks       []WatchedTask `json:"tasks"`
	Records     []TaskRecord  `json:"records"`
}

// WatchedTask is a Task a watch holds, live or deleted.
type WatchedTask struct {
	Task Task `json:"task"`
	// Deleted is set once the Task was found gone; Task is then its last
	// observed state.
	Deleted *Deletion `json:"deleted,omitempty"`
}

// Deletion is how a watch found a Task gone.
type Deletion struct {
	// At is when the deletion event was received, or when the relist that
	// no longer found the Task started.
	At time.Time `json:"at"`
	// Seen is set when a watch event reported the deletion, and unset when
	// a relist found the Task missing: it was deleted at an unknown time
	// between the last event and At.
	Seen bool `json:"seen"`
}

// state is what a watch holds between events.
type state struct {
	attempts                     map[types.UID]*attempt
	tasksVersion, recordsVersion string
	lastEventAt                  time.Time
}

// stateOf restores the state a checkpoint holds.
func stateOf(cp Checkpoint) (*state, error) {
	st := &state{
		attempts:     make(map[types.UID]*attempt),
		tasksVersion: cp.TasksVersion, recordsVersion: cp.RecordsVersion,
		lastEventAt: cp.LastEventAt,
	}
	for i := range cp.Tasks {
		t := cp.Tasks[i]
		if t.Task.UID == "" {
			return nil, fmt.Errorf("kelos: checkpoint: task %s without a uid", t.Task.Name)
		}
		st.attempts[t.Task.UID] = &attempt{uid: t.Task.UID, task: &t.Task, deleted: t.Deleted}
	}
	for i := range cp.Records {
		r := &cp.Records[i]
		a := st.attempts[r.Spec.TaskRef.UID]
		if a == nil {
			return nil, fmt.Errorf("kelos: checkpoint: taskrecord %s without its task", r.Name)
		}
		a.record = r
	}
	return st, nil
}

// checkpoint returns the state as cfg's checkpoint, in UID order.
func (s *state) checkpoint(cfg Config) Checkpoint {
	cp := Checkpoint{
		Cluster: cfg.Cluster, Namespace: cfg.Namespace,
		TasksVersion: s.tasksVersion, RecordsVersion: s.recordsVersion,
		LastEventAt: s.lastEventAt,
		Tasks:       []WatchedTask{}, Records: []TaskRecord{},
	}
	for _, uid := range slices.Sorted(maps.Keys(s.attempts)) {
		a := s.attempts[uid]
		if a.task == nil { // pruned before every checkpoint
			continue
		}
		task := *a.task
		task.ObjectMeta = persistedMeta(task.ObjectMeta)
		cp.Tasks = append(cp.Tasks, WatchedTask{Task: task, Deleted: a.deleted})
		if a.record != nil {
			record := *a.record
			record.ObjectMeta = persistedMeta(record.ObjectMeta)
			cp.Records = append(cp.Records, record)
		}
	}
	return cp
}

// persistedMeta keeps the metadata a run is built from. Annotations and
// managed fields are dropped: an annotation such as kubectl's
// last-applied-configuration can hold the prompt, which is never stored.
func persistedMeta(m metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: m.Name, Namespace: m.Namespace, UID: m.UID, ResourceVersion: m.ResourceVersion,
		CreationTimestamp: m.CreationTimestamp, Labels: m.Labels,
	}
}

// attempt returns the state's attempt for uid, adding it when missing.
func (s *state) attempt(uid types.UID) *attempt {
	a := s.attempts[uid]
	if a == nil {
		a = &attempt{uid: uid}
		s.attempts[uid] = a
	}
	return a
}

// prune drops the attempts no later event can change: those without a
// Task (their Task is gone, so their record joins nothing), those whose
// deleted Task has its record, and deleted Tasks past the retention.
// Their runs were committed; the store keeps them.
func (s *state) prune(now time.Time) {
	maps.DeleteFunc(s.attempts, func(_ types.UID, a *attempt) bool {
		if a.task == nil {
			return true
		}
		return a.deleted != nil && (a.record != nil || now.Sub(a.deleted.At) > deletedTaskRetention)
	})
}

// sortedAttempts returns attempts in UID order, each once.
func sortedAttempts(attempts []*attempt) []*attempt {
	out := slices.Clone(attempts)
	slices.SortFunc(out, func(a, b *attempt) int { return cmp.Compare(a.uid, b.uid) })
	return slices.CompactFunc(out, func(a, b *attempt) bool { return a.uid == b.uid })
}
