package model

import (
	"errors"
	"time"
)

// DetailDone is a completion observation's detail: DoneTrue or DoneFalse,
// what the read saw.
const (
	DetailDone = "done"
	DoneTrue   = "true"
	DoneFalse  = "false"
)

// CompletionState is a task's completion at an instant, as its records
// say.
type CompletionState string

// The completion states of a task that was completed.
const (
	// CompletionDone: completed, and no read since saw it not done.
	CompletionDone CompletionState = "done"
	// CompletionReopened: completed, then a read saw it not done.
	CompletionReopened CompletionState = "reopened"
)

// TaskCompletion is a task's latest completion before an instant, and
// whether it was reopened since.
type TaskCompletion struct {
	State CompletionState `json:"state"`
	// Done is the key of the latest task.done event, and DoneAt its time.
	Done   string    `json:"done"`
	DoneAt time.Time `json:"doneAt"`
	// Reopened says, for a reopened task, between which records it was
	// reopened.
	Reopened *Reopening `json:"reopened,omitempty"`
}

// Reopening is what the records say of a task's reopening: no source
// records when it happened, so only that it came after one time and no
// later than another.
type Reopening struct {
	// After is the latest of the completion and the reads after it that
	// still saw the task done.
	After time.Time `json:"after"`
	// By is the first read after that which saw the task not done, and
	// Observation the key of its completion observation.
	By          time.Time `json:"by"`
	Observation string    `json:"observation"`
}

// CompletionAt returns the completion of the story's task before t: its
// latest task.done event before t, reopened when a read after it, and
// after every read still seeing it done, saw it not done before t. It
// returns false when the story is a page's or no completion was recorded
// before t. A read's time is when sillage made it, a completion's is
// Vikunja's; they are compared as recorded.
func (s Story) CompletionAt(t time.Time) (TaskCompletion, bool) {
	if s.Root.Task == nil {
		return TaskCompletion{}, false
	}
	events := taskEvents(s.Events, *s.Root.Task)
	done, ok := latestDone(events, t)
	if !ok {
		return TaskCompletion{}, false
	}
	c := TaskCompletion{State: CompletionDone, Done: done.Key(), DoneAt: done.Time()}
	after := lastSeenDone(events, done, t)
	if open, by, ok := firstSeenOpen(events, after, t); ok {
		c.State = CompletionReopened
		c.Reopened = &Reopening{After: after, By: by, Observation: open.Key()}
	}
	return c, true
}

// taskEvents returns the events on task.
func taskEvents(events []Event, task TaskKey) []Event {
	var out []Event
	for _, e := range events {
		if e.Related.Task != nil && *e.Related.Task == task {
			out = append(out, e)
		}
	}
	return out
}

// latestDone returns the latest task.done event before t.
func latestDone(events []Event, t time.Time) (Event, bool) {
	var done Event
	found := false
	for _, e := range events {
		if e.Kind == KindTaskDone && e.Time().Before(t) && (!found || e.Time().After(done.Time())) {
			done, found = e, true
		}
	}
	return done, found
}

// lastSeenDone returns the latest time before t the task is known done
// since the completion done: the completion itself, the read it was taken
// from, or a read that observed the task done.
func lastSeenDone(events []Event, done Event, t time.Time) time.Time {
	after := done.Time()
	see := func(read time.Time) {
		if read.Before(t) && read.After(after) {
			after = read
		}
	}
	see(done.ObservedAt)
	for _, e := range events {
		if e.Kind == KindTaskCompletionObserved && e.Details[DetailDone] == DoneTrue {
			for _, read := range observedReads(e) {
				see(read)
			}
		}
	}
	return after
}

// firstSeenOpen returns the first read after after and before t that
// observed the task not done, and its observation. A series of reads
// seeing it not done begins at its first: the store extends one only over
// consecutive reads.
func firstSeenOpen(events []Event, after, t time.Time) (Event, time.Time, bool) {
	var open Event
	found := false
	for _, e := range events {
		if e.Kind != KindTaskCompletionObserved || e.Details[DetailDone] != DoneFalse {
			continue
		}
		if read := e.ObservedAt; read.After(after) && read.Before(t) && (!found || read.Before(open.ObservedAt)) {
			open, found = e, true
		}
	}
	return open, open.ObservedAt, found
}

// observedReads returns the reads an observation names: the first that
// saw it and, when a later one saw it again, the last.
func observedReads(e Event) []time.Time {
	if e.LastObservedAt == nil {
		return []time.Time{e.ObservedAt}
	}
	return []time.Time{e.ObservedAt, *e.LastObservedAt}
}

// validateCompletion checks that a completion observation says what its
// read saw.
func (e Event) validateCompletion() error {
	if e.Kind != KindTaskCompletionObserved {
		return nil
	}
	if v := e.Details[DetailDone]; v != DoneTrue && v != DoneFalse {
		return errors.New(`a completion observation's "done" detail is "true" or "false"`)
	}
	return nil
}
