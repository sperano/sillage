package kelos

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// EnvCluster names the environment variable holding the name sillage
	// gives the cluster on run keys and evidence. Kubernetes does not name
	// its own cluster, so it is configured.
	EnvCluster = "SILLAGE_KELOS_CLUSTER"
	// EnvNamespace names the environment variable holding the namespace
	// whose Tasks and TaskRecords are read.
	EnvNamespace = "SILLAGE_KELOS_NAMESPACE"
	// EnvLabelDomain names the environment variable holding the prefix of
	// the labels the spawners put on Tasks: <domain>/project,
	// <domain>/variant, <domain>/role and <domain>/task.
	EnvLabelDomain = "SILLAGE_KELOS_LABEL_DOMAIN"
	// EnvVikunjaInstance names the environment variable holding the host of
	// the Vikunja instance whose task ids the <domain>/task label holds.
	EnvVikunjaInstance = "SILLAGE_KELOS_VIKUNJA_INSTANCE"
)

// Config is what the reader needs to read one namespace of one cluster.
type Config struct {
	// Cluster names the cluster on every run key and evidence.
	Cluster string
	// Namespace is the namespace read.
	Namespace string
	// LabelDomain prefixes the spawners' Task labels.
	LabelDomain string
	// VikunjaInstance is the Vikunja host of the task ids in Task labels,
	// lower case and without a port, as model.TaskKey names it.
	VikunjaInstance string
}

// ConfigFromEnv reads a Config through getenv (os.Getenv in production).
// Every setting is required.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Cluster:         strings.TrimSpace(getenv(EnvCluster)),
		Namespace:       strings.TrimSpace(getenv(EnvNamespace)),
		LabelDomain:     strings.TrimSpace(getenv(EnvLabelDomain)),
		VikunjaInstance: strings.ToLower(strings.TrimSpace(getenv(EnvVikunjaInstance))),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports whether every setting is present and well formed. An
// empty namespace would list every namespace, and an empty label domain
// would match no label, so neither is allowed.
func (c Config) Validate() error {
	if c.Cluster == "" {
		return fmt.Errorf("kelos: %s is not set", EnvCluster)
	}
	if errs := validation.IsDNS1123Label(c.Namespace); len(errs) > 0 {
		return fmt.Errorf("kelos: %s must be a namespace name: %s", EnvNamespace, strings.Join(errs, "; "))
	}
	if errs := validation.IsDNS1123Subdomain(c.LabelDomain); len(errs) > 0 {
		return fmt.Errorf("kelos: %s must be a DNS subdomain: %s", EnvLabelDomain, strings.Join(errs, "; "))
	}
	if errs := validation.IsDNS1123Subdomain(c.VikunjaInstance); len(errs) > 0 {
		return fmt.Errorf("kelos: %s must be a host name: %s", EnvVikunjaInstance, strings.Join(errs, "; "))
	}
	return nil
}

// label returns the full key of one of the spawners' labels.
func (c Config) label(name string) string { return c.LabelDomain + "/" + name }
