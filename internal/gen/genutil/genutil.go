// Package genutil holds header helpers shared by the client and server
// generators.
package genutil

import "strings"

// CommentBlock renders text as "// "-prefixed lines, one per line of text,
// each ending in a newline. Surrounding whitespace of text and of every line
// is dropped. Empty text yields "".
func CommentBlock(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString("// ")
		b.WriteString(strings.TrimSpace(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// SPDXID returns the SPDX identifier to print in an
// "SPDX-License-Identifier:" header line, or "" when id is empty or the
// upstream copyright text already holds that exact line.
func SPDXID(id, copyright string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	line := "SPDX-License-Identifier: " + id
	for _, l := range strings.Split(copyright, "\n") {
		if strings.TrimSpace(l) == line {
			return ""
		}
	}
	return id
}
