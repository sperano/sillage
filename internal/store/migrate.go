package store

import (
	"cmp"
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
	// migrateLockID is the transaction advisory lock that keeps two
	// sillage processes from migrating the same database at once.
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
	slices.SortFunc(out, func(a, b migration) int { return cmp.Compare(a.version, b.version) })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("store: migration %s: version %d, want %d", m.name, m.version, i+1)
		}
	}
	return out, nil
}

// migrate applies the embedded migrations the database lacks, with their
// rows in schema_migrations, in one transaction holding an advisory lock:
// all of them or none, and one process at a time. It refuses a database
// that has a version this binary does not know: it was migrated by a newer
// sillage, and this one would misread it.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLockID); err != nil {
			return fmt.Errorf("store: migrate: lock: %w", err)
		}
		applied, err := appliedVersions(ctx, tx)
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
			if err := apply(ctx, tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}

func appliedVersions(ctx context.Context, tx pgx.Tx) ([]int, error) {
	_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("store: migrate: version table: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return versions, nil
}

// apply runs one migration and records it.
func apply(ctx context.Context, tx pgx.Tx, m migration) error {
	// Without arguments, Exec uses the simple protocol, which runs a file
	// of several statements.
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("store: migration %s: %w", m.name, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name); err != nil {
		return fmt.Errorf("store: migration %s: %w", m.name, err)
	}
	return nil
}
