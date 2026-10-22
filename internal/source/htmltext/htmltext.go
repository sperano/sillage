// Package htmltext turns the rendered HTML of a comment into plain text,
// so every source parses comments from the same text.
package htmltext

import (
	"html"
	"regexp"
	"strings"
)

// blockTags are the elements whose opening and closing tags end a line, so
// a heading, list item or quote never runs into the text next to it.
const blockTags = `p|div|h[1-6]|li|ul|ol|dl|dt|dd|blockquote|pre|hr|` +
	`table|caption|thead|tbody|tfoot|tr|td|th|` +
	`main|section|article|aside|nav|header|footer|address|details|summary|figure|figcaption`

var (
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>|</?(?:` + blockTags + `)\b[^>]*>`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
)

// Plain turns rendered HTML into text: one line per block element or
// <br>, tags dropped, entities decoded, runs of spaces collapsed.
func Plain(body string) string {
	// Newlines in HTML are spaces; only block boundaries and <br> end a
	// line.
	body = strings.NewReplacer("\r", " ", "\n", " ").Replace(body)
	lines := strings.Split(lineBreak.ReplaceAllString(body, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = html.UnescapeString(htmlTag.ReplaceAllString(line, ""))
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
