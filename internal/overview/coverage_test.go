package overview

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// testInstance names every source instance in these tests.
const testInstance = "example"

func TestMissingHistory(t *testing.T) {
	readAt := func(offset int) *time.Time { at := minute(offset); return &at }
	cov := func(source model.Source, read *time.Time, gaps ...model.Gap) model.Coverage {
		return model.Coverage{Source: source, Instance: testInstance, LastSuccessfulRead: read, Gaps: gaps}
	}
	gap := func(start, end int) model.Gap { return model.Gap{Start: minute(start), End: minute(end)} }
	kelos := model.Coverage{Source: model.SourceKelos, Instance: testInstance}.Key()
	cases := []struct {
		name     string
		coverage []model.Coverage
		sources  []string
		want     bool
	}{
		{"read during the window", []model.Coverage{cov(model.SourceKelos, readAt(50))}, []string{kelos}, false},
		{"read since, after the window", []model.Coverage{cov(model.SourceKelos, readAt(90))}, []string{kelos}, false},
		{"read at the window's start", []model.Coverage{cov(model.SourceKelos, readAt(0))}, []string{kelos}, false},
		{"last read before the window", []model.Coverage{cov(model.SourceKelos, readAt(-10))}, []string{kelos}, true},
		{"never read", []model.Coverage{cov(model.SourceKelos, nil)}, []string{kelos}, true},
		{"gap in the window", []model.Coverage{cov(model.SourceKelos, readAt(50), gap(10, 20))}, []string{kelos}, true},
		{"gap before the window", []model.Coverage{cov(model.SourceKelos, readAt(50), gap(-20, -10))}, []string{kelos}, false},
		{"gap ending at the start", []model.Coverage{cov(model.SourceKelos, readAt(50), gap(-20, 0))}, []string{kelos}, false},
		{"gap starting at the end", []model.Coverage{cov(model.SourceKelos, readAt(50), gap(60, 70))}, []string{kelos}, false},
		{"row source without coverage", nil, []string{kelos}, true},
		// GitHub not read for want of a repository: the row has no pull
		// request because nothing looked for one.
		{"unread source the row has no records from", []model.Coverage{
			cov(model.SourceKelos, readAt(50)),
			cov(model.SourceGitHub, nil),
		}, []string{kelos}, true},
		{"gap of a source the row has no records from", []model.Coverage{
			cov(model.SourceKelos, readAt(50)),
			cov(model.SourceVikunja, readAt(50), gap(30, 40)),
		}, []string{kelos}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := missingHistory(afternoon, c.coverage, c.sources); got != c.want {
				t.Errorf("missing history = %v, want %v", got, c.want)
			}
		})
	}
}

// The root's own source counts toward the row's sources: a task row that
// holds no Vikunja record still says whether its task was marked done, so
// with no Vikunja coverage its history is missing.
func TestRootSourceWithoutCoverage(t *testing.T) {
	opened := prEvent(model.KindPROpened, 10, "agent-gh", nil)
	read := minute(50)
	github := model.Coverage{Source: model.SourceGitHub, Instance: opened.Related.PR.Instance, LastSuccessfulRead: &read}
	task := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 1}}
	for _, c := range []struct {
		name     string
		root     model.Root
		coverage []model.Coverage
		want     bool
	}{
		{"task, GitHub only", task, []model.Coverage{github}, true},
		{"task, Vikunja read", task, []model.Coverage{github,
			{Source: model.SourceVikunja, Instance: task.Task.Instance, LastSuccessfulRead: &read}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			row, ok := buildRow(afternoon, "", model.Story{Root: c.root, Events: []model.Event{opened}, Coverage: c.coverage})
			if !ok {
				t.Fatal("no row for a pull request opened in the window")
			}
			if row.MissingHistory != c.want {
				t.Errorf("missing history = %v, want %v", row.MissingHistory, c.want)
			}
		})
	}
}
