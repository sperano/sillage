package store

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// EnvDatabaseURL names the environment variable holding the PostgreSQL
	// connection string. It carries the password: it is never logged.
	EnvDatabaseURL = "SILLAGE_DATABASE_URL"
	// EnvRetention names the environment variable holding the retention
	// policy; see ParseRetention.
	EnvRetention = "SILLAGE_RETENTION"

	// redacted replaces the connection string wherever it would be printed.
	redacted = "[redacted]"
	// retentionKeepAll is the explicit spelling of the default policy.
	retentionKeepAll = "forever"
	// daySuffix marks a retention in days ("90d"), which Go durations lack.
	daySuffix = "d"
	day       = 24 * time.Hour
	// minRetention is the shortest retention accepted, so that a typo such
	// as "6m" (six minutes, not months) cannot empty the store at startup.
	minRetention = day
)

// ErrNotConfigured is returned by ConfigFromEnv when EnvDatabaseURL is
// unset.
var ErrNotConfigured = errors.New("store: " + EnvDatabaseURL + " is not set")

// DatabaseURL is a PostgreSQL connection string. It prints as [redacted]
// through fmt, slog and text marshaling.
type DatabaseURL string

func (DatabaseURL) String() string { return redacted }

// GoString redacts %#v.
func (DatabaseURL) GoString() string { return redacted }

// Format redacts every fmt verb, including those (%d, %x) that would
// bypass String.
func (DatabaseURL) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue redacts slog attributes.
func (DatabaseURL) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText redacts JSON and other text encodings.
func (DatabaseURL) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Config is what the store needs to open the database.
type Config struct {
	DatabaseURL DatabaseURL
	Retention   Retention
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production).
// It returns ErrNotConfigured when EnvDatabaseURL is unset.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	url := strings.TrimSpace(getenv(EnvDatabaseURL))
	if url == "" {
		return Config{}, ErrNotConfigured
	}
	retention, err := ParseRetention(getenv(EnvRetention))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{DatabaseURL: DatabaseURL(url), Retention: retention}
	if _, err := cfg.poolConfig(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// poolConfig parses the connection string. Its error never quotes the
// string: pgx's would, with the host and user, and url.Parse's with the
// password.
func (c Config) poolConfig() (*pgxpool.Config, error) {
	pc, err := pgxpool.ParseConfig(string(c.DatabaseURL))
	if err != nil {
		return nil, fmt.Errorf("store: %s is not a valid PostgreSQL connection string", EnvDatabaseURL)
	}
	return pc, nil
}

// Retention is how long a story is kept after its last activity. The zero
// value keeps everything.
type Retention struct {
	keep time.Duration
}

// ParseRetention reads a retention policy: empty or "forever" keeps
// everything (the default), "<n>d" keeps n days, and a Go duration
// ("720h") keeps that long. Anything shorter than a day is refused. It
// never follows the Kelos Task TTL: keeping history past it is the store's
// purpose.
func ParseRetention(s string) (Retention, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == retentionKeepAll {
		return Retention{}, nil
	}
	keep, err := parseKeep(s)
	if err != nil {
		return Retention{}, fmt.Errorf("store: %s %q: %w", EnvRetention, s, err)
	}
	if keep < minRetention {
		return Retention{}, fmt.Errorf("store: %s %q: shorter than %s", EnvRetention, s, minRetention)
	}
	return Retention{keep: keep}, nil
}

func parseKeep(s string) (time.Duration, error) {
	days, ok := strings.CutSuffix(s, daySuffix)
	if !ok {
		return time.ParseDuration(s)
	}
	n, err := strconv.ParseInt(days, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/int64(day) {
		return 0, errors.New("not a positive number of days")
	}
	return time.Duration(n) * day, nil
}

// KeepAll reports whether nothing is ever pruned.
func (r Retention) KeepAll() bool { return r.keep == 0 }

// Cutoff is the time before which a story's last activity makes it
// prunable at now, and false when nothing is pruned.
func (r Retention) Cutoff(now time.Time) (time.Time, bool) {
	if r.KeepAll() {
		return time.Time{}, false
	}
	return now.Add(-r.keep), true
}

func (r Retention) String() string {
	if r.KeepAll() {
		return retentionKeepAll
	}
	return r.keep.String()
}
