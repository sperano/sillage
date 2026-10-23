package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

const (
	// storyCommand prints one story's JSON, assembled from the live
	// sources the environment configures.
	storyCommand = "story"
	// The story command's flags.
	taskFlag   = "task"
	pageFlag   = "page"
	localeFlag = "locale"
	repoFlag   = "repo"
	// defaultLocale is the locale of a page named without one.
	defaultLocale = "en"
	// exitPartial is the exit status of a story printed while a source
	// could not be read: the story is valid but says less than it could.
	exitPartial = 3
	// jsonIndent indents the printed story.
	jsonIndent = "  "
)

// errPartial marks a story printed while a source could not be read; the
// failed reads are already reported.
var errPartial = errors.New("some sources could not be read")

// storyCollector assembles a story; *collector.Collector is one.
type storyCollector interface {
	Collect(ctx context.Context, root model.Root, repos ...github.Repository) (collector.Result, error)
}

// openFunc returns the collector the environment configures, and its
// configuration.
type openFunc func(getenv func(string) string) (storyCollector, collector.Config, error)

// openCollector opens the live sources the environment configures.
func openCollector(getenv func(string) string) (storyCollector, collector.Config, error) {
	cfg, err := collector.ConfigFromEnv(getenv)
	if err != nil {
		return nil, collector.Config{}, err
	}
	c, err := cfg.Open()
	if err != nil {
		return nil, collector.Config{}, err
	}
	return c, cfg.Collector, nil
}

// storyArgs is what the story command was asked for.
type storyArgs struct {
	task, page int64
	locale     string
	repos      []github.Repository
}

// root is the story's root on the instances cfg names.
func (a storyArgs) root(cfg collector.Config) model.Root {
	if a.task > 0 {
		return model.Root{Task: &model.TaskKey{Instance: cfg.VikunjaInstance, ID: a.task}}
	}
	return model.Root{Page: &model.PageKey{Instance: cfg.WikiInstance, ID: a.page, Locale: a.locale}}
}

// parseStoryArgs parses the story command's flags: exactly one of -task
// and -page. Errors are written to output with the usage.
func parseStoryArgs(args []string, output io.Writer) (storyArgs, error) {
	a := storyArgs{locale: defaultLocale}
	flags := flag.NewFlagSet("sillage "+storyCommand, flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Int64Var(&a.task, taskFlag, 0, "print the story of Vikunja task `id`")
	flags.Int64Var(&a.page, pageFlag, 0, "print the story of wiki page `id`")
	flags.StringVar(&a.locale, localeFlag, a.locale, "the wiki page's `locale`")
	flags.Func(repoFlag, "also read the story's branch in `owner/name` (repeatable)", func(s string) error {
		r, err := github.ParseRepository(s)
		a.repos = append(a.repos, r)
		return err
	})
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return storyArgs{}, err
		}
		return storyArgs{}, fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 || (a.task > 0) == (a.page > 0) || a.task < 0 || a.page < 0 {
		fmt.Fprintf(output, "want exactly one positive -%s or -%s, and no argument\n", taskFlag, pageFlag)
		flags.Usage()
		return storyArgs{}, fmt.Errorf("%w: want one of -%s and -%s", errUsage, taskFlag, pageFlag)
	}
	return a, nil
}

// runStory prints the story the arguments ask for to stdout, and each
// source that could not be read to stderr.
func runStory(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer,
	open openFunc) error {
	a, err := parseStoryArgs(args, stderr)
	if err != nil {
		return err
	}
	c, cfg, err := open(getenv)
	if err != nil {
		return err
	}
	res, err := c.Collect(ctx, a.root(cfg), a.repos...)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", jsonIndent)
	if err := enc.Encode(res.Story); err != nil {
		return err
	}
	for _, f := range res.Failures {
		fmt.Fprintln(stderr, "sillage: not read:", f)
	}
	if len(res.Failures) > 0 {
		return errPartial
	}
	return nil
}
