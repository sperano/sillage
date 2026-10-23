package pglock

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/reconcile"
)

const (
	// envTestDatabaseURL names the environment variable holding a
	// PostgreSQL database the tests may lock; unset, they are skipped.
	envTestDatabaseURL = "SILLAGE_TEST_DATABASE_URL"
	// testCheck is the check interval in tests.
	testCheck = 10 * time.Millisecond
	// testWait bounds every wait of a test.
	testWait = time.Minute
)

// testLocks returns n locks on one key of the test database, never the
// collector's, so that a shared database's collector is left alone.
func testLocks(t *testing.T, n int) ([]*Lock, string) {
	t.Helper()
	url := os.Getenv(envTestDatabaseURL)
	if url == "" {
		t.Skip(envTestDatabaseURL + " is unset: no PostgreSQL to lock")
	}
	key := rand.Int64()
	locks := make([]*Lock, n)
	for i := range locks {
		l, err := New(url)
		if err != nil {
			t.Fatal(err)
		}
		l.key, l.check = key, testCheck
		locks[i] = l
	}
	return locks, url
}

func noop(context.Context) error { return nil }

func TestNewHidesTheURL(t *testing.T) {
	const secret = "fabricated-password"
	_, err := New("postgres://sillage:" + secret + "@db.example.com:5432/sillage?sslmode=bogus")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("error = %v, want one that does not repeat the password", err)
	}
}

func TestOneHolderAtATime(t *testing.T) {
	locks, _ := testLocks(t, 2)
	a, b := locks[0], locks[1]
	err := a.WithLock(t.Context(), func(context.Context) error {
		if err := b.WithLock(t.Context(), noop); !errors.Is(err, reconcile.ErrLockHeld) {
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

func TestCancelReleases(t *testing.T) {
	locks, _ := testLocks(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	err := locks[0].WithLock(ctx, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WithLock() = %v, want the cancellation", err)
	}
	if err := locks[1].WithLock(t.Context(), noop); err != nil {
		t.Errorf("WithLock() after a canceled holder = %v, want the lock taken", err)
	}
}

func TestLost(t *testing.T) {
	for _, sign := range []int64{1, -1} {
		locks, url := testLocks(t, 1)
		l := locks[0]
		// Both halves of the key range: a negative key's high half is
		// held as an unsigned classid.
		l.key = sign * (l.key | 1<<32)
		err := l.WithLock(t.Context(), func(ctx context.Context) error {
			if err := terminateHolder(t, url, l.key); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(testWait):
				return errors.New("the lock's context was not canceled")
			}
		})
		if !errors.Is(err, reconcile.ErrLockLost) {
			t.Errorf("key %d: WithLock() = %v, want ErrLockLost", l.key, err)
		}
	}
}

// terminateHolder ends the session holding the advisory lock key: a
// bigint key is held as its high 32 bits in classid and its low ones in
// objid, with objsubid 1, each unsigned.
func terminateHolder(t *testing.T, url string, key int64) error {
	t.Helper()
	admin, err := pgx.Connect(t.Context(), url)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(context.WithoutCancel(t.Context())) }()
	_, err = admin.Exec(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND objsubid = 1
		AND classid = (($1::bigint >> 32) & x'ffffffff'::bigint)::oid
		AND objid = ($1::bigint & x'ffffffff'::bigint)::oid`, key)
	return err
}
