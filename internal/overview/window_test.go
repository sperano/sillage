package overview

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestTouches(t *testing.T) {
	known := func(min int) model.Bound { return model.KnownAt(minute(min)) }
	unknown, open := model.UnknownBound(), model.OpenEnd()
	for _, c := range []struct {
		name       string
		start, end model.Bound
		want       bool
		latest     int // minutes past 14:00 when want
	}{
		{"inside", known(10), known(20), true, 20},
		{"across the start", known(-30), known(20), true, 20},
		{"across the whole window", known(-30), known(90), true, 60},
		{"open since before", known(-30), open, true, 60},
		{"before", known(-30), known(-10), false, 0},
		{"ending at the start", known(-30), known(0), true, 0},
		{"starting at the end", known(60), known(90), false, 0},
		{"after", known(70), known(90), false, 0},
		{"unknown end, start inside", known(10), unknown, true, 10},
		{"unknown end, start before", known(-10), unknown, false, 0},
		{"unknown start, end inside", unknown, known(10), true, 10},
		{"unknown start, end after", unknown, known(70), false, 0},
		{"both unknown", unknown, unknown, false, 0},
	} {
		i := model.Interval{Start: c.start, End: c.end}
		if got := touches(afternoon, i); got != c.want {
			t.Errorf("%s: touches = %v, want %v", c.name, got, c.want)
		}
		latest, ok := latestIn(afternoon, i)
		if ok != c.want || (ok && !latest.Equal(minute(c.latest))) {
			t.Errorf("%s: latest = %v %v, want %v at 14:00 + %d min", c.name, latest, ok, c.want, c.latest)
		}
	}
}

// A run waiting to start shows from its creation.
func TestRunExtentFromCreation(t *testing.T) {
	created := minute(-5)
	r := model.Run{Created: &created, Interval: model.Interval{Start: model.UnknownBound(), End: model.OpenEnd()}}
	if !touches(afternoon, runExtent(r)) {
		t.Error("a run created before the window and not ended does not touch it")
	}
	if a, ok := runAnchor(r); !ok || !a.Equal(created) {
		t.Errorf("anchor = %v %v, want the creation", a, ok)
	}
}
