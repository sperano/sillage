package ingest

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestRootSet(t *testing.T) {
	var s rootSet
	for _, r := range []model.Root{taskRoot(2), taskRoot(7), taskRoot(2), {}, taskRoot(1)} {
		s.add(r)
	}
	if got, want := refs(s.roots), refs([]model.Root{taskRoot(2), taskRoot(7), taskRoot(1)}); !slices.Equal(got, want) {
		t.Errorf("roots = %q, want %q", got, want)
	}
}
