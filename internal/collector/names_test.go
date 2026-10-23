package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestTaskOfName(t *testing.T) {
	tests := []struct {
		name string
		id   int64
		role model.RunRole
		ok   bool
	}{
		{"vikunja-widgets-agent-opus-102", 102, model.RoleImplementation, true},
		{"vikunja-big-widgets-agent-opus-7", 7, model.RoleImplementation, true},
		{"vikunja-discuss-widgets-agent-opus-101", 101, model.RoleDiscussion, true},
		{"vikunja-fu-widgets-102-c5701", 102, model.RoleTaskFollowUp, true},
		{"vikunja-fu-widgets-102-c5701-retry2", 102, model.RoleTaskFollowUp, true},
		{"vikunja-merge-widgets-7-true", 0, "", false},
		{"vikunja-pr-open-widgets-7-opened", 0, "", false},
		{"vikunja-fu-widgets-c5701", 0, "", false},
		{"pr-followup-widgets-7-c5701", 0, "", false},
		{"wiki-fu-widgets-57-c880", 0, "", false},
		{"vikunja-widgets-agent-opus", 0, "", false},
	}
	for _, tt := range tests {
		id, role, ok := taskOfName(tt.name)
		if id != tt.id || role != tt.role || ok != tt.ok {
			t.Errorf("taskOfName(%q) = %d, %q, %v; want %d, %q, %v", tt.name, id, role, ok, tt.id, tt.role, tt.ok)
		}
	}
}

func TestPROfName(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		number    int
		ok        bool
	}{
		{"pr-followup-widgets-7-c5701", "widgets", 7, true},
		{"pr-followup-big-widgets-12-l99", "big-widgets", 12, true},
		{"pr-followup-widgets-7-r3001", "widgets", 7, true},
		{"pr-followup-widgets-7-p3001-retry1", "widgets", 7, true},
		{"vikunja-merge-widgets-7-true", "widgets", 7, true},
		{"vikunja-merge-widgets-7-false", "widgets", 7, true},
		{"vikunja-pr-open-widgets-8-ready_for_review", "widgets", 8, true},
		{"pr-conflict-deadbeef-42", "", 0, false},
		{"pr-followup-widgets-7-x1", "", 0, false},
		{"vikunja-widgets-agent-opus-7", "", 0, false},
	}
	for _, tt := range tests {
		ws, n, ok := prOfName(tt.name)
		if ws != tt.workspace || n != tt.number || ok != tt.ok {
			t.Errorf("prOfName(%q) = %q, %d, %v; want %q, %d, %v", tt.name, ws, n, ok, tt.workspace, tt.number, tt.ok)
		}
	}
}

func TestPageOfName(t *testing.T) {
	tests := []struct {
		name string
		want wikiRequest
		ok   bool
	}{
		{"wiki-fu-widgets-57-c880", wikiRequest{"widgets", 57, 880}, true},
		{"wiki-fu-big-widgets-57-c880-retry3", wikiRequest{"big-widgets", 57, 880}, true},
		{"wiki-fu-widgets-c880", wikiRequest{}, false},
		{"vikunja-widgets-agent-opus-57", wikiRequest{}, false},
	}
	for _, tt := range tests {
		got, ok := pageOfName(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("pageOfName(%q) = %+v, %v; want %+v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRoleOfName(t *testing.T) {
	tests := map[string]model.RunRole{
		"vikunja-widgets-agent-opus-1":         model.RoleImplementation,
		"vikunja-discuss-widgets-agent-opus-1": model.RoleDiscussion,
		"vikunja-fu-widgets-1-c2":              model.RoleTaskFollowUp,
		"wiki-fu-widgets-57-c880":              model.RoleWikiFollowUp,
		"pr-followup-widgets-7-c1":             model.RoleUnknown,
		"":                                     model.RoleUnknown,
	}
	for name, want := range tests {
		if got := roleOfName(name); got != want {
			t.Errorf("roleOfName(%q) = %q, want %q", name, got, want)
		}
	}
}
