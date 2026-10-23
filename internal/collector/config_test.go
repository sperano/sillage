package collector

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
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
		"valid":            {func(*Config) {}, ""},
		"no wiki instance": {func(c *Config) { c.WikiInstance = "" }, "instances are required"},
		"invalid kelos":    {func(c *Config) { c.Kelos.Namespace = "" }, kelos.EnvNamespace},
		"other vikunja":    {func(c *Config) { c.Kelos.VikunjaInstance = "vikunja.example.org" }, kelos.EnvVikunjaInstance},
		"no ttl":           {func(c *Config) { c.KelosTaskTTL = 0 }, EnvKelosTaskTTL},
	}
	for name, tt := range tests {
		c := testConfig()
		tt.mutate(&c)
		err := c.validate()
		if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: validate() = %v, want %q", name, err, tt.want)
		}
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
		wiki.EnvURL:              "https://" + testWikiHost,
		wiki.EnvToken:            testToken,
		wiki.EnvAgentUsers:       "21",
		EnvWorkspaces:            "widgets=example-org/widgets",
		EnvVikunjaAgentUsers:     "agent-opus",
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := testEnv()
	got, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	c := got.Collector
	if c.VikunjaInstance != testVikunjaHost || c.GitHubInstance != testGitHubHost || c.WikiInstance != testWikiHost {
		t.Errorf("instances = %q, %q, %q", c.VikunjaInstance, c.GitHubInstance, c.WikiInstance)
	}
	if c.VikunjaServiceAccount != vikunja.DefaultServiceAccount || !slices.Equal(c.VikunjaAgentUsers, []string{testAgent}) {
		t.Errorf("vikunja accounts = %q, %q", c.VikunjaServiceAccount, c.VikunjaAgentUsers)
	}
	if c.Workspaces[testWorkspace] != testRepo || c.KelosTaskTTL != DefaultKelosTaskTTL || c.Kelos.Cluster != testCluster {
		t.Errorf("config = %+v", c)
	}
	if !slices.Equal(c.WikiAgentUsers, []int64{testWikiUser}) || got.Wiki.Token != testToken {
		t.Errorf("wiki config = %+v", got.Wiki)
	}
}

func TestConfigFromEnvReportsEveryProblem(t *testing.T) {
	env := testEnv()
	delete(env, vikunja.EnvToken)
	delete(env, github.EnvToken)
	env[EnvWorkspaces] = "widgets"
	env[EnvKelosTaskTTL] = "never"
	_, err := ConfigFromEnv(func(k string) string { return env[k] })
	for _, want := range []string{vikunja.EnvToken, github.EnvToken, EnvWorkspaces, EnvKelosTaskTTL} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ConfigFromEnv() error = %v, want it to name %s", err, want)
		}
	}
}

func TestConfigFromEnvRejectsAnotherVikunja(t *testing.T) {
	env := testEnv()
	env[kelos.EnvVikunjaInstance] = "vikunja.example.org"
	if _, err := ConfigFromEnv(func(k string) string { return env[k] }); err == nil {
		t.Error("ConfigFromEnv() error = nil, want the mismatched Vikunja hosts reported")
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

func TestNewRejectsMissingSource(t *testing.T) {
	if _, err := New(testConfig(), Sources{}); err == nil {
		t.Error("New() error = nil, want every source required")
	}
}
