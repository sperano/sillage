package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

// Loop reads the sources into the store, one cycle after another.
type Loop struct {
	cfg     Config
	ccfg    collector.Config
	src     Sources
	store   Store
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time
	// jitter spreads a wait; the identity in tests.
	jitter func(time.Duration) time.Duration
	// deliveries are the reads webhook deliveries ask for; nil without.
	deliveries Deliveries
	// conditional is the GitHub source, when it is a ConditionalGitHub;
	// spread spreads a restart's overdue backstop reads, and github counts
	// what the cycle asks of GitHub.
	conditional ConditionalGitHub
	spread      func(time.Duration) time.Duration
	github      githubCycle

	// roots and marks are what the store holds, loaded when the lock is
	// taken: each root's state, by ref, and each project's listing mark.
	roots map[model.Ref]*rootState
	marks map[int64]time.Time
	// unsaved are the roots whose state in roots has gaps the store does
	// not hold yet: their commit failed.
	unsaved map[model.Ref]bool
	// backoffs are the roots whose last read could not be saved, left
	// unread for a while.
	backoffs map[model.Ref]*backoff
	// githubResume is when the wait a GitHub rate limit asked for is over:
	// until then, no GitHub request is made.
	githubResume time.Time
	// hookMarks are the hooks' marks the store holds, by key, and
	// hooksChecked when the cycle that last read their logs started.
	hookMarks    map[string]int64
	hooksChecked time.Time
	// hookOwners are whether each hook, by key, sends to sillage, once
	// GitHub said where it sends.
	hookOwners map[string]bool
	// collecting is whether the loop holds the lock and has loaded the
	// store's state.
	collecting atomic.Bool
}

// New returns a loop reading src as ccfg describes, into store, each
// store operation within storeTimeout, and the roots deliveries ask for
// too unless it is nil. A source ccfg.Off names, or without a client, is
// off: never read.
func New(cfg Config, ccfg collector.Config, src Sources, store Store, deliveries Deliveries, m *Metrics,
	log *slog.Logger) (*Loop, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if store == nil || m == nil || log == nil {
		return nil, errors.New("reconcile: a store, metrics and a logger are required")
	}
	c, err := collector.New(ccfg, src.Sources)
	if err != nil {
		return nil, err
	}
	l := &Loop{
		cfg: cfg, ccfg: ccfg, src: src.without(c.Off()), store: boundedStore{s: store, timeout: storeTimeout},
		deliveries: deliveries, metrics: m, log: log, now: time.Now, jitter: jitter, spread: spread,
		hookOwners: make(map[string]bool),
	}
	if cg, ok := l.src.GitHub.(ConditionalGitHub); ok {
		l.conditional = cg
	}
	return l, nil
}

// jitter returns d spread by up to jitterFraction either way.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration((rand.Float64()*2-1)*jitterFraction*float64(d))
}

// sleep waits d, and reports false if ctx is done first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run collects until ctx is done, while it holds lock. While another
// process holds it, Run reads nothing, says so, and tries again every
// LockRetryInterval; so it does after losing the lock or failing to take
// it or to load the store. It says each reason once, until it changes.
func (l *Loop) Run(ctx context.Context, lock Locker) {
	said := ""
	say := func(level slog.Level, msg string, args ...any) {
		if msg != said {
			l.log.Log(ctx, level, msg, append(args, "retry", l.cfg.LockRetryInterval)...)
			said = msg
		}
	}
	for {
		err := lock.WithLock(ctx, func(ctx context.Context) error {
			return l.collect(ctx, func() { said = "" })
		})
		l.collecting.Store(false)
		l.metrics.lockHeld.Set(0)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, ErrLockHeld):
			say(slog.LevelInfo, "collector standing by: another sillage process holds the collector lock, "+
				"so this one reads nothing")
		case errors.Is(err, ErrLockLost):
			say(slog.LevelWarn, "collector lock lost: collection stops until the lock is taken again")
		case err != nil:
			say(slog.LevelError, "collector stopped: "+err.Error())
		}
		if !sleep(ctx, l.jitter(l.cfg.LockRetryInterval)) {
			return
		}
	}
}

// collect loads the store's state, calls started, and runs cycles until
// ctx is done.
func (l *Loop) collect(ctx context.Context, started func()) error {
	l.metrics.lockHeld.Set(1)
	if err := l.load(ctx); err != nil {
		l.metrics.storeErrors.WithLabelValues(operationLoad).Inc()
		return fmt.Errorf("reconcile: loading the collector's checkpoints: %w", err)
	}
	started()
	l.collecting.Store(true)
	for {
		l.cycle(ctx)
		if !l.wait(ctx) {
			return ctx.Err()
		}
	}
}

// Collecting reports whether the loop is collecting: it holds the lock
// and has loaded the store's state, so that the reads deliveries ask for
// are taken by its next cycle.
func (l *Loop) Collecting() bool { return l.collecting.Load() }

// load reads every root's state and project's mark, and opens a gap for
// every source of every root: from the root's last read, or the last
// cycle before the restart if later, to the next successful read.
func (l *Loop) load(ctx context.Context) error {
	cps, err := l.store.Checkpoints(ctx, checkpointPrefix)
	if err != nil {
		return err
	}
	l.roots, l.marks, l.unsaved = make(map[model.Ref]*rootState), make(map[int64]time.Time), make(map[model.Ref]bool)
	l.backoffs = make(map[model.Ref]*backoff)
	l.metrics.backoffRoots.Set(0)
	l.hookMarks, l.hooksChecked = make(map[string]int64), time.Time{}
	var lastCycle time.Time
	if data, ok := cps[heartbeatKey(l.ccfg.Kelos.Cluster)]; ok {
		if err := json.Unmarshal(data, &lastCycle); err != nil {
			return fmt.Errorf("%s: %w", heartbeatKey(l.ccfg.Kelos.Cluster), err)
		}
	}
	retired := make(map[string]int)
	for key, data := range cps {
		switch {
		case strings.HasPrefix(key, rootPrefix):
			st := newRootState(model.Root{})
			if err := json.Unmarshal(data, st); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			for _, source := range st.dropRetiredSources() {
				retired[source]++
			}
			l.roots[st.Root.Ref()] = st
		case strings.HasPrefix(key, projectPrefix+l.ccfg.VikunjaInstance+"/"):
			if err := l.loadMark(key, data); err != nil {
				return err
			}
		case strings.HasPrefix(key, hookPrefix+l.ccfg.GitHubInstance+"/"):
			if err := l.loadHookMark(key, data); err != nil {
				return err
			}
		}
	}
	for _, source := range slices.Sorted(maps.Keys(retired)) {
		l.log.Warn("dropped a retired source from the stored root states", "source", source, "roots", retired[source])
	}
	reason := restartReason(lastCycle, l.now())
	err = l.openGaps(ctx, func(st *sourceState) bool {
		if st.Source == model.SourceKelos {
			return st.openAfter(lastCycle, reason+lostMarkers)
		}
		return st.openAfter(lastCycle, reason)
	})
	if err != nil {
		return fmt.Errorf("committing the restart's gaps: %w", err)
	}
	now := l.now()
	for _, st := range l.roots {
		l.scheduleLoaded(st, now)
	}
	l.metrics.roots.Set(float64(len(l.roots)))
	l.log.Info("collector started", "roots", len(l.roots), "lastCycle", lastCycle)
	return nil
}

// lostMarkers ends the reason of a restart's gap on Kelos coverage: the
// phase markers pods sent meanwhile may not have been received (the
// receiver serves whether or not this process collects, and nothing tells
// sillage whether a pod sent one), and nothing sends them again.
const lostMarkers = "; the phase markers pods sent meanwhile may not have been received"

// restartReason is the reason of the gaps a restart at now opens.
func restartReason(lastCycle, now time.Time) string {
	if lastCycle.IsZero() {
		return "sillage was not collecting until it restarted at " + now.UTC().Format(time.RFC3339) +
			": what the source showed meanwhile, and no longer shows, is unknown"
	}
	return "sillage was not collecting after its last cycle, which ended at " + lastCycle.UTC().Format(time.RFC3339) +
		", until it restarted at " + now.UTC().Format(time.RFC3339) +
		": what the source showed meanwhile, and no longer shows, is unknown"
}

func (l *Loop) loadMark(key string, data []byte) error {
	var mark projectMark
	if err := json.Unmarshal(data, &mark); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	l.marks[mark.Project] = mark.Updated
	return nil
}
