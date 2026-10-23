package kelos

import (
	"maps"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestOriginPage(t *testing.T) {
	wiki := map[string]string{
		AnnotationSource: OriginWiki, AnnotationSourceInstance: "Wiki.Example.com",
		AnnotationWikiPage: "57", AnnotationWikiLocale: "en",
	}
	got, ok := origin(wiki).Page()
	if want := (model.PageKey{Instance: "wiki.example.com", ID: 57, Locale: "en"}); !ok || got != want {
		t.Errorf("Page() = %v, %v, want %v", got, ok, want)
	}
	for name, change := range map[string][2]string{
		"another source": {AnnotationSource, OriginGitHub},
		"no instance":    {AnnotationSourceInstance, ""},
		"page not an id": {AnnotationWikiPage, "page"},
		"page zero":      {AnnotationWikiPage, "0"},
		"no locale":      {AnnotationWikiLocale, ""},
	} {
		values := withValue(wiki, change[0], change[1])
		if got, ok := origin(values).Page(); ok {
			t.Errorf("%s: Page() = %v, want none", name, got)
		}
	}
}

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

// A task label is on the Vikunja instance a Vikunja Origin names, else on
// the configured one.
func TestTaskLinkTakesTheOriginsVikunja(t *testing.T) {
	for name, tt := range map[string]struct {
		values map[string]string
		want   string
	}{
		"vikunja origin": {map[string]string{AnnotationSource: OriginVikunja, AnnotationSourceInstance: "Tasks.Example.com"}, "tasks.example.com"},
		"github origin":  {map[string]string{AnnotationSource: OriginGitHub, AnnotationSourceInstance: "github.example.com"}, testVikunja},
		"no instance":    {map[string]string{AnnotationSource: OriginVikunja}, testVikunja},
	} {
		task := annotated("vikunja-widgets-agent-opus-7", "u1", tt.values)
		task.Labels = map[string]string{testDomain + "/task": "7"}
		recs, err := Normalize(testConfig(), Snapshot{Tasks: []Task{task}})
		if err != nil {
			t.Fatal(err)
		}
		wantRef := model.TaskKey{Instance: tt.want, ID: 7}.Ref()
		if len(recs.Correlations) != 1 || recs.Correlations[0].To != wantRef {
			t.Errorf("%s: correlations = %v, want one to %s", name, recs.Correlations, wantRef)
		}
	}
}
