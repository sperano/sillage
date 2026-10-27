package kelostext

import (
	"regexp"
	"slices"
	"strings"
)

// The location table, Hollingsworth #368, merged 2026-10-24: the first
// line ends at the action phrase's period, and a two-column table of where
// the run is follows it, one row per label below in this order, those
// unknown left out (JOB UID on GitHub only):
//
//	AGENT X (MODEL Y) IS ON THE CASE. THIS ONE HAS THE STUFF.
//	| KELOS TASK | NAME |
//	| JOB UID    | UID  |
//	| POD        | POD  |
//	| NODE       | NODE |
//
// Rendered comment HTML turns into one line per cell, the label
// then its value; Markdown (GitHub) keeps one line per row, after an empty
// header row and its separator.
const (
	labelTask = "KELOS TASK"
	labelJob  = "JOB UID"
	labelPod  = "POD"
	labelNode = "NODE"
)

// tableLabels are the location table's row labels.
var tableLabels = []string{labelTask, labelJob, labelPod, labelNode}

// markdownFiller matches a Markdown table cell that holds no row: empty,
// or a header separator.
var markdownFiller = regexp.MustCompile(`^(?:|:?-+:?)$`)

// locationRows returns the rows of the location table that starts text,
// by label, blank lines skipped. It stops at the first line that is not
// part of the table; a label seen twice keeps its first value.
func locationRows(text string) map[string]string {
	rows := map[string]string{}
	add := func(label, value string) {
		if _, seen := rows[label]; !seen {
			rows[label] = value
		}
	}
	lines := nonBlankLines(text)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if cells, ok := markdownRow(line); ok {
			switch {
			case !slices.ContainsFunc(cells, func(c string) bool { return !markdownFiller.MatchString(c) }):
			case len(cells) == 2 && slices.Contains(tableLabels, cells[0]):
				add(cells[0], cells[1])
			default:
				return rows
			}
			continue
		}
		if !slices.Contains(tableLabels, line) || i+1 == len(lines) {
			return rows
		}
		add(line, lines[i+1])
		i++
	}
	return rows
}

// markdownRow returns the trimmed cells of a Markdown table row, and
// whether line is one.
func markdownRow(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") || len(line) < len("||") {
		return nil, false
	}
	cells := strings.Split(line[1:len(line)-1], "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells, true
}

// nonBlankLines returns text's lines, trimmed, blank ones dropped.
func nonBlankLines(text string) []string {
	var lines []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
