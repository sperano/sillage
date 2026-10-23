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

// joinRetention is how long a watch holds one half of a run for the other
// to join it by UID: a deleted Task for its TaskRecord, and a record whose
// Task it has not seen yet for that Task. Kelos writes the record when the
// Task completes, long before the Task's TTL deletes it, so the halves
// arrive apart only when the two watches deliver out of order (replaying a
// backlog after a restart above all) or the controller lags; a day covers
// both. Past it, a half joins nothing here, and the store completes the
// run.
const joinRetention = 24 * time.Hour

// Sink stores what a Watcher reads: the store implements it. It sees
// only model records and an opaque checkpoint, so it needs no Kubernetes
// or Kelos type.
type Sink interface {
	// Checkpoint returns the checkpoint last committed under key, with ok
	// false when none was.
	Checkpoint(ctx context.Context, key string) (data []byte, ok bool, err error)
	// Commit stores, atomically, what one list or watch event changed and
	// the checkpoint it brings the watch to, under key. The records are the
	// runs and correlations it changed, one run.observed event per object
	// seen, and the Kelos coverage: read up to the event, with a gap when a
	// watch expired. A run built from a TaskRecord alone (its Task deleted
	// before it was read) must complete the stored run, not replace it. A
	// commit that fails is read again from the previous checkpoint.
	Commit(ctx context.Context, key string, recs model.Records, checkpoint []byte) error
}

// Checkpoint is where a watch of one namespace stands, which a Sink stores
// as opaque JSON so that a restarted Watcher resumes the same watches with
// the objects it held. It holds the live Tasks, the deleted Tasks still
// awaiting a TaskRecord, the TaskRecords of those Tasks, and the records
// still awaiting their Task: a run whose Task is gone and whose record was
// read is complete, and leaves it.
type Checkpoint struct {
	// TasksVersion and RecordsVersion are the resourceVersions the two
	// watches resume from.
	TasksVersion   string `json:"tasksVersion"`
	RecordsVersion string `json:"recordsVersion"`
	// LastEventAt is when the last event or bookmark was received, or the
	// last list started: the namespace is known up to it.
	LastEventAt time.Time       `json:"lastEventAt"`
	Tasks       []WatchedTask   `json:"tasks"`
	Records     []WatchedRecord `json:"records"`
}

// WatchedTask is a Task a watch holds, live or deleted.
type WatchedTask struct {
	Task Task `json:"task"`
	// Deleted is set once the Task was found gone; Task is then its last
	// observed state.
	Deleted *Deletion `json:"deleted,omitempty"`
}

// WatchedRecord is a TaskRecord a watch holds.
type WatchedRecord struct {
	Record TaskRecord `json:"record"`
	// AwaitingTaskSince is set while the record's Task has not been seen:
	// when the watch received the record.
	AwaitingTaskSince *time.Time `json:"awaitingTaskSince,omitempty"`
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

// checkpointKey is the key a Sink stores the namespace's checkpoint under.
func (c Config) checkpointKey() string {
	return string(model.SourceKelos) + "/" + c.Cluster + "/" + c.Namespace
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
		w := &cp.Records[i]
		uid := w.Record.Spec.TaskRef.UID
		a := st.attempts[uid]
		switch {
		case uid == "":
			return nil, fmt.Errorf("kelos: checkpoint: taskrecord %s without a task uid", w.Record.Name)
		case a == nil && w.AwaitingTaskSince == nil:
			return nil, fmt.Errorf("kelos: checkpoint: taskrecord %s without its task", w.Record.Name)
		case a == nil:
			a = st.attempt(uid)
			a.awaitingTaskSince = *w.AwaitingTaskSince
		}
		a.record = &w.Record
	}
	return st, nil
}

// checkpoint returns the state as a checkpoint, in UID order.
func (s *state) checkpoint() Checkpoint {
	cp := Checkpoint{
		TasksVersion: s.tasksVersion, RecordsVersion: s.recordsVersion,
		LastEventAt: s.lastEventAt,
		Tasks:       []WatchedTask{}, Records: []WatchedRecord{},
	}
	for _, uid := range slices.Sorted(maps.Keys(s.attempts)) {
		a := s.attempts[uid]
		if a.task != nil {
			task := *a.task
			task.ObjectMeta = persistedMeta(task.ObjectMeta)
			cp.Tasks = append(cp.Tasks, WatchedTask{Task: task, Deleted: a.deleted})
		}
		if a.record != nil {
			record := *a.record
			record.ObjectMeta = persistedMeta(record.ObjectMeta)
			w := WatchedRecord{Record: record}
			if a.task == nil {
				since := a.awaitingTaskSince
				w.AwaitingTaskSince = &since
			}
			cp.Records = append(cp.Records, w)
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

// prune drops the attempts no later event can change: a listed record
// without its Task (the Task was gone from the list taken after it), a
// deleted Task with its record, and a half past the retention. Their runs
// were committed; the store keeps them.
func (s *state) prune(now time.Time) {
	expired := func(since time.Time) bool { return now.Sub(since) > joinRetention }
	maps.DeleteFunc(s.attempts, func(_ types.UID, a *attempt) bool {
		if a.task == nil {
			return a.awaitingTaskSince.IsZero() || expired(a.awaitingTaskSince)
		}
		return a.deleted != nil && (a.record != nil || expired(a.deleted.At))
	})
}

// sortedAttempts returns attempts in UID order, each once.
func sortedAttempts(attempts []*attempt) []*attempt {
	out := slices.Clone(attempts)
	slices.SortFunc(out, func(a, b *attempt) int { return cmp.Compare(a.uid, b.uid) })
	return slices.CompactFunc(out, func(a, b *attempt) bool { return a.uid == b.uid })
}
