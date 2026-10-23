package kelos

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

// update rewrites every records.golden.json from its story's objects:
// go test ./internal/source/kelos -run TestNormalizeGolden -update
var update = flag.Bool("update", false, "rewrite the Kelos fixtures' golden files")

const (
	goldenFile = "records.golden.json"
	// goldenPerm is the mode of a rewritten golden file.
	goldenPerm = 0o644
)

// storyRecords reads a story through the fake client and normalizes it.
func storyRecords(t *testing.T, story string) model.Records {
	t.Helper()
	recs, err := Normalize(testConfig(), readStory(t, story))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return recs
}

func stories(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestNormalizeGolden(t *testing.T) {
	for _, story := range stories(t) {
		t.Run(story, func(t *testing.T) {
			recs := storyRecords(t, story)
			// Normalize produces runs and correlations only.
			got, err := json.MarshalIndent(struct {
				Runs         []model.Run         `json:"runs"`
				Correlations []model.Correlation `json:"correlations"`
			}{recs.Runs, recs.Correlations}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(fixtureDir, story, goldenFile)
			if *update {
				if err := os.WriteFile(path, got, goldenPerm); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the normalized records; run with -update and review the diff", path)
			}
		})
	}
}

// Every story's records build into a story about the Vikunja task they
// name, or about any task when they name none.
func TestNormalizedStoriesBuild(t *testing.T) {
	for _, story := range stories(t) {
		t.Run(story, func(t *testing.T) {
			root := model.Root{Task: &model.TaskKey{Instance: testVikunja, ID: testTaskID}}
			if _, err := model.BuildStory(root, storyRecords(t, story)); err != nil {
				t.Errorf("BuildStory: %v", err)
			}
		})
	}
}

// onlyRun returns the story's one run.
func onlyRun(t *testing.T, recs model.Records) model.Run {
	t.Helper()
	if len(recs.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(recs.Runs))
	}
	return recs.Runs[0]
}

// evidenceRefs returns each evidence's ref.
func evidenceRefs(run model.Run) []string {
	var refs []string
	for _, ev := range run.Evidence {
		refs = append(refs, ev.Ref)
	}
	return refs
}

func noteOf(t *testing.T, run model.Run, refPrefix string) string {
	t.Helper()
	for _, ev := range run.Evidence {
		if strings.HasPrefix(ev.Ref, refPrefix) {
			return ev.Note
		}
	}
	t.Fatalf("run %s: no evidence %s*, got %v", run.Key, refPrefix, evidenceRefs(run))
	return ""
}

func checkNote(t *testing.T, note string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(note, w) {
			t.Errorf("note %q lacks %q", note, w)
		}
	}
}

func TestNormalizeRejectsObjectsWithoutUIDOrSeenTwice(t *testing.T) {
	task := Task{}
	task.Name, task.UID = "t", "u1"
	record := TaskRecord{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "t", UID: "u1"}, Phase: PhaseSucceeded}}
	record.Name = "u1"
	noUID := Task{}
	noUID.Name = "no-uid"
	tests := []struct {
		name string
		s    Snapshot
		want string
	}{
		{"task without uid", Snapshot{Tasks: []Task{noUID}}, "task tasks/agents/no-uid: no uid"},
		{"task twice", Snapshot{Tasks: []Task{task, task}}, "task tasks/agents/t: uid u1 seen twice"},
		{"record without uid", Snapshot{Records: []TaskRecord{{}}}, "no spec.taskRef.uid"},
		{"record twice", Snapshot{Records: []TaskRecord{record, record}}, "taskrecord taskrecords/agents/u1: task uid u1 seen twice"},
		{"record twice with its task", Snapshot{Tasks: []Task{task}, Records: []TaskRecord{record, record}}, "task uid u1 seen twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Normalize(testConfig(), tt.s); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeRejectsInvalidConfig(t *testing.T) {
	cfg := testConfig()
	cfg.LabelDomain = ""
	if _, err := Normalize(cfg, Snapshot{}); err == nil || !strings.Contains(err.Error(), EnvLabelDomain) {
		t.Errorf("error = %v, want the label domain rejected", err)
	}
}

// An object the model rejects fails the read, naming the run.
func TestNormalizeRejectsInvalidRun(t *testing.T) {
	s := readStory(t, "task-only")
	s.Tasks[0].Status.CompletionTime.Time = s.Tasks[0].Status.StartTime.Add(-1)
	_, err := Normalize(testConfig(), s)
	if err == nil || !strings.Contains(err.Error(), "uid/00000000-0000-4000-8000-000000000203") ||
		!strings.Contains(err.Error(), "ends before it starts") {
		t.Errorf("error = %v, want the run's interval rejected", err)
	}
}
