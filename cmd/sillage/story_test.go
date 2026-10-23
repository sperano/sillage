package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

var testCollectorConfig = collector.Config{VikunjaInstance: "vikunja.example.com", WikiInstance: "wiki.example.com"}

// fakeCollector records what it is asked and answers res.
type fakeCollector struct {
	res   collector.Result
	err   error
	root  model.Root
	repos []github.Repository
}

func (f *fakeCollector) Collect(_ context.Context, root model.Root, repos ...github.Repository) (collector.Result, error) {
	f.root, f.repos = root, repos
	return f.res, f.err
}

func (f *fakeCollector) open(func(string) string) (storyCollector, collector.Config, error) {
	return f, testCollectorConfig, nil
}

func TestParseStoryArgs(t *testing.T) {
	widgets := github.Repository{Owner: "example-org", Name: "widgets"}
	tests := []struct {
		args []string
		want storyArgs
	}{
		{[]string{"-task", "102"}, storyArgs{task: 102, locale: defaultLocale}},
		{[]string{"--page", "57", "--locale", "fr"}, storyArgs{page: 57, locale: "fr"}},
		{[]string{"-task", "102", "-repo", "example-org/widgets"}, storyArgs{
			task: 102, locale: defaultLocale, repos: []github.Repository{widgets},
		}},
	}
	for _, tt := range tests {
		got, err := parseStoryArgs(tt.args, io.Discard)
		if err != nil || got.task != tt.want.task || got.page != tt.want.page || got.locale != tt.want.locale ||
			!slices.Equal(got.repos, tt.want.repos) {
			t.Errorf("parseStoryArgs(%q) = %+v, %v; want %+v", tt.args, got, err, tt.want)
		}
	}
}

func TestParseStoryArgsRejects(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-task", "1", "-page", "2"},
		{"-task", "-1"},
		{"-task", "1", "extra"},
		{"-task", "one"},
		{"-task", "1", "-repo", "widgets"},
	} {
		var out bytes.Buffer
		if _, err := parseStoryArgs(args, &out); !errors.Is(err, errUsage) || !strings.Contains(out.String(), "-"+taskFlag) {
			t.Errorf("parseStoryArgs(%q) error = %v, output %q; want a usage error with the usage", args, err, out.String())
		}
	}
	if _, err := parseStoryArgs([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("parseStoryArgs(-h) error = %v, want flag.ErrHelp", err)
	}
}

func testStory(t *testing.T, root model.Root) model.Story {
	t.Helper()
	s, err := model.BuildStory(root, model.Records{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunStoryPrintsTheStory(t *testing.T) {
	root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 102}}
	fake := &fakeCollector{res: collector.Result{Story: testStory(t, root)}}
	var stdout, stderr bytes.Buffer
	if err := runStory(t.Context(), []string{"-task", "102"}, nil, &stdout, &stderr, fake.open); err != nil {
		t.Fatalf("runStory() error = %v, stderr %q", err, stderr.String())
	}
	if fake.root.Task == nil || *fake.root.Task != *root.Task {
		t.Errorf("collected %+v, want task 102 of the configured Vikunja", fake.root)
	}
	var got model.Story
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got.Root.Task == nil || got.Root.Task.ID != 102 {
		t.Errorf("printed %q (%v), want the story's JSON", stdout.String(), err)
	}
}

func TestRunStoryPageRoot(t *testing.T) {
	root := model.Root{Page: &model.PageKey{Instance: "wiki.example.com", ID: 57, Locale: defaultLocale}}
	fake := &fakeCollector{res: collector.Result{Story: testStory(t, root)}}
	if err := runStory(t.Context(), []string{"-page", "57"}, nil, io.Discard, io.Discard, fake.open); err != nil {
		t.Fatal(err)
	}
	if fake.root.Page == nil || *fake.root.Page != *root.Page {
		t.Errorf("collected %+v, want page 57 of the configured wiki", fake.root)
	}
}

func TestRunStoryReportsFailedReads(t *testing.T) {
	root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 102}}
	fake := &fakeCollector{res: collector.Result{
		Story:    testStory(t, root),
		Failures: []collector.Failure{{Source: model.SourceGitHub, Instance: "github.example.com", Err: errors.New("down")}},
	}}
	var stdout, stderr bytes.Buffer
	err := runStory(t.Context(), []string{"-task", "102"}, nil, &stdout, &stderr, fake.open)
	if !errors.Is(err, errPartial) || stdout.Len() == 0 || !strings.Contains(stderr.String(), "github github.example.com: down") {
		t.Errorf("runStory() = %v, stdout %d bytes, stderr %q; want the story printed and the failure reported",
			err, stdout.Len(), stderr.String())
	}
}

func TestRunStoryReturnsErrors(t *testing.T) {
	failing := &fakeCollector{err: errors.New("invalid records")}
	if err := runStory(t.Context(), []string{"-task", "1"}, nil, io.Discard, io.Discard, failing.open); err == nil {
		t.Error("runStory() error = nil, want the collector's error")
	}
	cantOpen := func(func(string) string) (storyCollector, collector.Config, error) {
		return nil, collector.Config{}, errors.New("no config")
	}
	if err := runStory(t.Context(), []string{"-task", "1"}, nil, io.Discard, io.Discard, cantOpen); err == nil {
		t.Error("runStory() error = nil, want the configuration error")
	}
}

func TestOpenCollectorRejectsAnEmptyEnvironment(t *testing.T) {
	if _, _, err := openCollector(func(string) string { return "" }); err == nil {
		t.Error("openCollector() error = nil, want the missing settings reported")
	}
}
