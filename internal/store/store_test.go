package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sperano/sillage/internal/model"
)

// testDatabaseEnv names the PostgreSQL the database tests run against.
// They skip without it: agent pods have no database.
const testDatabaseEnv = "SILLAGE_TEST_DATABASE_URL"

const (
	testVikunja   = "vikunja.example.com"
	testWiki      = "wiki.example.com"
	testGitHub    = "github.example.com"
	testCluster   = "cluster-a"
	testNamespace = "agents"
	testRepo      = "acme/widgets"
	// searchPathParam is the connection parameter that puts each test in
	// its own schema.
	searchPathParam = "search_path"
)

// testSchemas numbers the schemas this process creates.
var testSchemas atomic.Int64

// testBase is minute 0 of the tests' times.
var testBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func at(minute int) time.Time { return testBase.Add(time.Duration(minute) * time.Minute) }

func ptr[T any](v T) *T { return &v }

// testDatabaseURL returns the test database, or skips the test.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Skipf("%s is not set: no PostgreSQL to test against", testDatabaseEnv)
	}
	return url
}

// testPoolConfig returns a connection to a new, empty schema, dropped when
// the test ends.
func testPoolConfig(t *testing.T) *pgxpool.Config {
	t.Helper()
	url := testDatabaseURL(t)
	admin, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatalf("connecting to %s: %v", testDatabaseEnv, err)
	}
	schema := fmt.Sprintf("sillage_test_%d_%d", os.Getpid(), testSchemas.Add(1))
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("dropping %s: %v", schema, err)
		}
		_ = admin.Close(ctx)
	})
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams[searchPathParam] = schema
	return pc
}

// newTestStore opens a store on a new schema.
func newTestStore(t *testing.T, retention Retention) *Store {
	t.Helper()
	s, err := open(t.Context(), testPoolConfig(t), retention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func mustSave(t *testing.T, s *Store, root model.Root, recs model.Records) {
	t.Helper()
	if err := s.Save(t.Context(), Batch{Root: root, Records: recs}); err != nil {
		t.Fatal(err)
	}
}

func mustStory(t *testing.T, s *Store, root model.Root) model.Story {
	t.Helper()
	story, err := s.Story(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	return story
}

func taskKey(id int64) model.TaskKey { return model.TaskKey{Instance: testVikunja, ID: id} }

func taskRoot(id int64) model.Root { return model.Root{Task: ptr(taskKey(id))} }

func pageKey(id int64) model.PageKey { return model.PageKey{Instance: testWiki, ID: id, Locale: "en"} }

func pageRoot(id int64) model.Root { return model.Root{Page: ptr(pageKey(id))} }

func vikunjaEvidence(ref string) model.Evidence {
	return model.Evidence{Source: model.SourceVikunja, Instance: testVikunja, Ref: ref}
}

func kelosEvidence(ref, note string) model.Evidence {
	return model.Evidence{Source: model.SourceKelos, Instance: testCluster, Ref: ref, Note: note}
}

// comment is a comment on task, posted at minute and read at minute+1.
func comment(task int64, id string, minute int) model.Event {
	return model.Event{
		Source: model.SourceVikunja, Instance: testVikunja, ID: id, IDKind: model.IDSource,
		Kind: model.KindComment, SourceTime: ptr(at(minute)), ObservedAt: at(minute + 1),
		Actor:    model.Account{Login: "owner", Kind: model.ActorPerson},
		Related:  model.Related{Task: ptr(taskKey(task))},
		Evidence: vikunjaEvidence("GET /api/v1/tasks/comments"),
	}
}

// observation is a state of task seen by a read at minute.
func observation(task int64, kind model.EventKind, detail, value string, minute int) model.Event {
	object := fmt.Sprintf("tasks/%d", task)
	return model.Event{
		Source: model.SourceVikunja, Instance: testVikunja,
		ID: model.Fingerprint(at(minute), object, string(kind), value), IDKind: model.IDFingerprint,
		Kind: kind, ObservedAt: at(minute), Actor: model.Account{Kind: model.ActorUnknown},
		Related:  model.Related{Task: ptr(taskKey(task))},
		Details:  map[string]string{detail: value},
		Evidence: vikunjaEvidence("GET /api/v1/tasks"),
	}
}

func uidKey(uid string) model.RunKey {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}
}

func podRunKey(pod string) model.RunKey {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: pod}
}

// finishedRun is a run read from its TaskRecord alone: it ran from minute
// start to end.
func finishedRun(uid string, start, end int, outcome model.Outcome) model.Run {
	return model.Run{
		Key: uidKey(uid), TaskName: "vikunja-widgets-agent-7", Agent: "agent", Role: model.RoleImplementation,
		Interval: model.Interval{Start: model.KnownAt(at(start)), End: model.KnownAt(at(end))},
		Outcome:  outcome,
		Evidence: []model.Evidence{kelosEvidence("taskrecords/agents/"+uid, "Task gone")},
	}
}

// placeholderRun is a run known only from its announcement.
func placeholderRun(pod string) model.Run {
	return model.Run{
		Key: podRunKey(pod), Pod: pod, TaskName: "vikunja-widgets-agent-7", Role: model.RoleImplementation,
		Interval: model.Interval{Start: model.UnknownBound(), End: model.UnknownBound()},
		Outcome:  model.OutcomeUnknown,
		Evidence: []model.Evidence{vikunjaEvidence("GET /api/v1/tasks/7/comments")},
	}
}

func link(from, to model.Ref) model.Correlation {
	return model.Correlation{
		From: from, To: to, Confidence: model.ConfidenceIdentifier,
		Evidence: []model.Evidence{vikunjaEvidence("GET /api/v1/tasks/7/comments")},
	}
}

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	pc := testPoolConfig(t)
	for range 2 {
		s, err := open(t.Context(), pc, Retention{})
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
			t.Fatal(err)
		}
		migrations, err := loadMigrations(migrationFiles)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(migrations) {
			t.Errorf("schema_migrations has %d rows, want %d", n, len(migrations))
		}
		s.Close()
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	pc := testPoolConfig(t)
	s, err := open(t.Context(), pc, Retention{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(t.Context(), "INSERT INTO schema_migrations (version, name) VALUES (9999, 'future.sql')")
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(t.Context(), pc, Retention{}); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("open = %v, want a newer-schema error", err)
	}
}

func TestOpenErrorHidesPassword(t *testing.T) {
	const password = "s3cret-password"
	cfg := Config{DatabaseURL: DatabaseURL("postgres://sillage:" + password + "@127.0.0.1:1/sillage?connect_timeout=1")}
	_, err := Open(t.Context(), cfg)
	if err == nil {
		t.Fatal("Open succeeded against a closed port")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("error %q contains the password", err)
	}
}

func TestStoryNotFound(t *testing.T) {
	s := newTestStore(t, Retention{})
	if _, err := s.Story(t.Context(), taskRoot(1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("Story = %v, want ErrNotFound", err)
	}
	if _, err := s.Coverage(t.Context(), taskRoot(1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("Coverage = %v, want ErrNotFound", err)
	}
}

func TestSaveRejectsInvalidBatch(t *testing.T) {
	s := newTestStore(t, Retention{})
	bad := comment(7, "comments/1", 0)
	bad.ObservedAt = time.Time{}
	err := s.Save(t.Context(), Batch{Root: taskRoot(7), Records: model.Records{Events: []model.Event{bad}}})
	if err == nil {
		t.Fatal("Save accepted an event without an observed time")
	}
	if _, err := s.Story(t.Context(), taskRoot(7)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a rejected batch stored its root: %v", err)
	}
}
