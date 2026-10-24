package kelos

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestRoleOf(t *testing.T) {
	const (
		role = testDomain + "/role"
		task = testDomain + "/task"
	)
	tests := []struct {
		name     string
		labels   map[string]string
		taskName string
		want     model.RunRole
	}{
		{"implementation", map[string]string{task: "7"}, "vikunja-widgets-agent-opus-7", model.RoleImplementation},
		{"discussion", map[string]string{task: "7"}, "vikunja-discuss-widgets-agent-opus-7", model.RoleDiscussion},
		{"task follow-up", map[string]string{task: "7", role: "task-followup"}, "x", model.RoleTaskFollowUp},
		{"pr follow-up", map[string]string{role: "pr-followup"}, "x", model.RolePRFollowUp},
		{"wiki follow-up", map[string]string{role: "wiki-followup"}, "x", model.RoleWikiFollowUp},
		{"conflict", map[string]string{role: "pr-conflict"}, "x", model.RoleConflictFix},
		{"pr open", map[string]string{role: "pr-open"}, "x", model.RoleAutomation},
		{"merge close", map[string]string{role: "merge-close"}, "x", model.RoleAutomation},
		{"conflict reconcile", map[string]string{role: "conflict-reconcile"}, "x", model.RoleAutomation},
		{"unknown role", map[string]string{task: "7", role: "reviewer"}, "x", model.RoleUnknown},
		{"task label not an id", map[string]string{task: "seven"}, "vikunja-widgets-agent-opus-7", model.RoleUnknown},
		{"empty role", map[string]string{task: "7", role: ""}, "x", model.RoleUnknown},
		{"no labels", nil, "vikunja-widgets-agent-opus-7", model.RoleUnknown},
		{"another domain", map[string]string{"other.example.com/task": "7"}, "x", model.RoleUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, why := testConfig().roleOf(tt.labels, tt.taskName)
			if got != tt.want {
				t.Errorf("role = %q, want %q", got, tt.want)
			}
			if (why == "") != (got == model.RoleUnknown) {
				t.Errorf("reason %q for role %q: want one exactly when the role is known", why, got)
			}
		})
	}
}

func TestTaskID(t *testing.T) {
	tests := []struct {
		value string
		want  int64
		ok    bool
	}{
		{"204", 204, true},
		{"", 0, false},
		{"0", 0, false},
		{"-3", 0, false},
		{"12a", 0, false},
	}
	for _, tt := range tests {
		got, ok := testConfig().TaskID(map[string]string{testDomain + "/task": tt.value})
		if got != tt.want || ok != tt.ok {
			t.Errorf("TaskID(%q) = %d, %v, want %d, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}
