package collector

import (
	"regexp"
	"strconv"

	"github.com/sperano/sillage/internal/model"
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
	// wikiFollowUpName: wiki-fu-<workspace>-<page>-c<comment>.
	wikiFollowUpName = regexp.MustCompile(`^wiki-fu-(` + k8sLabelPattern + `)-(` + idPattern + `)-c(` +
		idPattern + `)` + retrySuffix + `$`)
)

// taskOfName returns the Vikunja task id a Task name names by the task
// and comment spawners' conventions, and the role it implies.
func taskOfName(name string) (int64, model.RunRole, bool) {
	if m := taskFollowUpName.FindStringSubmatch(name); m != nil {
		return parseID(m[1]), model.RoleTaskFollowUp, true
	}
	if otherVikunjaName.MatchString(name) {
		return 0, "", false
	}
	if m := taskRunName.FindStringSubmatch(name); m != nil {
		if m[1] != "" {
			return parseID(m[2]), model.RoleDiscussion, true
		}
		return parseID(m[2]), model.RoleImplementation, true
	}
	return 0, "", false
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

// wikiRequest is what a wiki follow-up's Task name names.
type wikiRequest struct {
	workspace string
	page      int64
	comment   int64
}

// pageOfName returns what a wiki follow-up's Task name names.
func pageOfName(name string) (wikiRequest, bool) {
	m := wikiFollowUpName.FindStringSubmatch(name)
	if m == nil {
		return wikiRequest{}, false
	}
	return wikiRequest{workspace: m[1], page: parseID(m[2]), comment: parseID(m[3])}, true
}

// roleOfName is the role a Task name implies, or unknown.
func roleOfName(name string) model.RunRole {
	if _, role, ok := taskOfName(name); ok {
		return role
	}
	if _, ok := pageOfName(name); ok {
		return model.RoleWikiFollowUp
	}
	return model.RoleUnknown
}

// parseID parses an id the patterns above matched; they admit only
// positive decimals, so it fails only past int64, which yields 0.
func parseID(s string) int64 {
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}
