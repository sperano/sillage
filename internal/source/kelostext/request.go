package kelostext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// CommandGordon starts a request since 2026-10-21.
	CommandGordon = "/gordon"
	// CommandKelos started a request until 2026-10-21; the history keeps
	// such requests.
	CommandKelos = "/kelos"
	// mentionPrefix starts the agent a request names: /gordon @<name>.
	mentionPrefix = "@"
)

// commands are the request prefixes every source recognises.
var commands = []string{CommandGordon, CommandKelos}

// Commands returns the request prefixes every source recognises, the
// current one first.
func Commands() []string {
	return append([]string(nil), commands...)
}

// Request is a comment asking an agent to act.
type Request struct {
	// Command is the prefix the comment starts with.
	Command string
	// Agent is the agent the request names with @<name>; empty when it
	// names none.
	Agent string
}

// ParseRequest reports whether a comment's plain text starts with one of
// Commands as a word, after any leading space, and returns the request.
// The agent it names with "<command> @<name>" loses trailing punctuation.
func ParseRequest(text string) (Request, bool) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	for _, command := range commands {
		rest, ok := strings.CutPrefix(text, command)
		if !ok {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(rest); rest != "" && !unicode.IsSpace(r) {
			continue
		}
		return Request{Command: command, Agent: mentioned(rest)}, true
	}
	return Request{}, false
}

// mentioned returns the agent the first word of rest names, if any.
func mentioned(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	name, ok := strings.CutPrefix(fields[0], mentionPrefix)
	if !ok {
		return ""
	}
	// "/gordon @sol, please ..." names sol: punctuation ends the name.
	return strings.TrimRightFunc(name, unicode.IsPunct)
}
