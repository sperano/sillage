package github

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelostext"
)

// A PR follow-up or conflict run's start announcement is a conversation
// comment in Markdown, posted with the Workspace's token: the owner's
// account for a PAT, the App's bot for a GitHub App. Its first line is the
// lead sentence kelostext parses. Since 2026-10-22 it reads
//
//	AGENT `AGENT` (MODEL `MODEL`) IS ON YOUR FOLLOW-UP REQUEST. HEARD YOU
//	LOUD AND CLEAR. KELOS TASK `NAME`, POD `POD`.
//
//	COFFEE TIME! REPORT TO FOLLOW.
//
// and before, from 2026-10-05,
//
//	Agent `AGENT` (model `MODEL`) has started on the follow-up request:
//	[Kelos Task `NAME`](...), pod `POD`.
//
//	The link shows ... (namespace `NAMESPACE`).
//
// A conflict run's phrase is the conflict one of the same form. Since the
// account can be the owner's, a comment is an announcement only when its
// Task name is one Kelos gives a run on this pull request.

// rolesByPhrase maps each start phrase posted on GitHub to the run role it
// announces.
var rolesByPhrase = map[string]model.RunRole{
	kelostext.PhraseFollowUpLowercase: model.RolePRFollowUp,
	kelostext.PhraseFollowUpColon:     model.RolePRFollowUp,
	kelostext.PhraseFollowUp:          model.RolePRFollowUp,
	kelostext.PhraseConflictLowercase: model.RoleConflictFix,
	kelostext.PhraseConflictColon:     model.RoleConflictFix,
	kelostext.PhraseConflict:          model.RoleConflictFix,
}

const (
	// hex8 matches the 8 hexadecimal digits of a conflict Task name's
	// hashes.
	hex8 = `[0-9a-f]{8}`
	// retrySuffix matches the suffix a restarted Task's name gets.
	retrySuffix = `(?:-retry[0-9]+)?`
)

var (
	// grammar parses the announcements of rolesByPhrase.
	grammar = kelostext.NewGrammar(slices.Collect(maps.Keys(rolesByPhrase))...)
	// taskNames match the Task names Kelos gives each role's runs; the
	// group is the pull request number. A follow-up's name ends with the
	// kind (c: comment, l: inline comment, r: review, p: polled review)
	// and id of its request.
	taskNames = map[model.RunRole]*regexp.Regexp{
		model.RolePRFollowUp:  regexp.MustCompile(`^pr-followup-.+-([0-9]+)-[clpr][0-9]+` + retrySuffix + `$`),
		model.RoleConflictFix: regexp.MustCompile(`^pr-conflict-` + hex8 + `-([0-9]+)-` + hex8 + `-` + hex8 + retrySuffix + `$`),
	}
	// markdownLink matches an inline link, [text](url).
	markdownLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// announcement is what a start announcement says about its run.
type announcement struct {
	kelostext.Start
	Role model.RunRole
}

// parseAnnouncement parses a comment's Markdown body. It returns
// kelostext.ErrNotAnnouncement when the body has no start phrase, and
// kelostext.ErrUnparsedAnnouncement when it has one but its first line
// does not have the expected form or its Task name is not one of a run of
// its role on pull request number.
func parseAnnouncement(body string, number int) (announcement, error) {
	s, err := grammar.Parse(markdownText(body))
	if err != nil {
		return announcement{}, err
	}
	role := rolesByPhrase[s.Phrase]
	m := taskNames[role].FindStringSubmatch(s.TaskName)
	if m == nil || m[1] != strconv.Itoa(number) {
		return announcement{}, kelostext.ErrUnparsedAnnouncement
	}
	return announcement{Start: s, Role: role}, nil
}

// markdownText is the text of the Markdown Kelos posts: links reduced to
// their text, code spans to their content.
func markdownText(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.ReplaceAll(markdownLink.ReplaceAllString(body, "$1"), "`", "")
}

// announced makes e the announcement a. The agent it names is the run's
// executor.
func announced(e *model.Event, a announcement) {
	e.Kind = model.KindRunAnnounced
	e.Evidence.Note += ", and the Kelos Task" + namedAlong(a) + " it names"
	if a.Agent != "" {
		e.Executor = &model.Executor{Name: a.Agent, Kind: model.ActorAgent}
	}
	details := [][2]string{
		{"taskName", a.TaskName},
		{"pod", a.Pod},
		{"namespace", a.Namespace},
		{"model", a.Model},
		{"role", string(a.Role)},
	}
	for _, d := range details {
		if d[1] != "" {
			setDetail(e, d[0], d[1])
		}
	}
}

// namedAlong lists what a names besides its Kelos Task, for an evidence
// note: the pod and namespace, when stated.
func namedAlong(a announcement) string {
	switch {
	case a.Pod != "" && a.Namespace != "":
		return ", pod and namespace"
	case a.Pod != "":
		return " and pod"
	case a.Namespace != "":
		return " and namespace"
	}
	return ""
}

func setDetail(e *model.Event, key, value string) {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	e.Details[key] = value
}
