package reconcile

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// due is a root a cycle reads, and what says it changed.
type due struct {
	root model.Root
	// kelosRuns identifies the runs the cycle's Kelos read named the root
	// with; empty when it named none.
	kelosRuns string
	// updated is the task's updated time a project listing gave; nil when
	// none listed it.
	updated *time.Time
	// projects are the projects whose listings named it.
	projects []int64
	// delivery is the kind of read a webhook delivery asked for
	// (deliveryCaused, deliveryCoalesced); empty when none did.
	delivery string
	// github is how the read reads the root's GitHub side, and trigger
	// what made it read it whole. githubOnly is whether nothing but its
	// GitHub side (the backstop, an owed read) made the root due.
	github     githubRead
	trigger    string
	githubOnly bool
	// owed is since when the root is owed the GitHub read a trigger asked
	// for and the rate limit deferred; nil when it is owed none.
	owed *time.Time
	// lost is the earliest delivery naming the root that GitHub could not
	// hand to sillage nor send again; nil when none did. hooks are the
	// keys of the hooks whose logs showed one.
	lost  *lostDelivery
	hooks []string
}

// cycleRun is one cycle's collector, over the cycle's one Kelos read,
// that read, and when it started.
type cycleRun struct {
	c       *collector.Collector
	read    *cycleKelos
	kelosAt time.Time
}

// rootCollector returns a collector over the cycle's Kelos read, reading
// GitHub through gh: one root's.
func (l *Loop) rootCollector(cr cycleRun, gh *rootGitHub) (*collector.Collector, error) {
	src := l.cycleSources(cr.read)
	if src.GitHub != nil {
		src.GitHub = gh
	}
	return collector.New(l.ccfg, src)
}

// cycle reads what changed, what deliveries asked for and the GitHub
// reads owed, in compareDues' order, and commits each root it read, then
// the project marks and when the cycle ended. A root backing off is not
// read. The shutdown stops it between two roots or inside one, which then
// commits nothing.
func (l *Loop) cycle(ctx context.Context) {
	start := l.now()
	l.startGitHubCycle()
	cr, discovered, err := l.readKelos(ctx)
	if err != nil {
		l.log.Error("no cycle: the collector refused its sources", "error", err)
		return
	}
	dues := l.kelosDue(discovered)
	marks := cycleMarks{projects: l.listProjects(ctx, dues), hooks: l.checkHooks(ctx, dues),
		incomplete: make(map[int64]bool), hooksIncomplete: make(map[string]bool)}
	l.addDelivered(dues)
	l.addOwed(dues)
	committed, delivered := 0, 0
	for _, d := range slices.SortedFunc(maps.Values(dues), compareDues) {
		if ctx.Err() != nil {
			return
		}
		if l.backingOff(d) {
			marks.read(d, false, false)
			continue
		}
		if d.delivery != "" {
			delivered++
			l.metrics.deliveryReads.WithLabelValues(d.delivery).Inc()
		}
		ok, read := l.collectRoot(ctx, cr, d)
		if ok {
			committed++
		}
		marks.read(d, ok, read)
	}
	if ctx.Err() != nil {
		return
	}
	l.commitCycle(ctx, marks)
	l.metrics.cycles.Inc()
	l.log.Info("collection cycle", "due", len(dues), "delivered", delivered, "committed", committed,
		"backoff", len(l.backoffs), "duration", l.now().Sub(start), l.endGitHubCycle())
}

func compareRefs(a, b model.Ref) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID))
}

// meterFor returns the meter of one source instance.
func (l *Loop) meterFor(src model.Source, instance string) meter {
	return meter{m: l.metrics, now: l.now, source: src, instance: instance}
}

// readKelos reads the namespace once for the cycle, and returns the
// cycle's collector over that read and the roots it names. A read whose
// objects do not normalize serves no root: it fails, as metrics and gaps
// say. With Kelos off, nothing is read and no root is named.
func (l *Loop) readKelos(ctx context.Context) (cycleRun, []collector.Discovered, error) {
	read := &cycleKelos{}
	c, err := collector.New(l.ccfg, l.cycleSources(read))
	if err != nil {
		return cycleRun{}, nil, err
	}
	cr := cycleRun{c: c, read: read, kelosAt: l.now()}
	if l.src.Kelos == nil {
		return cr, nil, nil
	}
	var discovered []collector.Discovered
	read.snap, read.err = timed(ctx, l.meterFor(model.SourceKelos, l.ccfg.Kelos.Cluster),
		func(ctx context.Context) (kelos.Snapshot, error) {
			snap, err := l.src.Kelos.Read(ctx)
			if err != nil {
				return snap, err
			}
			if discovered, err = c.Discover(snap); err != nil {
				return snap, fmt.Errorf("its objects do not normalize: %w", err)
			}
			return snap, nil
		})
	if read.err != nil {
		if ctx.Err() == nil {
			l.kelosFailed(ctx, read.err)
		}
		return cr, nil, nil
	}
	return cr, discovered, nil
}

// cycleSources are the sources of a cycle's collector: each read metered,
// and Kelos answered by the cycle's one read. A source that is off stays
// without a client.
func (l *Loop) cycleSources(read *cycleKelos) collector.Sources {
	var s collector.Sources
	if l.src.Vikunja != nil {
		s.Vikunja = meteredVikunja{src: l.src.Vikunja, meter: l.meterFor(model.SourceVikunja, l.ccfg.VikunjaInstance)}
	}
	if l.src.Kelos != nil {
		s.Kelos = read
	}
	if l.src.GitHub != nil {
		s.GitHub = meteredGitHub{src: l.src.GitHub, meter: l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)}
	}
	return s
}

// kelosDue returns the roots Kelos names that are due: never read or
// named by other runs than at their last read, GitHub included; with a
// Kelos gap open (a failed Kelos read or a restart), which this read can
// close, or not read for a refresh interval, GitHub kept; or due for the
// GitHub backstop.
func (l *Loop) kelosDue(discovered []collector.Discovered) map[model.Ref]*due {
	dues := make(map[model.Ref]*due)
	for _, disc := range discovered {
		runs := digest(model.Records{Runs: disc.Runs})
		d := &due{root: disc.Root, kelosRuns: runs}
		st := l.roots[disc.Root.Ref()]
		if st == nil || st.KelosRuns != runs {
			d.readGitHub(triggerRun)
			dues[disc.Root.Ref()] = d
			continue
		}
		refresh := st.kelosGapOpen(l.ccfg.Kelos.Cluster) || l.now().Sub(st.Read) >= l.jitter(l.cfg.RefreshInterval)
		if l.backstopDue(st) {
			d.github, d.githubOnly = githubBackstop, !refresh
		}
		if refresh || d.github != githubKept {
			dues[disc.Root.Ref()] = d
		}
	}
	return dues
}

// encode marshals a checkpoint.
func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic("reconcile: a checkpoint does not marshal: " + err.Error())
	}
	return data
}
