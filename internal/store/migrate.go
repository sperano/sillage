package store

import (
	"cmp"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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

// checksum identifies the migration's SQL: a shipped migration is never
// edited, so a database that applied other SQL under its version is
// refused rather than left to drift from what this binary expects.
func (m migration) checksum() string {
	sum := sha256.Sum256([]byte(m.sql))
	return hex.EncodeToString(sum[:])
}

// appliedMigration is a schema_migrations row.
type appliedMigration struct {
	version  int
	name     string
	checksum string
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
// that has a version this binary does not know (it was migrated by a newer
// sillage, and this one would misread it) or that applied a version under
// another name or SQL.
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
		if n := len(applied); n > 0 && applied[n-1].version > len(migrations) {
			return fmt.Errorf("store: the database is at schema version %d, newer than this binary's %d",
				applied[n-1].version, len(migrations))
		}
		if err := checkApplied(applied, migrations); err != nil {
			return err
		}
		for _, m := range migrations[len(applied):] {
			if err := apply(ctx, tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}

// checkApplied refuses applied migrations that are not the embedded ones:
// a gap in their versions, another name or another checksum. applied is
// in version order, with no version past the last of migrations, so it is
// no longer than migrations.
func checkApplied(applied []appliedMigration, migrations []migration) error {
	for i, a := range applied {
		m := migrations[i]
		switch {
		case a.version != m.version:
			return fmt.Errorf("store: the database lacks schema version %d but has %d", m.version, a.version)
		case a.name != m.name || a.checksum != m.checksum():
			return fmt.Errorf("store: schema version %d was applied as %s with other SQL than this binary's %s",
				a.version, a.name, m.name)
		}
	}
	return nil
}

// appliedVersions returns the schema_migrations rows in version order,
// creating the table first if need be.
func appliedVersions(ctx context.Context, tx pgx.Tx) ([]appliedMigration, error) {
	_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		checksum   text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("store: migrate: version table: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	applied, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appliedMigration, error) {
		var a appliedMigration
		err := row.Scan(&a.version, &a.name, &a.checksum)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return applied, nil
}

// apply runs one migration and records it.
func apply(ctx context.Context, tx pgx.Tx, m migration) error {
	// Without arguments, Exec uses the simple protocol, which runs a file
	// of several statements.
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("store: migration %s: %w", m.name, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		m.version, m.name, m.checksum()); err != nil {
		return fmt.Errorf("store: migration %s: %w", m.name, err)
	}
	return nil
}
