package vikunja

import (
	"errors"
	"testing"
)

func TestParseAnnouncement(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Announcement
	}{
		{
			name: "implementation",
			body: `<p>Agent <code>agent-opus</code> (model <code>example-model-2[1m]</code>) has started ` +
				`working on this task: Kelos Task <a href="https://kelos.example.com/x"><code>vikunja-demo-agent-opus-4201</code></a>, ` +
				`pod <code>vikunja-demo-agent-opus-4201-x7k2p</code>.</p><p>The link shows its latest log lines once you are ` +
				`logged in to the <a href="https://kelos.example.com">Kelos Console</a> (namespace <code>agents</code>).</p>`,
			want: Announcement{
				Role: RoleImplementation, Agent: "agent-opus", Model: "example-model-2[1m]",
				Namespace: "agents", TaskName: "vikunja-demo-agent-opus-4201",
				Pod: "vikunja-demo-agent-opus-4201-x7k2p",
			},
		},
		{
			name: "follow-up",
			body: `<p>Agent <code>agent-sol</code> (model <code>example-model-3</code>) has started on the follow-up ` +
				`request: Kelos Task <a href="https://kelos.example.com/y"><code>vikunja-fu-demo-4201-c9003</code></a>, ` +
				`pod <code>vikunja-fu-demo-4201-c9003-ab12c</code>.</p><p>(namespace <code>agents</code>)</p>`,
			want: Announcement{
				Role: RoleTaskFollowUp, Agent: "agent-sol", Model: "example-model-3",
				Namespace: "agents", TaskName: "vikunja-fu-demo-4201-c9003",
				Pod: "vikunja-fu-demo-4201-c9003-ab12c",
			},
		},
		{
			name: "no agent, model, pod or namespace",
			body: `<p>The agent has started working on this task: Kelos Task <a href="https://kelos.example.com/z">` +
				`<code>vikunja-demo-4202</code></a>.</p>`,
			want: Announcement{Role: RoleImplementation, TaskName: "vikunja-demo-4202"},
		},
		{
			name: "line breaks inside the paragraph",
			body: "<p>Agent <code>agent-opus</code>\nhas started working on this task:\nKelos Task " +
				"<code>vikunja-demo-4203</code>, pod <code>vikunja-demo-4203-zz9yy</code>.</p>",
			want: Announcement{
				Role: RoleImplementation, Agent: "agent-opus",
				TaskName: "vikunja-demo-4203", Pod: "vikunja-demo-4203-zz9yy",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(tt.body)
			if err != nil {
				t.Fatalf("parseAnnouncement: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseAnnouncementRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{"plain comment", `<p>Merged: <a href="https://github.example.com/acme/demo/pull/12">#12</a></p>`, errNotAnnouncement},
		{"empty", "", errNotAnnouncement},
		{"no Kelos Task", "<p>Agent <code>agent-opus</code> has started working on this task.</p>", errUnparsedAnnouncement},
		{"phrase not in the first paragraph", "<p>Hello.</p><p>The agent has started working on this task: " +
			"Kelos Task <code>vikunja-demo-4204</code>.</p>", errUnparsedAnnouncement},
		{"invalid Task name", "<p>The agent has started working on this task: Kelos Task <code>Not_A_Name</code>.</p>",
			errUnparsedAnnouncement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(tt.body)
			if !errors.Is(err, tt.want) {
				t.Errorf("parseAnnouncement = %+v, %v; want error %v", got, err, tt.want)
			}
		})
	}
}

func TestPlainText(t *testing.T) {
	got := plainText("<p>A &amp; <code>B</code></p>\n<p>  C\n  D <br/>E</p>")
	if want := "A & B\nC D\nE"; got != want {
		t.Errorf("plainText = %q, want %q", got, want)
	}
}
