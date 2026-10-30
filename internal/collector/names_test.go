package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// pastInt64 is a decimal id the name patterns admit but int64 cannot hold.
const pastInt64 = "9223372036854775808"

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
		{"vikunja-widgets-agent-opus-" + pastInt64, 0, "", false},
		{"vikunja-fu-widgets-" + pastInt64 + "-c5701", 0, "", false},
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

func TestRoleOfName(t *testing.T) {
	tests := map[string]model.RunRole{
		"vikunja-widgets-agent-opus-1":         model.RoleImplementation,
		"vikunja-discuss-widgets-agent-opus-1": model.RoleDiscussion,
		"vikunja-fu-widgets-1-c2":              model.RoleTaskFollowUp,
		"wiki-fu-widgets-57-c880":              model.RoleUnknown,
		"pr-followup-widgets-7-c1":             model.RoleUnknown,
		"":                                     model.RoleUnknown,
	}
	for name, want := range tests {
		if got := roleOfName(name); got != want {
			t.Errorf("roleOfName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestProjectsOfName(t *testing.T) {
	projects := map[string]github.Repository{
		"widgets": testRepo, "big-widgets": testRepo, "big": testRepo, "discuss": testRepo,
	}
	tests := []struct {
		name string
		id   int64
		want []string
	}{
		{"vikunja-widgets-agent-opus-102", 102, []string{"widgets"}},
		{"vikunja-big-widgets-agent-opus-7", 7, []string{"big", "big-widgets"}},
		{"vikunja-discuss-widgets-agent-opus-101", 101, []string{"discuss", "widgets"}},
		{"vikunja-fu-widgets-102-c5701-retry2", 102, []string{"widgets"}},
		{"vikunja-widgets-agent-opus-102", 103, nil},
		{"vikunja-fu-widgets-102-c5701", 103, nil},
		{"vikunja-gadgets-agent-opus-102", 102, nil},
		{"vikunja-merge-widgets-7-true", 7, nil},
		{"pr-followup-widgets-7-c5701", 7, nil},
		{"vikunja-widgets-102", 102, nil},
		{"vikunja-widgets-agent-opus-" + pastInt64, 0, nil},
	}
	for _, tt := range tests {
		if got := projectsOfName(tt.name, tt.id, projects); !slices.Equal(got, tt.want) {
			t.Errorf("projectsOfName(%q, %d) = %v, want %v", tt.name, tt.id, got, tt.want)
		}
	}
}
