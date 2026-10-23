package collector

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
)

const (
	// EnvWorkspaces names the environment variable mapping Kelos workspace
	// names to the GitHub repositories they check out, as comma-separated
	// <workspace>=<owner>/<repository> pairs:
	//
	//	SILLAGE_WORKSPACES="widgets=example-org/widgets,docs=example-org/docs"
	//
	// A Vikunja project's runs carry its project name, not its workspace's,
	// so a project named differently from its workspace needs a pair of
	// its own. Unset, no repository is known and GitHub is not read unless
	// one is asked for.
	EnvWorkspaces = "SILLAGE_WORKSPACES"
	// EnvVikunjaAgentUsers names the environment variable listing the
	// Vikunja usernames of the agents' accounts, comma-separated: their
	// comments have an agent actor, and only theirs and the service
	// account's are followed to the wiki pages they link. Optional.
	EnvVikunjaAgentUsers = "SILLAGE_VIKUNJA_AGENT_USERS"
	// EnvKelosTaskTTL names the environment variable holding how long
	// Kelos keeps a finished Task, as a Go duration. Unset means
	// DefaultKelosTaskTTL.
	EnvKelosTaskTTL = "SILLAGE_KELOS_TASK_TTL"
	// DefaultKelosTaskTTL is the Task TTL the Hollingsworth chart sets.
	DefaultKelosTaskTTL = 72 * time.Hour

	// listSeparator separates the items of a list setting, and
	// pairSeparator a workspace from its repository.
	listSeparator = ","
	pairSeparator = "="
)

// Config is what the collector needs besides its sources' clients: how to
// name each source instance, and what joins records across them.
type Config struct {
	// VikunjaInstance is the Vikunja host, as model.TaskKey names it.
	VikunjaInstance string
	// VikunjaServiceAccount posts the start announcements.
	VikunjaServiceAccount string
	// VikunjaAgentUsers are the agents' Vikunja usernames.
	VikunjaAgentUsers []string
	// Kelos is the namespace read; it also names the cluster on run keys.
	Kelos kelos.Config
	// GitHubInstance is the GitHub web host, as model.PRKey names it.
	GitHubInstance string
	// GitHubAnnouncers are the GitHub logins whose comments are parsed as
	// start announcements.
	GitHubAnnouncers []string
	// WikiInstance is the Wiki.js host, as model.PageKey names it.
	WikiInstance string
	// WikiAgentUsers are the agents' wiki user ids.
	WikiAgentUsers []int64
	// Workspaces maps a Kelos workspace (or Vikunja project) name to its
	// repository.
	Workspaces map[string]github.Repository
	// KelosTaskTTL is how long Kelos keeps a finished Task: before it,
	// only runs that left a TaskRecord can still be read.
	KelosTaskTTL time.Duration
}

func (c Config) validate() error {
	var errs []error
	if c.VikunjaInstance == "" || c.GitHubInstance == "" || c.WikiInstance == "" {
		errs = append(errs, errors.New("collector: the Vikunja, GitHub and wiki instances are required"))
	}
	if err := c.Kelos.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.Kelos.VikunjaInstance != c.VikunjaInstance {
		errs = append(errs, fmt.Errorf("collector: %s %q is not the Vikunja host %q: no Task label would name a task read",
			kelos.EnvVikunjaInstance, c.Kelos.VikunjaInstance, c.VikunjaInstance))
	}
	if c.KelosTaskTTL <= 0 {
		errs = append(errs, fmt.Errorf("collector: %s must be positive", EnvKelosTaskTTL))
	}
	return errors.Join(errs...)
}

// parseWorkspaces parses EnvWorkspaces. An empty value maps nothing.
func parseWorkspaces(value string) (map[string]github.Repository, error) {
	out := make(map[string]github.Repository)
	for item := range strings.SplitSeq(value, listSeparator) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, repo, ok := strings.Cut(item, pairSeparator)
		name, repo = strings.TrimSpace(name), strings.TrimSpace(repo)
		if !ok || strings.Contains(repo, pairSeparator) || len(validation.IsDNS1123Label(name)) > 0 {
			return nil, fmt.Errorf("collector: %s: %q is not <workspace>=<owner>/<repository>", EnvWorkspaces, item)
		}
		r, err := github.ParseRepository(repo)
		if err != nil {
			return nil, fmt.Errorf("collector: %s: workspace %s: %w", EnvWorkspaces, name, err)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("collector: %s: workspace %s is listed twice", EnvWorkspaces, name)
		}
		out[name] = r
	}
	return out, nil
}

// parseList splits a comma-separated list, dropping empty items.
func parseList(value string) []string {
	var out []string
	for item := range strings.SplitSeq(value, listSeparator) {
		if item = strings.TrimSpace(item); item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// parseTTL parses EnvKelosTaskTTL; empty means DefaultKelosTaskTTL.
func parseTTL(value string) (time.Duration, error) {
	if value = strings.TrimSpace(value); value == "" {
		return DefaultKelosTaskTTL, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("collector: %s: %q is not a positive duration", EnvKelosTaskTTL, value)
	}
	return d, nil
}
