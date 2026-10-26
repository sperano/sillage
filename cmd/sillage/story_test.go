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

var testCollectorConfig = collector.Config{VikunjaInstance: "vikunja.example.com"}

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
		{[]string{"-task", "102"}, storyArgs{task: 102}},
		{[]string{"-task", "102", "-repo", "example-org/widgets"}, storyArgs{
			task: 102, repos: []github.Repository{widgets},
		}},
	}
	for _, tt := range tests {
		got, err := parseStoryArgs(tt.args, io.Discard)
		if err != nil || got.task != tt.want.task ||
			!slices.Equal(got.repos, tt.want.repos) {
			t.Errorf("parseStoryArgs(%q) = %+v, %v; want %+v", tt.args, got, err, tt.want)
		}
	}
}

func TestParseStoryArgsRejects(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-task", "-1"},
		{"-task", "1", "extra"},
		{"-task", "one"},
		{"-task", "1", "-repo", "widgets"},
		{"-page", "57"},
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

// assertPrintedTask fails t unless stdout holds the JSON of task id's story.
func assertPrintedTask(t *testing.T, stdout []byte, id int64) {
	t.Helper()
	var got model.Story
	if err := json.Unmarshal(stdout, &got); err != nil || got.Root.Task == nil || got.Root.Task.ID != id {
		t.Errorf("printed %q (%v), want the JSON of task %d's story", stdout, err, id)
	}
}

func TestRunStoryPrintsTheStory(t *testing.T) {
	root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 102}}
	fake := &fakeCollector{res: collector.Result{Story: testStory(t, root)}}
	var stdout, stderr bytes.Buffer
	args := []string{"-task", "102", "-repo", "example-org/widgets"}
	if err := runStory(t.Context(), args, nil, &stdout, &stderr, fake.open); err != nil {
		t.Fatalf("runStory() error = %v, stderr %q", err, stderr.String())
	}
	if fake.root.Task == nil || *fake.root.Task != *root.Task {
		t.Errorf("collected %+v, want task 102 of the configured Vikunja", fake.root)
	}
	if want := []github.Repository{{Owner: "example-org", Name: "widgets"}}; !slices.Equal(fake.repos, want) {
		t.Errorf("collected with repositories %v, want %v", fake.repos, want)
	}
	assertPrintedTask(t, stdout.Bytes(), 102)
}

func TestRunStoryReportsFailedReads(t *testing.T) {
	root := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 102}}
	fake := &fakeCollector{res: collector.Result{
		Story:    testStory(t, root),
		Failures: []collector.Failure{{Source: model.SourceGitHub, Instance: "github.example.com", Err: errors.New("down")}},
	}}
	var stdout, stderr bytes.Buffer
	err := runStory(t.Context(), []string{"-task", "102"}, nil, &stdout, &stderr, fake.open)
	if !errors.Is(err, errPartial) || !strings.Contains(stderr.String(), "github github.example.com: down") {
		t.Errorf("runStory() = %v, stderr %q; want errPartial and the failure reported", err, stderr.String())
	}
	assertPrintedTask(t, stdout.Bytes(), 102)
}

func TestRunStoryReturnsErrors(t *testing.T) {
	errInvalid := errors.New("invalid records")
	failing := &fakeCollector{err: errInvalid}
	if err := runStory(t.Context(), []string{"-task", "1"}, nil, io.Discard, io.Discard, failing.open); !errors.Is(err, errInvalid) {
		t.Errorf("runStory() error = %v, want the collector's error", err)
	}
	errNoConfig := errors.New("no config")
	cantOpen := func(func(string) string) (storyCollector, collector.Config, error) {
		return nil, collector.Config{}, errNoConfig
	}
	if err := runStory(t.Context(), []string{"-task", "1"}, nil, io.Discard, io.Discard, cantOpen); !errors.Is(err, errNoConfig) {
		t.Errorf("runStory() error = %v, want the configuration error", err)
	}
}

func TestOpenCollectorRejectsAnEmptyEnvironment(t *testing.T) {
	if _, _, err := openCollector(func(string) string { return "" }); err == nil {
		t.Error("openCollector() error = nil, want the missing settings reported")
	}
}
