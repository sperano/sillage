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
	names := model.StoryNames(t)
	for _, name := range names {
		if _, ok := storytest.Invariants[name]; !ok {
			t.Errorf("fixture %s has no entry in storytest.Invariants", name)
		}
	}
	for name := range storytest.Invariants {
		if !slices.Contains(names, name) {
			t.Errorf("storytest.Invariants names %s, which is not a fixture", name)
		}
	}
}

func TestFixtureInvariants(t *testing.T) {
	for name, check := range storytest.Invariants {
		t.Run(name, func(t *testing.T) {
			check(t, model.BuildFixture(t, name))
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
