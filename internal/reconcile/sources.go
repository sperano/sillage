package reconcile

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// ProjectLister lists a Vikunja project's tasks changed since a time;
// *vikunja.Client is one.
type ProjectLister interface {
	ChangedTasks(ctx context.Context, project int64, since time.Time) ([]vikunja.TaskChange, error)
}

// Sources are the clients the loop reads through; each must be set.
type Sources struct {
	collector.Sources
	Projects ProjectLister
}

// SourcesOf returns the loop's sources of the live clients.
func SourcesOf(c collector.Clients) Sources {
	return Sources{Sources: c.Sources(), Projects: c.Vikunja}
}

func (s Sources) validate() error {
	if s.Vikunja == nil || s.Kelos == nil || s.GitHub == nil || s.Wiki == nil || s.Projects == nil {
		return errors.New("reconcile: every source needs a client")
	}
	return nil
}

// meter times each read of one source instance into the metrics.
type meter struct {
	m        *Metrics
	now      func() time.Time
	source   model.Source
	instance string
}

// timed runs read and records it.
func timed[T any](m meter, read func() (T, error)) (T, error) {
	start := m.now()
	v, err := read()
	m.m.observeRead(m.source, m.instance, start, m.now(), err)
	return v, err
}

type meteredVikunja struct {
	src collector.VikunjaSource
	meter
}

func (v meteredVikunja) Fetch(ctx context.Context, id int64) (vikunja.Snapshot, error) {
	return timed(v.meter, func() (vikunja.Snapshot, error) { return v.src.Fetch(ctx, id) })
}

type meteredGitHub struct {
	src collector.GitHubSource
	meter
}

func (g meteredGitHub) FetchBranch(ctx context.Context, base github.Repository, head github.Branch) (
	[]github.Snapshot, error) {
	return timed(g.meter, func() ([]github.Snapshot, error) { return g.src.FetchBranch(ctx, base, head) })
}

// cycleWiki meters page reads and lists the pages once per cycle: every
// root the cycle reads resolves its links against the same listing. A
// listing that failed is kept for the cycle, unless its root's context
// ended it: that says nothing of the wiki, and the next root lists again.
type cycleWiki struct {
	src collector.WikiSource
	meter
	mu     sync.Mutex
	listed bool
	pages  []wiki.Page
	err    error
}

func (w *cycleWiki) Fetch(ctx context.Context, id int64) (wiki.Snapshot, error) {
	return timed(w.meter, func() (wiki.Snapshot, error) { return w.src.Fetch(ctx, id) })
}

func (w *cycleWiki) Pages(ctx context.Context) ([]wiki.Page, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.listed {
		return w.pages, w.err
	}
	pages, err := timed(w.meter, func() ([]wiki.Page, error) { return w.src.Pages(ctx) })
	if err != nil && ctx.Err() != nil {
		return nil, err
	}
	w.listed, w.pages, w.err = true, pages, err
	return pages, err
}

// cycleKelos answers every root of a cycle with the cycle's one read of
// the namespace.
type cycleKelos struct {
	snap kelos.Snapshot
	err  error
}

func (k cycleKelos) Read(context.Context) (kelos.Snapshot, error) { return k.snap, k.err }
