package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/web"
)

const (
	// testTimeout bounds every wait in these tests.
	testTimeout = 5 * time.Second
	// buildTimeout bounds building and running the binary, which takes
	// longer than testTimeout on a cold build cache.
	buildTimeout = 2 * time.Minute
	// versionSymbol is the -X target the Dockerfile sets VERSION into.
	versionSymbol = "main.version"
	// dockerfilePath is the image's Dockerfile, from this package.
	dockerfilePath = "../../Dockerfile"
	// startLogPrefix and listenLogPrefix begin the log lines that run writes
	// before and once it serves, after slog's default timestamp.
	startLogPrefix  = "INFO starting "
	listenLogPrefix = "INFO listening "
)

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
	go func() { served <- serve(ctx, ln, newHandler(web.FS(), api.New(api.Config{}))) }()

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

// TestDockerfileSetsVersion pins the -X flag the Dockerfile passes to the
// symbol TestVersionLoggedAtStartup proves the linker sets, so that renaming
// version fails here instead of shipping images that all report the default.
func TestDockerfileSetsVersion(t *testing.T) {
	dockerfile, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read the Dockerfile: %v", err)
	}
	if want := "-X " + versionSymbol + "="; !strings.Contains(string(dockerfile), want) {
		t.Errorf("%s does not pass %q to the linker", dockerfilePath, want)
	}
}

// TestVersionLoggedAtStartup builds the binary with and without the -X flag
// the Dockerfile passes, and checks that it logs its version once at startup.
func TestVersionLoggedAtStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	tests := []struct {
		name    string
		ldflags string
		want    string
	}{
		{"unset", "", defaultVersion},
		{"set by the linker", "-X " + versionSymbol + "=v1.2.3-test", "v1.2.3-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), buildTimeout)
			defer cancel()
			bin := buildSillage(ctx, t, tt.ldflags)
			starts := startLines(ctx, t, bin)
			want := []string{startLogPrefix + "version=" + tt.want}
			if !slices.Equal(starts, want) {
				t.Errorf("start log lines = %q, want %q", starts, want)
			}
		})
	}
}

// buildSillage builds this package with ldflags and returns the binary.
func buildSillage(ctx context.Context, t *testing.T, ldflags string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build the binary: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "sillage")
	out, err := exec.CommandContext(ctx, goTool, "build", "-o", bin, "-ldflags", ldflags, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// startLines runs bin until it listens, stops it with SIGTERM as a
// container runtime would, and returns its start log lines without their
// timestamps.
func startLines(ctx context.Context, t *testing.T, bin string) []string {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, "-listen", "127.0.0.1:0")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Reaps the binary when a failure skips the Wait below; a second Wait
	// only returns an error.
	t.Cleanup(func() { _ = cmd.Wait() })
	var starts []string
	listening := false
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		line := sc.Text()
		if _, msg, ok := strings.Cut(line, startLogPrefix); ok {
			starts = append(starts, startLogPrefix+msg)
		}
		if strings.Contains(line, listenLogPrefix) {
			listening = true
			break
		}
	}
	if !listening {
		t.Fatal("the binary exited or timed out before it listened")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("exit after SIGTERM: %v", err)
	}
	return starts
}

// TestServerBoundsWrites checks that a response's write is bounded, after
// the API's own bound on answering.
func TestServerBoundsWrites(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.WriteTimeout <= api.DefaultTimeout {
		t.Errorf("WriteTimeout = %v, want more than the API's %v", srv.WriteTimeout, api.DefaultTimeout)
	}
	if srv.ReadHeaderTimeout == 0 || srv.IdleTimeout == 0 {
		t.Error("the server leaves its read-header or idle timeout unset")
	}
}
