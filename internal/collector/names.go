package collector

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// The Kelos Task names the Hollingsworth spawners give, as patterns. A
// name is a hint, never an identity: a retry reuses it. Workspace and
// project names may hold dashes, so each pattern anchors the ids at the
// end.
const (
	// k8sLabelPattern matches a workspace, project or variant name, or
	// several joined by dashes.
	k8sLabelPattern = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`
	// idPattern matches a positive decimal id.
	idPattern = `[1-9][0-9]*`
	// retrySuffix ends the name of a follow-up a reconciler started again.
	retrySuffix = `(?:-retry` + idPattern + `)?`
)

var (
	// taskRunName: vikunja-<project>-<variant>-<task>, and
	// vikunja-discuss-… for a task labelled discuss.
	taskRunName = regexp.MustCompile(`^vikunja-(discuss-)?` + k8sLabelPattern + `-(` + idPattern + `)$`)
	// taskFollowUpName: vikunja-fu-<project>-<task>-c<comment>.
	taskFollowUpName = regexp.MustCompile(`^vikunja-fu-` + k8sLabelPattern + `-(` + idPattern + `)-c` +
		idPattern + retrySuffix + `$`)
	// otherVikunjaName: the bookkeeping Tasks that share the vikunja-
	// prefix and end in a number of their own.
	otherVikunjaName = regexp.MustCompile(`^vikunja-(?:fu|merge|pr-open)-`)
	// prRunNames: pr-followup-<workspace>-<number>-<c|l|r|p><id>,
	// vikunja-merge-<workspace>-<number>-<merged>, and
	// vikunja-pr-open-<workspace>-<number>-<action>.
	prRunNames = []*regexp.Regexp{
		regexp.MustCompile(`^pr-followup-(` + k8sLabelPattern + `)-(` + idPattern + `)-[clrp]` + idPattern +
			retrySuffix + `$`),
		regexp.MustCompile(`^vikunja-merge-(` + k8sLabelPattern + `)-(` + idPattern + `)-(?:true|false)$`),
		regexp.MustCompile(`^vikunja-pr-open-(` + k8sLabelPattern + `)-(` + idPattern + `)-[a-z_]+$`),
	}
)

// taskOfName returns the Vikunja task id a Task name names by the task
// and comment spawners' conventions, and the role it implies.
func taskOfName(name string) (int64, model.RunRole, bool) {
	if m := taskFollowUpName.FindStringSubmatch(name); m != nil {
		return taskOfID(m[1], model.RoleTaskFollowUp)
	}
	if otherVikunjaName.MatchString(name) {
		return 0, "", false
	}
	if m := taskRunName.FindStringSubmatch(name); m != nil {
		if m[1] != "" {
			return taskOfID(m[2], model.RoleDiscussion)
		}
		return taskOfID(m[2], model.RoleImplementation)
	}
	return 0, "", false
}

// taskOfID is taskOfName's result for a name naming task id with role:
// nothing when the id does not parse.
func taskOfID(id string, role model.RunRole) (int64, model.RunRole, bool) {
	n, ok := parseID(id)
	if !ok {
		return 0, "", false
	}
	return n, role, true
}

// prOfName returns the workspace and pull request number a PR
// follow-up's or bookkeeping Task's name names.
func prOfName(name string) (workspace string, number int, ok bool) {
	for _, re := range prRunNames {
		if m := re.FindStringSubmatch(name); m != nil {
			n, err := strconv.Atoi(m[2])
			return m[1], n, err == nil
		}
	}
	return "", 0, false
}

// roleOfName is the role a Task name implies, or unknown.
func roleOfName(name string) model.RunRole {
	if _, role, ok := taskOfName(name); ok {
		return role
	}
	return model.RoleUnknown
}

// parseID parses an id the patterns above matched; they admit only
// positive decimals, so it fails only past int64: such a name names
// nothing.
func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

// The rest of a task's Task name after vikunja-[discuss-]<project>- and
// after vikunja-fu-<project>-, its task id captured.
var (
	taskRunRest      = regexp.MustCompile(`^` + k8sLabelPattern + `-(` + idPattern + `)$`)
	taskFollowUpRest = regexp.MustCompile(`^(` + idPattern + `)-c` + idPattern + retrySuffix + `$`)
)

// projectsOfName returns the names among projects that a Task name of
// task id names as its project by the task and comment spawners'
// conventions, sorted. A project name may hold dashes, so several can
// match.
func projectsOfName(name string, id int64, projects map[string]github.Repository) []string {
	var out []string
	for p := range projects {
		if nameOfProject(name, p, id) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// nameOfProject reports whether name is the name of a Task of task id in
// project p.
func nameOfProject(name, p string, id int64) bool {
	for prefix, rest := range map[string]*regexp.Regexp{
		"vikunja-": taskRunRest, "vikunja-discuss-": taskRunRest, "vikunja-fu-": taskFollowUpRest,
	} {
		tail, ok := strings.CutPrefix(name, prefix+p+"-")
		if !ok {
			continue
		}
		if m := rest.FindStringSubmatch(tail); m != nil {
			if n, ok := parseID(m[1]); ok && n == id {
				return true
			}
		}
	}
	return false
}
