package kelos

import (
	"strings"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{
		EnvCluster:         " cluster-a ",
		EnvNamespace:       "agents",
		EnvLabelDomain:     "vikunja.example.com",
		EnvVikunjaInstance: "vikunja.example.com",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg != testConfig() {
		t.Errorf("config = %+v, want %+v", cfg, testConfig())
	}
}

func TestConfigFromEnvRejects(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"no cluster", EnvCluster, ""},
		{"no namespace", EnvNamespace, ""},
		{"bad namespace", EnvNamespace, "Agents_1"},
		{"no label domain", EnvLabelDomain, ""},
		{"label domain with slash", EnvLabelDomain, "vikunja.example.com/"},
		{"no vikunja instance", EnvVikunjaInstance, ""},
		{"vikunja url", EnvVikunjaInstance, "https://vikunja.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{
				EnvCluster:         "cluster-a",
				EnvNamespace:       "agents",
				EnvLabelDomain:     "vikunja.example.com",
				EnvVikunjaInstance: "vikunja.example.com",
			}
			env[tt.key] = tt.value
			_, err := ConfigFromEnv(func(k string) string { return env[k] })
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error = %v, want one naming %s", err, tt.key)
			}
		})
	}
}
