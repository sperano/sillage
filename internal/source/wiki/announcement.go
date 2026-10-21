package wiki

import (
	"errors"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A wiki follow-up's start announcement is posted by the agent's own wiki
// user. Its rendered HTML reads
//
//	<p>Agent <code>AGENT</code> (model <code>MODEL</code>) has started on
//	this follow-up request: Kelos Task <a href="..."><code>NAME</code></a>,
//	pod <code>POD</code>.</p><p>... (namespace <code>NAMESPACE</code>).</p>
//
// "Agent <code>AGENT</code>" reads "The agent" when the variant is unknown,
// and the model and pod parts are left out when unknown. NAME is
// wiki-fu-<workspace>-<page id>-c<comment id>, with -retry<n> appended to
// a retry.

const (
	// phraseFollowUp is the announcement's verb.
	phraseFollowUp = "has started on this follow-up request"
	// requestPrefix starts a comment asking an agent to act.
	requestPrefix = "/kelos"
	// mentionPrefix starts the agent a request names: /kelos @<name>.
	mentionPrefix = "@"
	// k8sName matches a Kubernetes object name (DNS subdomain).
	k8sName = `[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?`
	// k8sLabel matches a namespace or workspace name (DNS label).
	k8sLabel = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`
	// idPattern matches a positive decimal id.
	idPattern = `[1-9][0-9]*`
)

var (
	// errNotAnnouncement reports a comment without the start phrase.
	errNotAnnouncement = errors.New("not a start announcement")
	// errUnparsedAnnouncement reports a comment with the start phrase that
	// does not name a Kelos Task of the page in the expected form.
	errUnparsedAnnouncement = errors.New("start announcement not in the expected form")

	paragraphBreak = regexp.MustCompile(`(?i)</p\s*>|<br\s*/?>`)
	htmlTag        = regexp.MustCompile(`<[^>]*>`)
	leadSentence   = regexp.MustCompile(`^(?:Agent (\S+)|The agent)(?: \(model (\S+)\))? ` +
		regexp.QuoteMeta(phraseFollowUp) + `: Kelos Task (` + k8sName + `)(?:, pod (` + k8sName + `))?\.$`)
	namespaceNote = regexp.MustCompile(`\(namespace (` + k8sLabel + `)\)`)
	// taskName is a wiki follow-up's Kelos Task name. The workspace is
	// matched greedily: the page and comment ids are the last two parts.
	taskName = regexp.MustCompile(`^wiki-fu-(` + k8sLabel + `)-(` + idPattern + `)-c(` + idPattern + `)(?:-retry` +
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
// errNotAnnouncement when the text has no start phrase, and
// errUnparsedAnnouncement when it has one but its first line does not
// have the expected form or its Task name names another page than pageID.
func parseAnnouncement(text string, pageID int64) (Announcement, error) {
	if !strings.Contains(text, phraseFollowUp) {
		return Announcement{}, errNotAnnouncement
	}
	first, _, _ := strings.Cut(text, "\n")
	m := leadSentence.FindStringSubmatch(first)
	if m == nil {
		return Announcement{}, errUnparsedAnnouncement
	}
	name := taskName.FindStringSubmatch(m[3])
	if name == nil || name[2] != strconv.FormatInt(pageID, 10) {
		return Announcement{}, errUnparsedAnnouncement
	}
	request, err := strconv.ParseInt(name[3], 10, 64)
	if err != nil {
		return Announcement{}, errUnparsedAnnouncement
	}
	a := Announcement{
		Agent:          m[1],
		Model:          m[2],
		TaskName:       m[3],
		Pod:            m[4],
		Workspace:      name[1],
		RequestComment: request,
	}
	if ns := namespaceNote.FindStringSubmatch(text); ns != nil {
		a.Namespace = ns[1]
	}
	return a, nil
}

// parseRequest reports whether a comment's plain text starts with
// requestPrefix as a word, and returns the agent it names with
// /kelos @<name>, if any.
func parseRequest(text string) (agent string, ok bool) {
	rest, ok := strings.CutPrefix(text, requestPrefix)
	if !ok {
		return "", false
	}
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && !unicode.IsSpace(r) {
		return "", false
	}
	if fields := strings.Fields(rest); len(fields) > 0 {
		if name, mentioned := strings.CutPrefix(fields[0], mentionPrefix); mentioned {
			agent = name
		}
	}
	return agent, true
}

// plainText turns rendered comment HTML into text: one line per
// paragraph, tags dropped, entities decoded, runs of spaces collapsed.
func plainText(body string) string {
	// Newlines in HTML are spaces; only paragraph breaks end a line.
	body = strings.NewReplacer("\r", " ", "\n", " ").Replace(body)
	lines := strings.Split(paragraphBreak.ReplaceAllString(body, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = html.UnescapeString(htmlTag.ReplaceAllString(line, ""))
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
