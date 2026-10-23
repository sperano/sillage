package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testPassword = "s3cret-password"

func TestConfigFromEnv(t *testing.T) {
	url := "postgres://sillage:" + testPassword + "@db.example.com:5432/sillage"
	tests := []struct {
		name    string
		env     map[string]string
		want    Retention
		wantErr string
	}{
		{"defaults", map[string]string{EnvDatabaseURL: url}, Retention{}, ""},
		{"retention", map[string]string{EnvDatabaseURL: url, EnvRetention: "90d"}, Retention{keep: 90 * day}, ""},
		{"no database", map[string]string{EnvRetention: "90d"}, Retention{}, "is not set"},
		{"bad retention", map[string]string{EnvDatabaseURL: url, EnvRetention: "soon"}, Retention{}, EnvRetention},
		{"bad url", map[string]string{EnvDatabaseURL: "postgres://sillage:" + testPassword + "@db.example.com:port"},
			Retention{}, "not a valid PostgreSQL connection string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), testPassword) {
					t.Errorf("err %q contains the password", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(cfg.DatabaseURL) != url || cfg.Retention != tt.want {
				t.Errorf("cfg = %#v (retention %s), want retention %s", cfg, cfg.Retention, tt.want)
			}
		})
	}
}

func TestConfigFromEnvNotConfigured(t *testing.T) {
	_, err := ConfigFromEnv(func(string) string { return "" })
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestDatabaseURLRedacted(t *testing.T) {
	cfg := Config{DatabaseURL: DatabaseURL("postgres://sillage:" + testPassword + "@db.example.com/sillage")}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(cfg.DatabaseURL), fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg), string(data)} {
		if strings.Contains(out, testPassword) || strings.Contains(out, "db.example.com") {
			t.Errorf("%q shows the connection string", out)
		}
	}
	if got := cfg.DatabaseURL.LogValue().String(); got != redacted {
		t.Errorf("LogValue = %q, want %q", got, redacted)
	}
}

func TestParseRetention(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"forever", 0, false},
		{" 90d ", 90 * day, false},
		{"720h", 720 * time.Hour, false},
		{"24h", day, false},
		{"1d", day, false},
		{"6m", 0, true},
		{"23h", 0, true},
		{"0d", 0, true},
		{"-3d", 0, true},
		{"xd", 0, true},
		{"99999999999d", 0, true},
		{"never", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRetention(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if got.keep != tt.want {
				t.Errorf("keep = %s, want %s", got.keep, tt.want)
			}
		})
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := at(0)
	if _, ok := (Retention{}).Cutoff(now); ok {
		t.Error("the default retention has a cutoff")
	}
	got, ok := Retention{keep: 2 * day}.Cutoff(now)
	if !ok || !got.Equal(now.Add(-2*day)) {
		t.Errorf("Cutoff = %v, %v; want two days before now", got, ok)
	}
	if s := (Retention{}).String(); s != retentionKeepAll {
		t.Errorf("String = %q, want %q", s, retentionKeepAll)
	}
}
