package reconcile

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/source/vikunja"
)

func projectHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, Config{
		Interval: time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		VikunjaProjects: []int64{testProject},
	})
	return h
}

func TestProjectListedSinceItsMark(t *testing.T) {
	h := projectHarness(t)
	updated := time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC)
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: updated})
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if len(h.projects.since) != 2 || !h.projects.since[0].IsZero() ||
		!h.projects.since[1].Equal(updated.Add(-listingOverlap)) {
		t.Errorf("listed since %v, want everything, then the mark less the overlap", h.projects.since)
	}
	if n := h.vikunja.count(); n != 1 {
		t.Errorf("task read %d times; listed again unchanged, want once", n)
	}
	l := h.newLoop(t, h.loop.cfg)
	if got := l.marks[testProject]; !got.Equal(updated) {
		t.Errorf("mark after a restart = %v, want %v", got, updated)
	}
}

func TestProjectMarkWaitsForTheTasksRead(t *testing.T) {
	h := projectHarness(t)
	h.vikunja.set(errUnavailable)
	updated := time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC)
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: updated})
	h.loop.cycle(t.Context())
	if _, ok := h.loop.marks[testProject]; ok {
		t.Error("the mark advanced past a task whose read failed")
	}
	h.vikunja.set(nil)
	h.loop.cycle(t.Context())
	if got := h.loop.marks[testProject]; !got.Equal(updated) {
		t.Errorf("mark = %v, want %v once the task was read", got, updated)
	}
	if n := h.vikunja.count(); n != 2 {
		t.Errorf("task read %d times, want again after the failure", n)
	}
}
