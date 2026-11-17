package collector

import (
	"context"
	"fmt"
	"maps"
	"regexp"
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
	// prLink matches a link to a pull request on the GitHub instance.
	prLink *regexp.Regexp
}

// New returns a collector reading src as cfg describes. A source cfg.Off
// names, or without a client, is off.
func New(cfg Config, src Sources, opts ...Option) (*Collector, error) {
	off := offSources(cfg, src)
	if err := cfg.validate(off); err != nil {
		return nil, err
	}
	c := &Collector{cfg: cfg, src: src, off: off, now: time.Now, prLink: prLinkPattern(cfg.GitHubInstance)}
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

// Refusal is a part of a source read the source refused (403 Forbidden or
// 404 Not Found) while the rest was read: the read still succeeded, and
// the story's coverage notes what is unknown.
type Refusal struct {
	Source   model.Source
	Instance string
	// Resource is what was refused: github.ResourceCheckRuns or
	// github.ResourceReviewComments.
	Resource string
	// Code is the HTTP status the source answered; Count how many of the
	// read's requests it answered with it, those remembered included.
	Code  int
	Count int
	// Remembered is how many of Count the client did not send, as it
	// remembered the refusal of an earlier read: Count-Remembered were
	// refused just now.
	Remembered int
}

// Sent is how many requests the source refused in this read: Count less
// those remembered.
func (r Refusal) Sent() int { return r.Count - r.Remembered }

func (r Refusal) String() string {
	if r.Remembered > 0 {
		return fmt.Sprintf("%s %s: %s: %d on %d requests (%d remembered)",
			r.Source, r.Instance, r.Resource, r.Code, r.Count, r.Remembered)
	}
	return fmt.Sprintf("%s %s: %s: %d on %d requests", r.Source, r.Instance, r.Resource, r.Code, r.Count)
}

// Result is a story, the reads that failed while assembling it, and the
// parts of the others that were refused.
type Result struct {
	Story    model.Story
	Failures []Failure
	Refusals []Refusal
	// Searches counts the repositories whose pull requests of the story's
	// branch were listed because none of its records named a repository:
	// one listing each, of every repository EnvWorkspaces maps to.
	Searches int
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
	return Result{Story: story, Failures: a.failures, Refusals: a.refusals, Searches: a.searches}, nil
}
