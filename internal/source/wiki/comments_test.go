package wiki

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func TestCommentKinds(t *testing.T) {
	announcement := gordonAnnouncementHTML("agent-opus", "m", "wiki-fu-widgets-60-c1", "wiki-fu-widgets-60-c1-abcde")
	tests := []struct {
		name     string
		author   user
		render   string
		want     model.EventKind
		wantNote string
	}{
		{"request", testOwner, "<p>/gordon split it</p>", model.KindRequest, ""},
		{"request, former command", testOwner, "<p>/kelos split it</p>", model.KindRequest, ""},
		{"reply", testOpus, "<p>Done.</p>", model.KindComment, ""},
		{"announcement by an agent", testOpus, announcement, model.KindRunAnnounced,
			"and the Kelos Task, pod and namespace it names"},
		{"lowercase announcement by an agent", testOpus,
			announcementHTML("agent-opus", "m", "wiki-fu-widgets-60-c1", "wiki-fu-widgets-60-c1-abcde"),
			model.KindRunAnnounced, "and the Kelos Task, pod and namespace it names"},
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

// A request's "command" detail is the command it starts with.
func TestRequestDetails(t *testing.T) {
	tests := []struct {
		render string
		want   map[string]string
	}{
		{"<p>/gordon @agent-sol split it</p>", map[string]string{"authorId": "7", "command": "/gordon", "agent": "agent-sol"}},
		{"<p>/kelos split it</p>", map[string]string{"authorId": "7", "command": "/kelos"}},
	}
	for _, tt := range tests {
		n := normalizer{
			s:      Snapshot{Instance: testInstance, Page: handPage(t0, testOwner), ObservedAt: testObservedAt},
			agents: testAgents,
			key:    model.PageKey{Instance: testInstance, ID: 60, Locale: "en"},
		}
		e := n.comment(Comment{ID: 1, Render: tt.render, AuthorID: testOwner.ID, AuthorName: testOwner.Name, CreatedAt: t0})
		if !maps.Equal(e.Details, tt.want) {
			t.Errorf("%s: details %v, want %v", tt.render, e.Details, tt.want)
		}
	}
}

// The evidence note names only what the announcement states.
func TestAnnouncedNote(t *testing.T) {
	tests := []struct {
		pod, namespace, want string
	}{
		{"pod-a", "agents", "comment 1, and the Kelos Task, pod and namespace it names"},
		{"pod-a", "", "comment 1, and the Kelos Task and pod it names"},
		{"", "agents", "comment 1, and the Kelos Task and namespace it names"},
		{"", "", "comment 1, and the Kelos Task it names"},
	}
	for _, tt := range tests {
		t.Run("pod="+tt.pod+",namespace="+tt.namespace, func(t *testing.T) {
			e := model.Event{Evidence: model.Evidence{Note: "comment 1"}}
			announced(&e, Announcement{TaskName: "wiki-fu-widgets-60-c1", Pod: tt.pod, Namespace: tt.namespace, RequestComment: 1})
			if got := e.Evidence.Note; got != tt.want {
				t.Errorf("note %q, want %q", got, tt.want)
			}
		})
	}
}
