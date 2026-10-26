package vikunja

import (
	"errors"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/htmltext"
	"github.com/sperano/sillage/internal/source/kelostext"
)

func TestParseAnnouncement(t *testing.T) {
	tests := []struct {
		name string
		body string
		want announcement
	}{
		{
			name: "implementation",
			body: `<p>AGENT <code>agent-opus</code> (MODEL <code>example-model-2[1m]</code>) IS ON THE CASE. THIS ONE HAS ` +
				`THE STUFF. KELOS TASK <code>vikunja-demo-agent-opus-4220</code>, POD <code>vikunja-demo-agent-opus-4220-q8w4e</code>.</p>` +
				`<p>WANT TO WATCH? OPEN THE <a href="https://kelos.example.com">KELOS CONSOLE</a>. SIGN IN, SELECT NAMESPACE ` +
				`<code>agents</code>, AND FIND TASK <code>vikunja-demo-agent-opus-4220</code> IN RESOURCES.</p>` +
				`<p>COFFEE TIME! REPORT TO FOLLOW.</p>`,
			want: announcement{
				role: model.RoleImplementation, agent: "agent-opus", model: "example-model-2[1m]",
				namespace: "agents", taskName: "vikunja-demo-agent-opus-4220",
				pod: "vikunja-demo-agent-opus-4220-q8w4e",
			},
		},
		{
			name: "location table",
			body: `<p>AGENT <code>agent-opus</code> (MODEL <code>example-model-2[1m]</code>) IS ON THE CASE. THIS ONE HAS ` +
				`THE STUFF.</p><table><tbody><tr><td>KELOS TASK</td><td><code>vikunja-demo-agent-opus-4230</code></td></tr>` +
				`<tr><td>POD</td><td><code>vikunja-demo-agent-opus-4230-r5t6y</code></td></tr>` +
				`<tr><td>NODE</td><td><code>node-a</code></td></tr></tbody></table>` +
				`<p>WANT TO WATCH? OPEN THE <a href="https://kelos.example.com">KELOS CONSOLE</a>. SIGN IN, SELECT NAMESPACE ` +
				`<code>agents</code>, AND FIND TASK <code>vikunja-demo-agent-opus-4230</code> IN RESOURCES.</p>` +
				`<p>THE WHOLE STORY IS IN THE <a href="https://sillage.example.com/task/vikunja.example.com/4230">` +
				`ACTIVITY TIMELINE</a>.</p><p>FOLLOW-UPS, LABELS AND AGENT USERS: THE ` +
				`<a href="https://vikunja.example.com/tasks/1">KELOS CHEATSHEET</a>.</p><p>COFFEE TIME! REPORT TO FOLLOW.</p>`,
			want: announcement{
				role: model.RoleImplementation, agent: "agent-opus", model: "example-model-2[1m]",
				namespace: "agents", taskName: "vikunja-demo-agent-opus-4230",
				pod: "vikunja-demo-agent-opus-4230-r5t6y",
			},
		},
		{
			name: "follow-up",
			body: `<p>AGENT <code>agent-sol</code> (MODEL <code>example-model-3</code>) IS ON YOUR FOLLOW-UP REQUEST. ` +
				`HEARD YOU LOUD AND CLEAR. KELOS TASK <code>vikunja-fu-demo-4220-c9102</code>, POD ` +
				`<code>vikunja-fu-demo-4220-c9102-mn3bv</code>.</p><p>COFFEE TIME! CATCH YOU LATER.</p>`,
			want: announcement{
				role: model.RoleTaskFollowUp, agent: "agent-sol", model: "example-model-3",
				taskName: "vikunja-fu-demo-4220-c9102", pod: "vikunja-fu-demo-4220-c9102-mn3bv",
			},
		},
		{
			name: "colon, follow-up",
			body: `<p>THE AGENT IS ON THE FOLLOW-UP REQUEST: KELOS TASK <code>vikunja-fu-demo-4220-c9104</code>.</p>` +
				`<p>OPEN THE <a href="https://kelos.example.com">KELOS CONSOLE</a>. SIGN IN, SELECT NAMESPACE ` +
				`<code>agents</code>, AND FIND TASK <code>vikunja-fu-demo-4220-c9104</code> IN RESOURCES.</p>`,
			want: announcement{role: model.RoleTaskFollowUp, namespace: "agents", taskName: "vikunja-fu-demo-4220-c9104"},
		},
		{
			name: "colon, implementation",
			body: `<p>AGENT <code>agent-opus</code> IS ON THE CASE: KELOS TASK <code>vikunja-demo-4221</code>.</p>`,
			want: announcement{role: model.RoleImplementation, agent: "agent-opus", taskName: "vikunja-demo-4221"},
		},
		{
			name: "lowercase implementation",
			body: `<p>Agent <code>agent-opus</code> (model <code>example-model-2[1m]</code>) has started ` +
				`working on this task: Kelos Task <a href="https://kelos.example.com/x"><code>vikunja-demo-agent-opus-4201</code></a>, ` +
				`pod <code>vikunja-demo-agent-opus-4201-x7k2p</code>.</p><p>The link shows its latest log lines once you are ` +
				`logged in to the <a href="https://kelos.example.com">Kelos Console</a> (namespace <code>agents</code>).</p>`,
			want: announcement{
				role: model.RoleImplementation, agent: "agent-opus", model: "example-model-2[1m]",
				namespace: "agents", taskName: "vikunja-demo-agent-opus-4201",
				pod: "vikunja-demo-agent-opus-4201-x7k2p",
			},
		},
		{
			name: "lowercase follow-up",
			body: `<p>Agent <code>agent-sol</code> (model <code>example-model-3</code>) has started on the follow-up ` +
				`request: Kelos Task <a href="https://kelos.example.com/y"><code>vikunja-fu-demo-4201-c9003</code></a>, ` +
				`pod <code>vikunja-fu-demo-4201-c9003-ab12c</code>.</p><p>(namespace <code>agents</code>)</p>`,
			want: announcement{
				role: model.RoleTaskFollowUp, agent: "agent-sol", model: "example-model-3",
				namespace: "agents", taskName: "vikunja-fu-demo-4201-c9003",
				pod: "vikunja-fu-demo-4201-c9003-ab12c",
			},
		},
		{
			name: "no agent, model, pod or namespace",
			body: `<p>The agent has started working on this task: Kelos Task <a href="https://kelos.example.com/z">` +
				`<code>vikunja-demo-4202</code></a>.</p>`,
			want: announcement{role: model.RoleImplementation, taskName: "vikunja-demo-4202"},
		},
		{
			name: "line breaks inside the paragraph",
			body: "<p>AGENT <code>agent-opus</code>\nIS ON THE CASE.\nTHIS ONE HAS THE STUFF. KELOS TASK " +
				"<code>vikunja-demo-4203</code>, POD <code>vikunja-demo-4203-zz9yy</code>.</p>",
			want: announcement{
				role: model.RoleImplementation, agent: "agent-opus",
				taskName: "vikunja-demo-4203", pod: "vikunja-demo-4203-zz9yy",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(htmltext.Plain(tt.body))
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
		{"plain comment", `<p>Merged: <a href="https://github.example.com/acme/demo/pull/12">#12</a></p>`,
			kelostext.ErrNotAnnouncement},
		{"empty", "", kelostext.ErrNotAnnouncement},
		{"a wiki phrase", "<p>AGENT <code>agent-opus</code> IS ON THIS FOLLOW-UP REQUEST: KELOS TASK " +
			"<code>wiki-fu-widgets-57-c1</code>.</p>", kelostext.ErrNotAnnouncement},
		{"no Kelos Task", "<p>AGENT <code>agent-opus</code> IS ON THE CASE. THIS ONE HAS THE STUFF.</p>",
			kelostext.ErrUnparsedAnnouncement},
		{"lowercase, no Kelos Task", "<p>Agent <code>agent-opus</code> has started working on this task.</p>",
			kelostext.ErrUnparsedAnnouncement},
		{"phrase not in the first paragraph", "<p>Hello.</p><p>THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. " +
			"KELOS TASK <code>vikunja-demo-4204</code>.</p>", kelostext.ErrUnparsedAnnouncement},
		{"invalid Task name", "<p>The agent has started working on this task: Kelos Task <code>Not_A_Name</code>.</p>",
			kelostext.ErrUnparsedAnnouncement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(htmltext.Plain(tt.body))
			if !errors.Is(err, tt.want) {
				t.Errorf("parseAnnouncement = %+v, %v; want error %v", got, err, tt.want)
			}
		})
	}
}
