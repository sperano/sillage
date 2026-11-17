package vikunja

import (
	"maps"
	"slices"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelostext"
)

// The start announcement Kelos posts on a Vikunja task, as the service
// account, is HTML. Its short form (Hollingsworth #524) is an opener, a
// table naming the model (when known), the Kelos Task and the node (when
// known), the links (when any) and the closer:
//
//	<p>THIS ONE HAS THE STUFF!</p><table><tbody><tr><td>MODEL</td>
//	<td><code>MODEL</code></td></tr><tr><td>KELOS TASK</td>
//	<td><code>NAME</code></td></tr><tr><td>NODE</td><td><code>NODE</code>
//	</td></tr></tbody></table><p><a href="...">lucarne</a>
//	<a href="...">sillage</a></p><p>COFFEE TIME! REPORT TO FOLLOW.</p>
//
// It names no agent, pod or namespace: the collector joins it to its run by
// the Task name and when it was posted. A follow-up opens with HEARD YOU
// LOUD AND CLEAR! instead.
//
// The long form's first paragraph is the lead sentence kelostext parses;
// a later one names the namespace. Since 2026-10-24 a location table
// follows the lead (kelostext's table.go); from 2026-10-22 it read
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
var rolesByPhrase = map[string]model.RunRole{
	kelostext.PhraseTaskStartedLowercase: model.RoleImplementation,
	kelostext.PhraseTaskStartedColon:     model.RoleImplementation,
	kelostext.PhraseTaskStarted:          model.RoleImplementation,
	kelostext.OpenerTaskStarted:          model.RoleImplementation,
	kelostext.PhraseFollowUpLowercase:    model.RoleTaskFollowUp,
	kelostext.PhraseFollowUpColon:        model.RoleTaskFollowUp,
	kelostext.PhraseFollowUp:             model.RoleTaskFollowUp,
	kelostext.OpenerFollowUp:             model.RoleTaskFollowUp,
}

// grammar parses the announcements of rolesByPhrase.
var grammar = kelostext.NewGrammar(slices.Collect(maps.Keys(rolesByPhrase))...)

// announcement is what a Kelos start notice says about its run. Its Task
// name is a display and correlation hint, never the run's identity.
type announcement struct {
	role      model.RunRole
	agent     string
	model     string
	namespace string
	taskName  string
	pod       string
}

// parseAnnouncement parses a comment's plain text. It returns
// kelostext.ErrNotAnnouncement when the text has no start phrase and
// kelostext.ErrUnparsedAnnouncement when it has one but the rest does not
// match.
func parseAnnouncement(text string) (announcement, error) {
	start, err := grammar.Parse(text)
	if err != nil {
		return announcement{}, err
	}
	return announcement{
		role:      rolesByPhrase[start.Phrase],
		agent:     start.Agent,
		model:     start.Model,
		namespace: start.Namespace,
		taskName:  start.TaskName,
		pod:       start.Pod,
	}, nil
}
