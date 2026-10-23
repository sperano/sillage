package kelostext

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"
)

// A start announcement's first line names the agent, its model, what it
// started and the Kelos Task:
//
//	AGENT X (MODEL Y) IS ON THE CASE. THIS ONE HAS THE STUFF. KELOS TASK NAME, POD POD.
//	AGENT X (MODEL Y) IS ON THE CASE: KELOS TASK NAME, POD POD.
//	Agent X (model Y) has started working on this task: Kelos Task NAME, pod POD.
//
// newest form first. "AGENT X" reads "THE AGENT" or "AN AGENT" when the
// variant is unknown, and the model and pod parts are left out when
// unknown. The action phrase depends on the surface and the run; the
// constants below list each, by the Hollingsworth change that introduced
// it (helm/kelos/scripts/). A later paragraph may name the namespace.

// Action phrases of the first form, lowercase, followed by a colon, until
// Hollingsworth #292. The wiki phrase dates from 2026-10-17, the others
// from 2026-10-05.
const (
	// PhraseTaskStartedLowercase: a Vikunja task's first run.
	PhraseTaskStartedLowercase = "has started working on this task"
	// PhraseFollowUpLowercase: a follow-up on a Vikunja task or a PR.
	PhraseFollowUpLowercase = "has started on the follow-up request"
	// PhraseWikiFollowUpLowercase: a follow-up on a wiki page.
	PhraseWikiFollowUpLowercase = "has started on this follow-up request"
	// PhraseConflictLowercase: a PR's merge conflict resolution.
	PhraseConflictLowercase = "has started resolving the pull request's merge conflict"
)

// Action phrases of Gordon Cole's first voice, uppercase, followed by a
// colon: Hollingsworth #292, merged 2026-10-22 00:17 UTC.
const (
	// PhraseTaskStartedColon: a Vikunja task's first run.
	PhraseTaskStartedColon = "IS ON THE CASE"
	// PhraseFollowUpColon: a follow-up on a Vikunja task or a PR.
	PhraseFollowUpColon = "IS ON THE FOLLOW-UP REQUEST"
	// PhraseWikiFollowUpColon: a follow-up on a wiki page.
	PhraseWikiFollowUpColon = "IS ON THIS FOLLOW-UP REQUEST"
	// PhraseConflictColon: a PR's merge conflict resolution.
	PhraseConflictColon = "IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT"
)

// Action phrases of the current form, two uppercase sentences followed by
// a period: Hollingsworth #296, merged 2026-10-22 02:54 UTC. The follow-up
// phrase is the same on every surface.
const (
	PhraseTaskStarted = "IS ON THE CASE. THIS ONE HAS THE STUFF"
	PhraseFollowUp    = "IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR"
	PhraseConflict    = "IS RESOLVING THE PULL REQUEST'S MERGE CONFLICT. PUT CAUTION IN THE SHOTGUN SEAT"
)

const (
	// K8sName matches a Kubernetes object name (DNS subdomain).
	K8sName = `[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?`
	// K8sLabel matches a namespace name (DNS label).
	K8sLabel = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`
	// leadAgent matches who started: an agent named or not, and its model.
	leadAgent = `^(?:(?:Agent|AGENT) (\S+)|The agent|THE AGENT|An agent|AN AGENT)(?: \((?:model|MODEL) (\S+)\))? `
	// leadTask matches the Kelos Task and pod after the action phrase.
	leadTask = `[:.] (?:Kelos Task|KELOS TASK) (` + K8sName + `)(?:, (?:pod|POD) (` + K8sName + `))?\.$`
)

var (
	// ErrNotAnnouncement reports a text with none of a grammar's phrases.
	ErrNotAnnouncement = errors.New("not a start announcement")
	// ErrUnparsedAnnouncement reports a text with one of a grammar's
	// phrases whose first line does not have the expected form.
	ErrUnparsedAnnouncement = errors.New("start announcement not in the expected form")

	// namespaceNote matches the namespace as the lowercase form's console
	// paragraph names it, "(namespace NS)", or as the uppercase one's does,
	// "SELECT NAMESPACE NS".
	namespaceNote = regexp.MustCompile(`\(namespace (` + K8sLabel + `)\)|SELECT NAMESPACE (` + K8sLabel + `)\b`)
)

// Start is what a start announcement's text says about its run. It names
// the Kelos Task, not its UID: a retry can reuse the name, so the Kelos
// source must resolve it (the pod name is per attempt) before it
// identifies a run.
type Start struct {
	// Phrase is the action phrase, as one of the Phrase constants.
	Phrase string
	// Agent and Model are empty when the announcement does not name them.
	Agent string
	Model string
	// Namespace is the Kelos Task's namespace; empty if not stated.
	Namespace string
	TaskName  string
	// Pod is empty when the announcement names no pod.
	Pod string
}

// Grammar parses the start announcements of one surface: the first line's
// form above, with one of the surface's action phrases.
type Grammar struct {
	phrases []string
	lead    *regexp.Regexp
}

// NewGrammar returns the grammar of announcements that use one of phrases.
func NewGrammar(phrases ...string) *Grammar {
	sorted := slices.Clone(phrases)
	// Longest first, so a phrase that starts another is tried after it.
	slices.SortFunc(sorted, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	quoted := make([]string, len(sorted))
	for i, p := range sorted {
		quoted[i] = regexp.QuoteMeta(p)
	}
	return &Grammar{
		phrases: sorted,
		lead:    regexp.MustCompile(leadAgent + `(` + strings.Join(quoted, "|") + `)` + leadTask),
	}
}

// Parse parses a comment's plain text, one line per paragraph. It returns
// ErrNotAnnouncement when the text has none of g's phrases, and
// ErrUnparsedAnnouncement when it has one but its first line does not
// have the expected form.
func (g *Grammar) Parse(text string) (Start, error) {
	if !slices.ContainsFunc(g.phrases, func(p string) bool { return strings.Contains(text, p) }) {
		return Start{}, ErrNotAnnouncement
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	m := g.lead.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil {
		return Start{}, ErrUnparsedAnnouncement
	}
	return Start{
		Agent:     m[1],
		Model:     m[2],
		Phrase:    m[3],
		TaskName:  m[4],
		Pod:       m[5],
		Namespace: namespace(text),
	}, nil
}

// namespace returns the namespace text names, or "".
func namespace(text string) string {
	m := namespaceNote.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return cmp.Or(m[1], m[2])
}
