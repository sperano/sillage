package cost_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/sperano/sillage/internal/cost"
	"github.com/sperano/sillage/internal/cost/costtest"
)

var update = flag.Bool("update", false, "rewrite the cost fixtures' golden files")

// fixturesDir holds the cost fixtures: input.json and cost.golden.json.
const fixturesDir = "../../testdata/fixtures/cost"

// loadInput reads the fixture of name as a cost input.
func loadInput(t *testing.T, name string) cost.Input {
	t.Helper()
	in, err := costtest.Load(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// fixtureNames lists the cost fixtures.
func fixtureNames(t *testing.T) []string {
	t.Helper()
	names, err := costtest.Names(fixturesDir)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func defaultTable(t *testing.T) *cost.Table {
	t.Helper()
	tab, err := cost.DefaultTable()
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestCostGolden(t *testing.T) {
	tab := defaultTable(t)
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			got, err := json.MarshalIndent(cost.Compute(tab, loadInput(t, name)), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(fixturesDir, name, "cost.golden.json")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from Compute's output; rerun with -update and review the diff", path)
			}
		})
	}
}
