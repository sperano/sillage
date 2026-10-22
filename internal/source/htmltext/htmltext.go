// Package htmltext turns the rendered HTML of a comment into plain text,
// so every source parses comments from the same text.
package htmltext

import (
	"html"
	"regexp"
	"strings"
)

var (
	paragraphBreak = regexp.MustCompile(`(?i)</p\s*>|<br\s*/?>`)
	htmlTag        = regexp.MustCompile(`<[^>]*>`)
)

// Plain turns rendered HTML into text: one line per paragraph, tags
// dropped, entities decoded, runs of spaces collapsed.
func Plain(body string) string {
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
