package store

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestSearchRoots(t *testing.T) {
	s := newTestStore(t, Retention{})
	saves := []struct {
		root   model.Root
		title  string
		minute int
	}{
		{taskRoot(7), "Widget cache", 0},
		{taskRoot(8), "Gadget 100% coverage", 10},
		{pageRoot(57), "", 20},
	}
	for _, sv := range saves {
		recs := model.Records{}
		if sv.root.Task != nil {
			recs.Events = []model.Event{comment(sv.root.Task.ID, "comments/"+sv.title, sv.minute)}
		}
		if err := s.Save(t.Context(), Batch{Root: sv.root, Title: sv.title, Records: recs}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name  string
		query RootQuery
		want  []string
	}{
		{"everything, most recent first", RootQuery{}, []string{"Gadget 100% coverage", "Widget cache", ""}},
		{"title, any case", RootQuery{Text: "WIDGET"}, []string{"Widget cache"}},
		{"key", RootQuery{Text: "tasks/8"}, []string{"Gadget 100% coverage"}},
		{"wiki instance", RootQuery{Text: testWiki}, []string{""}},
		{"wildcard taken literally", RootQuery{Text: "0%"}, []string{"Gadget 100% coverage"}},
		{"underscore taken literally", RootQuery{Text: "_"}, nil},
		{"limit", RootQuery{Limit: 1}, []string{"Gadget 100% coverage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.SearchRoots(t.Context(), tt.query)
			if err != nil {
				t.Fatal(err)
			}
			var titles []string
			for _, r := range got {
				titles = append(titles, r.Title)
			}
			if len(titles) != len(tt.want) {
				t.Fatalf("titles = %q, want %q", titles, tt.want)
			}
			for i := range titles {
				if titles[i] != tt.want[i] {
					t.Errorf("titles = %q, want %q", titles, tt.want)
				}
			}
		})
	}
}

func TestSearchRootsLastActivity(t *testing.T) {
	s := newTestStore(t, Retention{})
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/1", 5)}})
	mustSave(t, s, taskRoot(7), model.Records{Events: []model.Event{comment(7, "comments/0", 1)}})
	got, err := s.SearchRoots(t.Context(), RootQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].LastActivity == nil || !got[0].LastActivity.Equal(at(5)) {
		t.Errorf("roots = %+v, want task 7 last active at minute 5", got)
	}
}

// TestCoverageMergedAcrossReads reads Kelos twice: the second read's gap
// before its TTL window shrinks to what the first read had not seen, the
// minutes before it and those between the two windows.
func TestCoverageMergedAcrossReads(t *testing.T) {
	s := newTestStore(t, Retention{})
	root := taskRoot(7)
	read := func(minute, gapEnd int) model.Coverage {
		return model.Coverage{Source: model.SourceKelos, Instance: testCluster, LastSuccessfulRead: ptr(at(minute)),
			Gaps: []model.Gap{{Start: at(0), End: at(gapEnd), Reason: "Tasks expired"}}}
	}
	mustSave(t, s, root, model.Records{Coverage: []model.Coverage{read(10, 5)}})
	mustSave(t, s, root, model.Records{Coverage: []model.Coverage{read(20, 15)}})
	got, err := s.Coverage(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].LastSuccessfulRead.Equal(at(20)) {
		t.Fatalf("coverage = %+v, want one read at minute 20", got)
	}
	gaps := got[0].Gaps
	if len(gaps) != 2 || !gaps[0].Start.Equal(at(0)) || !gaps[0].End.Equal(at(5)) ||
		!gaps[1].Start.After(at(10)) || !gaps[1].End.Equal(at(15)) {
		t.Errorf("gaps = %+v, want minutes 0 to 5 and 10 to 15", gaps)
	}
}
