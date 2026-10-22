package wiki

import (
	"errors"
	"testing"

	"github.com/sperano/sillage/internal/source/htmltext"
)

func TestParseAnnouncement(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Announcement
	}{
		{
			name: "full",
			body: announcementHTML("agent-opus", "example-model-2[1m]", "wiki-fu-widgets-57-c880", "wiki-fu-widgets-57-c880-fghij"),
			want: Announcement{
				Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "wiki-fu-widgets-57-c880", Pod: "wiki-fu-widgets-57-c880-fghij",
				Workspace: "widgets", RequestComment: 880,
			},
		},
		{
			name: "no agent, model, pod or namespace",
			body: "<p>The agent has started on this follow-up request: Kelos Task <code>wiki-fu-widgets-57-c881</code>.</p>",
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

// announcementHTML renders an announcement as Wiki.js does.
func announcementHTML(agent, model, task, pod string) string {
	return `<p>Agent <code>` + agent + `</code> (model <code>` + model + `</code>) has started on this follow-up ` +
		`request: Kelos Task <a href="https://kelos.example.com/x"><code>` + task + `</code></a>, pod <code>` + pod +
		"</code>.</p>\n<p>The link shows its latest log lines once you are logged in to the " +
		`<a href="https://kelos.example.com">Kelos Console</a> (namespace <code>agents</code>).</p>` + "\n"
}

func TestParseAnnouncementRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{"plain comment", "<p>Done, see the page.</p>", errNotAnnouncement},
		{"empty", "", errNotAnnouncement},
		{"another page", announcementHTML("agent-opus", "m", "wiki-fu-widgets-58-c1", "wiki-fu-widgets-58-c1-abcde"),
			errUnparsedAnnouncement},
		{"not a wiki follow-up", announcementHTML("agent-opus", "m", "vikunja-fu-widgets-57-c1", "p-1"),
			errUnparsedAnnouncement},
		{"no Kelos Task", "<p>Agent <code>agent-opus</code> has started on this follow-up request.</p>",
			errUnparsedAnnouncement},
		{"phrase not in the first paragraph", "<p>Hello.</p><p>The agent has started on this follow-up request: " +
			"Kelos Task <code>wiki-fu-widgets-57-c1</code>.</p>", errUnparsedAnnouncement},
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

func TestParseRequest(t *testing.T) {
	tests := []struct {
		text      string
		wantAgent string
		wantOK    bool
	}{
		{"/kelos split the section", "", true},
		{"/kelos", "", true},
		{"/kelos @agent-sol tighten the steps", "agent-sol", true},
		{"/kelos @agent-sol, tighten the steps", "agent-sol", true},
		{"/kelos @agent-sol: tighten the steps", "agent-sol", true},
		{"/kelos @sol.", "sol", true},
		{"/kelos @, tighten the steps", "", true},
		{"/kelos\tplease", "", true},
		{"/kelosx do it", "", false},
		{"please /kelos do it", "", false},
		{"Done.", "", false},
	}
	for _, tt := range tests {
		agent, ok := parseRequest(tt.text)
		if agent != tt.wantAgent || ok != tt.wantOK {
			t.Errorf("parseRequest(%q) = %q, %v; want %q, %v", tt.text, agent, ok, tt.wantAgent, tt.wantOK)
		}
	}
}
