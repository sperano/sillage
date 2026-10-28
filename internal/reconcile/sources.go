package reconcile

import (
	"context"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// ProjectLister lists a Vikunja project's tasks changed since a time;
// *vikunja.Client is one.
type ProjectLister interface {
	ChangedTasks(ctx context.Context, project int64, since time.Time) ([]vikunja.TaskChange, error)
}

// Sources are the clients the loop reads through. A source without a
// client is off, as the collector's are: it is never read, and its
// coverage of every root read says why. Without Projects, no project is
// listed.
type Sources struct {
	collector.Sources
	Projects ProjectLister
}

// SourcesOf returns the loop's sources of the live clients. A client that
// is not set stays nil, so that its source is off.
func SourcesOf(c collector.Clients) Sources {
	s := Sources{Sources: c.Sources()}
	if c.Vikunja != nil {
		s.Projects = c.Vikunja
	}
	return s
}

// without returns s without the clients of the sources off names: those
// are never read, whatever client they have.
func (s Sources) without(off map[model.Source]string) Sources {
	if _, ok := off[model.SourceVikunja]; ok {
		s.Vikunja, s.Projects = nil, nil
	}
	if _, ok := off[model.SourceKelos]; ok {
		s.Kelos = nil
	}
	if _, ok := off[model.SourceGitHub]; ok {
		s.GitHub = nil
	}
	return s
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

// cycleKelos answers every root of a cycle with the cycle's one read of
// the namespace.
type cycleKelos struct {
	snap kelos.Snapshot
	err  error
}

func (k cycleKelos) Read(context.Context) (kelos.Snapshot, error) { return k.snap, k.err }
