// Package costtest reads the cost fixtures (testdata/fixtures/cost/): a
// story's runs, the usage events of their pods and the usage feed's
// state, as cost.Compute takes them. The cost package's tests and the
// API's fake store read the same corpus.
package costtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sperano/sillage/internal/cost"
	"github.com/sperano/sillage/internal/model"
)

// InputFile is each fixture's input, in its directory.
const InputFile = "input.json"

// fixture is a fixture's input.json.
type fixture struct {
	Description string             `json:"description"`
	Root        model.Root         `json:"root"`
	Runs        []model.Run        `json:"runs"`
	Events      []model.UsageEvent `json:"events"`
	Pods        []struct {
		Run  model.RunKey `json:"run"`
		Pods []string     `json:"pods"`
	} `json:"pods"`
	Feed cost.Feed `json:"feed"`
}

// Load reads the fixture in dir: its story built by model.BuildStory, its
// events validated and in UTC, in the file's order.
func Load(dir string) (cost.Input, error) {
	b, err := os.ReadFile(filepath.Join(dir, InputFile))
	if err != nil {
		return cost.Input{}, err
	}
	var f fixture
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return cost.Input{}, fmt.Errorf("%s: %w", dir, err)
	}
	story, err := model.BuildStory(f.Root, model.Records{Runs: f.Runs})
	if err != nil {
		return cost.Input{}, fmt.Errorf("%s: %w", dir, err)
	}
	in := cost.Input{Story: story, Feed: f.Feed, Pods: map[model.RunKey][]string{}}
	for _, e := range f.Events {
		if err := e.Validate(); err != nil {
			return cost.Input{}, fmt.Errorf("%s: %w", dir, err)
		}
		in.Events = append(in.Events, e.Normalize())
	}
	for _, p := range f.Pods {
		in.Pods[p.Run] = p.Pods
	}
	return in, nil
}

// Names lists the fixtures in dir.
func Names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, errors.New("costtest: no fixtures in " + dir)
	}
	return names, nil
}
