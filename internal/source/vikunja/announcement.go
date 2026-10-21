package vikunja

import (
	"errors"
	"html"
	"regexp"
	"strings"
)

// The start announcement Kelos posts on a Vikunja task is HTML of the form
//
//	<p>Agent <code>AGENT</code> (model <code>MODEL</code>) has started working
//	on this task: Kelos Task <a href="..."><code>NAME</code></a>, pod
//	<code>POD</code>.</p><p>... (namespace <code>NAMESPACE</code>).</p>
//
// "Agent <code>AGENT</code>" reads "The agent" when the variant is unknown,
// and the model and pod parts are left out when unknown. A follow-up run
// says "has started on the follow-up request" instead.

const (
	// phraseImplementation and phraseFollowUp are the announcement verbs.
	phraseImplementation = "has started working on this task"
	phraseFollowUp       = "has started on the follow-up request"
	// k8sName matches a Kubernetes object name (DNS subdomain).
	k8sName = `[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?`
	// k8sLabel matches a namespace name (DNS label).
	k8sLabel = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`
)

var (
	// errNotAnnouncement reports a comment with neither start phrase.
	errNotAnnouncement = errors.New("not a start announcement")
	// errUnparsedAnnouncement reports a comment with a start phrase whose
	// first paragraph does not have the expected form.
	errUnparsedAnnouncement = errors.New("start announcement not in the expected form")

	paragraphBreak = regexp.MustCompile(`(?i)</p\s*>|<br\s*/?>`)
	htmlTag        = regexp.MustCompile(`<[^>]*>`)
	leadSentence   = regexp.MustCompile(`^(?:Agent (\S+)|The agent)(?: \(model (\S+)\))? (` +
		regexp.QuoteMeta(phraseImplementation) + `|` + regexp.QuoteMeta(phraseFollowUp) +
		`): Kelos Task (` + k8sName + `)(?:, pod (` + k8sName + `))?\.$`)
	namespaceNote = regexp.MustCompile(`\(namespace (` + k8sLabel + `)\)`)
)

// rolesByPhrase maps each start phrase to the run role it announces.
var rolesByPhrase = map[string]Role{
	phraseImplementation: RoleImplementation,
	phraseFollowUp:       RoleTaskFollowUp,
}

// parseAnnouncement parses a comment body. It returns errNotAnnouncement
// when the body has no start phrase and errUnparsedAnnouncement when it has
// one but the rest does not match.
func parseAnnouncement(body string) (Announcement, error) {
	text := plainText(body)
	if !strings.Contains(text, phraseImplementation) && !strings.Contains(text, phraseFollowUp) {
		return Announcement{}, errNotAnnouncement
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	m := leadSentence.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil {
		return Announcement{}, errUnparsedAnnouncement
	}
	a := Announcement{
		Role:     rolesByPhrase[m[3]],
		Agent:    m[1],
		Model:    m[2],
		TaskName: m[4],
		Pod:      m[5],
	}
	if ns := namespaceNote.FindStringSubmatch(text); ns != nil {
		a.Namespace = ns[1]
	}
	return a, nil
}

// plainText turns comment HTML into text: one line per paragraph, tags
// dropped, entities decoded, runs of spaces collapsed.
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
