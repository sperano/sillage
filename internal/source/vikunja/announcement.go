package vikunja

import (
	"maps"
	"slices"

	"github.com/sperano/sillage/internal/source/kelostext"
)

// The start announcement Kelos posts on a Vikunja task, as the service
// account, is HTML. Its first paragraph is the lead sentence kelostext
// parses; a later one names the namespace. Since 2026-10-22 it reads
//
//	<p>AGENT <code>AGENT</code> (MODEL <code>MODEL</code>) IS ON THE CASE.
//	THIS ONE HAS THE STUFF. KELOS TASK <code>NAME</code>, POD
//	<code>POD</code>.</p><p>... SELECT NAMESPACE <code>NAMESPACE</code>
//	...</p>...<p>COFFEE TIME! REPORT TO FOLLOW.</p>
//
// and before, from 2026-10-05,
//
//	<p>Agent <code>AGENT</code> (model <code>MODEL</code>) has started
//	working on this task: Kelos Task <a href="..."><code>NAME</code></a>,
//	pod <code>POD</code>.</p><p>... (namespace <code>NAMESPACE</code>).</p>
//
// A follow-up run's phrase is the follow-up one of the same form.

// rolesByPhrase maps each start phrase posted on Vikunja to the run role
// it announces.
var rolesByPhrase = map[string]Role{
	kelostext.PhraseTaskStartedLowercase: RoleImplementation,
	kelostext.PhraseTaskStartedColon:     RoleImplementation,
	kelostext.PhraseTaskStarted:          RoleImplementation,
	kelostext.PhraseFollowUpLowercase:    RoleTaskFollowUp,
	kelostext.PhraseFollowUpColon:        RoleTaskFollowUp,
	kelostext.PhraseFollowUp:             RoleTaskFollowUp,
}

// grammar parses the announcements of rolesByPhrase.
var grammar = kelostext.NewGrammar(slices.Collect(maps.Keys(rolesByPhrase))...)

// parseAnnouncement parses a comment's plain text. It returns
// kelostext.ErrNotAnnouncement when the text has no start phrase and
// kelostext.ErrUnparsedAnnouncement when it has one but the rest does not
// match.
func parseAnnouncement(text string) (Announcement, error) {
	s, err := grammar.Parse(text)
	if err != nil {
		return Announcement{}, err
	}
	return Announcement{
		Role:      rolesByPhrase[s.Phrase],
		Agent:     s.Agent,
		Model:     s.Model,
		Namespace: s.Namespace,
		TaskName:  s.TaskName,
		Pod:       s.Pod,
	}, nil
}
