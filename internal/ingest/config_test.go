package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretsFromEnv(t *testing.T) {
	env := map[string]string{EnvGitHubSecret: " gh secret\n", EnvVikunjaSecret: ""}
	s := SecretsFromEnv(func(k string) string { return env[k] })
	if s.GitHub != " gh secret\n" || s.Vikunja != "" {
		t.Errorf("SecretsFromEnv = %q, %q; want the values as they are", string(s.GitHub), string(s.Vikunja))
	}
	if !s.Any() || (Secrets{}).Any() {
		t.Error("Any() is wrong")
	}
}

func TestSecretsAreRedacted(t *testing.T) {
	s := Secrets{GitHub: "gh-secret-value", Vikunja: "vk-secret-value"}
	var logged bytes.Buffer
	slog.New(slog.NewTextHandler(&logged, nil)).Info("config", "secrets", s, "github", s.GitHub)
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), logged.String(),
		string(encoded)} {
		if strings.Contains(out, "secret-value") || !strings.Contains(out, redacted) {
			t.Errorf("%q leaks a secret or lacks %s", out, redacted)
		}
	}
}
