package domain

import "strings"

// NormalizeBody puts a card body in the form every storage keeps it in: line
// breaks are "\n", and blank lines at the start and end are gone. Spaces at the
// start of the first text line stay, since they can mean an indented code block.
// The markdown format drops these blank lines when it reads a file, so a body
// kept anywhere else must not have them, or a board could differ between storages.
func NormalizeBody(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
