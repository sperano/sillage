package reconcile

import (
	"maps"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// The keys of the loop's checkpoints. Every one starts with
// checkpointPrefix, so that the loop reads only its own, and names its
// source instance, so that no two are kept under one key.
const (
	checkpointPrefix = "collector/"
	// rootPrefix keys a root's state, after its ref.
	rootPrefix = checkpointPrefix + "roots/"
	// projectPrefix keys a Vikunja project's listing mark, after the
	// instance and the project id.
	projectPrefix = checkpointPrefix + "vikunja-projects/"
	// heartbeatPrefix keys when the last cycle ended, after the Kelos
	// cluster: a cycle is one read of it.
	heartbeatPrefix = checkpointPrefix + "heartbeat/"
)

// rootState is what the loop knows of one root between two reads.
type rootState struct {
	Root model.Root `json:"root"`
	// Digest identifies the records of the root's last read that failed
	// nowhere; empty before one.
	Digest string `json:"digest,omitempty"`
	// Read is when the root was last read, whatever the outcome.
	Read time.Time `json:"read"`
	// KelosRuns identifies the runs the last Kelos read named the root
	// with.
	KelosRuns string `json:"kelosRuns,omitempty"`
	// Updated is the task's updated time as the last project listing that
	// named it said.
	Updated *time.Time `json:"updated,omitempty"`
	// Sources are the root's source instances, by coverage key.
	Sources map[string]*sourceState `json:"sources,omitempty"`
}

// sourceState is how far one source instance of a root is known.
type sourceState struct {
	Source   model.Source `json:"source"`
	Instance string       `json:"instance"`
	// FirstSuccess and LastSuccess are when the first and the last
	// successful reads started.
	FirstSuccess *time.Time `json:"firstSuccess,omitempty"`
	LastSuccess  *time.Time `json:"lastSuccess,omitempty"`
	// Open is the gap a failed read or a restart opened, which the next
	// successful read closes.
	Open *openGap `json:"open,omitempty"`
	// Gaps are the gaps closed so far.
	Gaps []model.Gap `json:"gaps,omitempty"`
	// ReadGaps and Notes are what the collector said was missing at the
	// last read that did not fail.
	ReadGaps []model.Gap `json:"readGaps,omitempty"`
	Notes    []string    `json:"notes,omitempty"`
}

// openGap is a gap whose end is the next successful read.
type openGap struct {
	Start  time.Time `json:"start"`
	Reason string    `json:"reason"`
}

func newRootState(root model.Root) *rootState {
	return &rootState{Root: root, Sources: make(map[string]*sourceState)}
}

// rootKey is the checkpoint key of root's state.
func rootKey(root model.Root) string { return rootPrefix + root.Ref().String() }

// heartbeatKey is the checkpoint key of the last cycle's end on cluster.
func heartbeatKey(cluster string) string { return heartbeatPrefix + cluster }

// checkpoint is s as the store keeps it: under its root's own source
// instance, and tied to the root, so that the store drops it with the
// root.
func (s *rootState) checkpoint() Checkpoint {
	root := s.Root
	cp := Checkpoint{Source: model.SourceVikunja, Instance: root.Task.Instance, Root: &root, Value: encode(s)}
	return cp
}

// clone returns a deep copy of s, to change and commit while s stays what
// the store holds.
func (s *rootState) clone() *rootState {
	out := *s
	out.Updated = clonePointer(s.Updated)
	out.Sources = make(map[string]*sourceState, len(s.Sources))
	for key, st := range s.Sources {
		c := *st
		c.FirstSuccess, c.LastSuccess = clonePointer(st.FirstSuccess), clonePointer(st.LastSuccess)
		c.Open = clonePointer(st.Open)
		c.Gaps, c.ReadGaps, c.Notes = slices.Clone(st.Gaps), slices.Clone(st.ReadGaps), slices.Clone(st.Notes)
		out.Sources[key] = &c
	}
	return &out
}

// clonePointer returns a pointer to a copy of what p points to; nil for
// nil.
func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// dropRetiredSources removes the sources sillage no longer reads, whose
// coverage no store commit accepts, and returns their keys, sorted.
func (s *rootState) dropRetiredSources() []string {
	var dropped []string
	for key, st := range s.Sources {
		if !st.Source.Valid() {
			dropped = append(dropped, key)
			delete(s.Sources, key)
		}
	}
	slices.Sort(dropped)
	return dropped
}

// source returns the state of the source instance cov covers, adding it.
func (s *rootState) source(cov model.Coverage) *sourceState {
	st, ok := s.Sources[cov.Key()]
	if !ok {
		st = &sourceState{Source: cov.Source, Instance: cov.Instance}
		s.Sources[cov.Key()] = st
	}
	return st
}

// kelos returns the state of the root's Kelos coverage on cluster; nil
// if it has none.
func (s *rootState) kelos(cluster string) *sourceState {
	return s.Sources[model.Coverage{Source: model.SourceKelos, Instance: cluster}.Key()]
}

// kelosSince is when Kelos cluster was first read for the root; zero if
// never. Another cluster's reads say nothing of this one's Task TTL.
func (s *rootState) kelosSince(cluster string) time.Time {
	if st := s.kelos(cluster); st != nil && st.FirstSuccess != nil {
		return *st.FirstSuccess
	}
	return time.Time{}
}

// kelosGapOpen reports whether the root's Kelos coverage on cluster has
// a gap open, which only a read of the root closes.
func (s *rootState) kelosGapOpen(cluster string) bool {
	st := s.kelos(cluster)
	return st != nil && st.Open != nil
}

// coverage returns the root's coverage, in key order.
func (s *rootState) coverage() []model.Coverage {
	out := make([]model.Coverage, 0, len(s.Sources))
	for _, key := range slices.Sorted(maps.Keys(s.Sources)) {
		out = append(out, s.Sources[key].coverage())
	}
	return out
}

// succeed records a successful read that started at at, with what the
// collector said it left unknown: it closes the open gap there.
func (st *sourceState) succeed(at time.Time, cov model.Coverage) {
	if st.Open != nil {
		if at.After(st.Open.Start) {
			st.Gaps = append(st.Gaps, model.Gap{Start: st.Open.Start, End: at, Reason: st.Open.Reason})
		}
		st.Open = nil
	}
	if st.FirstSuccess == nil {
		st.FirstSuccess = &at
	}
	if st.LastSuccess == nil || at.After(*st.LastSuccess) {
		st.LastSuccess = &at
	}
	st.unread(cov)
}

// unread records a read that neither succeeded nor failed (nothing named
// what to read): only what the collector said it left unknown changes.
func (st *sourceState) unread(cov model.Coverage) {
	st.ReadGaps = slices.Clone(cov.Gaps)
	st.Notes = slices.Clone(cov.Notes)
}

// fail records a failed read: it opens a gap from the last success,
// unless one is open, and reports whether it did. Before any success
// nothing is known, so there is no gap to open.
func (st *sourceState) fail(reason string) bool {
	if st.Open != nil || st.LastSuccess == nil {
		return false
	}
	st.Open = &openGap{Start: *st.LastSuccess, Reason: reason}
	return true
}

// openAfter opens a gap from the later of the last success and after
// (zero when unknown) until the next successful read, unless one is open,
// and reports whether it did: what was read is known until after, as when
// sillage stopped after its last cycle, or a shared read last succeeded.
func (st *sourceState) openAfter(after time.Time, reason string) bool {
	if st.Open != nil || st.LastSuccess == nil {
		return false
	}
	start := *st.LastSuccess
	if after.After(start) {
		start = after
	}
	st.Open = &openGap{Start: start, Reason: reason}
	return true
}

// coverage is the source's coverage: the last success, every closed gap
// and those of the last read, and its notes. An open gap is not in it:
// after the last success, everything is unknown anyway.
func (st *sourceState) coverage() model.Coverage {
	gaps := slices.Concat(st.Gaps, st.ReadGaps)
	slices.SortFunc(gaps, func(a, b model.Gap) int { return a.Start.Compare(b.Start) })
	return model.Coverage{
		Source: st.Source, Instance: st.Instance, LastSuccessfulRead: st.LastSuccess,
		Gaps: gaps, Notes: slices.Clone(st.Notes),
	}
}
