package wiki

import (
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

const (
	// EnvURL names the environment variable holding the Wiki.js base URL
	// (https://wiki.example.com); GraphQL is served under it at /graphql.
	EnvURL = "SILLAGE_WIKI_URL"
	// EnvToken names the environment variable holding the bearer token: a
	// Wiki.js API key or a user's JWT. How it is issued is a deployment
	// decision.
	EnvToken = "SILLAGE_WIKI_TOKEN"
	// EnvAgentUsers names the environment variable listing the wiki user
	// ids of the agents' accounts, comma-separated. Their records have an
	// agent actor, and only their comments are read as start
	// announcements.
	EnvAgentUsers = "SILLAGE_WIKI_AGENT_USERS"
	// redacted replaces the token wherever it would be printed.
	redacted = "[redacted]"
)

// Token is a Wiki.js bearer token. It prints as [redacted] through fmt and
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

// Config is what the client needs to read one Wiki.js instance.
type Config struct {
	// BaseURL is the instance's URL without a trailing slash.
	BaseURL string
	// Instance names the source instance on every record: the URL's host.
	Instance string
	// Token is sent as a bearer token. It should only grant read access;
	// see Client.Fetch for what a read-only token cannot see.
	Token Token
	// AgentUsers are the wiki user ids of the agents' accounts.
	AgentUsers []int64
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production)
// and derives Instance from the URL.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		BaseURL: strings.TrimRight(strings.TrimSpace(getenv(EnvURL)), "/"),
		Token:   Token(strings.TrimSpace(getenv(EnvToken))),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	agents, err := parseUserIDs(getenv(EnvAgentUsers))
	if err != nil {
		return Config{}, err
	}
	cfg.AgentUsers = agents
	cfg.Instance = instanceOf(cfg.BaseURL)
	return cfg, nil
}

// instanceOf names the instance a base URL belongs to: its host, in lower
// case. baseURL must be valid.
func instanceOf(baseURL string) string {
	u, _ := url.Parse(baseURL)
	return strings.ToLower(u.Host)
}

func (c Config) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("wiki: %s is not set", EnvURL)
	}
	u, err := url.Parse(c.BaseURL)
	// The value is left out of these errors: it could carry credentials.
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("wiki: %s must be an http(s) URL", EnvURL)
	}
	if u.User != nil {
		return fmt.Errorf("wiki: %s must not carry credentials; use %s", EnvURL, EnvToken)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("wiki: %s must not have a query or fragment", EnvURL)
	}
	if c.Token == "" {
		return fmt.Errorf("wiki: %s is not set", EnvToken)
	}
	return nil
}

// parseUserIDs parses a comma-separated list of positive user ids. An
// empty list is an error: without it no comment is read as a start
// announcement, which would hide every wiki run.
func parseUserIDs(list string) ([]int64, error) {
	var ids []int64
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("wiki: %s: %q is not a positive user id", EnvAgentUsers, item)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("wiki: %s is not set", EnvAgentUsers)
	}
	return ids, nil
}
