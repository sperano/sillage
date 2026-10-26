package kelos

import (
	"maps"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestOriginPullRequest(t *testing.T) {
	gh := map[string]string{
		AnnotationSource: OriginGitHub, AnnotationSourceInstance: "github.example.com",
		AnnotationRepository: "example-org/widgets", AnnotationPR: "41",
	}
	got, ok := origin(gh).PullRequest()
	if want := (model.PRKey{Instance: "github.example.com", Repository: "example-org/widgets", Number: 41}); !ok || got != want {
		t.Errorf("PullRequest() = %v, %v, want %v", got, ok, want)
	}
	for name, change := range map[string][2]string{
		"scheduled":         {AnnotationSource, OriginSchedule},
		"no instance":       {AnnotationSourceInstance, ""},
		"no number":         {AnnotationPR, ""},
		"negative number":   {AnnotationPR, "-4"},
		"number too large":  {AnnotationPR, "2147483648"},
		"no owner":          {AnnotationRepository, "widgets"},
		"nested repository": {AnnotationRepository, "example-org/widgets/extra"},
	} {
		values := withValue(gh, change[0], change[1])
		if got, ok := origin(values).PullRequest(); ok {
			t.Errorf("%s: PullRequest() = %v, want none", name, got)
		}
	}
}

// withValue returns a copy of values with name set to v, or removed when v
// is empty.
func withValue(values map[string]string, name, v string) map[string]string {
	out := maps.Clone(values)
	if v == "" {
		delete(out, name)
	} else {
		out[name] = v
	}
	return out
}

// A task label is always on the configured Vikunja instance; a Vikunja
// Origin on that instance adds its evidence, one on another host does not.
func TestTaskLinkStaysOnTheConfiguredVikunja(t *testing.T) {
	for name, tt := range map[string]struct {
		values   map[string]string
		evidence int
	}{
		"same vikunja":    {map[string]string{AnnotationSource: OriginVikunja, AnnotationSourceInstance: "Vikunja.Example.com"}, 2},
		"another vikunja": {map[string]string{AnnotationSource: OriginVikunja, AnnotationSourceInstance: "tasks.example.com"}, 1},
		"github origin":   {map[string]string{AnnotationSource: OriginGitHub, AnnotationSourceInstance: "github.example.com"}, 1},
		"no instance":     {map[string]string{AnnotationSource: OriginVikunja}, 1},
	} {
		task := annotated("vikunja-widgets-agent-opus-7", "u1", tt.values)
		task.Labels = map[string]string{testDomain + "/task": "7"}
		recs, err := Normalize(testConfig(), Snapshot{Tasks: []Task{task}})
		if err != nil {
			t.Fatal(err)
		}
		wantRef := model.TaskKey{Instance: testVikunja, ID: 7}.Ref()
		if len(recs.Correlations) != 1 || recs.Correlations[0].To != wantRef ||
			len(recs.Correlations[0].Evidence) != tt.evidence {
			t.Errorf("%s: correlations = %v, want one to %s with %d evidence", name, recs.Correlations, wantRef, tt.evidence)
		}
	}
}
