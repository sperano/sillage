package loki

import (
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/source/kelos"
)

func TestConfigFromEnv(t *testing.T) {
	good := map[string]string{
		EnvURL: " http://loki.example.com/ ", EnvOrgID: "tenant-1",
		kelos.EnvCluster: "cluster-a", kelos.EnvNamespace: "kelos-agents",
	}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"valid", good, ""},
		{"no url", with(EnvURL, ""), EnvURL},
		{"url without scheme", with(EnvURL, "loki.example.com"), EnvURL},
		{"url with query", with(EnvURL, "http://loki.example.com/?a=b"), EnvURL},
		{"no cluster", with(kelos.EnvCluster, ""), kelos.EnvCluster},
		{"no namespace", with(kelos.EnvNamespace, ""), kelos.EnvNamespace},
		{"namespace needing quoting", with(kelos.EnvNamespace, `a"b`), kelos.EnvNamespace},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one naming %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.BaseURL != "http://loki.example.com" || cfg.OrgID != "tenant-1" ||
				cfg.Cluster != "cluster-a" || cfg.Namespace != "kelos-agents" {
				t.Errorf("config = %+v", cfg)
			}
		})
	}
}
