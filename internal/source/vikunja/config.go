package vikunja

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

const (
	// EnvURL names the environment variable holding the Vikunja base URL,
	// without the /api/v1 suffix (https://vikunja.example.com).
	EnvURL = "SILLAGE_VIKUNJA_URL"
	// EnvToken names the environment variable holding a read-only Vikunja
	// API token.
	EnvToken = "SILLAGE_VIKUNJA_TOKEN"
	// EnvServiceAccount names the environment variable holding the username
	// of the account Kelos posts its start announcements as.
	EnvServiceAccount = "SILLAGE_VIKUNJA_SERVICE_ACCOUNT"
	// DefaultServiceAccount is used when EnvServiceAccount is unset. The
	// account was renamed in place from kelos, so Vikunja reports this
	// username as the author of every announcement, the old ones included.
	DefaultServiceAccount = "gordon"
	// redacted replaces the token wherever it would be printed.
	redacted = "[redacted]"
)

// Token is a Vikunja API token. It prints as [redacted] through fmt and
// log/slog, so a Config can be logged without leaking it.
type Token string

// String implements fmt.Stringer.
func (Token) String() string { return redacted }

// GoString implements fmt.GoStringer, for %#v.
func (Token) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Token) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, so JSON and other text
// encodings print the placeholder too.
func (Token) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Config is what the client needs to read one Vikunja instance.
type Config struct {
	// BaseURL is the instance's URL without a trailing slash; it also names
	// the source instance on every event.
	BaseURL string
	// Token is sent as a bearer token. It must only grant read access.
	Token Token
	// ServiceAccount is the username whose comments are parsed as Kelos
	// start announcements.
	ServiceAccount string
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		BaseURL:        strings.TrimRight(strings.TrimSpace(getenv(EnvURL)), "/"),
		Token:          Token(strings.TrimSpace(getenv(EnvToken))),
		ServiceAccount: strings.TrimSpace(getenv(EnvServiceAccount)),
	}
	if cfg.ServiceAccount == "" {
		cfg.ServiceAccount = DefaultServiceAccount
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("vikunja: %s is not set", EnvURL)
	}
	u, err := url.Parse(c.BaseURL)
	// The value is left out of these errors: it could carry credentials.
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("vikunja: %s must be an http(s) URL", EnvURL)
	}
	if u.User != nil {
		return fmt.Errorf("vikunja: %s must not carry credentials; use %s", EnvURL, EnvToken)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("vikunja: %s must not have a query or fragment", EnvURL)
	}
	if c.Token == "" {
		return fmt.Errorf("vikunja: %s is not set", EnvToken)
	}
	return nil
}
