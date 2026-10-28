package collector

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
)

func TestParseWorkspaces(t *testing.T) {
	got, err := parseWorkspaces(" widgets = example-org/widgets ,, docs=example-org/docs ")
	want := map[string]github.Repository{
		"widgets": testRepo,
		"docs":    {Owner: "example-org", Name: "docs"},
	}
	if err != nil || !maps.Equal(got, want) {
		t.Errorf("parseWorkspaces() = %v, %v; want %v", got, err, want)
	}
	if got, err := parseWorkspaces(""); err != nil || len(got) != 0 {
		t.Errorf("parseWorkspaces(\"\") = %v, %v; want an empty map", got, err)
	}
}

func TestParseWorkspacesRejects(t *testing.T) {
	tests := map[string]string{
		"no pair":         "widgets",
		"no workspace":    "=example-org/widgets",
		"bad workspace":   "Widgets=example-org/widgets",
		"bad repository":  "widgets=example-org",
		"listed twice":    "widgets=example-org/widgets,widgets=example-org/other",
		"bad second pair": "widgets=example-org/widgets,docs",
		"two separators":  "widgets=example-org/widgets=docs",
	}
	for name, value := range tests {
		if _, err := parseWorkspaces(value); err == nil || !strings.Contains(err.Error(), EnvWorkspaces) {
			t.Errorf("%s: parseWorkspaces(%q) error = %v, want one naming %s", name, value, err, EnvWorkspaces)
		}
	}
}

func TestParseList(t *testing.T) {
	if got := parseList(" agent-opus, ,agent-sol,agent-opus "); !slices.Equal(got, []string{"agent-opus", "agent-sol"}) {
		t.Errorf("parseList() = %q", got)
	}
}

func TestParseTTL(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"", DefaultKelosTaskTTL, true},
		{"24h", 24 * time.Hour, true},
		{"0s", 0, false},
		{"-1h", 0, false},
		{"three days", 0, false},
	}
	for _, tt := range tests {
		got, err := parseTTL(tt.value)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("parseTTL(%q) = %v, %v; want %v, ok %v", tt.value, got, err, tt.want, tt.ok)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	tests := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"valid":              {func(*Config) {}, ""},
		"no GitHub instance": {func(c *Config) { c.GitHubInstance = "" }, "instances are required"},
		"invalid kelos":      {func(c *Config) { c.Kelos.Namespace = "" }, kelos.EnvNamespace},
		"other vikunja":      {func(c *Config) { c.Kelos.VikunjaInstance = "vikunja.example.org" }, kelos.EnvVikunjaInstance},
		"no ttl":             {func(c *Config) { c.KelosTaskTTL = 0 }, EnvKelosTaskTTL},
	}
	for name, tt := range tests {
		c := testConfig()
		tt.mutate(&c)
		err := c.validate(nil)
		if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: validate() = %v, want %q", name, err, tt.want)
		}
	}
}

func TestConfigValidateWithKelosOff(t *testing.T) {
	off := map[model.Source]string{model.SourceKelos: "off"}
	c := testConfig()
	c.Kelos = kelos.Config{Cluster: testCluster}
	c.KelosTaskTTL = 0
	if err := c.validate(off); err != nil {
		t.Errorf("validate() = %v, want only the cluster required of Kelos off", err)
	}
	c.Kelos.Cluster = ""
	if err := c.validate(off); err == nil || !strings.Contains(err.Error(), kelos.EnvCluster) {
		t.Errorf("validate() without a cluster = %v, want it to name %s", err, kelos.EnvCluster)
	}
	c = testConfig()
	c.Off = map[model.Source]string{model.SourceGitHub: ""}
	if err := c.validate(c.Off); err == nil {
		t.Error("validate() of a source off without a reason = nil")
	}
}

// testEnv is a complete environment for ConfigFromEnv.
func testEnv() map[string]string {
	return map[string]string{
		vikunja.EnvURL:           "https://Vikunja.example.com:8443",
		vikunja.EnvToken:         testToken,
		kelos.EnvCluster:         testCluster,
		kelos.EnvNamespace:       testNamespace,
		kelos.EnvLabelDomain:     testVikunjaHost,
		kelos.EnvVikunjaInstance: testVikunjaHost,
		github.EnvAPIURL:         "https://" + testGitHubHost + testGitHubAPIPath,
		github.EnvToken:          testToken,
		EnvWorkspaces:            "widgets=example-org/widgets",
		EnvVikunjaAgentUsers:     "agent-opus",
		github.EnvAnnouncers:     testAgent,
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := testEnv()
	got, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	c := got.Collector
	if c.VikunjaInstance != testVikunjaHost || c.GitHubInstance != testGitHubHost {
		t.Errorf("instances = %q, %q", c.VikunjaInstance, c.GitHubInstance)
	}
	if c.VikunjaServiceAccount != vikunja.DefaultServiceAccount || !slices.Equal(c.VikunjaAgentUsers, []string{testAgent}) {
		t.Errorf("vikunja accounts = %q, %q", c.VikunjaServiceAccount, c.VikunjaAgentUsers)
	}
	if c.Workspaces[testWorkspace] != testRepo || c.KelosTaskTTL != DefaultKelosTaskTTL || c.Kelos.Cluster != testCluster {
		t.Errorf("config = %+v", c)
	}
	if !slices.Equal(c.GitHubAnnouncers, []string{testAgent}) {
		t.Errorf("github announcers = %q", c.GitHubAnnouncers)
	}
}

func TestConfigFromEnvTurnsSourcesOff(t *testing.T) {
	env := testEnv()
	delete(env, vikunja.EnvToken)
	delete(env, github.EnvToken)
	env[EnvWorkspaces] = "widgets"
	env[EnvKelosTaskTTL] = "never"
	got, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v, want the sources off instead", err)
	}
	c := got.Collector
	if c.VikunjaInstance != testVikunjaHost || c.GitHubInstance != testGitHubHost || c.Kelos.Cluster != testCluster {
		t.Errorf("instances = %q, %q, %q; want each still named", c.VikunjaInstance, c.GitHubInstance, c.Kelos.Cluster)
	}
	for source, wants := range map[model.Source][]string{
		model.SourceVikunja: {vikunja.EnvToken},
		model.SourceGitHub:  {github.EnvToken, EnvWorkspaces},
		model.SourceKelos:   {EnvKelosTaskTTL},
	} {
		for _, want := range wants {
			if !strings.Contains(c.Off[source], want) {
				t.Errorf("Off[%s] = %q, want it to name %s", source, c.Off[source], want)
			}
		}
	}
}

func TestConfigFromEnvNamesTheInstancesOfOffSources(t *testing.T) {
	env := testEnv()
	delete(env, vikunja.EnvURL)
	delete(env, kelos.EnvNamespace)
	delete(env, github.EnvAPIURL)
	delete(env, github.EnvToken)
	got, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	c := got.Collector
	if c.VikunjaInstance != testVikunjaHost || c.Kelos.Cluster != testCluster || c.GitHubInstance != "github.com" {
		t.Errorf("instances = %q, %q, %q; want them named from the settings left", c.VikunjaInstance, c.Kelos.Cluster,
			c.GitHubInstance)
	}
	if len(c.Off) != 3 {
		t.Errorf("Off = %q, want every source off", c.Off)
	}
}

func TestConfigFromEnvRejectsUnnamedInstances(t *testing.T) {
	_, err := ConfigFromEnv(func(k string) string {
		return map[string]string{github.EnvAPIURL: "ftp://ghe.example.com"}[k]
	})
	for _, want := range []string{vikunja.EnvURL, kelos.EnvVikunjaInstance, kelos.EnvCluster, github.EnvAPIURL} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ConfigFromEnv() error = %v, want it to name %s", err, want)
		}
	}
}

func TestConfigFromEnvTurnsKelosOffForAnotherVikunja(t *testing.T) {
	env := testEnv()
	env[kelos.EnvVikunjaInstance] = "vikunja.example.org"
	got, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if off := got.Collector.Off; !strings.Contains(off[model.SourceKelos], kelos.EnvVikunjaInstance) || len(off) != 1 {
		t.Errorf("Off = %q, want Kelos off for the mismatched Vikunja hosts", off)
	}
}

func TestOpen(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	path := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := "apiVersion: v1\nkind: Config\nclusters:\n- name: example\n  cluster: {server: 'https://kube.example.com:6443'}\n" +
		"users:\n- name: reader\n  user: {token: fabricated}\ncontexts:\n- name: example\n  context: {cluster: example, user: reader}\n" +
		"current-context: example\n"
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
	env := testEnv()
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c, err := cfg.Open(); err != nil || c == nil {
		t.Errorf("Open() = %v, %v", c, err)
	}
}

func TestOpenClientsTurnsKelosOffWithoutACluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("HOME", t.TempDir())
	env := testEnv()
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	clients := cfg.OpenClients()
	if clients.Kelos != nil || clients.Vikunja == nil || clients.GitHub == nil {
		t.Errorf("clients = %+v, want every one but Kelos's", clients)
	}
	if cfg.Collector.Off[model.SourceKelos] == "" || len(cfg.Collector.Off) != 1 {
		t.Errorf("Off = %q, want Kelos off and why", cfg.Collector.Off)
	}
	if _, err := New(cfg.Collector, clients.Sources()); err != nil {
		t.Errorf("New() over the clients = %v", err)
	}
}
