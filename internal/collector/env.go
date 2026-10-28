package collector

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/sperano/sillage/internal/model"
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
// (os.Getenv in production). A source whose settings are incomplete or
// invalid is off, and Collector.Off says why: EnvWorkspaces is GitHub's,
// EnvKelosTaskTTL Kelos's. It fails only when an instance cannot be named,
// and then reports every one at once: no coverage could say what is not
// read.
func ConfigFromEnv(getenv func(string) string) (EnvConfig, error) {
	off := make(map[model.Source]string)
	vcfg, verr := vikunja.ConfigFromEnv(getenv)
	offIf(off, model.SourceVikunja, verr)
	kcfg, kerr := kelos.ConfigFromEnv(getenv)
	ttl, terr := parseTTL(getenv(EnvKelosTaskTTL))
	gcfg, gerr := github.ConfigFromEnv(getenv)
	workspaces, werr := parseWorkspaces(getenv(EnvWorkspaces))
	offIf(off, model.SourceGitHub, gerr, werr)
	inst, err := instancesFromEnv(getenv, kcfg, gcfg)
	if err != nil {
		return EnvConfig{}, err
	}
	if kerr != nil {
		kcfg = kelos.Config{Cluster: inst.cluster}
	}
	cfg := Config{
		VikunjaInstance:       inst.vikunja,
		VikunjaServiceAccount: serviceAccount(vcfg),
		VikunjaAgentUsers:     parseList(getenv(EnvVikunjaAgentUsers)),
		Kelos:                 kcfg,
		GitHubInstance:        inst.github,
		GitHubAnnouncers:      gcfg.Announcers,
		Workspaces:            workspaces,
		KelosTaskTTL:          ttl,
		Off:                   off,
	}
	if kerr == nil && terr == nil {
		kerr = cfg.kelosValid()
	}
	offIf(off, model.SourceKelos, kerr, terr)
	if err := cfg.validate(off); err != nil {
		return EnvConfig{}, err
	}
	return EnvConfig{Collector: cfg, Vikunja: vcfg, GitHub: gcfg}, nil
}

// offIf turns source s off in off for errs, unless every one is nil.
func offIf(off map[model.Source]string, s model.Source, errs ...error) {
	var reasons []string
	for _, err := range errs {
		if err != nil {
			reasons = append(reasons, err.Error())
		}
	}
	if len(reasons) > 0 {
		off[s] = strings.Join(reasons, "; ")
	}
}

// serviceAccount is the Vikunja account that posts the start
// announcements: the default while Vikunja is off, when none was read.
func serviceAccount(cfg vikunja.Config) string {
	if cfg.ServiceAccount == "" {
		return vikunja.DefaultServiceAccount
	}
	return cfg.ServiceAccount
}

// instances names each source's instance.
type instances struct {
	vikunja, cluster, github string
}

// instancesFromEnv names each source's instance from its configuration,
// or, for a source whose configuration is incomplete, from the settings
// that name it: the Vikunja host from vikunja.EnvURL, else from
// kelos.EnvVikunjaInstance; the cluster from kelos.EnvCluster; GitHub's
// from its API URL.
func instancesFromEnv(getenv func(string) string, kcfg kelos.Config, gcfg github.Config) (instances, error) {
	var errs []error
	inst := instances{cluster: kcfg.Cluster, github: gcfg.Instance}
	var err error
	if inst.vikunja, err = hostOf(strings.TrimSpace(getenv(vikunja.EnvURL))); err != nil {
		if inst.vikunja = kelosVikunjaInstance(getenv, kcfg); inst.vikunja == "" {
			errs = append(errs, fmt.Errorf("collector: neither %s nor %s names the Vikunja instance",
				vikunja.EnvURL, kelos.EnvVikunjaInstance))
		}
	}
	if inst.cluster == "" {
		if inst.cluster = strings.TrimSpace(getenv(kelos.EnvCluster)); inst.cluster == "" {
			errs = append(errs, fmt.Errorf("collector: %s is not set: the Kelos cluster has no name", kelos.EnvCluster))
		}
	}
	if inst.github == "" {
		if inst.github, err = github.InstanceFromEnv(getenv); err != nil {
			errs = append(errs, fmt.Errorf("collector: the GitHub instance has no name: %w", err))
		}
	}
	return inst, errors.Join(errs...)
}

// kelosVikunjaInstance is the Vikunja host kelos.EnvVikunjaInstance names;
// empty when it is not a host name.
func kelosVikunjaInstance(getenv func(string) string, kcfg kelos.Config) string {
	if kcfg.VikunjaInstance != "" {
		return kcfg.VikunjaInstance
	}
	host := strings.ToLower(strings.TrimSpace(getenv(kelos.EnvVikunjaInstance)))
	if len(validation.IsDNS1123Subdomain(host)) > 0 {
		return ""
	}
	return host
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
// nil pointer, so that its source is off.
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

// OpenClients returns the clients of the sources e configures; a source
// that is off has none. A source whose client cannot be made is off too,
// and e.Collector.Off says why. The Kubernetes client is the in-cluster
// one, else the kubeconfig's current context.
func (e *EnvConfig) OpenClients() Clients {
	var c Clients
	if e.on(model.SourceVikunja) {
		c.Vikunja = vikunja.NewClient(e.Vikunja, nil)
	}
	if e.on(model.SourceKelos) {
		reader, err := openKelos(e.Collector.Kelos)
		e.offIf(model.SourceKelos, err)
		c.Kelos = reader
	}
	if e.on(model.SourceGitHub) {
		gh, err := github.NewClient(e.GitHub, nil)
		e.offIf(model.SourceGitHub, err)
		c.GitHub = gh
	}
	return c
}

// on reports whether e leaves source s on.
func (e *EnvConfig) on(s model.Source) bool {
	_, off := e.Collector.Off[s]
	return !off
}

// offIf turns source s off for err, unless it is nil.
func (e *EnvConfig) offIf(s model.Source, err error) {
	if err == nil {
		return
	}
	if e.Collector.Off == nil {
		e.Collector.Off = make(map[model.Source]string)
	}
	offIf(e.Collector.Off, s, err)
}

// openKelos returns the reader of the namespace cfg names; nil with the
// error when it cannot be made.
func openKelos(cfg kelos.Config) (*kelos.Reader, error) {
	dyn, err := kelos.NewDynamicClient()
	if err != nil {
		return nil, err
	}
	return kelos.NewReader(dyn, cfg)
}

// Open returns a collector of the sources e configures, through
// OpenClients.
func (e *EnvConfig) Open() (*Collector, error) {
	return New(e.Collector, e.OpenClients().Sources())
}
