package collector

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// VikunjaSource reads a task and its comments.
type VikunjaSource interface {
	Fetch(ctx context.Context, taskID int64) (vikunja.Snapshot, error)
}

// KelosSource reads a namespace's Tasks and TaskRecords.
type KelosSource interface {
	Read(ctx context.Context) (kelos.Snapshot, error)
}

// GitHubSource reads the pull requests of a branch.
type GitHubSource interface {
	FetchBranch(ctx context.Context, base github.Repository, head github.Branch) ([]github.Snapshot, error)
}

// Sources are the clients the collector reads through. A source without
// a client is off: it is never read, and its coverage says why.
type Sources struct {
	Vikunja VikunjaSource
	Kelos   KelosSource
	GitHub  GitHubSource
}

// NoClient is why a source without a client is off, when Config.Off
// gives no other reason.
const NoClient = "no client is configured"

// Collector assembles stories from its sources.
type Collector struct {
	cfg Config
	src Sources
	// off says why each source that is off is not read.
	off map[model.Source]string
	now func() time.Time
	// origins keeps and recalls the Tasks' origins; nil without one.
	origins OriginMemory
}

// New returns a collector reading src as cfg describes. A source cfg.Off
// names, or without a client, is off.
func New(cfg Config, src Sources, opts ...Option) (*Collector, error) {
	off := offSources(cfg, src)
	if err := cfg.validate(off); err != nil {
		return nil, err
	}
	c := &Collector{cfg: cfg, src: src, off: off, now: time.Now}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// offSources says why each source cfg or src leaves off is not read.
func offSources(cfg Config, src Sources) map[model.Source]string {
	off := maps.Clone(cfg.Off)
	if off == nil {
		off = make(map[model.Source]string)
	}
	for s, set := range map[model.Source]bool{
		model.SourceVikunja: src.Vikunja != nil, model.SourceKelos: src.Kelos != nil, model.SourceGitHub: src.GitHub != nil,
	} {
		if _, ok := off[s]; !ok && !set {
			off[s] = NoClient
		}
	}
	return off
}

// Off says why each source that is off is not read.
func (c *Collector) Off() map[model.Source]string { return maps.Clone(c.off) }

// on reports whether source s is read.
func (c *Collector) on(s model.Source) bool {
	_, off := c.off[s]
	return !off
}

// Failure is a source read that failed: its source contributes no record
// to the story, and its coverage has no read.
type Failure struct {
	Source   model.Source
	Instance string
	Err      error
}

func (f Failure) Error() string { return fmt.Sprintf("%s %s: %v", f.Source, f.Instance, f.Err) }

func (f Failure) Unwrap() error { return f.Err }

// Result is a story and the reads that failed while assembling it.
type Result struct {
	Story    model.Story
	Failures []Failure
}

// Collect reads root's records from every source and assembles its story.
// repos are read for the root's branch besides those its runs' workspaces
// name. An error means the story could not be assembled: an invalid root,
// or records BuildStory rejects, which is a collector bug. A source that
// fails is reported in Result.Failures instead.
func (c *Collector) Collect(ctx context.Context, root model.Root, repos ...github.Repository) (Result, error) {
	return c.collect(ctx, root, Kept{}, repos)
}

func (c *Collector) collect(ctx context.Context, root model.Root, kept Kept, repos []github.Repository) (
	Result, error) {
	if err := root.Validate(); err != nil {
		return Result{}, err
	}
	a := newAssembly(c, root)
	a.kept = kept
	if root.Task != nil {
		if err := a.collectTask(ctx, *root.Task, repos); err != nil {
			return Result{}, err
		}
	}
	story, err := a.build()
	if err != nil {
		return Result{}, fmt.Errorf("collector: the assembled records are invalid (a collector bug): %w", err)
	}
	return Result{Story: story, Failures: a.failures}, nil
}
