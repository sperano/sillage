package github

import (
	"slices"
	"strings"
	"testing"
)

func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantAPI      string
		wantInstance string
		wantErr      string
	}{
		{"github.com by default", map[string]string{EnvToken: testToken}, DefaultAPIURL, dotComInstance, ""},
		{"enterprise", map[string]string{EnvAPIURL: " https://GHE.example.com/api/v3/ ", EnvToken: testToken},
			"https://GHE.example.com/api/v3", "ghe.example.com", ""},
		{"no token", map[string]string{EnvAPIURL: DefaultAPIURL}, "", "", EnvToken + " is not set"},
		{"not http", map[string]string{EnvAPIURL: "ftp://ghe.example.com", EnvToken: testToken}, "", "", "http(s) URL"},
		{"no host", map[string]string{EnvAPIURL: "https://", EnvToken: testToken}, "", "", "http(s) URL"},
		{"credentials", map[string]string{EnvAPIURL: "https://u:p@ghe.example.com", EnvToken: testToken}, "", "", "credentials"},
		{"query", map[string]string{EnvAPIURL: "https://ghe.example.com/?x=1", EnvToken: testToken}, "", "", "query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(envOf(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "u:p") {
					t.Errorf("error %q repeats the URL", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.APIURL != tt.wantAPI || cfg.Instance != tt.wantInstance || cfg.Token != testToken {
				t.Errorf("config = %q, %q, token set %v; want %q, %q", cfg.APIURL, cfg.Instance,
					cfg.Token == testToken, tt.wantAPI, tt.wantInstance)
			}
		})
	}
}

func TestConfigAnnouncers(t *testing.T) {
	tests := []struct {
		value string
		want  []string
	}{
		{"", nil},
		{" owner-gh , example-agents[bot],, ", []string{"owner-gh", "example-agents[bot]"}},
	}
	for _, tt := range tests {
		cfg, err := ConfigFromEnv(envOf(map[string]string{EnvToken: testToken, EnvAnnouncers: tt.value}))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cfg.Announcers, tt.want) {
			t.Errorf("%s=%q: announcers %q, want %q", EnvAnnouncers, tt.value, cfg.Announcers, tt.want)
		}
	}
}

func TestParseRepository(t *testing.T) {
	if r, err := ParseRepository("example-org/widgets"); err != nil || r != testRepo {
		t.Errorf("ParseRepository = %v, %v; want %v", r, err, testRepo)
	}
	for _, bad := range []string{"", "widgets", "/widgets", "example-org/", "a/b/c", "a/..", "a/b?c", "a b/c"} {
		if _, err := ParseRepository(bad); err == nil {
			t.Errorf("ParseRepository(%q) accepted it", bad)
		}
	}
}

func TestTokenRedacted(t *testing.T) {
	text, err := Token(testToken).MarshalText()
	if err != nil || string(text) != redacted {
		t.Errorf("MarshalText = %q, %v", text, err)
	}
	if v := Token(testToken).LogValue().String(); v != redacted {
		t.Errorf("LogValue = %q", v)
	}
}
