package ingest

import "log/slog"

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

// Secret is a webhook secret. It prints as [redacted] through fmt and
// log/slog, so a Secrets can be logged without leaking it.
type Secret string

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer, for %#v.
func (Secret) GoString() string { return redacted }

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
