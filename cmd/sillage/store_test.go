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

func TestOpenStoreRetriesConnectionFailures(t *testing.T) {
	wantStore := new(store.Store)
	connectErr := fmt.Errorf("%w: fabricated refusal", store.ErrConnecting)
	openCalls := 0
	open := func(context.Context, store.Config) (*store.Store, error) {
		openCalls++
		if openCalls < 3 {
			return nil, connectErr
		}
		return wantStore, nil
	}
	waits := 0
	wait := func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != storeConnectRetryInterval {
			t.Errorf("retry delay = %s, want %s", delay, storeConnectRetryInterval)
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
	if openCalls != storeConnectAttempts || waits != storeConnectAttempts-1 {
		t.Errorf("open calls = %d, waits = %d; want %d, %d",
			openCalls, waits, storeConnectAttempts, storeConnectAttempts-1)
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
