package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/store"
)

// kelosConsoleURLEnv names the environment variable holding the Kelos
// console's base URL, where the frontend links a run of the cluster
// kelos.EnvCluster names. Unset, runs have no link.
const kelosConsoleURLEnv = "SILLAGE_KELOS_CONSOLE_URL"

// newAPI returns the read API over st, nil without a database, with the
// Kelos console the frontend links runs to.
func newAPI(getenv func(string) string, st *store.Store) (http.Handler, error) {
	cfg := api.Config{Version: version}
	if st != nil {
		// Only a real store: a nil *Store would be a non-nil Reader.
		cfg.Store = st
	}
	consoles, err := kelosConsoles(getenv)
	if err != nil {
		return nil, err
	}
	cfg.KelosConsoles = consoles
	return api.New(cfg), nil
}

// kelosConsoles reads the Kelos console's base URL, keyed by the cluster
// name runs carry: nil when none is set. The URL must be an absolute http
// or https URL without a query or fragment, which the run links would drop.
func kelosConsoles(getenv func(string) string) (map[string]string, error) {
	raw := strings.TrimSpace(getenv(kelosConsoleURLEnv))
	if raw == "" {
		slog.Info("no Kelos console configured: runs have no link", "env", kelosConsoleURLEnv)
		return nil, nil
	}
	// Trimmed as kelos.ConfigFromEnv trims it, so that the key is the
	// cluster name on run keys.
	cluster := strings.TrimSpace(getenv(kelos.EnvCluster))
	if cluster == "" {
		return nil, fmt.Errorf("%s is set without %s: the console is that cluster's",
			kelosConsoleURLEnv, kelos.EnvCluster)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%s must be an http or https URL without a query or fragment",
			kelosConsoleURLEnv)
	}
	return map[string]string{cluster: raw}, nil
}
