package kelostext

import (
	"errors"
	"testing"
)

// testGrammar knows every phrase, as a surface posting all of them would.
var testGrammar = NewGrammar(
	PhraseTaskStartedLowercase, PhraseFollowUpLowercase, PhraseConflictLowercase,
	PhraseTaskStartedColon, PhraseFollowUpColon, PhraseConflictColon,
	PhraseTaskStarted, PhraseFollowUp, PhraseConflict,
)

func TestParseStart(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Start
	}{
		{
			name: "current",
			text: "AGENT agent-opus (MODEL example-model-2[1m]) IS ON THE CASE. THIS ONE HAS THE STUFF. " +
				"KELOS TASK vikunja-demo-agent-opus-4220, POD vikunja-demo-agent-opus-4220-q8w4e.\n" +
				"WANT TO WATCH? OPEN THE KELOS CONSOLE. SIGN IN, SELECT NAMESPACE agents, AND FIND TASK " +
				"vikunja-demo-agent-opus-4220 IN RESOURCES.\nCOFFEE TIME! REPORT TO FOLLOW.",
			want: Start{
				Phrase: PhraseTaskStarted, Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "vikunja-demo-agent-opus-4220", Pod: "vikunja-demo-agent-opus-4220-q8w4e",
			},
		},
		{
			name: "location table, rendered HTML",
			text: "AGENT agent-opus (MODEL example-model-2[1m]) IS ON THE CASE. THIS ONE HAS THE STUFF.\n" +
				"KELOS TASK\nvikunja-demo-agent-opus-4230\nPOD\nvikunja-demo-agent-opus-4230-r5t6y\nNODE\nnode-a\n" +
				"WANT TO WATCH? OPEN THE KELOS CONSOLE. SIGN IN, SELECT NAMESPACE agents, AND FIND TASK " +
				"vikunja-demo-agent-opus-4230 IN RESOURCES.\nTHE WHOLE STORY IS IN THE ACTIVITY TIMELINE.\n" +
				"COFFEE TIME! REPORT TO FOLLOW.",
			want: Start{
				Phrase: PhraseTaskStarted, Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "vikunja-demo-agent-opus-4230", Pod: "vikunja-demo-agent-opus-4230-r5t6y",
			},
		},
		{
			name: "location table, Markdown, job UID",
			text: "AGENT agent-sol IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT. PUT CAUTION IN THE SHOTGUN SEAT.\n\n" +
				"| | |\n|---|---|\n| KELOS TASK | pr-conflict-0a1b2c3d-52-4e5f6a7b-8c9d0e1f |\n" +
				"| JOB UID | 00000000-0000-4000-8000-000000000052 |\n" +
				"| POD | pr-conflict-0a1b2c3d-52-4e5f6a7b-8c9d0e1f-fghij |\n| NODE | node-b |\n\n" +
				"COFFEE TIME! REPORT TO FOLLOW.",
			want: Start{
				Phrase: PhraseConflict, Agent: "agent-sol", TaskName: "pr-conflict-0a1b2c3d-52-4e5f6a7b-8c9d0e1f",
				Pod: "pr-conflict-0a1b2c3d-52-4e5f6a7b-8c9d0e1f-fghij",
			},
		},
		{
			name: "location table, no agent, model or pod",
			text: "AN AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR.\nKELOS TASK\ngithub-fu-widgets-62-c971",
			want: Start{Phrase: PhraseFollowUp, TaskName: "github-fu-widgets-62-c971"},
		},
		{
			name: "current follow-up, no agent, model or pod",
			text: "THE AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. KELOS TASK vikunja-fu-demo-4220-c9102.",
			want: Start{Phrase: PhraseFollowUp, TaskName: "vikunja-fu-demo-4220-c9102"},
		},
		{
			name: "current, an agent",
			text: "AN AGENT IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. KELOS TASK github-fu-widgets-62-c970.",
			want: Start{Phrase: PhraseFollowUp, TaskName: "github-fu-widgets-62-c970"},
		},
		{
			name: "current conflict, apostrophe",
			text: "AGENT agent-sol IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT. PUT CAUTION IN THE SHOTGUN SEAT. " +
				"KELOS TASK pr-conflict-0a1b2c3d-51-4e5f6a7b-8c9d0e1f, POD pr-conflict-0a1b2c3d-51-4e5f6a7b-8c9d0e1f-abcde.",
			want: Start{
				Phrase: PhraseConflict, Agent: "agent-sol", TaskName: "pr-conflict-0a1b2c3d-51-4e5f6a7b-8c9d0e1f",
				Pod: "pr-conflict-0a1b2c3d-51-4e5f6a7b-8c9d0e1f-abcde",
			},
		},
		{
			// "IS ON THE CASE" starts the current phrase: the colon picks it.
			name: "colon",
			text: "AGENT agent-opus (MODEL m) IS ON THE CASE: KELOS TASK vikunja-demo-4221, POD vikunja-demo-4221-zz9yy.\n" +
				"OPEN THE KELOS CONSOLE. SIGN IN, SELECT NAMESPACE agents, AND FIND TASK vikunja-demo-4221 IN RESOURCES.",
			want: Start{
				Phrase: PhraseTaskStartedColon, Agent: "agent-opus", Model: "m", Namespace: "agents",
				TaskName: "vikunja-demo-4221", Pod: "vikunja-demo-4221-zz9yy",
			},
		},
		{
			name: "lowercase",
			text: "Agent agent-opus (model example-model-2[1m]) has started working on this task: Kelos Task " +
				"vikunja-demo-agent-opus-4201, pod vikunja-demo-agent-opus-4201-x7k2p.\nThe link shows its latest " +
				"log lines once you are logged in to the Kelos Console (namespace agents).",
			want: Start{
				Phrase: PhraseTaskStartedLowercase, Agent: "agent-opus", Model: "example-model-2[1m]", Namespace: "agents",
				TaskName: "vikunja-demo-agent-opus-4201", Pod: "vikunja-demo-agent-opus-4201-x7k2p",
			},
		},
		{
			name: "leading blank lines",
			text: "\n  The agent has started on the follow-up request: Kelos Task vikunja-fu-demo-4201-c9003.",
			want: Start{Phrase: PhraseFollowUpLowercase, TaskName: "vikunja-fu-demo-4201-c9003"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testGrammar.Parse(tt.text)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseStartRejects(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{"plain comment", "Merged: #12", ErrNotAnnouncement},
		{"empty", "", ErrNotAnnouncement},
		{"no Kelos Task", "AGENT agent-opus IS ON THE CASE. THIS ONE HAS THE STUFF.", ErrUnparsedAnnouncement},
		{"phrase not in the first line", "Hello.\nTHE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. " +
			"KELOS TASK vikunja-demo-4204.", ErrUnparsedAnnouncement},
		{"invalid Task name", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK Not_A_Name.",
			ErrUnparsedAnnouncement},
		{"text after the sentence", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK vikunja-demo-4204. Also",
			ErrUnparsedAnnouncement},
		{"location table without a Task", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF.\nPOD\nvikunja-demo-4204-abcde",
			ErrUnparsedAnnouncement},
		{"location table, invalid Task name", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF.\nKELOS TASK\nNot_A_Name",
			ErrUnparsedAnnouncement},
		{"location table, invalid pod name", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF.\n" +
			"| KELOS TASK | vikunja-demo-4204 |\n| POD | Not_A_Pod |", ErrUnparsedAnnouncement},
		{"location table after another paragraph", "THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF.\nHello.\n" +
			"KELOS TASK\nvikunja-demo-4204", ErrUnparsedAnnouncement},
		// The first form of 2026-10-04 named no agent; it is not parsed.
		{"Task first", "Kelos Task vikunja-demo-4204 has started working on this task.", ErrUnparsedAnnouncement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testGrammar.Parse(tt.text)
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse = %+v, %v; want error %v", got, err, tt.want)
			}
		})
	}
}

// A grammar knows only its own phrases.
func TestGrammarPhrases(t *testing.T) {
	g := NewGrammar(PhraseFollowUp)
	_, err := g.Parse("THE AGENT IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK vikunja-demo-4204.")
	if !errors.Is(err, ErrNotAnnouncement) {
		t.Errorf("another surface's phrase: error %v, want %v", err, ErrNotAnnouncement)
	}
}

func TestNewGrammarPanics(t *testing.T) {
	for _, phrases := range [][]string{nil, {PhraseFollowUp, ""}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewGrammar(%q) did not panic", phrases)
				}
			}()
			NewGrammar(phrases...)
		}()
	}
}
