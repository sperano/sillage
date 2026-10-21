package kelos

import (
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
)

// The names, after the configured domain, of the labels the spawners put
// on Tasks. Kelos copies a Task's labels onto its TaskRecord.
const (
	labelProject = "project"
	labelVariant = "variant"
	labelRole    = "role"
	labelTask    = "task"
)

// The values of the role label. A Task the task spawner starts has no role
// label, only a task label.
const (
	roleTaskFollowUp      = "task-followup"
	rolePRFollowUp        = "pr-followup"
	roleWikiFollowUp      = "wiki-followup"
	rolePRConflict        = "pr-conflict"
	rolePROpen            = "pr-open"
	roleMergeClose        = "merge-close"
	roleConflictReconcile = "conflict-reconcile"
)

// discussNamePrefix starts the name of a Task the task spawner starts for
// a task labelled discuss: such a Task carries no label saying so.
const discussNamePrefix = "vikunja-discuss-"

// roleLabels maps each role label to its run role.
var roleLabels = map[string]model.RunRole{
	roleTaskFollowUp:      model.RoleTaskFollowUp,
	rolePRFollowUp:        model.RolePRFollowUp,
	roleWikiFollowUp:      model.RoleWikiFollowUp,
	rolePRConflict:        model.RoleConflictFix,
	rolePROpen:            model.RoleAutomation,
	roleMergeClose:        model.RoleAutomation,
	roleConflictReconcile: model.RoleAutomation,
}

// roleOf derives a run's role from its labels and Task name, and says from
// what. A role label it does not know, or no label at all, is unknown,
// with an empty reason.
func (c Config) roleOf(labels map[string]string, taskName string) (model.RunRole, string) {
	roleKey, taskKey := c.label(labelRole), c.label(labelTask)
	if value, ok := labels[roleKey]; ok {
		if role, known := roleLabels[value]; known {
			return role, labelPath(roleKey) + " " + value
		}
		return model.RoleUnknown, ""
	}
	if labels[taskKey] == "" {
		return model.RoleUnknown, ""
	}
	if strings.HasPrefix(taskName, discussNamePrefix) {
		return model.RoleDiscussion, labelPath(taskKey) + " without " + roleKey + ", name prefix " + discussNamePrefix
	}
	return model.RoleImplementation, labelPath(taskKey) + " without " + roleKey
}

// taskID returns the Vikunja task id the task label holds, and whether it
// holds one.
func (c Config) taskID(labels map[string]string) (int64, bool) {
	id, err := strconv.ParseInt(labels[c.label(labelTask)], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// labelPath is the field path of a label.
func labelPath(key string) string { return "metadata.labels[" + key + "]" }
