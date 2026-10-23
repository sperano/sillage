package wiki

import (
	"errors"
	"testing"

	"github.com/sperano/sillage/internal/source/htmltext"
	"github.com/sperano/sillage/internal/source/kelostext"
)

func TestParseAnnouncement(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Announcement
	}{
		{
			name: "current",
			body: gordonAnnouncementHTML("agent-opus", "example-model-2[1m]", "wiki-fu-widgets-57-c880", "wiki-fu-widgets-57-c880-fghij"),
			want: Announcement{
				Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "wiki-fu-widgets-57-c880", Pod: "wiki-fu-widgets-57-c880-fghij",
				Workspace: "widgets", RequestComment: 880,
			},
		},
		{
			name: "current, an agent, no model or pod",
			body: "<p>AN AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. KELOS TASK " +
				"<code>wiki-fu-widgets-57-c883</code>.</p>",
			want: Announcement{TaskName: "wiki-fu-widgets-57-c883", Workspace: "widgets", RequestComment: 883},
		},
		{
			name: "colon",
			body: "<p>AGENT <code>agent-sol</code> IS ON THIS FOLLOW-UP REQUEST: KELOS TASK <code>wiki-fu-widgets-57-c884</code>, " +
				"POD <code>wiki-fu-widgets-57-c884-uvwxy</code>.</p>\n<p>OPEN THE <a href=\"https://kelos.example.com\">KELOS " +
				"CONSOLE</a>. SIGN IN, SELECT NAMESPACE <code>agents</code>, AND FIND TASK <code>wiki-fu-widgets-57-c884</code> " +
				"IN RESOURCES.</p>\n",
			want: Announcement{
				Agent: "agent-sol", Namespace: "agents", TaskName: "wiki-fu-widgets-57-c884",
				Pod: "wiki-fu-widgets-57-c884-uvwxy", Workspace: "widgets", RequestComment: 884,
			},
		},
		{
			name: "lowercase",
			body: announcementHTML("agent-opus", "example-model-2[1m]", "wiki-fu-widgets-57-c880", "wiki-fu-widgets-57-c880-fghij"),
			want: Announcement{
				Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "wiki-fu-widgets-57-c880", Pod: "wiki-fu-widgets-57-c880-fghij",
				Workspace: "widgets", RequestComment: 880,
			},
		},
		{
			name: "lowercase, an agent, no model, pod or namespace",
			body: "<p>An agent has started on this follow-up request: Kelos Task <code>wiki-fu-widgets-57-c881</code>.</p>",
			want: Announcement{TaskName: "wiki-fu-widgets-57-c881", Workspace: "widgets", RequestComment: 881},
		},
		{
			name: "followed by a list",
			body: "<div>The agent has started on this follow-up request: Kelos Task <code>wiki-fu-widgets-57-c882</code>.</div>" +
				"<ul><li>Logs in the console.</li></ul>",
			want: Announcement{TaskName: "wiki-fu-widgets-57-c882", Workspace: "widgets", RequestComment: 882},
		},
		{
			name: "retry, workspace with hyphens and digits",
			body: "<p>Agent <code>agent-sol</code> has started on this follow-up request: Kelos Task " +
				"<code>wiki-fu-team-2-tools-57-c9-retry2</code>, pod <code>wiki-fu-team-2-tools-57-c9-retry2-zz9yy</code>.</p>",
			want: Announcement{
				Agent: "agent-sol", TaskName: "wiki-fu-team-2-tools-57-c9-retry2",
				Pod: "wiki-fu-team-2-tools-57-c9-retry2-zz9yy", Workspace: "team-2-tools", RequestComment: 9,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(htmltext.Plain(tt.body), 57)
			if err != nil {
				t.Fatalf("parseAnnouncement: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// announcementHTML renders a lowercase announcement, as posted until
// 2026-10-22, as Wiki.js does.
func announcementHTML(agent, model, task, pod string) string {
	return `<p>Agent <code>` + agent + `</code> (model <code>` + model + `</code>) has started on this follow-up ` +
		`request: Kelos Task <a href="https://kelos.example.com/x"><code>` + task + `</code></a>, pod <code>` + pod +
		"</code>.</p>\n<p>The link shows its latest log lines once you are logged in to the " +
		`<a href="https://kelos.example.com">Kelos Console</a> (namespace <code>agents</code>).</p>` + "\n"
}

// gordonAnnouncementHTML renders a current announcement as Wiki.js does.
func gordonAnnouncementHTML(agent, model, task, pod string) string {
	return `<p>AGENT <code>` + agent + `</code> (MODEL <code>` + model + `</code>) IS ON YOUR FOLLOW-UP REQUEST. ` +
		`HEARD YOU LOUD AND CLEAR. KELOS TASK <code>` + task + `</code>, POD <code>` + pod + "</code>.</p>\n" +
		`<p>WANT TO WATCH? OPEN THE <a href="https://kelos.example.com">KELOS CONSOLE</a>. SIGN IN, SELECT NAMESPACE ` +
		`<code>agents</code>, AND FIND TASK <code>` + task + "</code> IN RESOURCES.</p>\n<p>COFFEE TIME! REPORT TO FOLLOW.</p>\n"
}

func TestParseAnnouncementRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{"plain comment", "<p>Done, see the page.</p>", kelostext.ErrNotAnnouncement},
		{"empty", "", kelostext.ErrNotAnnouncement},
		{"a Vikunja phrase", "<p>THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK " +
			"<code>wiki-fu-widgets-57-c1</code>.</p>", kelostext.ErrNotAnnouncement},
		{"another page", announcementHTML("agent-opus", "m", "wiki-fu-widgets-58-c1", "wiki-fu-widgets-58-c1-abcde"),
			kelostext.ErrUnparsedAnnouncement},
		{"another page, current", gordonAnnouncementHTML("agent-opus", "m", "wiki-fu-widgets-58-c1", "wiki-fu-widgets-58-c1-abcde"),
			kelostext.ErrUnparsedAnnouncement},
		{"not a wiki follow-up", gordonAnnouncementHTML("agent-opus", "m", "vikunja-fu-widgets-57-c1", "p-1"),
			kelostext.ErrUnparsedAnnouncement},
		{"no Kelos Task", "<p>Agent <code>agent-opus</code> has started on this follow-up request.</p>",
			kelostext.ErrUnparsedAnnouncement},
		{"phrase not in the first paragraph", "<p>Hello.</p><p>AN AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD " +
			"AND CLEAR. KELOS TASK <code>wiki-fu-widgets-57-c1</code>.</p>", kelostext.ErrUnparsedAnnouncement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(htmltext.Plain(tt.body), 57)
			if !errors.Is(err, tt.want) {
				t.Errorf("parseAnnouncement = %+v, %v; want error %v", got, err, tt.want)
			}
		})
	}
}
