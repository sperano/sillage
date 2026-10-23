package reconcile

import (
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
	// A cycle that does not read the root, then another restart.
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

func TestKelosFailureOpensAGapOnEveryRoot(t *testing.T) {
	h := newHarness(t, hourly)
	h.loop.cycle(t.Context())
	read := *coverageOf(t, h.store.story(t, testRoot()), model.SourceKelos).LastSuccessfulRead
	reads := h.vikunja.count()
	h.kelos.set(kelos.Snapshot{}, errors.New("fabricated API server outage"))
	h.loop.cycle(t.Context())
	if h.vikunja.count() != reads {
		t.Fatal("the root was read while Kelos was down; the test needs it unread")
	}
	h.kelos.set(kelos.Snapshot{Tasks: []kelos.Task{testKelosTask(kelos.PhaseSucceeded)}}, nil)
	h.loop.cycle(t.Context())
	cov := coverageOf(t, h.store.story(t, testRoot()), model.SourceKelos)
	got := gapsWith(cov, "kelos read failed")
	if len(got) != 1 || !got[0].Start.Equal(read) || !got[0].End.Equal(*cov.LastSuccessfulRead) {
		t.Errorf("kelos failure gaps = %v, want one from %v to the next read %v", got, read, cov.LastSuccessfulRead)
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
