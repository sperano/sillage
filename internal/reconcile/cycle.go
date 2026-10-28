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
}

// cycleRun is one cycle's collector, over the cycle's one Kelos read, and
// when that read started.
type cycleRun struct {
	c       *collector.Collector
	kelosAt time.Time
}

// cycle reads what changed, and commits each root it read, then the
// project marks and when the cycle ended. The shutdown stops it between
// two roots or inside one, which then commits nothing.
func (l *Loop) cycle(ctx context.Context) {
	start := l.now()
	cr, discovered, err := l.readKelos(ctx)
	if err != nil {
		l.log.Error("no cycle: the collector refused its sources", "error", err)
		return
	}
	dues := l.kelosDue(discovered)
	marks := l.listProjects(ctx, dues)
	incomplete := make(map[int64]bool)
	committed := 0
	for _, ref := range slices.SortedFunc(maps.Keys(dues), compareRefs) {
		if ctx.Err() != nil {
			return
		}
		d := dues[ref]
		ok, read := l.collectRoot(ctx, cr, d)
		if ok {
			committed++
		}
		if !read {
			for _, p := range d.projects {
				incomplete[p] = true
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	l.commitCycle(ctx, marks, incomplete)
	l.metrics.cycles.Inc()
	l.log.Info("collection cycle", "due", len(dues), "committed", committed, "duration", l.now().Sub(start))
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
	cr := cycleRun{c: c, kelosAt: l.now()}
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

// kelosDue returns the roots Kelos names that are due: never read, named
// by other runs than at their last read, with a Kelos gap open (a failed
// Kelos read or a restart), which this read can close, or not read for a
// refresh interval.
func (l *Loop) kelosDue(discovered []collector.Discovered) map[model.Ref]*due {
	dues := make(map[model.Ref]*due)
	for _, d := range discovered {
		runs := digest(model.Records{Runs: d.Runs})
		st := l.roots[d.Root.Ref()]
		if st == nil || st.KelosRuns != runs || st.kelosGapOpen(l.ccfg.Kelos.Cluster) ||
			l.now().Sub(st.Read) >= l.jitter(l.cfg.RefreshInterval) {
			dues[d.Root.Ref()] = &due{root: d.Root, kelosRuns: runs}
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
