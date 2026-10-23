package wiki

import (
	"regexp"
	"strconv"

	"github.com/sperano/sillage/internal/source/kelostext"
)

// A wiki follow-up's start announcement is posted by the agent's own wiki
// user, in Markdown that Wiki.js renders. Its first paragraph is the lead
// sentence kelostext parses; a later one names the namespace. Since
// 2026-10-22 it reads
//
//	<p>AGENT <code>AGENT</code> (MODEL <code>MODEL</code>) IS ON YOUR
//	FOLLOW-UP REQUEST. HEARD YOU LOUD AND CLEAR. KELOS TASK
//	<code>NAME</code>, POD <code>POD</code>.</p><p>... SELECT NAMESPACE
//	<code>NAMESPACE</code> ...</p><p>COFFEE TIME! REPORT TO FOLLOW.</p>
//
// and before, from 2026-10-17,
//
//	<p>Agent <code>AGENT</code> (model <code>MODEL</code>) has started on
//	this follow-up request: Kelos Task <a href="..."><code>NAME</code></a>,
//	pod <code>POD</code>.</p><p>... (namespace <code>NAMESPACE</code>).</p>
//
// NAME is wiki-fu-<workspace>-<page id>-c<comment id>, with -retry<n>
// appended to a retry.

// idPattern matches a positive decimal id.
const idPattern = `[1-9][0-9]*`

var (
	// grammar parses the start announcements posted on the wiki.
	grammar = kelostext.NewGrammar(
		kelostext.PhraseWikiFollowUpLowercase,
		kelostext.PhraseWikiFollowUpColon,
		kelostext.PhraseFollowUp,
	)
	// taskName is a wiki follow-up's Kelos Task name. The workspace, a
	// DNS label, is matched greedily: the page and comment ids are the
	// last two parts.
	taskName = regexp.MustCompile(`^wiki-fu-(` + kelostext.K8sLabel + `)-(` + idPattern + `)-c(` + idPattern + `)(?:-retry` +
		idPattern + `)?$`)
)

// Announcement is what a start announcement says about its run. It names
// the Kelos Task, not its UID: a retry can reuse the name, so the Kelos
// source must resolve it (the pod name is per attempt) before it
// identifies a run.
type Announcement struct {
	// Agent and Model are empty when the announcement does not name them.
	Agent string
	Model string
	// Namespace is the Kelos Task's namespace; empty if not stated.
	Namespace string
	TaskName  string
	// Pod is empty when the announcement names no pod.
	Pod string
	// Workspace is the Kelos workspace the Task name names: the
	// repository the follow-up works on.
	Workspace string
	// RequestComment is the id of the comment that asked for the run.
	RequestComment int64
}

// parseAnnouncement parses a comment's plain text. It returns
// kelostext.ErrNotAnnouncement when the text has no start phrase, and
// kelostext.ErrUnparsedAnnouncement when it has one but its first line
// does not have the expected form or its Task name names another page
// than pageID.
func parseAnnouncement(text string, pageID int64) (Announcement, error) {
	s, err := grammar.Parse(text)
	if err != nil {
		return Announcement{}, err
	}
	name := taskName.FindStringSubmatch(s.TaskName)
	if name == nil || name[2] != strconv.FormatInt(pageID, 10) {
		return Announcement{}, kelostext.ErrUnparsedAnnouncement
	}
	request, err := strconv.ParseInt(name[3], 10, 64)
	if err != nil {
		return Announcement{}, kelostext.ErrUnparsedAnnouncement
	}
	return Announcement{
		Agent:          s.Agent,
		Model:          s.Model,
		Namespace:      s.Namespace,
		TaskName:       s.TaskName,
		Pod:            s.Pod,
		Workspace:      name[1],
		RequestComment: request,
	}, nil
}
