package reconcile

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/source/github"
)

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr []string
	}{
		{name: "defaults", env: nil, want: DefaultConfig()},
		{
			name: "overrides",
			env: map[string]string{
				EnvInterval: "1m", EnvRefreshInterval: " 2h ", EnvLockRetryInterval: "30s", EnvVikunjaProjects: "3, 5,3,",
				EnvDeliveryDelay: "5s", EnvDeliveryRootsPerCycle: " 7 ", EnvBackstopInterval: "12h",
				EnvGitHubHooks: "acme/widgets:9001, acme/gears:9002,acme/widgets:9001,", EnvHookCheckInterval: "10m",
			},
			want: Config{
				Interval: time.Minute, RefreshInterval: 2 * time.Hour, BackstopInterval: 12 * time.Hour,
				LockRetryInterval: 30 * time.Second,
				VikunjaProjects:   []int64{3, 5}, DeliveryDelay: 5 * time.Second, DeliveryRootsPerCycle: 7,
				GitHubHooks: []github.Hook{
					{Repository: github.Repository{Owner: "acme", Name: "widgets"}, ID: 9001},
					{Repository: github.Repository{Owner: "acme", Name: "gears"}, ID: 9002},
				},
				HookCheckInterval: 10 * time.Minute,
			},
		},
		{
			name: "every problem",
			env: map[string]string{
				EnvInterval: "0s", EnvRefreshInterval: "soon", EnvLockRetryInterval: "-1m", EnvVikunjaProjects: "3,x",
				EnvDeliveryDelay: "0", EnvDeliveryRootsPerCycle: "0", EnvBackstopInterval: "daily",
				EnvGitHubHooks: "acme/widgets", EnvHookCheckInterval: "never",
			},
			wantErr: []string{
				EnvInterval, EnvRefreshInterval, EnvLockRetryInterval, EnvVikunjaProjects, EnvDeliveryDelay,
				EnvDeliveryRootsPerCycle, EnvBackstopInterval, EnvGitHubHooks, EnvHookCheckInterval,
			},
		},
		{name: "negative project", env: map[string]string{EnvVikunjaProjects: "-3"}, wantErr: []string{EnvVikunjaProjects}},
		{
			name: "roots per cycle not a number", env: map[string]string{EnvDeliveryRootsPerCycle: "many"},
			wantErr: []string{EnvDeliveryRootsPerCycle},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConfigFromEnv(func(k string) string { return tt.env[k] })
			if len(tt.wantErr) > 0 {
				for _, want := range tt.wantErr {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("error = %v, want it to name %s", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Interval != tt.want.Interval || got.RefreshInterval != tt.want.RefreshInterval ||
				got.BackstopInterval != tt.want.BackstopInterval ||
				got.LockRetryInterval != tt.want.LockRetryInterval || !slices.Equal(got.VikunjaProjects, tt.want.VikunjaProjects) ||
				got.DeliveryDelay != tt.want.DeliveryDelay || got.DeliveryRootsPerCycle != tt.want.DeliveryRootsPerCycle ||
				!slices.Equal(got.GitHubHooks, tt.want.GitHubHooks) || got.HookCheckInterval != tt.want.HookCheckInterval {
				t.Errorf("ConfigFromEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestJitter(t *testing.T) {
	const d = time.Minute
	spread := time.Duration(jitterFraction * float64(d))
	for range 100 {
		if got := jitter(d); got < d-spread || got > d+spread {
			t.Fatalf("jitter(%v) = %v, want within %v of it", d, got, spread)
		}
	}
}
