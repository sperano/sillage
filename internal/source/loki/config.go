package loki

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/source/kelos"
)

const (
	// EnvURL names the environment variable holding Loki's base URL
	// (http://grafana-loki-gateway.grafana.svc), without the /loki path.
	EnvURL = "SILLAGE_LOKI_URL"
	// EnvOrgID names the optional environment variable holding the
	// X-Scope-OrgID tenant to read; unset, no header is sent (Loki without
	// authentication).
	EnvOrgID = "SILLAGE_LOKI_ORG_ID"

	// DefaultPageLimit is how many entries one request asks for. It is
	// under Loki's default max_entries_limit_per_query.
	DefaultPageLimit = 1000
	// DefaultTimeout bounds one request.
	DefaultTimeout = 60 * time.Second
	// DefaultMaxResponseBytes bounds one response's body.
	DefaultMaxResponseBytes = 64 << 20
)

// Config is what the client needs to read one namespace of one cluster.
type Config struct {
	// BaseURL is Loki's URL, with no query or fragment.
	BaseURL string
	// OrgID is sent as X-Scope-OrgID when set.
	OrgID string
	// Cluster names the cluster on every event: Loki has no cluster name.
	Cluster string
	// Namespace is the k8s_namespace_name whose streams are read.
	Namespace string
	// PageLimit is the entries one request asks for; DefaultPageLimit when
	// zero.
	PageLimit int
	// Timeout bounds one request; DefaultTimeout when zero.
	Timeout time.Duration
	// MaxResponseBytes bounds one response; DefaultMaxResponseBytes when
	// zero.
	MaxResponseBytes int64
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production):
// EnvURL and EnvOrgID, and the cluster and namespace the Kelos source
// reads (kelos.EnvCluster, kelos.EnvNamespace).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		BaseURL:   strings.TrimRight(strings.TrimSpace(getenv(EnvURL)), "/"),
		OrgID:     strings.TrimSpace(getenv(EnvOrgID)),
		Cluster:   strings.TrimSpace(getenv(kelos.EnvCluster)),
		Namespace: strings.TrimSpace(getenv(kelos.EnvNamespace)),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports whether every required setting is present and well
// formed.
func (c Config) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("loki: %s must be an http or https URL without a query or fragment", EnvURL)
	}
	if c.Cluster == "" || strings.TrimSpace(c.Cluster) != c.Cluster {
		return fmt.Errorf("loki: %s is not set, or has leading or trailing whitespace", kelos.EnvCluster)
	}
	if c.Namespace == "" || strings.ContainsAny(c.Namespace, "\"\\\n") {
		return fmt.Errorf("loki: %s must be a namespace name", kelos.EnvNamespace)
	}
	if c.PageLimit < 0 || c.Timeout < 0 || c.MaxResponseBytes < 0 {
		return fmt.Errorf("loki: the page limit, timeout and response bound cannot be negative")
	}
	return nil
}

func (c Config) pageLimit() int {
	if c.PageLimit == 0 {
		return DefaultPageLimit
	}
	return c.PageLimit
}

func (c Config) timeout() time.Duration {
	if c.Timeout == 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

func (c Config) maxResponseBytes() int64 {
	if c.MaxResponseBytes == 0 {
		return DefaultMaxResponseBytes
	}
	return c.MaxResponseBytes
}
