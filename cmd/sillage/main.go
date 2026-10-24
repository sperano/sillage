// Command sillage serves sillage's HTTP endpoints: a health check, the
// Prometheus metrics, the read API over the store (internal/api, under
// /api/, which must stay behind the Authentik forward-auth), the webhook
// ingest paths (internal/ingest, under /ingest/, authenticated by each
// delivery's signature) and the embedded frontend. With
// SILLAGE_DATABASE_URL set, it migrates and prunes the store at startup,
// then runs the collector, which keeps the store current; without,
// nothing is collected and the API answers 503 for what it would read.
// The collector does not take the reads deliveries ask for yet; until it
// does, the ingest paths answer 503 to every delivery and record nothing.
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

	"github.com/sperano/sillage/internal/api"
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
	// responseWriteTime is how long a response may take to send, once
	// the handler has it.
	responseWriteTime = time.Minute
	// writeTimeout bounds a request from its headers to the end of its
	// response: the API's own bound on answering (the default: newAPI sets
	// no other), then responseWriteTime to send it, so that a client that
	// stops reading cannot hold the connection.
	writeTimeout = api.DefaultTimeout + responseWriteTime
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

// run parses the configuration, listens, starts the collector the
// environment configures, and serves until ctx is done. The collector
// stops with the server, and run waits for it.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := parseConfig(args, getenv, stderr)
	if err != nil {
		return err
	}
	slog.Info("starting", "version", version)
	// Listen first: a bad or busy address fails the start before the store
	// is migrated or pruned.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.listenAddr)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, getenv)
	if err != nil {
		_ = ln.Close()
		return err
	}
	if st != nil {
		defer st.Close()
	}
	apiHandler, err := newAPI(getenv, st)
	if err != nil {
		_ = ln.Close()
		return err
	}
	// The collector loop does not take the reads deliveries ask for (an
	// ingest.Queue) yet: until it does, the ingest paths refuse every
	// delivery.
	ingestHandler, err := newIngest(getenv, st, nil, nil)
	if err != nil {
		_ = ln.Close()
		return err
	}
	metrics, m := newMetrics()
	collectCtx, stopCollecting := context.WithCancel(ctx)
	defer stopCollecting()
	wait, err := startCollector(collectCtx, getenv, st, m, slog.Default())
	if err != nil {
		_ = ln.Close()
		return err
	}
	err = serve(ctx, ln, newHandler(web.FS(), apiHandler, ingestHandler, metrics))
	// The collector stops before the store closes.
	stopCollecting()
	wait()
	return err
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
	srv := newServer(h)
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

// newServer returns the server of h, with its timeouts.
func newServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
