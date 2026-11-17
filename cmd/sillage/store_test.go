package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/store"
)

const (
	wantStoreConnectAttempts       = 30
	wantStoreConnectAttemptTimeout = 5 * time.Second
	wantStoreConnectRetryInterval  = 2 * time.Second
)

func TestOpenStoreWithoutDatabase(t *testing.T) {
	st, err := openStore(t.Context(), func(string) string { return "" })
	if st != nil || err != nil {
		t.Errorf("openStore = %v, %v; want no store and no error", st, err)
	}
}

func TestOpenStoreErrors(t *testing.T) {
	env := map[string]string{
		store.EnvDatabaseURL: "postgres://sillage:s3cret-password@127.0.0.1:1/sillage",
		store.EnvRetention:   "soon",
	}
	st, err := openStore(t.Context(), func(k string) string { return env[k] })
	if err == nil {
		st.Close()
		t.Fatal("openStore succeeded")
	}
	if !strings.Contains(err.Error(), store.EnvRetention) {
		t.Errorf("error %q, want it to mention %s", err, store.EnvRetention)
	}
	if strings.Contains(err.Error(), "s3cret-password") {
		t.Errorf("error %q contains the password", err)
	}
}

func TestOpenStoreUnreachableDatabase(t *testing.T) {
	const password = "s3cret-password"
	env := map[string]string{
		store.EnvDatabaseURL: "postgres://sillage:" + password + "@127.0.0.1:1/sillage?connect_timeout=1",
	}
	openCalls := 0
	open := func(ctx context.Context, cfg store.Config) (*store.Store, error) {
		openCalls++
		return store.Open(ctx, cfg)
	}
	st, err := openStoreWith(t.Context(), func(k string) string { return env[k] }, open,
		func(context.Context, time.Duration) error { return nil },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		st.Close()
		t.Fatal("openStoreWith succeeded")
	}
	if !errors.Is(err, store.ErrConnecting) || openCalls != wantStoreConnectAttempts {
		t.Errorf("error = %v after %d opens; want ErrConnecting after %d",
			err, openCalls, wantStoreConnectAttempts)
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("error %q contains the password", err)
	}
}

func TestOpenStoreRetriesConnectionFailures(t *testing.T) {
	wantStore := new(store.Store)
	connectErr := fmt.Errorf("%w: fabricated refusal", store.ErrConnecting)
	openCalls := 0
	open := func(_ context.Context, cfg store.Config) (*store.Store, error) {
		openCalls++
		if cfg.MaxConnectTimeout != wantStoreConnectAttemptTimeout {
			t.Errorf("connect timeout = %s, want %s", cfg.MaxConnectTimeout, wantStoreConnectAttemptTimeout)
		}
		if openCalls < 3 {
			return nil, connectErr
		}
		return wantStore, nil
	}
	waits := 0
	wait := func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != wantStoreConnectRetryInterval {
			t.Errorf("retry delay = %s, want %s", delay, wantStoreConnectRetryInterval)
		}
		return nil
	}
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))

	got, err := openStoreRetry(t.Context(), store.Config{}, open, wait, log)
	if err != nil || got != wantStore {
		t.Fatalf("openStoreRetry = %v, %v; want store", got, err)
	}
	if openCalls != 3 || waits != 2 {
		t.Errorf("open calls = %d, waits = %d; want 3, 2", openCalls, waits)
	}
	if count := strings.Count(logs.String(), "store connection failed; retrying"); count != waits {
		t.Errorf("retry logs = %d, want %d; logs: %s", count, waits, logs.String())
	}
}

func TestOpenStoreSpendsConnectionRetryBudget(t *testing.T) {
	connectErr := fmt.Errorf("%w: fabricated refusal", store.ErrConnecting)
	openCalls := 0
	open := func(context.Context, store.Config) (*store.Store, error) {
		openCalls++
		return nil, connectErr
	}
	waits := 0
	wait := func(context.Context, time.Duration) error { waits++; return nil }

	_, err := openStoreRetry(t.Context(), store.Config{}, open, wait,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, store.ErrConnecting) {
		t.Fatalf("openStoreRetry error = %v, want ErrConnecting", err)
	}
	if openCalls != wantStoreConnectAttempts || waits != wantStoreConnectAttempts-1 {
		t.Errorf("open calls = %d, waits = %d; want %d, %d",
			openCalls, waits, wantStoreConnectAttempts, wantStoreConnectAttempts-1)
	}
}

func TestOpenStoreStopsWhenRetryWaitFails(t *testing.T) {
	connectErr := fmt.Errorf("%w: fabricated refusal", store.ErrConnecting)
	openCalls, waitCalls := 0, 0
	open := func(context.Context, store.Config) (*store.Store, error) {
		openCalls++
		return nil, connectErr
	}
	wait := func(context.Context, time.Duration) error {
		waitCalls++
		return context.Canceled
	}

	_, err := openStoreRetry(t.Context(), store.Config{}, open, wait,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, store.ErrConnecting) || !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want ErrConnecting and context.Canceled", err)
	}
	if openCalls != 1 || waitCalls != 1 {
		t.Errorf("open calls = %d, wait calls = %d; want 1, 1", openCalls, waitCalls)
	}
}

func TestOpenStoreDoesNotScheduleRetryAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	connectErr := fmt.Errorf("%w: fabricated refusal", store.ErrConnecting)
	var logs bytes.Buffer
	_, err := openStoreRetry(ctx, store.Config{},
		func(context.Context, store.Config) (*store.Store, error) { return nil, connectErr },
		func(context.Context, time.Duration) error { t.Fatal("wait called"); return nil },
		slog.New(slog.NewTextHandler(&logs, nil)))
	if !errors.Is(err, store.ErrConnecting) || !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want ErrConnecting and context.Canceled", err)
	}
	if logs.Len() != 0 {
		t.Errorf("logged a retry after cancellation: %s", logs.String())
	}
}

func TestWaitForStoreRetry(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := waitForStoreRetry(cancelled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait error = %v, want context.Canceled", err)
	}
	if err := waitForStoreRetry(t.Context(), 0); err != nil {
		t.Errorf("zero wait error = %v, want nil", err)
	}
}

func TestOpenStoreDoesNotRetryOtherErrors(t *testing.T) {
	wantErr := errors.New("fabricated migration failure")
	openCalls := 0
	open := func(context.Context, store.Config) (*store.Store, error) {
		openCalls++
		return nil, wantErr
	}

	_, err := openStoreRetry(t.Context(), store.Config{}, open,
		func(context.Context, time.Duration) error { t.Fatal("wait called"); return nil },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, wantErr) || openCalls != 1 {
		t.Errorf("openStoreRetry = %v after %d calls; want original error after 1", err, openCalls)
	}
}
