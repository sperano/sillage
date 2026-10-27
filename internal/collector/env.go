package collector

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// EnvConfig is the collector's configuration and its sources' clients'
// configurations, as read from the environment.
type EnvConfig struct {
	Collector Config
	Vikunja   vikunja.Config
	GitHub    github.Config
}

// ConfigFromEnv reads every source's settings and the collector's own
// (EnvWorkspaces, EnvVikunjaAgentUsers, EnvKelosTaskTTL) through getenv
// (os.Getenv in production), and reports every problem at once.
func ConfigFromEnv(getenv func(string) string) (EnvConfig, error) {
	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	vcfg, err := vikunja.ConfigFromEnv(getenv)
	collect(err)
	kcfg, err := kelos.ConfigFromEnv(getenv)
	collect(err)
	gcfg, err := github.ConfigFromEnv(getenv)
	collect(err)
	workspaces, err := parseWorkspaces(getenv(EnvWorkspaces))
	collect(err)
	ttl, err := parseTTL(getenv(EnvKelosTaskTTL))
	collect(err)
	if err := errors.Join(errs...); err != nil {
		return EnvConfig{}, err
	}
	host, err := hostOf(vcfg.BaseURL)
	if err != nil {
		return EnvConfig{}, err
	}
	cfg := Config{
		VikunjaInstance:       host,
		VikunjaServiceAccount: vcfg.ServiceAccount,
		VikunjaAgentUsers:     parseList(getenv(EnvVikunjaAgentUsers)),
		Kelos:                 kcfg,
		GitHubInstance:        gcfg.Instance,
		GitHubAnnouncers:      gcfg.Announcers,
		Workspaces:            workspaces,
		KelosTaskTTL:          ttl,
	}
	if err := cfg.validate(); err != nil {
		return EnvConfig{}, err
	}
	return EnvConfig{Collector: cfg, Vikunja: vcfg, GitHub: gcfg}, nil
}

// hostOf is a base URL's host, in lower case and without a port, as the
// model's keys name an instance.
func hostOf(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("collector: %s has no host", vikunja.EnvURL)
	}
	return strings.ToLower(u.Hostname()), nil
}

// Clients are the clients of the live sources an EnvConfig configures.
type Clients struct {
	Vikunja *vikunja.Client
	Kelos   *kelos.Reader
	GitHub  *github.Client
}

// Sources returns the clients as a collector reads them. A client that
// is not set stays nil, rather than become a non-nil interface holding a
// nil pointer, so that New refuses it.
func (c Clients) Sources() Sources {
	var s Sources
	if c.Vikunja != nil {
		s.Vikunja = c.Vikunja
	}
	if c.Kelos != nil {
		s.Kelos = c.Kelos
	}
	if c.GitHub != nil {
		s.GitHub = c.GitHub
	}
	return s
}

// OpenClients returns the clients of the sources e configures. The
// Kubernetes client is the in-cluster one, else the kubeconfig's current
// context.
func (e EnvConfig) OpenClients() (Clients, error) {
	dyn, err := kelos.NewDynamicClient()
	if err != nil {
		return Clients{}, err
	}
	reader, err := kelos.NewReader(dyn, e.Collector.Kelos)
	if err != nil {
		return Clients{}, err
	}
	gh, err := github.NewClient(e.GitHub, nil)
	if err != nil {
		return Clients{}, err
	}
	return Clients{Vikunja: vikunja.NewClient(e.Vikunja, nil), Kelos: reader, GitHub: gh}, nil
}

// Open returns a collector of the sources e configures, through
// OpenClients.
func (e EnvConfig) Open() (*Collector, error) {
	clients, err := e.OpenClients()
	if err != nil {
		return nil, err
	}
	return New(e.Collector, clients.Sources())
}
