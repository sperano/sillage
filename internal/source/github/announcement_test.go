package github

import (
	"errors"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelostext"
)

// testConflictTask is a conflict Task name on pull request 51.
const testConflictTask = "pr-conflict-0a1b2c3d-51-4e5f6a7b-8c9d0e1f"

func TestParseAnnouncement(t *testing.T) {
	tests := []struct {
		name string
		body string
		want announcement
	}{
		{
			name: "current follow-up",
			body: "AGENT `agent-sol` (MODEL `example-model-3`) IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. " +
				"KELOS TASK `pr-followup-widgets-51-c5301`, POD `pr-followup-widgets-51-c5301-x2c4v`.\n\n" +
				"COFFEE TIME! REPORT TO FOLLOW.",
			want: announcement{Role: model.RolePRFollowUp, Start: kelostext.Start{
				Phrase: kelostext.PhraseFollowUp, Agent: "agent-sol", Model: "example-model-3",
				TaskName: "pr-followup-widgets-51-c5301", Pod: "pr-followup-widgets-51-c5301-x2c4v",
			}},
		},
		{
			name: "location table",
			body: "AGENT `agent-sol` (MODEL `example-model-3`) IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR.\n\n" +
				"| | |\n|---|---|\n| KELOS TASK | `pr-followup-widgets-51-c5302` |\n" +
				"| JOB UID | `00000000-0000-4000-8000-000000005302` |\n| POD | `pr-followup-widgets-51-c5302-y7u8i` |\n" +
				"| NODE | `node-a` |\n\nCOFFEE TIME! REPORT TO FOLLOW.",
			want: announcement{Role: model.RolePRFollowUp, Start: kelostext.Start{
				Phrase: kelostext.PhraseFollowUp, Agent: "agent-sol", Model: "example-model-3",
				TaskName: "pr-followup-widgets-51-c5302", Pod: "pr-followup-widgets-51-c5302-y7u8i",
			}},
		},
		{
			name: "location table, conflict, CRLF",
			body: "AGENT `agent-opus` IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT. PUT CAUTION IN THE SHOTGUN SEAT.\r\n\r\n" +
				"| | |\r\n|---|---|\r\n| KELOS TASK | `" + testConflictTask + "` |\r\n\r\nCOFFEE TIME! REPORT TO FOLLOW.",
			want: announcement{Role: model.RoleConflictFix, Start: kelostext.Start{
				Phrase: kelostext.PhraseConflict, Agent: "agent-opus", TaskName: testConflictTask,
			}},
		},
		{
			name: "current conflict, retry, CRLF",
			body: "AGENT `agent-opus` IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT. PUT CAUTION IN THE SHOTGUN SEAT. " +
				"KELOS TASK `" + testConflictTask + "-retry1`.\r\n\r\nCOFFEE TIME! REPORT TO FOLLOW.",
			want: announcement{Role: model.RoleConflictFix, Start: kelostext.Start{
				Phrase: kelostext.PhraseConflict, Agent: "agent-opus", TaskName: testConflictTask + "-retry1",
			}},
		},
		{
			name: "colon, console paragraph",
			body: "THE AGENT IS ON THE FOLLOW-UP REQUEST: KELOS TASK `pr-followup-widgets-51-r3101`, POD " +
				"`pr-followup-widgets-51-r3101-abcde`.\n\nOPEN THE [KELOS CONSOLE](https://kelos.example.com). SIGN IN, " +
				"SELECT NAMESPACE `agents`, AND FIND TASK `pr-followup-widgets-51-r3101` IN RESOURCES. I'LL GET THE COFFEE.",
			want: announcement{Role: model.RolePRFollowUp, Start: kelostext.Start{
				Phrase: kelostext.PhraseFollowUpColon, Namespace: "agents",
				TaskName: "pr-followup-widgets-51-r3101", Pod: "pr-followup-widgets-51-r3101-abcde",
			}},
		},
		{
			name: "lowercase, linked Task",
			body: "Agent `agent-opus` (model `example-model-2[1m]`) has started on the follow-up request: " +
				"[Kelos Task `pr-followup-widgets-51-l5401`](https://kelos.example.com/api/resources/tasks/agents/" +
				"pr-followup-widgets-51-l5401/logs), pod `pr-followup-widgets-51-l5401-fghij`.\n\nThe link shows its " +
				"latest log lines once you are logged in to the [Kelos Console](https://kelos.example.com) (namespace `agents`).",
			want: announcement{Role: model.RolePRFollowUp, Start: kelostext.Start{
				Phrase: kelostext.PhraseFollowUpLowercase, Agent: "agent-opus", Model: "example-model-2[1m]",
				Namespace: "agents", TaskName: "pr-followup-widgets-51-l5401", Pod: "pr-followup-widgets-51-l5401-fghij",
			}},
		},
		{
			name: "lowercase conflict",
			body: "The agent has started resolving the pull request's merge conflict: Kelos Task `" + testConflictTask + "`.",
			want: announcement{Role: model.RoleConflictFix, Start: kelostext.Start{
				Phrase: kelostext.PhraseConflictLowercase, TaskName: testConflictTask,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(tt.body, 51)
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
		{"plain comment", "Preview deployed.", kelostext.ErrNotAnnouncement},
		{"a Vikunja phrase", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK `pr-followup-widgets-51-c1`.",
			kelostext.ErrNotAnnouncement},
		{"another pull request", "THE AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. " +
			"KELOS TASK `pr-followup-widgets-52-c1`.", kelostext.ErrUnparsedAnnouncement},
		{"a Vikunja follow-up's Task", "THE AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. " +
			"KELOS TASK `vikunja-fu-widgets-51-c1`.", kelostext.ErrUnparsedAnnouncement},
		{"follow-up phrase, conflict Task", "THE AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. " +
			"KELOS TASK `" + testConflictTask + "`.", kelostext.ErrUnparsedAnnouncement},
		{"quoted", "> THE AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. " +
			"KELOS TASK `pr-followup-widgets-51-c1`.\n\nWhy two?", kelostext.ErrUnparsedAnnouncement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnnouncement(tt.body, 51)
			if !errors.Is(err, tt.want) {
				t.Errorf("parseAnnouncement = %+v, %v; want error %v", got, err, tt.want)
			}
		})
	}
}
