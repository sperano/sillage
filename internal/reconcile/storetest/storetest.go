// Package storetest holds the contract of reconcile.Store as the loop
// relies on it, so that every implementation (the loop's in-memory fake,
// the PostgreSQL store's adapter) asserts the same behavior. It is
// imported by tests only.
package storetest

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/reconcile"
)

// Run asserts that the stores open returns keep reconcile.Store's
// contract. Each subtest opens a new, empty store.
func Run(t *testing.T, open func(*testing.T) reconcile.Store) {
	tests := []struct {
		name string
		test func(*testing.T, reconcile.Store)
	}{
		{"an unknown root has no records", unknownRoot},
		{"committed records are read back", recordsReadBack},
		{"a run a later read lacks is kept", runKept},
		{"a placeholder run is re-keyed by UID", placeholderRekeyed},
		{"coverage replaces the stored coverage of its instance", coverageReplaced},
		{"a commit of coverage alone adds no record", coverageAlone},
		{"checkpoints are listed by prefix across instances", checkpointsByPrefix},
		{"a failed commit writes nothing", failedCommit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.test(t, open(t)) })
	}
}

func mustCommit(t *testing.T, s reconcile.Store, c reconcile.Commit) {
	t.Helper()
	if err := s.Commit(t.Context(), c); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func commitRecords(t *testing.T, s reconcile.Store, recs model.Records) {
	t.Helper()
	mustCommit(t, s, reconcile.Commit{Root: ptr(root()), Records: recs})
}

func mustRecords(t *testing.T, s reconcile.Store) model.Records {
	t.Helper()
	recs, err := s.Records(t.Context(), root())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	return recs
}

func eventKeys(recs model.Records) []string {
	out := make([]string, 0, len(recs.Events))
	for _, e := range recs.Events {
		out = append(out, e.Key())
	}
	slices.Sort(out)
	return out
}

func runKeys(recs model.Records) []model.RunKey {
	out := make([]model.RunKey, 0, len(recs.Runs))
	for _, r := range recs.Runs {
		out = append(out, r.Key)
	}
	return out
}

// withoutCoverage is the story of recs' records but coverage, in the
// story's order, so that two reads of the same records compare equal.
func withoutCoverage(t *testing.T, recs model.Records) model.Story {
	t.Helper()
	recs.Coverage = nil
	story, err := model.BuildStory(root(), recs)
	if err != nil {
		t.Fatalf("the stored story is invalid: %v", err)
	}
	return story
}

// sameJSON reports whether got and want have the same JSON, the form
// the store keeps records in.
func sameJSON(t *testing.T, got, want any) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(g, w)
}

func unknownRoot(t *testing.T, s reconcile.Store) {
	recs := mustRecords(t, s)
	if len(recs.Events)+len(recs.Runs)+len(recs.PullRequests)+len(recs.Correlations)+len(recs.Coverage) != 0 {
		t.Errorf("records of a root never committed = %+v, want none", recs)
	}
}

func recordsReadBack(t *testing.T, s reconcile.Store) {
	run := kelosRun()
	announcement, pr := announcement("comments/900", 1, run), pullRequest()
	commitRecords(t, s, model.Records{
		Events: []model.Event{announcement}, Runs: []model.Run{run}, PullRequests: []model.PullRequest{pr},
		Correlations: []model.Correlation{announces(announcement, run)},
		Coverage:     []model.Coverage{coverage(model.SourceVikunja, vikunjaHost, 2)},
	})
	recs := mustRecords(t, s)
	if got := eventKeys(recs); !slices.Equal(got, []string{announcement.Key()}) {
		t.Errorf("events = %v, want the announcement", got)
	}
	if got := runKeys(recs); !slices.Equal(got, []model.RunKey{run.Key}) {
		t.Errorf("runs = %v, want %s", got, run.Key)
	}
	if len(recs.PullRequests) != 1 || !sameJSON(t, recs.PullRequests[0], pr) {
		t.Errorf("pull requests = %+v, want %+v with its check", recs.PullRequests, pr)
	}
	if len(recs.Correlations) != 1 || recs.Correlations[0].Pair() != announces(announcement, run).Pair() {
		t.Errorf("correlations = %+v, want the announcement's", recs.Correlations)
	}
	if len(recs.Coverage) != 1 || !recs.Coverage[0].LastSuccessfulRead.Equal(at(2)) {
		t.Errorf("coverage = %+v, want Vikunja read at minute 2", recs.Coverage)
	}
}

// runKept commits the run, then a read after its Task's TTL: the run is
// still the story's.
func runKept(t *testing.T, s reconcile.Store) {
	commitRecords(t, s, model.Records{Runs: []model.Run{kelosRun()}})
	commitRecords(t, s, model.Records{Events: []model.Event{comment("comments/901", 40)}})
	if got := runKeys(mustRecords(t, s)); !slices.Equal(got, []model.RunKey{kelosRun().Key}) {
		t.Errorf("runs = %v, want the run committed first", got)
	}
}

// placeholderRekeyed commits the announcement of a run known only by
// its pod, then the run as Kelos read it: the run, the announcement's
// run and the correlations name the UID.
func placeholderRekeyed(t *testing.T, s reconcile.Store) {
	placeholder := placeholderRun()
	announcement := announcement("comments/900", 1, placeholder)
	commitRecords(t, s, model.Records{
		Events: []model.Event{announcement}, Runs: []model.Run{placeholder},
		Correlations: []model.Correlation{announces(announcement, placeholder)},
	})
	commitRecords(t, s, model.Records{Runs: []model.Run{kelosRun()}})
	recs := mustRecords(t, s)
	if got := runKeys(recs); !slices.Equal(got, []model.RunKey{kelosRun().Key}) {
		t.Errorf("runs = %v, want the run keyed by UID alone", got)
	}
	if len(recs.Events) != 1 || recs.Events[0].Related.Run == nil || *recs.Events[0].Related.Run != kelosRun().Key {
		t.Errorf("events = %+v, want the announcement naming %s", recs.Events, kelosRun().Key)
	}
	for _, c := range recs.Correlations {
		if c.From == placeholder.Key.Ref() || c.To == placeholder.Key.Ref() {
			t.Errorf("correlation %s still names the placeholder", c)
		}
	}
	if _, err := model.BuildStory(root(), recs); err != nil {
		t.Errorf("the stored story is invalid: %v", err)
	}
}

// coverageReplaced commits, after a read at minute 10, a gap that starts
// before it, as the collector reports an announced run no Kelos read
// found: it is kept whole. The coverage of an instance the commit does
// not carry is kept.
func coverageReplaced(t *testing.T, s reconcile.Store) {
	commitRecords(t, s, model.Records{Coverage: []model.Coverage{
		coverage(model.SourceKelos, cluster, 10), coverage(model.SourceVikunja, vikunjaHost, 10),
	}})
	late := gap(5, 20, "announced, and no Kelos read found it")
	commitRecords(t, s, model.Records{Coverage: []model.Coverage{coverage(model.SourceKelos, cluster, 20, late)}})
	byKey := make(map[string]model.Coverage)
	for _, c := range mustRecords(t, s).Coverage {
		byKey[c.Key()] = c
	}
	kelos := byKey[coverage(model.SourceKelos, cluster, 0).Key()]
	if kelos.LastSuccessfulRead == nil || !kelos.LastSuccessfulRead.Equal(at(20)) || len(kelos.Gaps) != 1 ||
		!kelos.Gaps[0].Start.Equal(late.Start) || !kelos.Gaps[0].End.Equal(late.End) {
		t.Errorf("kelos coverage = %+v, want read at minute 20 with the gap %+v whole", kelos, late)
	}
	vikunja := byKey[coverage(model.SourceVikunja, vikunjaHost, 0).Key()]
	if vikunja.LastSuccessfulRead == nil || !vikunja.LastSuccessfulRead.Equal(at(10)) {
		t.Errorf("vikunja coverage = %+v, want it kept", vikunja)
	}
}

// coverageAlone commits what the loop commits for a read of an unchanged
// task: coverage, and no record.
func coverageAlone(t *testing.T, s reconcile.Store) {
	announcement, run := announcement("comments/900", 1, kelosRun()), kelosRun()
	commitRecords(t, s, model.Records{
		Events: []model.Event{announcement}, Runs: []model.Run{run}, PullRequests: []model.PullRequest{pullRequest()},
		Correlations: []model.Correlation{announces(announcement, run)},
	})
	before := withoutCoverage(t, mustRecords(t, s))
	commitRecords(t, s, model.Records{Coverage: []model.Coverage{coverage(model.SourceVikunja, vikunjaHost, 60)}})
	if after := withoutCoverage(t, mustRecords(t, s)); !sameJSON(t, after, before) {
		t.Errorf("records after a coverage commit = %+v, want %+v", after, before)
	}
}

func checkpointsByPrefix(t *testing.T, s reconcile.Store) {
	commitRecords(t, s, model.Records{})
	r := root()
	mustCommit(t, s, reconcile.Commit{Checkpoints: map[string]reconcile.Checkpoint{
		"loop/roots/a":         {Source: model.SourceVikunja, Instance: vikunjaHost, Root: &r, Value: []byte(`{"n":1}`)},
		"loop/beat/" + cluster: {Source: model.SourceKelos, Instance: cluster, Value: []byte(`"x"`)},
		"other":                {Source: model.SourceKelos, Instance: cluster, Value: []byte(`{}`)},
	}})
	mustCommit(t, s, reconcile.Commit{Checkpoints: map[string]reconcile.Checkpoint{
		"loop/roots/a": {Source: model.SourceVikunja, Instance: vikunjaHost, Root: &r, Value: []byte(`{"n":2}`)},
	}})
	got, err := s.Checkpoints(t.Context(), "loop/")
	if err != nil {
		t.Fatalf("Checkpoints: %v", err)
	}
	want := map[string]string{"loop/roots/a": `{"n":2}`, "loop/beat/" + cluster: `"x"`}
	if len(got) != len(want) {
		t.Errorf("checkpoints = %q, want %q", got, want)
	}
	for key, value := range want {
		if string(got[key]) != value {
			t.Errorf("checkpoint %s = %q, want %q", key, got[key], value)
		}
	}
}

// failedCommit commits a root's records with a checkpoint tied to a root
// the store does not hold: neither is written.
func failedCommit(t *testing.T, s reconcile.Store) {
	other := model.Root{Task: &model.TaskKey{Instance: vikunjaHost, ID: taskID + 1}}
	err := s.Commit(t.Context(), reconcile.Commit{
		Root: ptr(root()), Records: model.Records{Events: []model.Event{comment("comments/900", 1)}},
		Checkpoints: map[string]reconcile.Checkpoint{
			"loop/roots/b": {Source: model.SourceVikunja, Instance: vikunjaHost, Root: &other, Value: []byte(`{}`)},
		},
	})
	if err == nil {
		t.Fatal("Commit tied a checkpoint to a root the store does not hold")
	}
	if recs := mustRecords(t, s); len(recs.Events) != 0 {
		t.Errorf("events after the failed commit = %v, want none", eventKeys(recs))
	}
	if got, err := s.Checkpoints(t.Context(), "loop/"); err != nil || len(got) != 0 {
		t.Errorf("checkpoints after the failed commit = %q, %v; want none", got, err)
	}
}
