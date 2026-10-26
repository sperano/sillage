package store

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retireWikiMigrationVersion = 7

func TestEmbeddedMigrationsLoad(t *testing.T) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) == 0 || migrations[0].version != 1 || migrations[0].sql == "" {
		t.Errorf("migrations = %+v, want version 1 first", migrations)
	}
}

func TestLoadMigrations(t *testing.T) {
	file := func(names ...string) fstest.MapFS {
		fsys := fstest.MapFS{}
		for _, n := range names {
			fsys[migrationsDir+"/"+n] = &fstest.MapFile{Data: []byte("SELECT 1;")}
		}
		return fsys
	}
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		want    []int
		wantErr string
	}{
		{"in version order", file("0002_b.sql", "0001_a.sql", "0010_c.sql", "0003_d.sql", "0004_e.sql",
			"0005_f.sql", "0006_g.sql", "0007_h.sql", "0008_i.sql", "0009_j.sql"),
			[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, ""},
		{"a missing version", file("0001_a.sql", "0003_c.sql"), nil, "want 2"},
		{"a duplicate version", file("0001_a.sql", "01_b.sql"), nil, "want 2"},
		{"a bad name", file("0001_a.sql", "0002-b.sql"), nil, "name is not"},
		{"version zero", file("0000_a.sql"), nil, "name is not"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadMigrations(tt.fsys)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d migrations, want %d", len(got), len(tt.want))
			}
			for i, m := range got {
				if m.version != tt.want[i] {
					t.Errorf("migration %d version = %d, want %d", i, m.version, tt.want[i])
				}
			}
		})
	}
}

func TestCheckApplied(t *testing.T) {
	migrations := []migration{{1, "0001_a.sql", "SELECT 1;"}, {2, "0002_b.sql", "SELECT 2;"}}
	row := func(m migration) appliedMigration { return appliedMigration{m.version, m.name, m.checksum()} }
	edited := row(migrations[1])
	edited.checksum = migration{sql: "SELECT 3;"}.checksum()
	renamed := row(migrations[0])
	renamed.name = "0001_other.sql"
	tests := []struct {
		name    string
		applied []appliedMigration
		wantErr string
	}{
		{"none applied", nil, ""},
		{"some applied", []appliedMigration{row(migrations[0])}, ""},
		{"all applied", []appliedMigration{row(migrations[0]), row(migrations[1])}, ""},
		{"edited", []appliedMigration{row(migrations[0]), edited}, "other SQL"},
		{"renamed", []appliedMigration{renamed}, "other SQL"},
		{"a missing version", []appliedMigration{row(migrations[1])}, "lacks schema version 1"},
		{"more than the binary's", []appliedMigration{{0, "0000_zero.sql", ""}, row(migrations[0]),
			row(migrations[1])}, "lacks schema version 1"},
		{"past the binary's", []appliedMigration{row(migrations[0]), row(migrations[1]),
			{3, "0003_c.sql", ""}}, "unknown to this binary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkApplied(tt.applied, migrations)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("checkApplied = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("checkApplied = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRetireWikiMigrationDropsLegacyData(t *testing.T) {
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, testPoolConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < retireWikiMigrationVersion {
		t.Fatalf("have %d migrations, want at least %d", len(migrations), retireWikiMigrationVersion)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := appliedMigrations(ctx, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:retireWikiMigrationVersion-1] {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return apply(ctx, tx, migration) }); err != nil {
			t.Fatal(err)
		}
	}
	seedLegacyWikiData(t, pool)
	if err := migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	wantCount(t, pool, "SELECT count(*) FROM roots", 1)
	wantCount(t, pool, "SELECT count(*) FROM events", 1)
	wantCount(t, pool, "SELECT count(*) FROM coverage WHERE source = 'wiki'", 0)
	wantCount(t, pool, "SELECT count(*) FROM checkpoints WHERE source = 'wiki'", 0)
	wantCount(t, pool, "SELECT count(*) FROM correlations", 1)
	wantCount(t, pool, "SELECT count(*) FROM markers", 1)
	wantCount(t, pool, "SELECT count(*) FROM marker_limits", 1)
	wantCount(t, pool, "SELECT count(*) FROM run_origins", 1)
	wantCount(t, pool, "SELECT count(*) FROM runs", 1)
	wantCount(t, pool, "SELECT count(*) FROM pull_requests", 0)
	wantCount(t, pool, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'events' AND column_name IN ('supersedes', 'live_page')`, 0)
	wantCount(t, pool, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'roots'
		AND column_name IN ('kind', 'instance', 'source_id', 'locale')`, 0)
	var role string
	if err := pool.QueryRow(ctx, "SELECT body->>'role' FROM runs").Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "unknown" {
		t.Errorf("surviving retired run role = %q, want unknown", role)
	}
}

func seedLegacyWikiData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO roots (ref, kind, instance, source_id, locale, body) VALUES
			('task:vikunja.example.com/tasks/1', 'task', 'vikunja.example.com', 1, NULL,
			 '{"task":{"instance":"vikunja.example.com","id":1}}'),
			('page:wiki.example.com/pages/2/en', 'page', 'wiki.example.com', 2, 'en',
			 '{"page":{"instance":"wiki.example.com","id":2,"locale":"en"}}');
		INSERT INTO events (source, instance, event_id, kind, at, observed_at, last_observed_at, body) VALUES
			('vikunja', 'vikunja.example.com', 'comments/1', 'comment.created', now(), now(), now(), '{}'),
			('wiki', 'wiki.example.com', 'pages/2', 'page.edited', now(), now(), now(), '{}');
		INSERT INTO root_events (root_id, event_id)
		SELECT r.id, e.id FROM roots r CROSS JOIN events e
		WHERE r.ref = 'task:vikunja.example.com/tasks/1';
		INSERT INTO root_events (root_id, event_id)
		SELECT r.id, e.id FROM roots r JOIN events e ON e.source = 'wiki'
		WHERE r.ref = 'page:wiki.example.com/pages/2/en';
		INSERT INTO runs (cluster, namespace, uid, ref, body) VALUES
			('cluster-a', 'agents', 'uid-shared', 'run:cluster-a/agents/uid/uid-shared', '{"role":"wiki-follow-up"}'),
			('cluster-a', 'agents', 'uid-page', 'run:cluster-a/agents/uid/uid-page', '{"role":"wiki-follow-up"}');
		INSERT INTO root_runs (root_id, run_id)
		SELECT r.id, u.id FROM roots r CROSS JOIN runs u
		WHERE (r.kind = 'task' AND u.uid = 'uid-shared') OR (r.kind = 'page' AND u.uid = 'uid-page');
		INSERT INTO pull_requests (instance, repository, number, ref, branch)
		VALUES ('github.example.com', 'example/widgets', 2, 'pr:github.example.com/example/widgets/pull/2', 'legacy');
		INSERT INTO root_pull_requests (root_id, pull_request_id)
		SELECT r.id, p.id FROM roots r CROSS JOIN pull_requests p WHERE r.kind = 'page';
		INSERT INTO correlations (pair_key, from_ref, to_ref, body) VALUES
			('legacy-page', 'page:wiki.example.com/pages/2/en', 'run:cluster-a/agents/uid/uid-page', '{}'),
			('kept-task', 'task:vikunja.example.com/tasks/1', 'run:cluster-a/agents/uid/uid-shared', '{}');
		INSERT INTO root_correlations (root_id, correlation_id)
		SELECT r.id, c.id FROM roots r CROSS JOIN correlations c
		WHERE (r.kind = 'page' AND c.pair_key = 'legacy-page') OR (r.kind = 'task' AND c.pair_key = 'kept-task');
		INSERT INTO coverage (root_id, source, instance, body)
		SELECT id, 'wiki', 'wiki.example.com', '{}' FROM roots WHERE kind = 'task';
		INSERT INTO coverage (root_id, source, instance, body)
		SELECT id, 'vikunja', 'vikunja.example.com', '{}' FROM roots WHERE kind = 'task';
		INSERT INTO checkpoints (source, instance, scope, value, root_id, updated_at)
		SELECT 'wiki', 'wiki.example.com', 'legacy', '1', id, now() FROM roots WHERE kind = 'task';
		INSERT INTO markers (cluster, namespace, pod, pod_uid, span, edge, at, received_at, body) VALUES
			('cluster-a', 'agents', 'pod-wiki', 'pod-uid-wiki', '00000000000000000000000000000001', 'start', now(), now(),
			 '{"report":{"kind":"wiki-login"}}'),
			('cluster-a', 'agents', 'pod-agent', 'pod-uid-agent', '00000000000000000000000000000002', 'start', now(), now(),
			 '{"report":{"kind":"agent"}}');
		INSERT INTO marker_limits (pod_uid, cluster, namespace, pod, first_refused_at, last_refused_at, refused) VALUES
			('pod-uid-wiki', 'cluster-a', 'agents', 'pod-wiki', now(), now(), 1),
			('pod-uid-agent', 'cluster-a', 'agents', 'pod-agent', now(), now(), 1);
		INSERT INTO run_origins (cluster, namespace, uid, origin) VALUES
			('cluster-a', 'agents', 'uid-page', '{"annotations":{"source":"wiki"}}'),
			('cluster-a', 'agents', 'uid-shared', '{"annotations":{"source":"vikunja"}}');
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func wantCount(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(t.Context(), query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("%s: count = %d, want %d", query, got, want)
	}
}
