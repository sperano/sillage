package usagefeed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/loki"
)

var (
	since = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	clock = time.Date(2026, 3, 1, 3, 30, 0, 0, time.UTC)
)

type window struct{ from, to time.Time }

type fakeSource struct {
	windows []window
	results map[time.Time]loki.Result // by window start
	failAt  int                       // 1-based call that fails, 0 for none
	onQuery func()
}

func (f *fakeSource) Query(_ context.Context, from, to time.Time) (loki.Result, error) {
	f.windows = append(f.windows, window{from, to})
	if f.onQuery != nil {
		f.onQuery()
	}
	if len(f.windows) == f.failAt {
		return loki.Result{}, errors.New("loki down")
	}
	return f.results[from], nil
}

type fakeStore struct {
	through                    *time.Time
	saved                      [][]model.UsageEvent
	keys                       map[model.UsageKey]bool
	failSave, failSet, failGet bool
}

func (f *fakeStore) SaveUsage(_ context.Context, events []model.UsageEvent) (int, error) {
	if f.failSave {
		return 0, errors.New("disk full")
	}
	f.saved = append(f.saved, events)
	if f.keys == nil {
		f.keys = map[model.UsageKey]bool{}
	}
	n := 0
	for _, e := range events {
		if !f.keys[e.Key()] {
			f.keys[e.Key()] = true
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) UsageFeed(context.Context) (*time.Time, error) {
	if f.failGet {
		return nil, errors.New("db down")
	}
	return f.through, nil
}

func (f *fakeStore) SetUsageFeed(_ context.Context, t time.Time) error {
	if f.failSet {
		return errors.New("db down")
	}
	if f.through == nil || t.After(*f.through) {
		f.through = &t
	}
	return nil
}

func event(seq string) model.UsageEvent {
	return model.UsageEvent{PodUID: "uid", Event: "api_request", Stream: "s", Seq: seq}
}

func newFeed(t *testing.T, src Source, st Store, mod func(*Config)) (*Feed, *Metrics) {
	t.Helper()
	cfg := Config{Source: src, Store: st, Since: since, Now: func() time.Time { return clock }}
	if mod != nil {
		mod(&cfg)
	}
	m := NewMetrics(prometheus.NewRegistry())
	f, err := New(cfg, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return f, m
}

func TestFirstCycleReadsFromSinceInBoundedWindows(t *testing.T) {
	src, st := &fakeSource{}, &fakeStore{}
	f, _ := newFeed(t, src, st, nil)
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	end := clock.Add(-DefaultDelay)
	want := []window{
		{since, since.Add(time.Hour)}, {since.Add(time.Hour), since.Add(2 * time.Hour)},
		{since.Add(2 * time.Hour), since.Add(3 * time.Hour)}, {since.Add(3 * time.Hour), end},
	}
	if len(src.windows) != len(want) {
		t.Fatalf("windows = %v, want %v", src.windows, want)
	}
	for i := range want {
		if src.windows[i] != want[i] {
			t.Errorf("window %d = %v, want %v", i, src.windows[i], want[i])
		}
	}
	if st.through == nil || !st.through.Equal(end) {
		t.Errorf("read-through = %v, want %v", st.through, end)
	}
}

func TestCycleOverlapsTheReadThrough(t *testing.T) {
	through := since.Add(2 * time.Hour)
	src, st := &fakeSource{}, &fakeStore{through: &through}
	f, _ := newFeed(t, src, st, func(c *Config) { c.MaxWindow = 24 * time.Hour })
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(src.windows) != 1 || src.windows[0] != (window{through.Add(-DefaultOverlap), clock.Add(-DefaultDelay)}) {
		t.Errorf("windows = %v", src.windows)
	}
}

func TestCycleNeverReadsBeforeSince(t *testing.T) {
	through := since.Add(time.Minute)
	src, st := &fakeSource{}, &fakeStore{through: &through}
	f, _ := newFeed(t, src, st, func(c *Config) { c.MaxWindow = 24 * time.Hour })
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if src.windows[0].from != since {
		t.Errorf("from = %v, want Since %v", src.windows[0].from, since)
	}
}

func TestCycleReadsNothingWhenCaughtUp(t *testing.T) {
	through := clock.Add(-DefaultDelay)
	src, st := &fakeSource{}, &fakeStore{through: &through}
	f, _ := newFeed(t, src, st, func(c *Config) { c.Overlap = time.Nanosecond; c.Now = func() time.Time { return clock } })
	// Within the overlap there is a sliver to re-read: one window.
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(src.windows) != 1 || !st.through.Equal(through) {
		t.Errorf("windows = %v, read-through = %v", src.windows, st.through)
	}
	src.windows = nil
	f2, _ := newFeed(t, src, st, func(c *Config) { c.Since = clock; c.Overlap = time.Nanosecond })
	if err := f2.Cycle(t.Context()); err != nil || len(src.windows) != 0 {
		t.Errorf("a feed that starts after now read %v, %v", src.windows, err)
	}
}

func TestFailedWindowStopsTheCycleWithoutMovingTheCheckpoint(t *testing.T) {
	src := &fakeSource{failAt: 2}
	st := &fakeStore{}
	f, m := newFeed(t, src, st, nil)
	err := f.Cycle(t.Context())
	if err == nil || !strings.Contains(err.Error(), "loki down") {
		t.Fatalf("error = %v", err)
	}
	if want := since.Add(time.Hour); st.through == nil || !st.through.Equal(want) {
		t.Errorf("read-through = %v, want the end of the first window %v", st.through, want)
	}
	if len(src.windows) != 2 {
		t.Errorf("%d windows read, want the cycle to stop at the failure", len(src.windows))
	}
	if got := testutil.ToFloat64(m.errors.WithLabelValues(stageRead)); got != 1 {
		t.Errorf("read errors = %v, want 1", got)
	}
	// The next cycle retries the failed window.
	src.failAt, src.windows = 0, nil
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if want := since.Add(time.Hour - DefaultOverlap); src.windows[0].from != want {
		t.Errorf("retry from %v, want %v", src.windows[0].from, want)
	}
}

func TestFailedSaveKeepsTheCheckpoint(t *testing.T) {
	src, st := &fakeSource{}, &fakeStore{failSave: true}
	f, m := newFeed(t, src, st, nil)
	if err := f.Cycle(t.Context()); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v", err)
	}
	if st.through != nil || len(src.windows) != 1 {
		t.Errorf("read-through = %v after %d windows, want none moved and one read", st.through, len(src.windows))
	}
	if got := testutil.ToFloat64(m.errors.WithLabelValues(stageSave)); got != 1 {
		t.Errorf("save errors = %v, want 1", got)
	}
}

func TestFailedCheckpointStopsTheCycle(t *testing.T) {
	src, st := &fakeSource{}, &fakeStore{failSet: true}
	f, m := newFeed(t, src, st, nil)
	if err := f.Cycle(t.Context()); err == nil || !strings.Contains(err.Error(), "moving the read-through") {
		t.Fatalf("error = %v", err)
	}
	if len(src.windows) != 1 || testutil.ToFloat64(m.errors.WithLabelValues(stageCheckpoint)) != 1 {
		t.Errorf("windows = %v", src.windows)
	}
	st2 := &fakeStore{failGet: true}
	f2, _ := newFeed(t, &fakeSource{}, st2, nil)
	if err := f2.Cycle(t.Context()); err == nil || !strings.Contains(err.Error(), "reading the read-through") {
		t.Errorf("error = %v", err)
	}
}

func TestMetricsCountWhatWasRead(t *testing.T) {
	src := &fakeSource{results: map[time.Time]loki.Result{
		since: {Events: []model.UsageEvent{event("1"), event("2"), event("2")}, Skipped: 4, Malformed: 2, Stalled: 1},
	}}
	st := &fakeStore{}
	f, m := newFeed(t, src, st, func(c *Config) { c.MaxWindow = 24 * time.Hour })
	if err := f.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		got  float64
		want float64
	}{
		"read": {testutil.ToFloat64(m.read), 3}, "inserted": {testutil.ToFloat64(m.inserted), 2},
		"duplicates": {testutil.ToFloat64(m.duplicates), 1}, "skipped": {testutil.ToFloat64(m.skipped), 4},
		"malformed": {testutil.ToFloat64(m.malformed), 2}, "stalled": {testutil.ToFloat64(m.stalled), 1},
		"read-through": {testutil.ToFloat64(m.readThrough), float64(clock.Add(-DefaultDelay).Unix())},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestRepeatedCycleSavesEventsOnce(t *testing.T) {
	src := &fakeSource{results: map[time.Time]loki.Result{since: {Events: []model.UsageEvent{event("1")}}}}
	st := &fakeStore{}
	f, m := newFeed(t, src, st, func(c *Config) { c.MaxWindow = 24 * time.Hour })
	for range 2 {
		if err := f.Cycle(t.Context()); err != nil {
			t.Fatal(err)
		}
		src.results = map[time.Time]loki.Result{since: {Events: []model.UsageEvent{event("1")}}}
	}
	if got := testutil.ToFloat64(m.inserted); got != 1 {
		t.Errorf("inserted = %v, want 1", got)
	}
}

func TestRunCyclesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	const wantCycles = 3
	src := &fakeSource{}
	src.onQuery = func() {
		if len(src.windows) >= wantCycles*4 {
			cancel()
		}
	}
	f, _ := newFeed(t, src, &fakeStore{}, func(c *Config) { c.Interval = time.Nanosecond; c.MaxWindow = 24 * time.Hour })
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	<-done
	if len(src.windows) < wantCycles {
		t.Errorf("%d windows read, want at least %d cycles", len(src.windows), wantCycles)
	}
}

func TestNewValidates(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewMetrics(prometheus.NewRegistry())
	good := Config{Source: &fakeSource{}, Store: &fakeStore{}, Since: since}
	for name, mod := range map[string]func(*Config){
		"no source": func(c *Config) { c.Source = nil }, "no store": func(c *Config) { c.Store = nil },
		"no since": func(c *Config) { c.Since = time.Time{} }, "negative": func(c *Config) { c.Overlap = -1 },
	} {
		cfg := good
		mod(&cfg)
		if _, err := New(cfg, m, log); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if f, err := New(good, m, log); err != nil || f.cfg.Interval != DefaultInterval || f.cfg.Overlap != DefaultOverlap ||
		f.cfg.Delay != DefaultDelay || f.cfg.MaxWindow != DefaultMaxWindow || f.cfg.Now == nil {
		t.Errorf("defaults = %+v, %v", f, err)
	}
}
