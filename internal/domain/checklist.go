package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ChecklistItem is one markdown task-list line in a card body.
type ChecklistItem struct {
	Text    string
	Checked bool
}

// itemRE matches `- [ ] text`, `- [x] text`, `- [X] text` and the `*` forms,
// optionally indented. Group 1 is the mark.
var itemRE = regexp.MustCompile(`^[ \t]*[-*][ \t]+\[([ xX])\](?:[ \t]+(.*))?$`)

// fenceRun reports the fence character and run length when line opens or
// closes a fenced code block: three or more backticks or tildes, indented by
// at most three spaces. rest is whatever follows the run.
func fenceRun(line string) (char byte, n int, rest string, ok bool) {
	s := strings.TrimRight(line, "\r")
	indent := 0
	for indent < len(s) && indent < 4 && s[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent == len(s) {
		return 0, 0, "", false
	}
	s = s[indent:]
	char = s[0]
	if char != '`' && char != '~' {
		return 0, 0, "", false
	}
	for n < len(s) && s[n] == char {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	return char, n, s[n:], true
}

// ScanLines calls fn for every line of body (split on "\n", so the line has no
// newline and a body ending in a newline ends with an empty line). i is the
// 0-based line index. inFence is true for lines inside a fenced code block and
// for the fence delimiter lines themselves. A fence opened with backticks
// closes only with backticks, and one opened with tildes only with tildes; the
// closer must be at least as long as the opener and carry no text after it. An
// unclosed fence runs to the end of the body.
func ScanLines(body string, fn func(i int, line string, inFence bool)) {
	ScanFences(body, func(i int, line string, opener int) { fn(i, line, opener >= 0) })
}

// ScanFences is ScanLines that also says which line opened the fence a line
// belongs to: opener is that line's 0-based index (for the delimiter lines
// themselves too), or -1 outside a fence. Two fences back to back have
// different openers.
func ScanFences(body string, fn func(i int, line string, opener int)) {
	var open byte // fence character, or 0 outside a fence
	openLen, openAt := 0, -1
	for i, line := range strings.Split(body, "\n") {
		char, n, rest, ok := fenceRun(line)
		switch {
		case open == 0 && ok && !(char == '`' && strings.Contains(rest, "`")):
			open, openLen, openAt = char, n, i
			fn(i, line, openAt)
		case open != 0 && ok && char == open && n >= openLen && strings.TrimSpace(rest) == "":
			open = 0
			fn(i, line, openAt)
		case open != 0:
			fn(i, line, openAt)
		default:
			fn(i, line, -1)
		}
	}
}

// Checklist lists the task items in body, in order, ignoring fenced code.
func Checklist(body string) []ChecklistItem {
	var items []ChecklistItem
	ScanLines(body, func(_ int, line string, inFence bool) {
		if inFence {
			return
		}
		if m := itemRE.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			items = append(items, ChecklistItem{Text: strings.TrimSpace(m[2]), Checked: m[1] != " "})
		}
	})
	return items
}

// ToggleCheck flips the nth (1-based) checklist item of a live card's body.
// Only the mark character changes; the rest of the body is untouched. It
// returns ErrNotFound for an unknown card or an n outside the checklist.
func (b *Board) ToggleCheck(id CardID, n int) error {
	idx := -1
	for i := range b.Cards {
		if b.Cards[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: card %s", ErrNotFound, id)
	}
	lines := strings.Split(b.Cards[idx].Body, "\n")
	seen := 0
	found := false
	ScanLines(b.Cards[idx].Body, func(i int, line string, inFence bool) {
		if inFence || found {
			return
		}
		m := itemRE.FindStringSubmatchIndex(strings.TrimRight(line, "\r"))
		if m == nil {
			return
		}
		seen++
		if seen != n {
			return
		}
		found = true
		mark := m[2] // offset of the mark inside the brackets
		repl := "x"
		if line[mark] != ' ' {
			repl = " "
		}
		lines[i] = line[:mark] + repl + line[mark+1:]
	})
	if !found {
		return fmt.Errorf("%w: checklist item %d of %s", ErrNotFound, n, id)
	}
	b.Cards[idx].Body = strings.Join(lines, "\n")
	return nil
}
