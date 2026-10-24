package ingest

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/sperano/sillage/internal/collector"
)

const (
	// EnvGitHubSecret names the environment variable holding the secret
	// the GitHub repository webhooks sign their deliveries with.
	EnvGitHubSecret = "SILLAGE_GITHUB_WEBHOOK_SECRET"
	// EnvVikunjaSecret names the environment variable holding the secret
	// the Vikunja webhooks sign their deliveries with.
	EnvVikunjaSecret = "SILLAGE_VIKUNJA_WEBHOOK_SECRET"
	// redacted replaces a secret wherever it would be printed.
	redacted = "[redacted]"
)

// Secret is a webhook secret. It prints as [redacted] through fmt, with
// any verb but %p, and log/slog, so Secrets can be logged without leaking
// them.
type Secret string

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// Format implements fmt.Formatter, so that no verb prints the value: fmt
// calls String only for %v, %s, %q, %x and %X, and prints a string's
// value under any other. fmt calls no method for %p, which prints the
// value of anything but a pointer, and go vet does not flag it for a
// Formatter: never format a Secret with %p.
func (Secret) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, so JSON and other text
// encodings print the placeholder too.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Secrets are the secrets each source signs its deliveries with. An empty
// one refuses every delivery of its source: unsigned deliveries are never
// accepted.
type Secrets struct {
	GitHub  Secret
	Vikunja Secret
}

// SecretsFromEnv reads the secrets through getenv (os.Getenv in
// production), as they are: a secret is the exact HMAC key.
func SecretsFromEnv(getenv func(string) string) Secrets {
	return Secrets{GitHub: Secret(getenv(EnvGitHubSecret)), Vikunja: Secret(getenv(EnvVikunjaSecret))}
}

// Any reports whether a source has a secret.
func (s Secrets) Any() bool { return s.GitHub != "" || s.Vikunja != "" }

// Config is what the handler needs besides its store and scheduler.
type Config struct {
	// Collector names the source instances, and the roots of branches.
	Collector collector.Config
	Secrets   Secrets
}
