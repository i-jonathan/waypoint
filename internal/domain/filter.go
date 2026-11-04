package domain

import (
	"strings"
	"unicode"
)

type termKind int

const (
	termText    termKind = iota // substring of id, title or body
	termBlocked                 // the "blocked" keyword
	termNext                    // the "next" keyword
	termGroup                   // group:value
	termColumn                  // column:value
	termField                   // key:value on an extra field
)

// term is one parsed token. text, key and value are lower case.
type term struct {
	kind       termKind
	text       string // the whole token, for priority names and free text
	key, value string
}

// Filter is a parsed card query. Every term must match (AND).
type Filter struct{ terms []term }

// ParseFilter parses a query of whitespace-separated tokens. A double-quoted
// span groups words into one token or one value, and an unterminated quote
// runs to the end of the input. See Match for what each token means.
func ParseFilter(q string) Filter {
	var f Filter
	for _, tok := range splitQuery(q) {
		f.terms = append(f.terms, parseTerm(strings.ToLower(tok)))
	}
	return f
}

// Empty reports whether the filter has no terms and so matches every card.
func (f Filter) Empty() bool { return len(f.terms) == 0 }

// Match reports whether the card satisfies every term, judged against b. A
// term is, in order of precedence:
//   - a configured priority name: the card's priority equals it
//   - "blocked": the card has unfinished dependencies
//   - "next": the card is the board's next up card
//   - group:x or column:x: that name contains x
//   - key:value for a valid extra field key: that field contains value
//     (unless value starts with "//", so URLs stay free text)
//   - anything else: the id, title or body contains it
//
// Every comparison ignores case.
func (f Filter) Match(b *Board, c Card) bool {
	var next Card
	var haveNext, nextDone bool
	for _, t := range f.terms {
		if isPriorityName(b, t.text) {
			if !strings.EqualFold(c.Priority, t.text) {
				return false
			}
			continue
		}
		var ok bool
		switch t.kind {
		case termBlocked:
			ok = len(b.Blockers(c.ID)) > 0
		case termNext:
			if !nextDone {
				next, haveNext = b.NextUp()
				nextDone = true
			}
			ok = haveNext && next.ID == c.ID
		case termGroup:
			ok = strings.Contains(strings.ToLower(c.Group), t.value)
		case termColumn:
			ok = strings.Contains(strings.ToLower(c.Column), t.value)
		case termField:
			for _, fld := range c.Extra {
				if strings.ToLower(fld.Key) == t.key && strings.Contains(strings.ToLower(fld.Value), t.value) {
					ok = true
					break
				}
			}
		default:
			ok = strings.Contains(strings.ToLower(string(c.ID)), t.text) ||
				strings.Contains(strings.ToLower(c.Title), t.text) ||
				strings.Contains(strings.ToLower(c.Body), t.text)
		}
		if !ok {
			return false
		}
	}
	return true
}

func isPriorityName(b *Board, s string) bool {
	for _, p := range b.Settings.Priorities {
		if strings.EqualFold(p, s) {
			return true
		}
	}
	return false
}

// parseTerm classifies a lower-cased token.
func parseTerm(tok string) term {
	switch tok {
	case "blocked":
		return term{kind: termBlocked, text: tok}
	case "next":
		return term{kind: termNext, text: tok}
	}
	if key, value, ok := strings.Cut(tok, ":"); ok && key != "" && value != "" && !strings.HasPrefix(value, "//") {
		switch key {
		case "group":
			return term{kind: termGroup, text: tok, key: key, value: value}
		case "column":
			return term{kind: termColumn, text: tok, key: key, value: value}
		}
		if ValidFieldKey(key) {
			return term{kind: termField, text: tok, key: key, value: value}
		}
	}
	return term{kind: termText, text: tok}
}

// splitQuery splits q at unquoted whitespace and strips the quotes. Empty
// tokens are dropped.
func splitQuery(q string) []string {
	var toks []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}
