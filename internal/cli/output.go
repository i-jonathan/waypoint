package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"waypoint/internal/app"
	"waypoint/internal/domain"
)

const (
	emptyBoardText = "No cards yet. Add one with: waypoint add \"Title\"\n"
	noMatchText    = "No cards match.\n"
	dayLayout      = "2006-01-02"
)

// writeCardLines prints cards one per line, aligned in columns: id, column,
// priority, group, title, then tags. Widths come from the rows given. An empty
// priority or group cell is blank, and a column that is empty in every row is
// left out. No line ends in spaces.
func writeCardLines(w io.Writer, cards []app.CardView) {
	var wid [4]int
	rows := make([][5]string, len(cards))
	for i, cv := range cards {
		rows[i] = [5]string{string(cv.ID), cv.Column, cv.Priority, cv.Group, cv.Title}
		for j := range wid {
			wid[j] = max(wid[j], utf8.RuneCountInString(rows[i][j]))
		}
	}
	for i, cv := range cards {
		var b strings.Builder
		for j := range wid {
			if wid[j] == 0 {
				continue // nobody printed has this cell, so the column is dropped
			}
			b.WriteString(rows[i][j])
			b.WriteString(strings.Repeat(" ", wid[j]-utf8.RuneCountInString(rows[i][j])+2))
		}
		b.WriteString(rows[i][4])
		for _, tag := range cardTags(cv) {
			b.WriteString("  ")
			b.WriteString(tag)
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// cardTags are the bracketed markers shown after a card's title.
func cardTags(cv app.CardView) []string {
	var tags []string
	if cv.Next {
		tags = append(tags, "[NEXT]")
	}
	if len(cv.Blockers) > 0 {
		tags = append(tags, "[BLOCKED by "+blockerList(cv.Blockers)+"]")
	}
	if cv.Archived {
		tags = append(tags, "[ARCHIVED]")
	}
	return tags
}

// blockerList is "T-1, T-99?": ids in order, a "?" after one that does not exist.
func blockerList(bl []domain.Blocker) string {
	parts := make([]string, len(bl))
	for i, b := range bl {
		parts[i] = string(b.ID)
		if b.Missing {
			parts[i] += "?"
		}
	}
	return strings.Join(parts, ", ")
}

// writeCardDetail prints one card for "show": the id and title, one
// "key: value" line per set field, then a blank line and the body.
func writeCardDetail(w io.Writer, cv app.CardView) {
	fmt.Fprintf(w, "%s %s\n", cv.ID, cv.Title)
	field := func(key, value string) {
		if value != "" {
			fmt.Fprintf(w, "%s: %s\n", key, value)
		}
	}
	field("column", cv.Column)
	field("group", cv.Group)
	field("priority", cv.Priority)
	if len(cv.Depends) > 0 {
		ids := make([]string, len(cv.Depends))
		for i, d := range cv.Depends {
			ids[i] = string(d)
		}
		field("depends", strings.Join(ids, ", "))
	}
	if !cv.Created.IsZero() {
		field("created", cv.Created.Format(dayLayout))
	}
	if !cv.Done.IsZero() {
		field("done", cv.Done.Format(dayLayout))
	}
	for _, f := range cv.Extra {
		field(f.Key, f.Value)
	}
	if cv.Archived {
		field("archived", "yes")
	}
	if len(cv.Blockers) > 0 {
		field("blocked by", blockerList(cv.Blockers))
	}
	if cv.Checklist.Total > 0 {
		field("checklist", fmt.Sprintf("%d/%d", cv.Checklist.Done, cv.Checklist.Total))
	}
	if body := strings.TrimRight(cv.Body, "\r\n"); body != "" {
		fmt.Fprintf(w, "\n%s\n", body)
	}
}

// printWarnings writes board warnings to stderr, in both output modes, so
// stdout stays parseable.
func (r *runner) printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(r.env.Stderr, "warning: %s\n", w)
	}
}

// warnAboutBoard reads the board's warnings and prints them. It is best
// effort: a read failure here must not turn a finished command into an error.
func (r *runner) warnAboutBoard(svc *app.Service) {
	if bv, err := svc.Board(r.ctx); err == nil {
		r.printWarnings(bv.Warnings)
	}
}

// wrote reports a successful card write: the card as JSON, or msg as text,
// then any warnings the board now has.
func (r *runner) wrote(svc *app.Service, cv app.CardView, msg string) int {
	if r.json {
		r.printJSON(cv)
	} else {
		fmt.Fprintln(r.env.Stdout, msg)
	}
	r.warnAboutBoard(svc)
	return exitOK
}
