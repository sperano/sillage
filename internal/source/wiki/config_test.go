package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := ConfigFromEnv(envOf(map[string]string{
		EnvURL:        " https://Wiki.Example.com/ ",
		EnvToken:      testToken,
		EnvAgentUsers: " 21, 22,",
	}))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BaseURL != "https://Wiki.Example.com" || cfg.Instance != testInstance || cfg.Token != testToken ||
		!slices.Equal(cfg.AgentUsers, testAgents) {
		t.Errorf("got %+v", cfg)
	}
}

func TestConfigFromEnvRejects(t *testing.T) {
	tests := []struct {
		name, url, tok, agents string
	}{
		{"no URL", "", testToken, "21"},
		{"no token", "https://wiki.example.com", "", "21"},
		{"not http", "ftp://wiki.example.com", testToken, "21"},
		{"no host", "https://", testToken, "21"},
		{"credentials in URL", "https://user:" + testToken + "@wiki.example.com", testToken, "21"},
		{"query", "https://wiki.example.com/?x=1", testToken, "21"},
		{"fragment", "https://wiki.example.com/#top", testToken, "21"},
		{"no agent users", "https://wiki.example.com", testToken, " , "},
		{"agent user not a number", "https://wiki.example.com", testToken, "21,opus"},
		{"agent user not positive", "https://wiki.example.com", testToken, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConfigFromEnv(envOf(map[string]string{EnvURL: tt.url, EnvToken: tt.tok, EnvAgentUsers: tt.agents}))
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
	cfg := Config{BaseURL: "https://wiki.example.com", Instance: testInstance, Token: testToken, AgentUsers: testAgents}
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
	// Verbs that are not string verbs too: fmt reports them with the value.
	// The verbs are variables so vet lets the test use them on a string.
	for _, verb := range []string{"%q", "%x", "%d", "%t", "%10.3f"} {
		outputs[verb] = fmt.Sprintf(verb, cfg.Token)
		outputs[verb+" of the Config"] = fmt.Sprintf(verb, cfg)
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
