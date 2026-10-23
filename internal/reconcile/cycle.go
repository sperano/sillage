package reconcile

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
)

// rootTimeout bounds the reads of one root: longer than the GitHub
// client's longest wait for a rate limit, so that a rate limit fails as
// one rather than as a timeout.
const rootTimeout = 2 * github.DefaultMaxRateLimitWait

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
	cr, discovered := l.readKelos(ctx)
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

// meter returns the meter of one source instance.
func (l *Loop) meter(src model.Source, instance string) meter {
	return meter{m: l.metrics, now: l.now, source: src, instance: instance}
}

// readKelos reads the namespace once for the cycle, and returns the
// cycle's collector over that read and the roots it names.
func (l *Loop) readKelos(ctx context.Context) (cycleRun, []collector.Discovered) {
	at := l.now()
	snap, err := timed(l.meter(model.SourceKelos, l.ccfg.Kelos.Cluster), func() (kelos.Snapshot, error) {
		return l.src.Kelos.Read(ctx)
	})
	c, cerr := collector.New(l.ccfg, collector.Sources{
		Vikunja: meteredVikunja{l.src.Vikunja, l.meter(model.SourceVikunja, l.ccfg.VikunjaInstance)},
		Kelos:   cycleKelos{snap: snap, err: err},
		GitHub:  meteredGitHub{l.src.GitHub, l.meter(model.SourceGitHub, l.ccfg.GitHubInstance)},
		Wiki:    &cycleWiki{src: l.src.Wiki, meter: l.meter(model.SourceWiki, l.ccfg.WikiInstance)},
	})
	if cerr != nil {
		// New accepted the same configuration and sources.
		panic("reconcile: the collector refused a configuration it accepted: " + cerr.Error())
	}
	cr := cycleRun{c: c, kelosAt: at}
	if err != nil {
		if ctx.Err() == nil {
			l.log.Warn("source read failed", "source", model.SourceKelos, "instance", l.ccfg.Kelos.Cluster,
				"reason", reasonOf(err), "error", err)
		}
		return cr, nil
	}
	discovered, err := c.Discover(snap)
	if err != nil {
		l.log.Warn("the Kelos read names no root: its objects do not normalize", "error", err)
		return cr, nil
	}
	return cr, discovered
}

// kelosDue returns the roots Kelos names that are due: never read, named
// by other runs than at their last read, or not read for a refresh
// interval.
func (l *Loop) kelosDue(discovered []collector.Discovered) map[model.Ref]*due {
	dues := make(map[model.Ref]*due)
	for _, d := range discovered {
		runs := digest(model.Records{Runs: d.Runs})
		st := l.roots[d.Root.Ref()]
		if st == nil || st.KelosRuns != runs || l.now().Sub(st.Read) >= l.jitter(l.cfg.RefreshInterval) {
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
