package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/api"
	"github.com/sperano/sillage/internal/source/loki"
	"github.com/sperano/sillage/internal/store"
	"github.com/sperano/sillage/internal/usagefeed"
)

// usageSinceEnv names the environment variable holding, as an RFC 3339
// time, when the runners' usage telemetry started: the usage feed's first
// read starts there. Required with loki.EnvURL.
const usageSinceEnv = "SILLAGE_USAGE_SINCE"

// usageFeed is the usage feed the environment configures, not yet started.
type usageFeed struct {
	// feed is nil when the feed is off.
	feed *usagefeed.Feed
	// since is when the usage telemetry started, set when the feed is on.
	since time.Time
}

// config is what the cost endpoint says of the feed.
func (u usageFeed) config() api.UsageConfig {
	return api.UsageConfig{On: u.feed != nil, Since: u.since}
}

// newUsageFeed reads the usage feed's configuration into st. The feed is
// off, and logged once with the reason, without a store, with neither
// setting, with one but not the other, or with a setting that cannot be
// used: it never fails the start.
func newUsageFeed(getenv func(string) string, st *store.Store, reg prometheus.Registerer,
	log *slog.Logger) usageFeed {
	if st == nil {
		log.Info("usage feed off: no store", "env", store.EnvDatabaseURL)
		return usageFeed{}
	}
	cfg, since, err := usageSettings(getenv)
	if errors.Is(err, errUsageUnset) {
		log.Info("usage feed off", "reason", err.Error())
		return usageFeed{}
	}
	if err != nil {
		log.Warn("usage feed off: its configuration is incomplete", "reason", err.Error())
		return usageFeed{}
	}
	client, err := loki.New(cfg)
	if err != nil {
		log.Warn("usage feed off", "reason", err.Error())
		return usageFeed{}
	}
	feed, err := usagefeed.New(usagefeed.Config{Source: client, Store: st, Since: since},
		usagefeed.NewMetrics(reg), log)
	if err != nil {
		log.Warn("usage feed off", "reason", err.Error())
		return usageFeed{}
	}
	return usageFeed{feed: feed, since: since.UTC()}
}

// errUsageUnset says that neither setting of the usage feed is set.
var errUsageUnset = errors.New("neither " + loki.EnvURL + " nor " + usageSinceEnv + " is set")

// usageSettings reads the feed's Loki configuration and start time.
func usageSettings(getenv func(string) string) (loki.Config, time.Time, error) {
	url, sinceText := strings.TrimSpace(getenv(loki.EnvURL)), strings.TrimSpace(getenv(usageSinceEnv))
	switch {
	case url == "" && sinceText == "":
		return loki.Config{}, time.Time{}, errUsageUnset
	case url == "":
		return loki.Config{}, time.Time{}, fmt.Errorf("%s is set without %s", usageSinceEnv, loki.EnvURL)
	case sinceText == "":
		return loki.Config{}, time.Time{}, fmt.Errorf("%s is set without %s: it says when the telemetry started",
			loki.EnvURL, usageSinceEnv)
	}
	since, err := time.Parse(time.RFC3339, sinceText)
	if err != nil {
		return loki.Config{}, time.Time{}, fmt.Errorf("%s must be an RFC 3339 time: %w", usageSinceEnv, err)
	}
	cfg, err := loki.ConfigFromEnv(getenv)
	if err != nil {
		return loki.Config{}, time.Time{}, err
	}
	return cfg, since, nil
}

// start runs the feed until ctx is done, and returns a function that waits
// for it to stop. With the feed off, it starts nothing.
func (u usageFeed) start(ctx context.Context) func() {
	if u.feed == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		u.feed.Run(ctx)
	}()
	return func() { <-done }
}
