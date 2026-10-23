package main

import (
	"log/slog"
	"net/http"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/source/wiki"
	"github.com/sperano/sillage/internal/store"
)

// newAPI returns the read API over st, nil without a database, and, when
// the wiki source is configured (wiki.EnvURL), with its page listing, so
// that resolve finds a page by the path a wiki URL names.
func newAPI(getenv func(string) string, st *store.Store) (http.Handler, error) {
	cfg := api.Config{Version: version}
	if st != nil {
		// Only a real store: a nil *Store would be a non-nil Reader.
		cfg.Store = st
	}
	if getenv(wiki.EnvURL) == "" {
		slog.Info("no wiki configured: resolve finds wiki pages by id only", "env", wiki.EnvURL)
		return api.New(cfg), nil
	}
	wcfg, err := wiki.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	client, err := wiki.NewClient(wcfg, nil)
	if err != nil {
		return nil, err
	}
	cfg.Wiki, cfg.WikiInstance = client, wcfg.Instance
	return api.New(cfg), nil
}
