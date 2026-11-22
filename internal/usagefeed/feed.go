package usagefeed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/loki"
)

// The feed's defaults.
const (
	// DefaultInterval is how often a cycle runs.
	DefaultInterval = time.Minute
	// DefaultOverlap is how far before its read-through a cycle reads
	// again: exporters retry, and Loki accepts late entries.
	DefaultOverlap = 15 * time.Minute
	// DefaultDelay is how close to now a cycle reads: the last minute may
	// not have arrived yet.
	DefaultDelay = time.Minute
	// DefaultMaxWindow bounds the time one query covers.
	DefaultMaxWindow = time.Hour
)

// Source reads usage events from the telemetry store: those of the window
// from (inclusive) to to (exclusive). *loki.Client is one.
type Source interface {
	Query(ctx context.Context, from, to time.Time) (loki.Result, error)
}

// Store keeps usage events and how far they were read. *store.Store is
// one.
type Store interface {
	SaveUsage(ctx context.Context, events []model.UsageEvent) (int, error)
	UsageFeed(ctx context.Context) (*time.Time, error)
	SetUsageFeed(ctx context.Context, t time.Time) error
}

// Config configures a Feed.
type Config struct {
	Source Source
	Store  Store
	// Since is when the runners' usage telemetry started: the first read
	// starts there. Required.
	Since time.Time
	// Interval, Overlap, Delay and MaxWindow default to the Default
	// constants when zero.
	Interval, Overlap, Delay, MaxWindow time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Feed copies usage events from a Source into a Store.
type Feed struct {
	cfg Config
	m   *Metrics
	log *slog.Logger
}

// New returns a feed for cfg, which must be valid, recording into m.
func New(cfg Config, m *Metrics, log *slog.Logger) (*Feed, error) {
	if cfg.Source == nil || cfg.Store == nil || m == nil || log == nil {
		return nil, errors.New("usagefeed: a source, a store, metrics and a logger are required")
	}
	if cfg.Since.IsZero() {
		return nil, errors.New("usagefeed: Since is required")
	}
	if cfg.Interval < 0 || cfg.Overlap < 0 || cfg.Delay < 0 || cfg.MaxWindow < 0 {
		return nil, errors.New("usagefeed: durations cannot be negative")
	}
	cfg.Since = cfg.Since.UTC()
	cfg.Interval = durationOr(cfg.Interval, DefaultInterval)
	cfg.Overlap = durationOr(cfg.Overlap, DefaultOverlap)
	cfg.Delay = durationOr(cfg.Delay, DefaultDelay)
	cfg.MaxWindow = durationOr(cfg.MaxWindow, DefaultMaxWindow)
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Feed{cfg: cfg, m: m, log: log}, nil
}

func durationOr(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

// Run runs a cycle at once and then every Interval, until ctx is done. A
// failed cycle is logged and retried by the next.
func (f *Feed) Run(ctx context.Context) {
	for {
		if err := f.Cycle(ctx); err != nil && ctx.Err() == nil {
			f.log.Error("usage feed cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(f.cfg.Interval):
		}
	}
}

// Cycle reads what the feed has not read yet, window by window, up to
// Delay before now. It stops at the first window that fails to read or to
// save, without moving the read-through past it.
func (f *Feed) Cycle(ctx context.Context) error {
	through, err := f.cfg.Store.UsageFeed(ctx)
	if err != nil {
		f.m.errors.WithLabelValues(stageCheckpoint).Inc()
		return fmt.Errorf("usagefeed: reading the read-through: %w", err)
	}
	from := f.cfg.Since
	if through != nil {
		f.m.readThrough.Set(float64(through.Unix()))
		from = later(from, through.Add(-f.cfg.Overlap))
	}
	to := f.cfg.Now().UTC().Add(-f.cfg.Delay)
	for from.Before(to) {
		end := earlier(from.Add(f.cfg.MaxWindow), to)
		if err := f.window(ctx, from, end); err != nil {
			return err
		}
		from = end
	}
	return nil
}

// window reads, saves and checkpoints [from, to).
func (f *Feed) window(ctx context.Context, from, to time.Time) error {
	res, err := f.cfg.Source.Query(ctx, from, to)
	if err != nil {
		f.m.errors.WithLabelValues(stageRead).Inc()
		return fmt.Errorf("usagefeed: reading %s to %s: %w", from.Format(time.RFC3339), to.Format(time.RFC3339), err)
	}
	inserted, err := f.cfg.Store.SaveUsage(ctx, res.Events)
	if err != nil {
		f.m.errors.WithLabelValues(stageSave).Inc()
		return fmt.Errorf("usagefeed: saving %s to %s: %w", from.Format(time.RFC3339), to.Format(time.RFC3339), err)
	}
	f.record(from, to, res, inserted)
	if err := f.cfg.Store.SetUsageFeed(ctx, to); err != nil {
		f.m.errors.WithLabelValues(stageCheckpoint).Inc()
		return fmt.Errorf("usagefeed: moving the read-through to %s: %w", to.Format(time.RFC3339), err)
	}
	f.m.readThrough.Set(float64(to.Unix()))
	return nil
}

func (f *Feed) record(from, to time.Time, res loki.Result, inserted int) {
	f.m.read.Add(float64(len(res.Events)))
	f.m.inserted.Add(float64(inserted))
	f.m.duplicates.Add(float64(len(res.Events) - inserted))
	f.m.skipped.Add(float64(res.Skipped))
	f.m.malformed.Add(float64(res.Malformed))
	f.m.stalled.Add(float64(res.Stalled))
	if res.Malformed > 0 || res.Stalled > 0 {
		f.log.Warn("usage feed window had entries it could not use", "from", from, "to", to,
			"malformed", res.Malformed, "stalledPages", res.Stalled, "problems", res.Problems)
	}
	f.log.Debug("usage feed window read", "from", from, "to", to, "events", len(res.Events), "inserted", inserted)
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
