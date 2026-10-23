package collector

import (
	"errors"
	"net/url"
	"strings"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// EnvConfig is the collector's configuration and its sources' clients'
// configurations, as read from the environment.
type EnvConfig struct {
	Collector Config
	Vikunja   vikunja.Config
	GitHub    github.Config
	Wiki      wiki.Config
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
	wcfg, err := wiki.ConfigFromEnv(getenv)
	collect(err)
	workspaces, err := parseWorkspaces(getenv(EnvWorkspaces))
	collect(err)
	ttl, err := parseTTL(getenv(EnvKelosTaskTTL))
	collect(err)
	if err := errors.Join(errs...); err != nil {
		return EnvConfig{}, err
	}
	cfg := Config{
		VikunjaInstance:       hostOf(vcfg.BaseURL),
		VikunjaServiceAccount: vcfg.ServiceAccount,
		VikunjaAgentUsers:     parseList(getenv(EnvVikunjaAgentUsers)),
		Kelos:                 kcfg,
		GitHubInstance:        gcfg.Instance,
		WikiInstance:          wcfg.Instance,
		WikiAgentUsers:        wcfg.AgentUsers,
		Workspaces:            workspaces,
		KelosTaskTTL:          ttl,
	}
	if err := cfg.validate(); err != nil {
		return EnvConfig{}, err
	}
	return EnvConfig{Collector: cfg, Vikunja: vcfg, GitHub: gcfg, Wiki: wcfg}, nil
}

// hostOf is a valid base URL's host, in lower case and without a port, as
// the model's keys name an instance.
func hostOf(baseURL string) string {
	u, _ := url.Parse(baseURL)
	return strings.ToLower(u.Hostname())
}

// Open returns a collector of the sources e configures. The Kubernetes
// client is the in-cluster one, else the kubeconfig's current context.
func (e EnvConfig) Open() (*Collector, error) {
	dyn, err := kelos.NewDynamicClient()
	if err != nil {
		return nil, err
	}
	reader, err := kelos.NewReader(dyn, e.Collector.Kelos)
	if err != nil {
		return nil, err
	}
	gh, err := github.NewClient(e.GitHub, nil)
	if err != nil {
		return nil, err
	}
	wk, err := wiki.NewClient(e.Wiki, nil)
	if err != nil {
		return nil, err
	}
	return New(e.Collector, Sources{Vikunja: vikunja.NewClient(e.Vikunja, nil), Kelos: reader, GitHub: gh, Wiki: wk})
}
