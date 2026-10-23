// Command sillage serves sillage's HTTP endpoints. For now that is a health
// check and the embedded frontend; the read API comes later.
//
// "sillage story -task <id>" (or "-page <id>") instead prints one story's
// JSON, assembled from the live sources the environment configures.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sperano/sillage/web"
)

const (
	// defaultListenAddr is used when neither -listen nor listenAddrEnv is set.
	defaultListenAddr = ":8080"
	// listenAddrEnv names the environment variable that sets the listen
	// address; the -listen flag overrides it.
	listenAddrEnv = "SILLAGE_LISTEN_ADDR"
	// readHeaderTimeout bounds how long a client may take to send headers.
	readHeaderTimeout = 10 * time.Second
	// idleTimeout closes keep-alive connections left idle this long.
	idleTimeout = 2 * time.Minute
	// shutdownTimeout bounds how long in-flight requests may take to finish
	// once a termination signal arrives.
	shutdownTimeout = 10 * time.Second
	// exitUsage is the exit status for a command-line error, as with
	// flag.ExitOnError.
	exitUsage = 2
	// defaultVersion is the version of a binary built without setting
	// version, such as one from a plain go build.
	defaultVersion = "dev"
)

// version is the sillage release this binary was built from. Release
// builds set it with -ldflags "-X main.version=<version>"; the Dockerfile
// passes its VERSION build argument.
var version = defaultVersion

// errUsage marks command-line errors, which parseConfig has already reported
// along with the usage.
var errUsage = errors.New("usage error")

type config struct {
	listenAddr string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore the default handling so that a second
	// one kills a shutdown that hangs.
	context.AfterFunc(ctx, stop)
	var err error
	if args := os.Args[1:]; len(args) > 0 && args[0] == storyCommand {
		err = runStory(ctx, args[1:], os.Getenv, os.Stdout, os.Stderr, openCollector)
	} else {
		err = run(ctx, args, os.Getenv, os.Stderr)
	}
	stop()
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errUsage):
		os.Exit(exitUsage)
	case errors.Is(err, errPartial):
		os.Exit(exitPartial)
	default:
		fmt.Fprintln(os.Stderr, "sillage:", err)
		os.Exit(1)
	}
}

// run parses the configuration, listens and serves until ctx is done.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := parseConfig(args, getenv, stderr)
	if err != nil {
		return err
	}
	slog.Info("starting", "version", version)
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.listenAddr)
	if err != nil {
		return err
	}
	return serve(ctx, ln, newHandler(web.FS()))
}

// parseConfig reads the configuration from the command line and the
// environment; a flag wins over its environment variable, which wins over
// the default. Usage and command-line errors are written to output, and
// the returned error wraps errUsage (or is flag.ErrHelp for -h).
func parseConfig(args []string, getenv func(string) string, output io.Writer) (config, error) {
	cfg := config{listenAddr: defaultListenAddr}
	if addr := getenv(listenAddrEnv); addr != "" {
		cfg.listenAddr = addr
	}
	flags := flag.NewFlagSet("sillage", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.listenAddr, "listen", cfg.listenAddr,
		"`address` to listen on, also set by $"+listenAddrEnv)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config{}, err
		}
		return config{}, fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(output, "unexpected arguments: %q\n", flags.Args())
		flags.Usage()
		return config{}, fmt.Errorf("%w: unexpected arguments", errUsage)
	}
	return cfg, nil
}

// serve serves h on ln until ctx is done, then shuts down gracefully.
func serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	slog.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
