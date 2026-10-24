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

// SourcesOf returns the loop's sources of the live clients. A client that
// is not set stays nil, so that New refuses it.
func SourcesOf(c collector.Clients) Sources {
	s := Sources{Sources: c.Sources()}
	if c.Vikunja != nil {
		s.Projects = c.Vikunja
	}
	return s
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

// readTimeout bounds one read of a source: longer than the GitHub
// client's longest wait for a rate limit, so that a rate limit fails as
// one rather than as a timeout. Each read has its own, so that a root
// whose story spans several repositories has the same budget for each.
const readTimeout = 2 * github.DefaultMaxRateLimitWait

// timed runs read within readTimeout and records it.
func timed[T any](ctx context.Context, m meter, read func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	start := m.now()
	v, err := read(ctx)
	m.m.observeRead(m.source, m.instance, start, m.now(), err)
	return v, err
}

type meteredVikunja struct {
	src collector.VikunjaSource
	meter
}

func (v meteredVikunja) Fetch(ctx context.Context, id int64) (vikunja.Snapshot, error) {
	return timed(ctx, v.meter, func(ctx context.Context) (vikunja.Snapshot, error) { return v.src.Fetch(ctx, id) })
}

type meteredGitHub struct {
	src collector.GitHubSource
	meter
}

func (g meteredGitHub) FetchBranch(ctx context.Context, base github.Repository, head github.Branch) (
	[]github.Snapshot, error) {
	return timed(ctx, g.meter, func(ctx context.Context) ([]github.Snapshot, error) {
		return g.src.FetchBranch(ctx, base, head)
	})
}

// cycleWiki meters page reads and lists the pages once per cycle: every
// root the cycle reads resolves its links against the same listing. A
// listing that failed, its own deadline included, is kept for the cycle,
// unless the caller's context ended it: that says nothing of the wiki,
// and the next root lists again.
type cycleWiki struct {
	src collector.WikiSource
	meter
	mu     sync.Mutex
	listed bool
	pages  []wiki.Page
	err    error
}

func (w *cycleWiki) Fetch(ctx context.Context, id int64) (wiki.Snapshot, error) {
	return timed(ctx, w.meter, func(ctx context.Context) (wiki.Snapshot, error) { return w.src.Fetch(ctx, id) })
}

func (w *cycleWiki) Pages(ctx context.Context) ([]wiki.Page, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.listed {
		return w.pages, w.err
	}
	pages, err := timed(ctx, w.meter, w.src.Pages)
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
