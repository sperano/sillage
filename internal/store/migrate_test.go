package store

import (
	"strings"
	"testing"
	"testing/fstest"
)

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
