package model_test

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/model/storytest"
)

// TestEveryFixtureHasInvariants makes adding a fixture without asserting
// what it is for fail.
func TestEveryFixtureHasInvariants(t *testing.T) {
	names, checked := model.StoryNames(t), storytest.Names()
	for _, name := range names {
		if !slices.Contains(checked, name) {
			t.Errorf("fixture %s has no invariant in storytest", name)
		}
	}
	for _, name := range checked {
		if !slices.Contains(names, name) {
			t.Errorf("storytest has an invariant for %s, which is not a fixture", name)
		}
	}
}

func TestFixtureInvariants(t *testing.T) {
	for _, name := range storytest.Names() {
		t.Run(name, func(t *testing.T) {
			storytest.Check(t, name, model.BuildFixture(t, name))
		})
	}
}

// TestFixtureRefsResolve checks that every correlation in a fixture points
// to an event, run or pull request of the story, or to its root or a task
// or page its events name, so a typo in a fixture cannot go unnoticed.
func TestFixtureRefsResolve(t *testing.T) {
	for _, name := range model.StoryNames(t) {
		t.Run(name, func(t *testing.T) {
			storytest.RefsResolve(t, model.BuildFixture(t, name))
		})
	}
}
