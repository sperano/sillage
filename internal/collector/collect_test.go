package collector

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// coverageOf returns the story's coverage of source, failing without one.
func coverageOf(t *testing.T, s model.Story, source model.Source) model.Coverage {
	t.Helper()
	for _, c := range s.Coverage {
		if c.Source == source {
			return c
		}
	}
	t.Fatalf("no %s coverage in %v", source, s.Coverage)
	return model.Coverage{}
}

func eventsFrom(s model.Story, source model.Source) []model.Event {
	var out []model.Event
	for _, e := range s.Events {
		if e.Source == source {
			out = append(out, e)
		}
	}
	return out
}

func hasNote(c model.Coverage, part string) bool {
	return slices.ContainsFunc(c.Notes, func(n string) bool { return strings.Contains(n, part) })
}

func TestFailedReadYieldsNoRecordsAndNoRead(t *testing.T) {
	tests := []struct {
		story  string
		source model.Source
		fail   func(*Sources)
		absent func(model.Story) bool
	}{
		{
			story: "implementation-merged", source: model.SourceVikunja,
			fail:   func(s *Sources) { s.Vikunja = failingVikunja{} },
			absent: func(s model.Story) bool { return len(eventsFrom(s, model.SourceVikunja)) == 0 },
		},
		{
			story: "implementation-merged", source: model.SourceKelos,
			fail:   func(s *Sources) { s.Kelos = failingKelos{} },
			absent: func(s model.Story) bool { return len(s.Runs) == 1 && s.Runs[0].Key.Pod != "" },
		},
		{
			story: "implementation-merged", source: model.SourceGitHub,
			fail: func(s *Sources) { s.GitHub = failingGitHub{} },
			absent: func(s model.Story) bool {
				return len(s.PullRequests) == 0 && len(eventsFrom(s, model.SourceGitHub)) == 0
			},
		},
		{
			story: "discuss-no-pr", source: model.SourceWiki,
			fail:   func(s *Sources) { s.Wiki = failingWiki{} },
			absent: func(s model.Story) bool { return len(eventsFrom(s, model.SourceWiki)) == 0 },
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			c := newStoryCollector(t, storyCases[tt.story].dirs)
			tt.fail(&c.src)
			res, err := c.Collect(t.Context(), storyCases[tt.story].root)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Failures) != 1 || res.Failures[0].Source != tt.source || !errors.Is(res.Failures[0], errDown) {
				t.Errorf("failures = %v, want one of %s", res.Failures, tt.source)
			}
			if cov := coverageOf(t, res.Story, tt.source); cov.LastSuccessfulRead != nil {
				t.Errorf("%s coverage read at %v, want no read", tt.source, cov.LastSuccessfulRead)
			}
			if !tt.absent(res.Story) {
				t.Errorf("the story still holds records %s supplies", tt.source)
			}
		})
	}
}

func TestKelosFailureKeepsAnnouncedRunsUnknown(t *testing.T) {
	c := newStoryCollector(t, storyCases["implementation-merged"].dirs)
	c.src.Kelos = failingKelos{}
	res, err := c.Collect(t.Context(), taskRoot(102))
	if err != nil {
		t.Fatal(err)
	}
	run := res.Story.Runs[0]
	if run.Outcome != model.OutcomeUnknown || !strings.Contains(run.Evidence[0].Note, "could not be read") {
		t.Errorf("run = %+v, want an unknown outcome whose evidence says Kelos could not be read", run)
	}
	if gaps := coverageOf(t, res.Story, model.SourceKelos).Gaps; len(gaps) != 0 {
		t.Errorf("kelos gaps = %v, want none on a coverage without a read", gaps)
	}
}

func TestCollectRejectsAForeignRoot(t *testing.T) {
	c := newStoryCollector(t, storyDirs{})
	tests := []struct {
		name string
		root model.Root
	}{
		{"invalid", model.Root{}},
		{"other vikunja", model.Root{Task: &model.TaskKey{Instance: "vikunja.example.org", ID: 1}}},
		{"other wiki", model.Root{Page: &model.PageKey{Instance: "wiki.example.org", ID: 1, Locale: "en"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Collect(t.Context(), tt.root); err == nil {
				t.Error("Collect() error = nil, want the root refused")
			}
		})
	}
}

func TestCollectRejectsAPageOfAnotherLocale(t *testing.T) {
	c := newStoryCollector(t, storyDirs{wiki: storyCases["wiki-rooted-request"].dirs.wiki})
	root := pageRoot(57)
	root.Page.Locale = "fr"
	_, err := c.Collect(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), "/pages/57/en, not ") || !strings.Contains(err.Error(), "/pages/57/fr") {
		t.Errorf("Collect() error = %v, want the page's own locale and the root's reported", err)
	}
}

func TestGitHubNotReadWithoutRepository(t *testing.T) {
	cov := coverageOf(t, collectStory(t, "announced-run-only"), model.SourceGitHub)
	if cov.LastSuccessfulRead != nil || !hasNote(cov, "no repository known") {
		t.Errorf("github coverage = %+v, want no read and a note saying why", cov)
	}
}

func TestGitHubReadFromARepositoryAskedFor(t *testing.T) {
	dirs := storyCases["announced-run-only"].dirs
	dirs.github = storyCases["discuss-no-pr"].dirs.github
	res, err := newStoryCollector(t, dirs).Collect(t.Context(), taskRoot(105), testRepo)
	if err != nil {
		t.Fatal(err)
	}
	if cov := coverageOf(t, res.Story, model.SourceGitHub); cov.LastSuccessfulRead == nil || len(cov.Notes) != 0 {
		t.Errorf("github coverage = %+v, want a read and no note", cov)
	}
}

func TestUnmappedWorkspaceNoted(t *testing.T) {
	c := newStoryCollector(t, storyCases["implementation-merged"].dirs)
	c.cfg.Workspaces = nil
	res, err := c.Collect(t.Context(), taskRoot(102))
	if err != nil {
		t.Fatal(err)
	}
	cov := coverageOf(t, res.Story, model.SourceGitHub)
	if cov.LastSuccessfulRead != nil || !hasNote(cov, EnvWorkspaces+" for "+testWorkspace) {
		t.Errorf("github coverage = %+v, want no read and the unmapped workspace named", cov)
	}
}

func TestKelosTTLGapOnlyBeforeTheTTL(t *testing.T) {
	c := newStoryCollector(t, storyCases["implementation-merged"].dirs)
	c.now = func() time.Time { return time.Date(2026, 3, 3, 20, 0, 0, 0, time.UTC) }
	res, err := c.Collect(t.Context(), taskRoot(102))
	if err != nil {
		t.Fatal(err)
	}
	if gaps := coverageOf(t, res.Story, model.SourceKelos).Gaps; len(gaps) != 0 {
		t.Errorf("kelos gaps = %v, want none: the story is younger than the Task TTL", gaps)
	}
	gaps := coverageOf(t, collectStory(t, "implementation-merged"), model.SourceKelos).Gaps
	if len(gaps) != 1 || !gaps[0].End.Equal(testNow.Add(-DefaultKelosTaskTTL)) {
		t.Errorf("kelos gaps = %v, want one ending a Task TTL before the read", gaps)
	}
}

func TestLinkedPageJoinedToTask(t *testing.T) {
	s := collectStory(t, "discuss-no-pr")
	page := model.PageKey{Instance: testWikiHost, ID: 41, Locale: "en"}
	i := slices.IndexFunc(s.Correlations, func(c model.Correlation) bool {
		return c.From == page.Ref() && c.To == taskRoot(101).Task.Ref()
	})
	if i < 0 || s.Correlations[i].Confidence != model.ConfidenceIdentifier {
		t.Errorf("correlations = %v, want the linked page joined to the task by an identifier", s.Correlations)
	}
}

func TestFailedTaskReadLeavesLinkedPagesUnknown(t *testing.T) {
	src := emptySources()
	src.Vikunja = failingVikunja{}
	cov := coverageOf(t, collectWith(t, testConfig(), src, taskRoot(104)).Story, model.SourceWiki)
	if cov.LastSuccessfulRead != nil || !hasNote(cov, "could not be read") || hasNote(cov, "no comment") {
		t.Errorf("wiki coverage = %+v, want the linked pages unknown, not absent", cov)
	}
}
