package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// everyCycle re-reads every root Kelos names at each cycle, its GitHub
// side too.
var everyCycle = Config{
	Interval: time.Millisecond, RefreshInterval: time.Nanosecond, BackstopInterval: time.Nanosecond,
	LockRetryInterval: time.Millisecond,
}

func coverageOf(t *testing.T, s model.Story, src model.Source) model.Coverage {
	t.Helper()
	for _, c := range s.Coverage {
		if c.Source == src {
			return c
		}
	}
	t.Fatalf("no %s coverage in %v", src, s.Coverage)
	return model.Coverage{}
}

func written(c Commit) int {
	return len(c.Records.Events) + len(c.Records.Runs) + len(c.Records.PullRequests) + len(c.Records.Correlations)
}

func TestUnchangedTaskWritesNothingNew(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.loop.cycle(t.Context())
	first := h.store.lastCommit(t)
	if written(first) == 0 {
		t.Fatal("the first read wrote no record")
	}
	before := testutil.ToFloat64(h.loop.metrics.written.WithLabelValues(kindEvents))
	h.loop.cycle(t.Context())
	second := h.store.lastCommit(t)
	if n := written(second); n != 0 {
		t.Errorf("the second read of an unchanged task wrote %d records, want none", n)
	}
	if got := testutil.ToFloat64(h.loop.metrics.written.WithLabelValues(kindEvents)); got != before {
		t.Errorf("events written = %v after the second read, want %v", got, before)
	}
	was := coverageOf(t, model.Story{Records: first.Records}, model.SourceVikunja).LastSuccessfulRead
	now := coverageOf(t, model.Story{Records: second.Records}, model.SourceVikunja).LastSuccessfulRead
	if was == nil || now == nil || !now.After(*was) {
		t.Errorf("vikunja read %v, then %v: want the second read to advance it", was, now)
	}
}

func TestChangedTaskWritesAgain(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.loop.cycle(t.Context())
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
	h.loop.cycle(t.Context())
	c := h.store.lastCommit(t)
	if len(c.Records.Runs) != 1 || c.Records.Runs[0].Outcome != model.OutcomeSucceeded {
		t.Errorf("runs written = %v, want the run, now succeeded", c.Records.Runs)
	}
}

func TestFailingSourceYieldsAGap(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.loop.cycle(t.Context())
	read := *coverageOf(t, h.store.story(t, testRoot()), model.SourceVikunja).LastSuccessfulRead

	h.vikunja.set(errUnavailable)
	h.loop.cycle(t.Context())
	failedAt := time.Now()
	cov := coverageOf(t, h.store.story(t, testRoot()), model.SourceVikunja)
	if !cov.LastSuccessfulRead.Equal(read) || cov.Known(failedAt) {
		t.Errorf("after the failure, coverage = %+v: want the last success kept and now unknown", cov)
	}

	h.vikunja.set(nil)
	h.loop.cycle(t.Context())
	s := h.store.story(t, testRoot())
	cov = coverageOf(t, s, model.SourceVikunja)
	if len(cov.Gaps) != 1 {
		t.Fatalf("vikunja gaps = %v, want one", cov.Gaps)
	}
	g := cov.Gaps[0]
	if !g.Start.Equal(read) || !g.End.Equal(*cov.LastSuccessfulRead) || !strings.Contains(g.Reason, reasonServerError) {
		t.Errorf("gap = %+v, want from %v to the next success %v, a server error", g, read, cov.LastSuccessfulRead)
	}
	if cov.Known(failedAt) {
		t.Errorf("the story shows the failed read's time %v as known", failedAt)
	}
	if got := testutil.ToFloat64(h.loop.metrics.readErrors.WithLabelValues("vikunja", testVikunjaHost,
		reasonServerError)); got != 1 {
		t.Errorf("vikunja server errors = %v, want 1", got)
	}
}

func TestDeletedTaskKeepsItsRun(t *testing.T) {
	h := newHarness(t, Config{
		Interval: time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		VikunjaProjects: []int64{testProject},
	})
	h.loop.cycle(t.Context())
	ttl := ttlGaps(coverageOf(t, h.store.story(t, testRoot()), model.SourceKelos))
	// The Task's TTL ends; it left no TaskRecord. The task changes.
	h.kelos.set(kelos.Snapshot{}, nil)
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: time.Now()})
	reads := h.vikunja.count()
	h.loop.cycle(t.Context())
	if h.vikunja.count() == reads {
		t.Fatal("the changed task was not read again")
	}
	s := h.store.story(t, testRoot())
	if len(s.Runs) != 1 || s.Runs[0].Key.UID != testUID {
		t.Errorf("runs = %v, want the deleted Task's run and no run keyed by its pod", s.Runs)
	}
	if got := ttlGaps(coverageOf(t, s, model.SourceKelos)); len(got) != 1 || len(ttl) != 1 || !got[0].End.Equal(ttl[0].End) {
		t.Errorf("Task-TTL gaps = %v, then %v: want one, ending a TTL before the first read both times", ttl, got)
	}
}

// ttlGaps are the coverage's gaps for the Kelos Task TTL.
func ttlGaps(cov model.Coverage) []model.Gap {
	var out []model.Gap
	for _, g := range cov.Gaps {
		if strings.Contains(g.Reason, "Kelos deletes a finished Task") {
			out = append(out, g)
		}
	}
	return out
}

func TestRestartRecordsTheDowntime(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.loop.cycle(t.Context())
	before := h.store.story(t, testRoot())
	l := h.newLoop(t, everyCycle)
	l.cycle(t.Context())
	s := h.store.story(t, testRoot())
	for _, src := range []model.Source{model.SourceVikunja, model.SourceKelos} {
		was := coverageOf(t, before, src).LastSuccessfulRead
		cov := coverageOf(t, s, src)
		var restart []model.Gap
		for _, g := range cov.Gaps {
			if strings.Contains(g.Reason, "restarted") {
				restart = append(restart, g)
			}
		}
		if len(restart) != 1 || restart[0].Start.Before(*was) || !restart[0].End.Equal(*cov.LastSuccessfulRead) {
			t.Fatalf("%s restart gaps = %v, want one from the last cycle before the restart to the next read", src, restart)
		}
		// Only the Kelos coverage's runs have phase markers to miss, and
		// sillage cannot know that a pod sent one.
		missed := strings.Contains(restart[0].Reason, "phase markers pods sent meanwhile may not have been received")
		if missed != (src == model.SourceKelos) {
			t.Errorf("%s restart gap %q says markers may be missing: %v, want %v", src, restart[0].Reason, missed,
				src == model.SourceKelos)
		}
	}
}

func TestRestartDropsRetiredSources(t *testing.T) {
	const retired = "wiki/wiki.example.com"
	h := newHarness(t, everyCycle)
	h.loop.cycle(t.Context())
	key := rootKey(testRoot())
	var stored rootState
	if err := json.Unmarshal(h.store.cps[key], &stored); err != nil {
		t.Fatal(err)
	}
	read := testStart
	stored.Sources[retired] = &sourceState{Source: "wiki", Instance: "wiki.example.com", LastSuccess: &read}
	h.store.cps[key] = encode(&stored)

	l := h.newLoop(t, everyCycle)
	if _, ok := l.roots[testRoot().Ref()].Sources[retired]; ok {
		t.Fatalf("the loaded root state keeps the retired source %s", retired)
	}
	if !strings.Contains(h.logs.String(), "dropped a retired source") {
		t.Errorf("logs = %q, want the dropped retired source said", h.logs.String())
	}
	l.cycle(t.Context())
	for _, c := range h.store.story(t, testRoot()).Coverage {
		if !c.Source.Valid() {
			t.Errorf("the next read committed coverage of the retired source %s", c.Key())
		}
	}
}

func TestShutdownCommitsNothingPartial(t *testing.T) {
	h := newHarness(t, everyCycle)
	ctx, cancel := context.WithCancel(t.Context())
	h.vikunja.during = func(context.Context) { cancel() }
	h.loop.cycle(ctx)
	if n := len(h.store.commits); n != 0 {
		t.Errorf("%d commits after a shutdown inside the read, want none", n)
	}
}

func TestFailedCommitIsReadAgain(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.store.setCommitErr(errors.New("fabricated database outage"))
	h.loop.cycle(t.Context())
	h.store.setCommitErr(nil)
	h.loop.cycle(t.Context())
	if c := h.store.lastCommit(t); written(c) == 0 {
		t.Error("after a failed commit, the next read wrote nothing: its records are lost")
	}
	if got := testutil.ToFloat64(h.loop.metrics.storeErrors.WithLabelValues(operationCommit)); got != 1+1 {
		t.Errorf("commit errors = %v, want the root's and the cycle's", got)
	}
}

func TestKelosRootReadWhenItsRunsChange(t *testing.T) {
	h := newHarness(t, Config{Interval: time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond})
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 1 {
		t.Errorf("task read %d times while its runs did not change, want once", n)
	}
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
	h.loop.cycle(t.Context())
	if n := h.vikunja.count(); n != 1+1 {
		t.Errorf("task read %d times, want again once its run changed", n)
	}
}

func TestKelosFailureOpensAGapOnTheRootsRead(t *testing.T) {
	h := newHarness(t, Config{
		Interval: time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond,
		VikunjaProjects: []int64{testProject},
	})
	h.loop.cycle(t.Context())
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: time.Now()})
	h.loop.cycle(t.Context())
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseRunning)}}, nil)
	h.projects.set(testProject, vikunja.TaskChange{ID: testTaskID, Updated: time.Now().Add(time.Second)})
	h.loop.cycle(t.Context())
	s := h.store.story(t, testRoot())
	var failed int
	for _, g := range coverageOf(t, s, model.SourceKelos).Gaps {
		if strings.Contains(g.Reason, "kelos read failed ("+reasonFailed+")") {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("kelos gaps = %v, want one for the failed read", coverageOf(t, s, model.SourceKelos).Gaps)
	}
	if len(s.Runs) != 1 {
		t.Errorf("runs = %v, want the one run, kept through the failed read", s.Runs)
	}
}
