package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/sperano/sillage/web"
)

// testTimeout bounds every wait in these tests.
const testTimeout = 5 * time.Second

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"default", nil, nil, defaultListenAddr},
		{"env", nil, map[string]string{listenAddrEnv: "127.0.0.1:9000"}, "127.0.0.1:9000"},
		{"flag", []string{"-listen", ":9001"}, nil, ":9001"},
		{
			"flag overrides env",
			[]string{"-listen", ":9001"},
			map[string]string{listenAddrEnv: "127.0.0.1:9000"},
			":9001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			cfg, err := parseConfig(tt.args, getenv, io.Discard)
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if cfg.listenAddr != tt.want {
				t.Errorf("listenAddr = %q, want %q", cfg.listenAddr, tt.want)
			}
		})
	}
}

func TestParseConfigErrors(t *testing.T) {
	noEnv := func(string) string { return "" }
	tests := []struct {
		name   string
		args   []string
		wantIs error
	}{
		{"unknown flag", []string{"-nope"}, errUsage},
		{"positional argument", []string{"extra"}, errUsage},
		{"help", []string{"-h"}, flag.ErrHelp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConfig(tt.args, noEnv, io.Discard)
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want %v", err, tt.wantIs)
			}
		})
	}
}

func TestServeAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	served := make(chan error, 1)
	go func() { served <- serve(ctx, ln, newHandler(web.FS())) }()

	client := &http.Client{Timeout: testTimeout}
	resp, err := client.Get("http://" + ln.Addr().String() + healthzPath)
	if err != nil {
		t.Fatalf("GET %s: %v", healthzPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("serve did not return after the context was cancelled")
	}
}

func TestRunListenError(t *testing.T) {
	noEnv := func(string) string { return "" }
	err := run(t.Context(), []string{"-listen", "not-an-address"}, noEnv, io.Discard)
	if err == nil {
		t.Fatal("run succeeded, want a listen error")
	}
	if errors.Is(err, errUsage) {
		t.Errorf("err = %v, want a listen error, not a usage error", err)
	}
}
