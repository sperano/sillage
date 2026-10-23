package reconcile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// hourly re-reads a root only when its runs or task change.
var hourly = Config{Interval: time.Millisecond, RefreshInterval: time.Hour, LockRetryInterval: time.Millisecond}

// gapsWith returns the gaps of cov whose reason holds text.
func gapsWith(cov model.Coverage, text string) []model.Gap {
	var out []model.Gap
	for _, g := range cov.Gaps {
		if strings.Contains(g.Reason, text) {
			out = append(out, g)
		}
	}
	return out
}

func TestTwoRestartsKeepTheFirstDowntime(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	first := h.newLoop(t, hourly)
	open := first.roots[testRoot().Ref()].Sources["vikunja/"+testVikunjaHost].Open
	if open == nil {
		t.Fatal("the restart opened no gap")
	}
	// A cycle that does not read the root (Kelos names it no more), then
	// another restart.
	h.kelos.set(kelos.Snapshot{}, nil)
	first.cycle(t.Context())
	second := h.newLoop(t, hourly)
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
	second.cycle(t.Context())
	cov := coverageOf(t, h.store.story(t, testRoot()), model.SourceVikunja)
	got := gapsWith(cov, "restarted")
	if len(got) != 1 || !got[0].Start.Equal(open.Start) {
		t.Errorf("restart gaps = %v, want one from the first restart's %v", got, open.Start)
	}
}

// otherTaskID is a second task Kelos names.
const otherTaskID = 43

func taskRootOf(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: id}}
}

// twoRoots is a namespace whose runs name testTaskID and otherTaskID.
func twoRoots(phase kelos.Phase) kelos.Snapshot {
	return kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(phase), kelosTaskOf(otherTaskID, phase)}}
}

func TestKelosFailureOpensAGapOnEveryRoot(t *testing.T) {
	h := newHarness(t, hourly)
	h.vikunja.also = []int64{otherTaskID}
	h.kelos.set(twoRoots(kelos.PhaseRunning), nil)
	h.loop.cycle(t.Context())
	roots := []model.Root{testRoot(), taskRootOf(otherTaskID)}
	read := make(map[model.Ref]time.Time)
	for _, root := range roots {
		read[root.Ref()] = *coverageOf(t, h.store.story(t, root), model.SourceKelos).LastSuccessfulRead
	}
	reads := h.vikunja.count()
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.loop.cycle(t.Context())
	if h.vikunja.count() != reads {
		t.Fatal("a root was read while Kelos was down; the test needs them unread")
	}
	h.kelos.set(twoRoots(kelos.PhaseSucceeded), nil)
	h.loop.cycle(t.Context())
	for _, root := range roots {
		cov := coverageOf(t, h.store.story(t, root), model.SourceKelos)
		got := gapsWith(cov, "kelos read failed")
		if len(got) != 1 || !got[0].Start.Equal(read[root.Ref()]) || !got[0].End.Equal(*cov.LastSuccessfulRead) {
			t.Errorf("%v: kelos failure gaps = %v, want one from %v to the next read %v",
				root.Ref(), got, read[root.Ref()], cov.LastSuccessfulRead)
		}
	}
}

func TestKelosRecoveryClosesTheGapOfUnchangedRuns(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.loop.cycle(t.Context())
	// The same runs as before the outage: only the open gap makes the root
	// due before the refresh interval.
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseRunning)}}, nil)
	h.loop.cycle(t.Context())
	cov := coverageOf(t, h.store.story(t, testRoot()), model.SourceKelos)
	if got := gapsWith(cov, "kelos read failed"); len(got) != 1 {
		t.Errorf("kelos failure gaps = %v, want one, closed by the first read after the outage", got)
	}
}

func TestKelosReadThatDoesNotNormalizeOpensAGap(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	invalid := testKelosTask(kelos.PhaseRunning)
	invalid.UID = ""
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{invalid}}, nil)
	h.loop.cycle(t.Context())
	open := h.loop.roots[testRoot().Ref()].Sources["kelos/"+testCluster].Open
	if open == nil || !strings.Contains(open.Reason, "do not normalize") {
		t.Errorf("open gap = %+v, want one saying the read does not normalize", open)
	}
}

func TestUncommittedKelosGapsAreCommittedWithTheCycle(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	h.store.setCommitErr(errors.New("fabricated database outage"))
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.loop.cycle(t.Context())
	h.store.setCommitErr(nil)
	// Kelos is back but names the root no more, so the root is not read:
	// only the cycle's commit can save its gap.
	h.kelos.set(kelos.Snapshot{}, nil)
	h.loop.cycle(t.Context())
	var st rootState
	if err := json.Unmarshal(h.store.cps[rootKey(testRoot())], &st); err != nil {
		t.Fatal(err)
	}
	if open := st.Sources["kelos/"+testCluster].Open; open == nil {
		t.Error("the stored root state has no Kelos gap open, want the one whose commit failed")
	}
}

func TestRestartGapsFailTheLoadWhenUncommitted(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	h.store.setCommitErr(errors.New("fabricated database outage"))
	l := h.loop
	if err := l.load(t.Context()); err == nil {
		t.Error("load() = nil with the restart's gaps not committed, want an error")
	}
}

func TestKelosFailureGapStartsAtTheRootsLastRead(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	read := h.loop.roots[testRoot().Ref()].Sources["kelos/"+testCluster].LastSuccess
	// A shared read that does not read the root, then a failed one.
	h.loop.cycle(t.Context())
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.loop.cycle(t.Context())
	open := h.loop.roots[testRoot().Ref()].Sources["kelos/"+testCluster].Open
	if open == nil || !open.Start.Equal(*read) {
		t.Errorf("open gap = %+v, want one from the root's own last read %v", open, read)
	}
}
