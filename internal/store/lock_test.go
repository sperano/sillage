package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/reconcile"
)

// envTestDatabaseURL names the environment variable holding a PostgreSQL
// database the tests may lock; unset, they are skipped.
const envTestDatabaseURL = "SILLAGE_TEST_DATABASE_URL"

// testCheck is the lock check interval in tests.
const testCheck = 10 * time.Millisecond

func testLock(t *testing.T) (*AdvisoryLock, string) {
	t.Helper()
	url := os.Getenv(envTestDatabaseURL)
	if url == "" {
		t.Skip(envTestDatabaseURL + " is unset: no PostgreSQL to lock")
	}
	l, err := NewAdvisoryLock(url)
	if err != nil {
		t.Fatal(err)
	}
	l.check = testCheck
	return l, url
}

func TestNewAdvisoryLockHidesTheURL(t *testing.T) {
	const secret = "fabricated-password"
	_, err := NewAdvisoryLock("postgres://sillage:" + secret + "@db.example.com:5432/sillage?sslmode=bogus")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("error = %v, want one that does not repeat the password", err)
	}
}

func TestAdvisoryLockOneHolderAtATime(t *testing.T) {
	a, url := testLock(t)
	b, err := NewAdvisoryLock(url)
	if err != nil {
		t.Fatal(err)
	}
	err = a.WithLock(t.Context(), func(context.Context) error {
		if err := b.WithLock(t.Context(), func(context.Context) error { return nil }); !errors.Is(err, reconcile.ErrLockHeld) {
			t.Errorf("second WithLock() = %v, want ErrLockHeld", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := b.WithLock(t.Context(), func(context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Errorf("WithLock() after release = %v, ran %v: want the lock taken", err, ran)
	}
}

func TestAdvisoryLockLost(t *testing.T) {
	l, url := testLock(t)
	err := l.WithLock(t.Context(), func(ctx context.Context) error {
		admin, err := pgx.Connect(t.Context(), url)
		if err != nil {
			return err
		}
		defer admin.Close(context.WithoutCancel(ctx))
		if _, err := admin.Exec(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_locks
			WHERE locktype = 'advisory' AND objid = $1::bigint & x'ffffffff'::bigint`, l.key); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Minute):
			return errors.New("the lock's context was not canceled")
		}
	})
	if !errors.Is(err, reconcile.ErrLockLost) {
		t.Errorf("WithLock() = %v, want ErrLockLost", err)
	}
}
