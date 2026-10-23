package model

import (
	"bytes"
	"encoding/json"
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/sperano/sillage/internal/testzone"
)

// TestMain runs the tests outside UTC, so that output depending on the
// host's zone fails here too.
func TestMain(m *testing.M) { testzone.Main(m) }

// update rewrites every story.golden.json from its input.json:
// go test ./internal/model -run TestStoryGolden -update
var update = flag.Bool("update", false, "rewrite the fixture stories' golden files")

// storiesDir holds one directory per fixture story.
const storiesDir = "../../testdata/fixtures/stories"

const (
	inputFile  = "input.json"
	goldenFile = "story.golden.json"
	// goldenPerm is the mode of a rewritten golden file.
	goldenPerm = 0o644
	// shuffleRounds is how many random input orders each story is built
	// from in TestStoryDeterministic.
	shuffleRounds = 20
)

// fixture is the content of a story's input.json.
type fixture struct {
	Description string `json:"description"`
	Root        Root   `json:"root"`
	Records
}

func storyNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(storiesDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("no fixture stories in %s", storiesDir)
	}
	return names
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(storiesDir, name, inputFile))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f fixture
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if f.Description == "" {
		t.Fatalf("%s: description is required", name)
	}
	return f
}

func buildFixture(t *testing.T, name string) Story {
	t.Helper()
	f := loadFixture(t, name)
	story, err := BuildStory(f.Root, f.Records)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return story
}

func marshalStory(t *testing.T, s Story) []byte {
	t.Helper()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestStoryGolden(t *testing.T) {
	for _, name := range storyNames(t) {
		t.Run(name, func(t *testing.T) {
			got := marshalStory(t, buildFixture(t, name))
			path := filepath.Join(storiesDir, name, goldenFile)
			if *update {
				if err := os.WriteFile(path, got, goldenPerm); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("story differs from %s (run with -update after checking the change):\n%s", path, got)
			}
		})
	}
}

func TestStoryDeterministic(t *testing.T) {
	for _, name := range storyNames(t) {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name)
			want := marshalStory(t, buildFixture(t, name))
			for seed := range uint64(shuffleRounds) {
				recs := shuffled(f.Records, rand.New(rand.NewPCG(seed, seed)))
				story, err := BuildStory(f.Root, recs)
				if err != nil {
					t.Fatal(err)
				}
				if got := marshalStory(t, story); !bytes.Equal(got, want) {
					t.Fatalf("seed %d: the story depends on the input order", seed)
				}
			}
		})
	}
}

// shuffled returns recs with every list, nested ones included, in a random
// order. The nested lists are copies: recs is not modified.
func shuffled(recs Records, r *rand.Rand) Records {
	out := recs.clone()
	shuffle(r, out.Events)
	shuffle(r, out.Runs)
	shuffle(r, out.PullRequests)
	shuffle(r, out.Correlations)
	shuffle(r, out.Coverage)
	for _, run := range out.Runs {
		shuffle(r, run.Evidence)
	}
	for _, pr := range out.PullRequests {
		shuffle(r, pr.Checks)
	}
	for _, c := range out.Correlations {
		shuffle(r, c.Evidence)
	}
	for _, c := range out.Coverage {
		shuffle(r, c.Gaps)
		shuffle(r, c.Notes)
	}
	return out
}

func shuffle[T any](r *rand.Rand, s []T) {
	r.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}
