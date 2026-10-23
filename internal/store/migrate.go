package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationFiles are the schema migrations, applied in version order.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	migrationsDir = "migrations"
	// migrateLockID is the session advisory lock that keeps two sillage
	// processes from migrating the same database at once.
	migrateLockID int64 = 0x5111a6e0001
)

// migrationName is <version>_<name>.sql, the version a positive decimal.
var migrationName = regexp.MustCompile(`^0*([1-9][0-9]*)_[a-z0-9_]+\.sql$`)

// migration is one embedded schema change.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the migrations of fsys, in version order. Versions
// must be unique and contiguous from 1, so a missing file is caught here
// rather than skipped.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, migrationsDir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("store: migration %s: name is not <version>_<name>.sql", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("store: migration %s: %w", e.Name(), err)
		}
		sql, err := fs.ReadFile(fsys, path.Join(migrationsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: version, name: e.Name(), sql: string(sql)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("store: migration %s: version %d, want %d", m.name, m.version, i+1)
		}
	}
	return out, nil
}

// migrate applies the embedded migrations the database lacks, each in its
// own transaction with its row in schema_migrations. It refuses a database
// that has a version this binary does not know: it was migrated by a newer
// sillage, and this one would misread it.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLockID); err != nil {
		return fmt.Errorf("store: migrate: lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrateLockID)
	}()
	applied, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return err
	}
	if n := len(applied); n > 0 && applied[n-1] > len(migrations) {
		return fmt.Errorf("store: the database is at schema version %d, newer than this binary's %d",
			applied[n-1], len(migrations))
	}
	for _, m := range migrations {
		if slices.Contains(applied, m.version) {
			continue
		}
		if err := apply(ctx, conn.Conn(), m); err != nil {
			return err
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) ([]int, error) {
	_, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("store: migrate: version table: %w", err)
	}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return versions, nil
}

// apply runs one migration and records it, or neither.
func apply(ctx context.Context, conn *pgx.Conn, m migration) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Without arguments, Exec uses the simple protocol, which runs a
		// file of several statements.
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: migration %s: %w", m.name, err)
	}
	return nil
}
