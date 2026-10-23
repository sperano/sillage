package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
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

// WikiSource reads a page, and finds a page by its path.
type WikiSource interface {
	Fetch(ctx context.Context, pageID int64) (wiki.Snapshot, error)
	FindPage(ctx context.Context, locale, path string) (int64, bool, error)
}

// Sources are the clients the collector reads through; each must be set.
type Sources struct {
	Vikunja VikunjaSource
	Kelos   KelosSource
	GitHub  GitHubSource
	Wiki    WikiSource
}

// Collector assembles stories from its sources.
type Collector struct {
	cfg Config
	src Sources
	now func() time.Time
}

// New returns a collector reading src as cfg describes.
func New(cfg Config, src Sources) (*Collector, error) {
	if src.Vikunja == nil || src.Kelos == nil || src.GitHub == nil || src.Wiki == nil {
		return nil, errors.New("collector: every source needs a client")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Collector{cfg: cfg, src: src, now: time.Now}, nil
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
	if err := root.Validate(); err != nil {
		return Result{}, err
	}
	a := newAssembly(c, root)
	switch {
	case root.Task != nil:
		if root.Task.Instance != c.cfg.VikunjaInstance {
			return Result{}, fmt.Errorf("collector: task %s is not on %s", root.Task, c.cfg.VikunjaInstance)
		}
		a.collectTask(ctx, root.Task.ID, repos)
	default:
		if root.Page.Instance != c.cfg.WikiInstance {
			return Result{}, fmt.Errorf("collector: page %s is not on %s", root.Page, c.cfg.WikiInstance)
		}
		if err := a.collectPage(ctx, *root.Page, repos); err != nil {
			return Result{}, err
		}
	}
	story, err := a.build()
	if err != nil {
		return Result{}, fmt.Errorf("collector: the assembled records are invalid (a collector bug): %w", err)
	}
	return Result{Story: story, Failures: a.failures}, nil
}
