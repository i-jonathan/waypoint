// Package markdown reads and writes Waypoint boards as Markdown files.
package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"waypoint/internal/domain"
)

// ErrNotABoard is returned for a file that has no Waypoint front matter.
var ErrNotABoard = errors.New("not a Waypoint board (missing front matter)")

// ParseError is a problem in a board or archive file, with its 1-based line.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

var (
	// headingRE matches a column (##) or card (###) heading, with optional text.
	headingRE = regexp.MustCompile(`^(##|###)(?:[ \t]+(.*))?$`)
	// idRE is the shape of a card id at the start of a card heading.
	idRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-\d+$`)
	// fieldRE is the shape of a field line. Whether the key is acceptable is
	// decided afterwards, so a body list item such as "- Note: x" or
	// "- see: https://x" is not mistaken for a field.
	fieldRE = regexp.MustCompile(`^- ([^\s:]+):(?: (.*))?$`)
)

const dateLayout = "2006-01-02"

// Parse reads a board file. Archived is left empty.
func Parse(data []byte) (*domain.Board, error) {
	b, _, err := ParseAssigned(data)
	return b, err
}

// ParseAssigned is Parse that also returns the ids given to cards that had
// none, in file order, so the caller can write them back.
func ParseAssigned(data []byte) (*domain.Board, []domain.CardID, error) {
	lines := splitLines(data)
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil, ErrNotABoard
	}
	yamlText, closing, err := splitFrontMatter(lines)
	if err != nil {
		return nil, nil, err
	}
	settings, err := settingsFromYAML(yamlText)
	if err != nil {
		return nil, nil, err
	}
	b := &domain.Board{Settings: settings}

	columnIndex := make(map[string]int, len(settings.Columns))
	for i, c := range settings.Columns {
		columnIndex[c.Name] = i
	}
	raw, err := scanCards(lines[closing+1:], closing+1, func(name string) error {
		if _, ok := columnIndex[name]; !ok {
			return fmt.Errorf("unknown column %q", name)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// An explicit id at or above next_id moves next_id past it, so an id is
	// never reused. This happens before id-less cards are numbered.
	for _, r := range raw {
		if !r.hasID {
			continue
		}
		prefix, num, _ := strings.Cut(string(r.card.ID), "-")
		if prefix != settings.Prefix {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return nil, nil, &ParseError{Line: r.line, Msg: fmt.Sprintf("card id %s has a number that is too large", r.card.ID)}
		}
		b.Settings.NextID = max(b.Settings.NextID, n+1)
	}
	var assigned []domain.CardID
	for i := range raw {
		if raw[i].hasID {
			continue
		}
		id := domain.CardID(fmt.Sprintf("%s-%d", settings.Prefix, b.Settings.NextID))
		b.Settings.NextID++
		raw[i].card.ID = id
		assigned = append(assigned, id)
	}

	for _, r := range raw {
		b.Cards = append(b.Cards, r.card)
	}
	// The ## sections of a hand-edited file may appear in any order, and a
	// column may appear twice. Board order is column order, then file order.
	slices.SortStableFunc(b.Cards, func(x, y domain.Card) int {
		return columnIndex[x.Column] - columnIndex[y.Column]
	})
	return b, assigned, nil
}

// ParseArchive reads an archive file: one "## YYYY-MM" section per month with
// the archived cards beneath it. Cards have no column and must carry a done
// date and an id. The settings are accepted for symmetry with Parse; the
// archive never assigns ids.
func ParseArchive(data []byte, _ domain.Settings) ([]domain.Card, error) {
	lines := splitLines(data)
	raw, err := scanCards(lines, 0, func(name string) error {
		if _, err := time.Parse("2006-01", name); err != nil || len(name) != len("2006-01") {
			return fmt.Errorf("archive section %q must be YYYY-MM", name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var cards []domain.Card
	for _, r := range raw {
		switch {
		case !r.hasID:
			return nil, &ParseError{Line: r.line, Msg: fmt.Sprintf("archived card %q has no id", r.card.Title)}
		case r.card.Done.IsZero():
			return nil, &ParseError{Line: r.line, Msg: fmt.Sprintf("archived card %s has no done date", r.card.ID)}
		}
		r.card.Column = ""
		cards = append(cards, r.card)
	}
	return cards, nil
}

// splitLines strips a UTF-8 BOM, normalises CRLF to LF and splits into lines.
func splitLines(data []byte) []string {
	s := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

// rawCard is a card as read from the file, before ids are assigned.
type rawCard struct {
	card  domain.Card // Column holds the enclosing ## section name
	line  int         // file line of the card heading
	hasID bool
}

// scanCards reads the ## sections and ### cards of lines. base is the number
// of file lines before lines[0], for error positions. section validates the
// name of each ## heading; its error is reported on that line.
func scanCards(lines []string, base int, section func(name string) error) ([]rawCard, error) {
	fail := func(i int, format string, args ...any) error {
		return &ParseError{Line: base + i + 1, Msg: fmt.Sprintf(format, args...)}
	}

	// Mark which lines are inside fenced code. The extra last line cannot
	// close a fence, so it is inside one exactly when a fence is never closed.
	inFence := make([]bool, 0, len(lines)+1)
	domain.ScanLines(strings.Join(lines, "\n")+"\nx", func(_ int, _ string, f bool) {
		inFence = append(inFence, f)
	})
	if unclosed := inFence[len(lines)]; unclosed {
		start := len(lines) - 1
		for start > 0 && inFence[start-1] {
			start--
		}
		for i := start; i < len(lines); i++ {
			if headingRE.MatchString(lines[i]) {
				return nil, fail(i, "heading is inside a code fence that is never closed (opened on line %d)", base+start+1)
			}
		}
	}
	heading := func(i int) (marks, text string, ok bool) {
		if inFence[i] {
			return "", "", false
		}
		m := headingRE.FindStringSubmatch(lines[i])
		if m == nil {
			return "", "", false
		}
		return m[1], strings.TrimSpace(m[2]), true
	}

	var (
		cards    []rawCard
		current  string
		inColumn bool
		seen     = map[domain.CardID]int{} // id to heading line
	)
	for i := 0; i < len(lines); {
		marks, text, ok := heading(i)
		if !ok {
			i++ // text before the first heading, or between ## and the first ###
			continue
		}
		if marks == "##" {
			if err := section(text); err != nil {
				return nil, fail(i, "%v", err)
			}
			current, inColumn = text, true
			i++
			continue
		}

		// A ### card heading.
		if !inColumn {
			return nil, fail(i, "card %q appears before any ## column heading", text)
		}
		card := domain.Card{Column: current}
		first, rest := text, ""
		if k := strings.IndexAny(text, " \t"); k >= 0 {
			first, rest = text[:k], text[k+1:]
		}
		hasID := idRE.MatchString(first)
		title := text
		if hasID {
			card.ID, title = domain.CardID(first), strings.TrimSpace(rest)
		}
		if title = strings.TrimSpace(title); title == "" {
			return nil, fail(i, "card heading has no title")
		}
		card.Title = title
		if hasID {
			if line, dup := seen[card.ID]; dup {
				return nil, fail(i, "duplicate card id %s (first on line %d)", card.ID, line)
			}
			seen[card.ID] = base + i + 1
		}

		// Fields: after any blank lines, consecutive "- key: value" lines.
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		fieldsSeen := map[string]bool{}
		for ; j < len(lines); j++ {
			m := fieldRE.FindStringSubmatch(lines[j])
			if m == nil || !isFieldKey(m[1]) {
				break
			}
			key, value := m[1], strings.TrimRight(m[2], " \t")
			if fieldsSeen[key] {
				return nil, fail(j, "duplicate field %q", key)
			}
			fieldsSeen[key] = true
			if err := setField(&card, key, value); err != nil {
				return nil, fail(j, "%v", err)
			}
		}

		// Body: up to the next heading outside a fence.
		end := j
		for end < len(lines) {
			if _, _, isHeading := heading(end); isHeading {
				break
			}
			end++
		}
		card.Body = trimBlankLines(lines[j:end])
		cards = append(cards, rawCard{card: card, line: base + i + 1, hasID: hasID})
		i = end
	}
	return cards, nil
}

// isFieldKey reports whether key can start a field line: a built-in field or a
// valid extra field key.
func isFieldKey(key string) bool {
	switch key {
	case "group", "priority", "depends", "created", "done":
		return true
	}
	return domain.ValidFieldKey(key)
}

// setField stores one field on the card. Values are verbatim apart from
// trailing space; the date and depends fields are also trimmed at the front.
func setField(c *domain.Card, key, value string) error {
	switch key {
	case "group":
		c.Group = value
	case "priority":
		c.Priority = value
	case "depends":
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !idRE.MatchString(part) {
				return fmt.Errorf("invalid card id %q in depends (separate ids with commas)", part)
			}
			c.Depends = append(c.Depends, domain.CardID(part))
		}
	case "created", "done":
		t, err := parseDate(key, value)
		if err != nil {
			return err
		}
		if key == "created" {
			c.Created = t
		} else {
			c.Done = t
		}
	default:
		c.Extra = append(c.Extra, domain.Field{Key: key, Value: value})
	}
	return nil
}

// parseDate reads YYYY-MM-DD. An empty value means unset.
func parseDate(key, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s date %q (want YYYY-MM-DD)", key, value)
	}
	return t, nil
}

// trimBlankLines joins lines with "\n", dropping blank lines at both ends but
// leaving the first and last text lines exactly as written.
func trimBlankLines(lines []string) string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
