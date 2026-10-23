package vikunja

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := ConfigFromEnv(envOf(map[string]string{
		EnvURL:   " https://vikunja.example.com/ ",
		EnvToken: testToken,
	}))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	want := Config{
		BaseURL:        "https://vikunja.example.com",
		Instance:       "vikunja.example.com",
		Token:          testToken,
		ServiceAccount: DefaultServiceAccount,
	}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}

	cfg, err = ConfigFromEnv(envOf(map[string]string{
		EnvURL: "https://vikunja.example.com", EnvToken: testToken, EnvServiceAccount: "automation",
	}))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.ServiceAccount != "automation" {
		t.Errorf("ServiceAccount = %q, want %q", cfg.ServiceAccount, "automation")
	}
}

func TestConfigFromEnvRejects(t *testing.T) {
	tests := []struct {
		name string
		url  string
		tok  string
	}{
		{"no URL", "", testToken},
		{"no token", "https://vikunja.example.com", ""},
		{"not http", "ftp://vikunja.example.com", testToken},
		{"no host", "https://", testToken},
		{"credentials in URL", "https://user:" + testToken + "@vikunja.example.com", testToken},
		{"query", "https://vikunja.example.com/?x=1", testToken},
		{"fragment", "https://vikunja.example.com/#top", testToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConfigFromEnv(envOf(map[string]string{EnvURL: tt.url, EnvToken: tt.tok}))
			if err == nil {
				t.Fatal("ConfigFromEnv succeeded")
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error %q leaks the token", err)
			}
		})
	}
}

func TestTokenNeverPrinted(t *testing.T) {
	cfg := Config{BaseURL: "https://vikunja.example.com", Token: testToken, ServiceAccount: "kelos"}
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("config", "cfg", cfg, "token", cfg.Token)
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", cfg),
		"%+v":  fmt.Sprintf("%+v", cfg),
		"%#v":  fmt.Sprintf("%#v", cfg),
		"%s":   fmt.Sprintf("token=%s", cfg.Token),
		"slog": logged.String(),
		"json": string(encoded),
	}
	for how, out := range outputs {
		if strings.Contains(out, testToken) {
			t.Errorf("%s prints the token: %s", how, out)
		}
		if !strings.Contains(out, redacted) {
			t.Errorf("%s does not show %s: %s", how, redacted, out)
		}
	}
}
