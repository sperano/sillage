package github

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

const (
	// EnvAPIURL names the environment variable holding the GitHub REST API
	// base URL: https://api.github.com, or https://<host>/api/v3 for GitHub
	// Enterprise Server. Unset means DefaultAPIURL.
	EnvAPIURL = "SILLAGE_GITHUB_API_URL"
	// EnvToken names the environment variable holding a GitHub token with
	// read-only access to pull requests, issues and checks.
	EnvToken = "SILLAGE_GITHUB_TOKEN"
	// EnvAnnouncers names the environment variable holding the logins,
	// comma-separated, whose conversation comments are parsed as Kelos
	// start announcements: the accounts of the Workspaces' tokens (the
	// owner's for a PAT, the App's bot for a GitHub App). Unset, no
	// comment is read as one.
	EnvAnnouncers = "SILLAGE_GITHUB_ANNOUNCERS"
	// loginSeparator separates the logins of EnvAnnouncers.
	loginSeparator = ","
	// DefaultAPIURL is github.com's REST API.
	DefaultAPIURL = "https://api.github.com"
	// dotComAPIHost is github.com's API host; its records belong to the
	// github.com instance.
	dotComAPIHost = "api.github.com"
	// dotComInstance names the github.com instance.
	dotComInstance = "github.com"
	// redacted replaces the token wherever it would be printed.
	redacted = "[redacted]"
)

// Token is a GitHub token. It prints as [redacted] through fmt and
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

// Config is what the client needs to read one GitHub instance.
type Config struct {
	// APIURL is the REST API base URL without a trailing slash.
	APIURL string
	// Instance names the source instance on every record: the web host
	// (github.com, or the Enterprise Server's host), not the API host.
	Instance string
	// Token is sent as a bearer token. It must only grant read access.
	Token Token
	// Announcers are the logins whose conversation comments are parsed
	// as start announcements; hand them to Normalize.
	Announcers []string
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production)
// and derives Instance from the API URL.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		APIURL:     apiURLFromEnv(getenv),
		Token:      Token(strings.TrimSpace(getenv(EnvToken))),
		Announcers: logins(getenv(EnvAnnouncers)),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	cfg.Instance = instanceOf(cfg.APIURL)
	return cfg, nil
}

// InstanceFromEnv names the instance the API URL getenv gives belongs to,
// as ConfigFromEnv does, whether or not a token is set: a caller that
// cannot read GitHub still names what it does not read.
func InstanceFromEnv(getenv func(string) string) (string, error) {
	apiURL := apiURLFromEnv(getenv)
	if err := validateAPIURL(apiURL); err != nil {
		return "", err
	}
	return instanceOf(apiURL), nil
}

// apiURLFromEnv is EnvAPIURL without a trailing slash; DefaultAPIURL when
// unset.
func apiURLFromEnv(getenv func(string) string) string {
	if u := strings.TrimRight(strings.TrimSpace(getenv(EnvAPIURL)), "/"); u != "" {
		return u
	}
	return DefaultAPIURL
}

// logins splits a comma-separated list of logins, dropping blanks.
func logins(list string) []string {
	var out []string
	for _, l := range strings.Split(list, loginSeparator) {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// instanceOf names the instance an API URL belongs to: github.com for
// api.github.com, else the URL's host. apiURL must be valid.
func instanceOf(apiURL string) string {
	u, _ := url.Parse(apiURL)
	if strings.EqualFold(u.Host, dotComAPIHost) {
		return dotComInstance
	}
	return strings.ToLower(u.Host)
}

func (c Config) validate() error {
	if err := validateAPIURL(c.APIURL); err != nil {
		return err
	}
	if c.Token == "" {
		return fmt.Errorf("github: %s is not set", EnvToken)
	}
	return nil
}

// validateAPIURL reports whether apiURL is an http(s) URL without
// credentials, a query or a fragment.
func validateAPIURL(apiURL string) error {
	u, err := url.Parse(apiURL)
	// The value is left out of these errors: it could carry credentials.
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("github: %s must be an http(s) URL", EnvAPIURL)
	}
	if u.User != nil {
		return fmt.Errorf("github: %s must not carry credentials; use %s", EnvAPIURL, EnvToken)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("github: %s must not have a query or fragment", EnvAPIURL)
	}
	return nil
}

// Repository is a GitHub repository, owner/name.
type Repository struct {
	Owner string
	Name  string
}

// String returns owner/name.
func (r Repository) String() string { return r.Owner + "/" + r.Name }

// ParseRepository parses "owner/name".
func ParseRepository(s string) (Repository, error) {
	owner, name, ok := strings.Cut(s, "/")
	r := Repository{Owner: owner, Name: name}
	if !ok || r.validate() != nil {
		return Repository{}, fmt.Errorf("github: repository %q: want owner/name", s)
	}
	return r, nil
}

func (r Repository) validate() error {
	for _, part := range []string{r.Owner, r.Name} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/?#%: \t\r\n") {
			return fmt.Errorf("github: repository %q: want owner/name", r)
		}
	}
	return nil
}

// apiPath returns the repository's REST path, /repos/owner/name.
func (r Repository) apiPath() string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

// Branch names a pull request head: a branch of a repository, which is the
// base repository itself or a fork of it.
type Branch struct {
	Repository Repository
	Name       string
}

func (b Branch) validate() error {
	if err := b.Repository.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("github: branch of %s: a name is required", b.Repository)
	}
	return nil
}
