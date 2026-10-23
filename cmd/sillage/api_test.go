package main

import (
	"testing"

	"github.com/sperano/sillage/internal/source/wiki"
)

func TestNewAPI(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"no wiki", nil, false},
		{"wiki", map[string]string{
			wiki.EnvURL: "https://wiki.example.com", wiki.EnvToken: "token", wiki.EnvAgentUsers: "21",
		}, false},
		{"wiki without a token", map[string]string{wiki.EnvURL: "https://wiki.example.com"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := newAPI(func(key string) string { return tt.env[key] }, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && h == nil {
				t.Error("no handler")
			}
		})
	}
}
