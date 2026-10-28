package reconcile

import (
	"slices"
	"strings"
	"testing"
	"time"
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
				EnvDeliveryDelay: "5s", EnvDeliveryRootsPerCycle: " 7 ",
			},
			want: Config{
				Interval: time.Minute, RefreshInterval: 2 * time.Hour, LockRetryInterval: 30 * time.Second,
				VikunjaProjects: []int64{3, 5}, DeliveryDelay: 5 * time.Second, DeliveryRootsPerCycle: 7,
			},
		},
		{
			name: "every problem",
			env: map[string]string{
				EnvInterval: "0s", EnvRefreshInterval: "soon", EnvLockRetryInterval: "-1m", EnvVikunjaProjects: "3,x",
				EnvDeliveryDelay: "0", EnvDeliveryRootsPerCycle: "0",
			},
			wantErr: []string{
				EnvInterval, EnvRefreshInterval, EnvLockRetryInterval, EnvVikunjaProjects, EnvDeliveryDelay,
				EnvDeliveryRootsPerCycle,
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
				got.LockRetryInterval != tt.want.LockRetryInterval || !slices.Equal(got.VikunjaProjects, tt.want.VikunjaProjects) ||
				got.DeliveryDelay != tt.want.DeliveryDelay || got.DeliveryRootsPerCycle != tt.want.DeliveryRootsPerCycle {
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
