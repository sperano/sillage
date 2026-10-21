package wiki

import (
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func TestCommentKinds(t *testing.T) {
	announcement := announcementHTML("agent-opus", "m", "wiki-fu-widgets-60-c1", "wiki-fu-widgets-60-c1-abcde")
	tests := []struct {
		name     string
		author   user
		render   string
		want     model.EventKind
		wantNote string
	}{
		{"request", testOwner, "<p>/kelos split it</p>", model.KindRequest, ""},
		{"reply", testOpus, "<p>Done.</p>", model.KindComment, ""},
		{"announcement by an agent", testOpus, announcement, model.KindRunAnnounced, "the Kelos Task"},
		{"announcement quoted by a person", testOwner, announcement, model.KindComment, ""},
		{"announcement of another page", testOpus,
			announcementHTML("agent-opus", "m", "wiki-fu-widgets-61-c1", "wiki-fu-widgets-61-c1-abcde"),
			model.KindComment, "names no Kelos Task of this page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := normalizer{
				s:      Snapshot{Instance: testInstance, Page: handPage(t0, testOwner), ObservedAt: testObservedAt},
				agents: testAgents,
				key:    model.PageKey{Instance: testInstance, ID: 60, Locale: "en"},
			}
			e := n.comment(Comment{
				ID: 1, Render: tt.render, AuthorID: tt.author.ID, AuthorName: tt.author.Name, CreatedAt: t0.Add(time.Minute),
			})
			if err := e.Validate(); err != nil {
				t.Fatal(err)
			}
			if e.Kind != tt.want || !strings.Contains(e.Evidence.Note, tt.wantNote) {
				t.Errorf("kind %s, note %q; want %s, a note with %q", e.Kind, e.Evidence.Note, tt.want, tt.wantNote)
			}
			if (e.Executor != nil) != (tt.want == model.KindRunAnnounced) {
				t.Errorf("executor = %+v", e.Executor)
			}
		})
	}
}
