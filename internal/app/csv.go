package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"waypoint/internal/domain"
)

// ArchivedColumn is the value of the column cell of an archived card in a CSV
// file.
const ArchivedColumn = "Archived"

// csvKnown are the headers every CSV file may carry, in the order Waypoint
// writes them. Any other header is an extra card field.
var csvKnown = []string{"id", "title", "column", "group", "priority", "depends", "created", "done", "body"}

// groupPalette is the order new groups take their colours in. The names are
// the ones the web UI draws.
var groupPalette = []string{"blue", "amber", "green", "rose", "violet", "teal", "orange", "sky", "sage", "sand", "red"}

// CSVOptions says how a CSV import treats names the project does not have.
type CSVOptions struct {
	// Create adds unknown columns and groups instead of refusing them.
	Create bool
	// DryRun does everything but save: the result says what a real run would do,
	// and a file a real run would refuse is refused the same way.
	DryRun bool
}

// CSVResult says what a CSV import did.
type CSVResult struct {
	Created    []domain.CardID `json:"created"`
	Updated    []domain.CardID `json:"updated"`
	NewColumns []string        `json:"new_columns"`
	NewGroups  []string        `json:"new_groups"`
}

// ExportCSV writes every card, live and archived, as CSV with \n line endings.
// The columns are id,title,column,group,priority,depends,created,done,body and
// then one column for each extra field any card has, in the order first seen.
// Archived cards have column Archived.
func (s *Service) ExportCSV(ctx context.Context, w io.Writer) error {
	b, err := s.read(ctx)
	if err != nil {
		return err
	}
	if err := checkArchivedColumn(&b.Settings); err != nil {
		return err
	}
	var extras []string
	for _, list := range [][]domain.Card{b.Cards, b.Archived} {
		for _, c := range list {
			for _, f := range c.Extra {
				if !slices.Contains(extras, f.Key) {
					extras = append(extras, f.Key)
				}
			}
		}
	}
	cw := csv.NewWriter(w)
	cw.Write(slices.Concat(csvKnown, extras))
	row := func(c domain.Card, column string) {
		depends := make([]string, len(c.Depends))
		for i, d := range c.Depends {
			depends[i] = string(d)
		}
		cells := []string{string(c.ID), c.Title, column, c.Group, c.Priority, strings.Join(depends, ","), dateCell(c.Created), dateCell(c.Done), c.Body}
		for _, key := range extras {
			value := ""
			for _, f := range c.Extra {
				if f.Key == key {
					value = f.Value
				}
			}
			cells = append(cells, value)
		}
		cw.Write(cells)
	}
	for _, c := range b.Cards {
		row(c, c.Column)
	}
	for _, c := range b.Archived {
		row(c, ArchivedColumn)
	}
	cw.Flush()
	return cw.Error()
}

func dateCell(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return domain.StoredDate(t)
}

// MarkdownFormat renders a board, and its archived cards, as the two parts of
// the markdown file format. markdown.Format and markdown.FormatArchive are
// the real ones.
type MarkdownFormat struct {
	Board   func(*domain.Board) []byte
	Archive func([]domain.Card) []byte
}

// ExportMarkdown writes the project in the markdown file format: the board,
// then, when there are archived cards, the archive as "# Archive" with its
// "## YYYY-MM" sections. It is a read-only copy to read or keep. The two parts
// are the two files of a markdown project joined, so it is not itself a board
// file; split it at the "# Archive" line to read it back.
//
// The formatting is the markdown adapter's, which imports this package, so the
// caller passes it in (see MarkdownFormat).
func (s *Service) ExportMarkdown(ctx context.Context, w io.Writer, f MarkdownFormat) error {
	b, err := s.read(ctx)
	if err != nil {
		return err
	}
	out := f.Board(b)
	if len(b.Archived) > 0 {
		out = append(append(out, '\n'), f.Archive(b.Archived)...)
	}
	_, err = w.Write(out)
	return err
}

// csvRow is one data row of a CSV file.
type csvRow struct {
	line  int
	cells []string
}

// csvTable is a CSV file that has been read and is ready to apply.
type csvTable struct {
	header []string // lower-cased
	rows   []csvRow
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalid, fmt.Sprintf(format, args...))
}

// readCSV reads a whole CSV file as spreadsheets write it: a UTF-8 byte order
// mark and CRLF endings are fine, blank lines are skipped, and a row may be
// shorter than the header (the rest is empty) but not longer. Errors name the
// line and wrap domain.ErrInvalid.
func readCSV(r io.Reader) (csvTable, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return csvTable{}, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	var t csvTable
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return csvTable{}, invalidf("line %d: this is not valid CSV: %v", pe.StartLine, pe.Err)
			}
			return csvTable{}, invalidf("this is not valid CSV: %v", err)
		}
		line, _ := cr.FieldPos(0)
		if t.header == nil {
			t.header = make([]string, len(rec))
			for i, h := range rec {
				t.header[i] = strings.ToLower(strings.TrimSpace(h))
			}
			continue
		}
		if len(rec) > len(t.header) {
			return csvTable{}, invalidf("line %d: the row has %d values but the header has %d columns", line, len(rec), len(t.header))
		}
		t.rows = append(t.rows, csvRow{line, rec})
	}
	if t.header == nil {
		return csvTable{}, invalidf("the CSV file is empty: it needs a header line with at least a title column")
	}
	seen := map[string]bool{}
	hasTitle := false
	for _, h := range t.header {
		switch {
		case h == "":
			return csvTable{}, invalidf("line 1: a column in the header has no name")
		case seen[h]:
			return csvTable{}, invalidf("line 1: the column %q is in the header twice", h)
		case !slices.Contains(csvKnown, h) && !domain.ValidFieldKey(h):
			return csvTable{}, invalidf("line 1: %q is not a column Waypoint knows, and it cannot be a field name (lower case letters, digits, - and _)", h)
		}
		seen[h] = true
		hasTitle = hasTitle || h == "title"
	}
	if !hasTitle {
		return csvTable{}, invalidf("line 1: the header has no title column")
	}
	return t, nil
}

// body returns the body cell as written. Unlike every other cell it is not
// trimmed, because a body may start with indentation that means something
// (a code block). A cell of only spaces and line breaks is no body.
func (t csvTable) body(r csvRow) (string, bool) {
	i := slices.Index(t.header, "body")
	if i < 0 {
		return "", false
	}
	if i >= len(r.cells) || strings.TrimSpace(r.cells[i]) == "" {
		return "", true
	}
	return r.cells[i], true
}

// checkArchivedColumn refuses a project with a column named like the marker
// that CSV files use for archived cards, since a file could not tell them apart.
func checkArchivedColumn(s *domain.Settings) error {
	if name := columnNamed(s, ArchivedColumn); name != "" {
		return invalidf("this project has a column called %q, which CSV files use to mark archived cards; rename the column first", name)
	}
	return nil
}

// cell returns the trimmed value of a column in a row, and whether the file has
// that column at all.
func (t csvTable) cell(r csvRow, name string) (string, bool) {
	i := slices.Index(t.header, name)
	if i < 0 {
		return "", false
	}
	if i >= len(r.cells) {
		return "", true
	}
	return strings.TrimSpace(r.cells[i]), true
}

// ImportCSV puts the rows of a CSV file on the board (see readCSV for what a
// file may look like). An empty id makes a new card; an id the project has, live or
// archived, updates that card in place (an archived card stays archived, and a
// column the file does not have is left as it is); an id the project does not
// have makes a card with that id. Within a column the cards of the file follow
// the cards it does not mention, in file order. Unknown columns and groups are
// refused, naming them, unless o.Create. Nothing is changed unless every row
// is valid, and the first problem found is reported with its line.
func (s *Service) ImportCSV(ctx context.Context, r io.Reader, o CSVOptions) (CSVResult, error) {
	t, err := readCSV(r)
	if err != nil {
		return CSVResult{}, err
	}
	var res CSVResult
	_, err = s.apply(ctx, func(b *domain.Board) error {
		var err error
		if res, err = importRows(b, t, o, s.now()); err != nil {
			return err
		}
		if o.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return CSVResult{}, err
	}
	return res, nil
}

// importRows applies the table to b. It sets what it returns and never adds
// to it, because apply can run it again.
func importRows(b *domain.Board, t csvTable, o CSVOptions, now time.Time) (CSVResult, error) {
	res := CSVResult{Created: []domain.CardID{}, Updated: []domain.CardID{}, NewColumns: []string{}, NewGroups: []string{}}
	if err := checkArchivedColumn(&b.Settings); err != nil {
		return res, err
	}
	lineOf := map[domain.CardID]int{}

	// Pass 1: rows that are not blank, their ids, and the names they need.
	var rows []csvRow
	idLine := map[string]int{}
	var missingCols, missingGroups []string
	for _, r := range t.rows {
		if !slices.ContainsFunc(r.cells, func(c string) bool { return strings.TrimSpace(c) != "" }) {
			continue
		}
		rows = append(rows, r)
		if id, _ := t.cell(r, "id"); id != "" {
			if !domain.ValidCardID(id) {
				return res, invalidf("line %d: %q is not a card id (expected a prefix, a dash and a number, like %s-1)", r.line, id, b.Settings.Prefix)
			}
			if first, dup := idLine[id]; dup {
				return res, invalidf("line %d: the id %s is also on line %d", r.line, id, first)
			}
			idLine[id] = r.line
		}
		if col, _ := t.cell(r, "column"); col != "" && !strings.EqualFold(col, ArchivedColumn) && columnNamed(&b.Settings, col) == "" && !slices.ContainsFunc(missingCols, func(m string) bool { return strings.EqualFold(m, col) }) {
			missingCols = append(missingCols, col)
		}
		if g, _ := t.cell(r, "group"); g != "" && groupNamed(&b.Settings, g) == "" && !slices.ContainsFunc(missingGroups, func(m string) bool { return strings.EqualFold(m, g) }) {
			missingGroups = append(missingGroups, g)
		}
	}
	if (len(missingCols) > 0 || len(missingGroups) > 0) && !o.Create {
		var parts []string
		if len(missingCols) > 0 {
			parts = append(parts, "columns the project does not have: "+strings.Join(missingCols, ", "))
		}
		if len(missingGroups) > 0 {
			parts = append(parts, "groups the project does not have: "+strings.Join(missingGroups, ", "))
		}
		return res, invalidf("%s; run the import with --create to add them", strings.Join(parts, "; "))
	}
	for _, name := range missingCols {
		addColumn(&b.Settings, name)
	}
	for _, name := range missingGroups {
		b.Settings.Groups = append(b.Settings.Groups, domain.Group{Name: name, Color: nextGroupColor(b.Settings.Groups)})
	}
	res.NewColumns = append(res.NewColumns, missingCols...)
	res.NewGroups = append(res.NewGroups, missingGroups...)

	// New ids must not collide with an id the file names.
	for id := range idLine {
		prefix, num, _ := strings.Cut(id, "-")
		if n, err := strconv.Atoi(num); err == nil && prefix == b.Settings.Prefix {
			b.Settings.NextID = max(b.Settings.NextID, n+1)
		}
	}

	// Pass 2: build each card.
	live := slices.Clone(b.Cards)
	archived := slices.Clone(b.Archived)
	var placed, newArchived []domain.Card
	for _, r := range rows {
		fail := func(format string, args ...any) error {
			return invalidf("line %d: %s", r.line, fmt.Sprintf(format, args...))
		}
		id, _ := t.cell(r, "id")
		var c domain.Card
		var wasLive, wasArchived bool
		if id != "" {
			if i := slices.IndexFunc(live, func(x domain.Card) bool { return string(x.ID) == id }); i >= 0 {
				c, wasLive = live[i], true
				live = slices.Delete(live, i, i+1)
			} else if i := slices.IndexFunc(archived, func(x domain.Card) bool { return string(x.ID) == id }); i >= 0 {
				c, wasArchived = archived[i], true
			}
		}
		isNew := !wasLive && !wasArchived
		if isNew {
			if id == "" {
				id = fmt.Sprintf("%s-%d", b.Settings.Prefix, b.Settings.NextID)
				b.Settings.NextID++
			}
			c = domain.Card{ID: domain.CardID(id), Created: domain.Stamp(now)}
		}

		title, _ := t.cell(r, "title")
		if title == "" {
			return res, fail("the title is empty")
		}
		c.Title = title
		if v, ok := t.cell(r, "priority"); ok {
			if v == "" {
				c.Priority = ""
			} else if c.Priority = canonicalName(b.Settings.Priorities, v); c.Priority == "" {
				return res, fail("the priority %q is not one of %s", v, strings.Join(b.Settings.Priorities, ", "))
			}
		}
		if v, ok := t.cell(r, "group"); ok {
			c.Group = groupNamed(&b.Settings, v)
		}
		if v, ok := t.cell(r, "depends"); ok {
			c.Depends = nil
			for _, d := range strings.Split(v, ",") {
				if d = strings.TrimSpace(d); d != "" {
					if string(c.ID) == d {
						return res, fail("%s cannot depend on itself", d)
					}
					c.Depends = append(c.Depends, domain.CardID(d))
				}
			}
		}
		if v, ok := t.body(r); ok {
			c.Body = v
		}
		for _, h := range t.header {
			if slices.Contains(csvKnown, h) {
				continue
			}
			v, _ := t.cell(r, h)
			c.Extra = setField(c.Extra, h, v)
		}
		if v, _ := t.cell(r, "created"); v != "" {
			var err error
			if c.Created, err = domain.ParseStored(v); err != nil {
				return res, fail("created: %v", err)
			}
		}
		var done time.Time
		if v, _ := t.cell(r, "done"); v != "" {
			var err error
			if done, err = domain.ParseStored(v); err != nil {
				return res, fail("done: %v", err)
			}
		}

		column, _ := t.cell(r, "column")
		toArchive := strings.EqualFold(column, ArchivedColumn) || wasArchived
		switch {
		case toArchive:
			c.Column = ""
			if !done.IsZero() {
				c.Done = done
			}
			if c.Done.IsZero() { // an archived card must be dated to be read back
				c.Done = domain.Stamp(now)
			}
		default:
			name := columnNamed(&b.Settings, column)
			if column == "" {
				if wasLive {
					name = c.Column
				} else {
					name = b.Settings.Columns[0].Name
				}
			}
			col, _ := b.Column(name)
			changed := isNew || name != c.Column
			c.Column = name
			switch {
			case !done.IsZero():
				c.Done = done
			case changed && col.Done && c.Done.IsZero():
				c.Done = domain.Stamp(now)
			case changed && !col.Done:
				c.Done = time.Time{}
			}
			if isNew && prefixOf(c.ID) != b.Settings.Prefix {
				return res, fail("the id %s does not use this board's prefix %s", c.ID, b.Settings.Prefix)
			}
		}

		lineOf[c.ID] = r.line
		switch {
		case wasArchived:
			archived[slices.IndexFunc(archived, func(x domain.Card) bool { return x.ID == c.ID })] = c
		case toArchive:
			newArchived = append(newArchived, c)
		default:
			placed = append(placed, c)
		}
		if isNew {
			res.Created = append(res.Created, c.ID)
		} else {
			res.Updated = append(res.Updated, c.ID)
		}
	}

	b.Cards = slices.Concat(live, placed)
	order := func(name string) int {
		return slices.IndexFunc(b.Settings.Columns, func(c domain.Column) bool { return c.Name == name })
	}
	slices.SortStableFunc(b.Cards, func(x, y domain.Card) int { return order(x.Column) - order(y.Column) })
	b.Archived = slices.Concat(newArchived, archived)

	if err := b.Validate(); err != nil {
		for id, line := range lineOf {
			if strings.Contains(err.Error(), "card "+string(id)+":") {
				return res, invalidf("line %d: %s", line, strings.TrimPrefix(err.Error(), domain.ErrInvalid.Error()+": "))
			}
		}
		return res, err
	}
	return res, nil
}

func prefixOf(id domain.CardID) string {
	p, _, _ := strings.Cut(string(id), "-")
	return p
}

// setField sets, or with an empty value removes, an extra field, keeping the
// order of the fields already there.
func setField(fields []domain.Field, key, value string) []domain.Field {
	i := slices.IndexFunc(fields, func(f domain.Field) bool { return f.Key == key })
	switch {
	case i >= 0 && value == "":
		return slices.Delete(slices.Clone(fields), i, i+1)
	case i >= 0:
		out := slices.Clone(fields)
		out[i].Value = value
		return out
	case value != "":
		return append(slices.Clone(fields), domain.Field{Key: key, Value: value})
	}
	return fields
}

// canonicalName returns the spelling in list of name, ignoring case, or "".
func canonicalName(list []string, name string) string {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return n
		}
	}
	return ""
}

// nextGroupColor is the first palette colour no group uses, else the palette
// again from the start.
func nextGroupColor(groups []domain.Group) string {
	for _, c := range groupPalette {
		if !slices.ContainsFunc(groups, func(g domain.Group) bool { return g.Color == c }) {
			return c
		}
	}
	return groupPalette[len(groups)%len(groupPalette)]
}

// columnNamed returns the spelling of the column called name, ignoring case, or "".
func columnNamed(s *domain.Settings, name string) string {
	for _, c := range s.Columns {
		if strings.EqualFold(c.Name, name) {
			return c.Name
		}
	}
	return ""
}

// groupNamed returns the spelling of the group called name, ignoring case, or "".
func groupNamed(s *domain.Settings, name string) string {
	for _, g := range s.Groups {
		if strings.EqualFold(g.Name, name) {
			return g.Name
		}
	}
	return ""
}

// addColumn adds a column that cards can move to and from freely: it may move
// to every other column and every other column may move to it.
func addColumn(s *domain.Settings, name string) {
	col := domain.Column{Name: name}
	for i := range s.Columns {
		col.MovesTo = append(col.MovesTo, s.Columns[i].Name)
		s.Columns[i].MovesTo = append(s.Columns[i].MovesTo, name)
	}
	s.Columns = append(s.Columns, col)
}
