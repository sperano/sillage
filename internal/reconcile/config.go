package reconcile

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvInterval names the environment variable holding how long the loop
	// waits between two cycles, as a Go duration. Unset means
	// DefaultInterval.
	EnvInterval = "SILLAGE_COLLECT_INTERVAL"
	// DefaultInterval is the time between two cycles: how stale a new run
	// or a changed task can be before it is read.
	DefaultInterval = 5 * time.Minute
	// EnvRefreshInterval names the environment variable holding how long
	// a root the Kelos namespace still names is left unread when nothing
	// says it changed, as a Go duration. Unset means
	// DefaultRefreshInterval.
	EnvRefreshInterval = "SILLAGE_ROOT_REFRESH_INTERVAL"
	// DefaultRefreshInterval re-reads such a root once an hour: pull
	// request reviews and checks, and comments, change no Kelos run.
	DefaultRefreshInterval = time.Hour
	// EnvLockRetryInterval names the environment variable holding how long
	// a process that could not take the collector lock waits before it
	// tries again, as a Go duration. Unset means DefaultLockRetryInterval.
	EnvLockRetryInterval = "SILLAGE_LOCK_RETRY_INTERVAL"
	// DefaultLockRetryInterval lets a standby take over within a minute
	// of the collecting process stopping.
	DefaultLockRetryInterval = time.Minute
	// EnvVikunjaProjects names the environment variable listing the
	// Vikunja project ids whose changed tasks are read, comma-separated.
	// Unset, only the tasks Kelos names are read.
	EnvVikunjaProjects = "SILLAGE_VIKUNJA_PROJECTS"

	// jitterFraction spreads every wait by up to this fraction either way,
	// so that two loops, or a loop and a source's own schedule, drift
	// apart.
	jitterFraction = 0.1
	// listingOverlap re-lists the tasks updated this long before the last
	// change a listing saw: a task updated while the pages were read, or
	// a clock between Vikunja and its database, can be behind it. A task
	// listed again but unchanged is not read again.
	listingOverlap = 5 * time.Minute
	// projectSeparator separates the ids of EnvVikunjaProjects.
	projectSeparator = ","
)

// Config is how often the loop reads, and what besides Kelos it reads.
type Config struct {
	// Interval is the wait between two cycles.
	Interval time.Duration
	// RefreshInterval is how long a root Kelos names is left unread when
	// nothing says it changed.
	RefreshInterval time.Duration
	// LockRetryInterval is the wait between two attempts at the lock.
	LockRetryInterval time.Duration
	// VikunjaProjects are the projects whose changed tasks are read.
	VikunjaProjects []int64
}

// DefaultConfig is the configuration with every default and no project.
func DefaultConfig() Config {
	return Config{
		Interval: DefaultInterval, RefreshInterval: DefaultRefreshInterval, LockRetryInterval: DefaultLockRetryInterval,
	}
}

// ConfigFromEnv reads the configuration through getenv (os.Getenv in
// production), and reports every problem at once.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	var errs []error
	for _, d := range []struct {
		env string
		dst *time.Duration
	}{
		{EnvInterval, &cfg.Interval},
		{EnvRefreshInterval, &cfg.RefreshInterval},
		{EnvLockRetryInterval, &cfg.LockRetryInterval},
	} {
		if err := parseDuration(getenv(d.env), d.env, d.dst); err != nil {
			errs = append(errs, err)
		}
	}
	projects, err := parseProjects(getenv(EnvVikunjaProjects))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.VikunjaProjects = projects
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate reports whether every interval is positive.
func (c Config) validate() error {
	if c.Interval <= 0 || c.RefreshInterval <= 0 || c.LockRetryInterval <= 0 {
		return errors.New("reconcile: every interval must be positive")
	}
	return nil
}

// parseDuration sets dst from value, a positive Go duration, unless value
// is empty.
func parseDuration(value, env string, dst *time.Duration) error {
	if value = strings.TrimSpace(value); value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("reconcile: %s: %q is not a positive duration", env, value)
	}
	*dst = d
	return nil
}

// parseProjects parses EnvVikunjaProjects: positive ids, each once.
func parseProjects(value string) ([]int64, error) {
	var out []int64
	for item := range strings.SplitSeq(value, projectSeparator) {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("reconcile: %s: %q is not a project id", EnvVikunjaProjects, item)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}
