// Command migrate-pharmacy turns the pharmacy repo's hand-kept BOARD.md and
// DONE.md into a CSV file that "waypoint import --create" reads. It is a one-off
// for that repo, not part of Waypoint: it reads both files, writes nothing but
// the CSV on standard output, and never touches the files themselves.
//
//	go run ./scripts/migrate-pharmacy BOARD.md DONE.md > pharmacy.csv
//
// The mapping:
//   - a "## Column" heading in BOARD.md is a column, kept by name; a card under it
//     gets that column;
//   - every card in DONE.md gets the column Archived and keeps its done date;
//   - the "feature" field becomes the group; priority, depends, created and done
//     map across; any other field (merged, prs) becomes an extra field;
//   - the body is everything after the field lines, as written;
//   - the "### T-000" example inside a code fence is not a card.
//
// The next card id (from the "<!-- next-id: T-N -->" comment) is reported on
// standard error so it can be checked against the new project's counter.
package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Result is what a conversion found, for the person running it to check.
type Result struct {
	NextID  int // from the next-id comment, 0 if there is none
	MaxID   int // the highest card number in either file
	OnBoard int // cards in BOARD.md
	Done    int // cards in DONE.md
}

type field struct{ key, value string }

type card struct {
	id, title, column string
	line              int
	fields            []field
	body              string
}

var (
	columnHeading = regexp.MustCompile(`^## +(.+?) *$`)
	cardHeading   = regexp.MustCompile(`^### +([A-Za-z]+-[0-9]+) +(\S.*?) *$`)
	fieldLine     = regexp.MustCompile(`^- ([A-Za-z][A-Za-z0-9_-]*): *(.*?) *$`)
	nextIDComment = regexp.MustCompile(`<!--\s*next-id:\s*[A-Za-z]+-([0-9]+)\s*-->`)
	validKey      = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// csvColumns are the columns of Waypoint's CSV; a field may not take their names.
var csvColumns = []string{"id", "title", "column", "group", "priority", "depends", "created", "done", "body"}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: migrate-pharmacy BOARD.md DONE.md > pharmacy.csv")
		os.Exit(2)
	}
	board, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer board.Close()
	done, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer done.Close()

	var out strings.Builder // nothing reaches stdout unless every card was read
	res, err := convert(board, done, &out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate-pharmacy:", err)
		os.Exit(1)
	}
	fmt.Print(out.String())
	fmt.Fprintf(os.Stderr, "cards: %d on the board, %d done\n", res.OnBoard, res.Done)
	switch {
	case res.NextID == 0:
		fmt.Fprintf(os.Stderr, "no next-id comment found; the highest card number is T-%d\n", res.MaxID)
	default:
		fmt.Fprintf(os.Stderr, "next_id %d\n", res.NextID)
		if res.NextID <= res.MaxID {
			fmt.Fprintf(os.Stderr, "WARNING: the next-id comment (%d) is not past the highest card number (T-%d)\n", res.NextID, res.MaxID)
		}
	}
}

// convert reads both files and writes the CSV to w, or writes nothing and
// returns an error that names the card at fault.
func convert(board, done io.Reader, w io.Writer) (Result, error) {
	boardText, err := io.ReadAll(board)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if m := nextIDComment.FindSubmatch(boardText); m != nil {
		res.NextID, _ = strconv.Atoi(string(m[1]))
	}
	onBoard, err := parseCards(strings.NewReader(string(boardText)), false)
	if err != nil {
		return Result{}, fmt.Errorf("BOARD.md: %w", err)
	}
	finished, err := parseCards(done, true)
	if err != nil {
		return Result{}, fmt.Errorf("DONE.md: %w", err)
	}
	res.OnBoard, res.Done = len(onBoard), len(finished)

	all := slices.Concat(onBoard, finished)
	seen := map[string]bool{}
	var extras []string
	for _, c := range all {
		if seen[c.id] {
			return Result{}, fmt.Errorf("card %s appears twice (second time at line %d)", c.id, c.line)
		}
		seen[c.id] = true
		if _, num, ok := strings.Cut(c.id, "-"); ok {
			if n, err := strconv.Atoi(num); err == nil {
				res.MaxID = max(res.MaxID, n)
			}
		}
		for _, f := range c.fields {
			switch {
			case f.key == "feature" || slices.Contains([]string{"priority", "depends", "created", "done", "group"}, f.key):
			case slices.Contains(csvColumns, f.key):
				return Result{}, fmt.Errorf("card %s has a field called %q, which is a column of the CSV", c.id, f.key)
			case !validKey.MatchString(f.key):
				return Result{}, fmt.Errorf("card %s has a field %q that cannot be a Waypoint field name", c.id, f.key)
			case !slices.Contains(extras, f.key):
				extras = append(extras, f.key)
			}
		}
	}

	var buf strings.Builder
	cw := csv.NewWriter(&buf)
	cw.Write(slices.Concat(csvColumns, extras))
	for _, c := range all {
		row, err := c.row(extras)
		if err != nil {
			return Result{}, err
		}
		cw.Write(row)
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return Result{}, err
	}
	_, err = io.WriteString(w, buf.String())
	return res, err
}

// row is the card as one CSV record.
func (c card) row(extras []string) ([]string, error) {
	get := map[string]string{}
	for _, f := range c.fields {
		key := f.key
		if key == "feature" {
			key = "group"
		}
		if _, dup := get[key]; dup {
			return nil, fmt.Errorf("card %s has the field %q twice (or both feature and group)", c.id, f.key)
		}
		get[key] = f.value
	}
	depends := strings.Join(strings.FieldsFunc(get["depends"], func(r rune) bool { return r == ',' || r == ' ' }), ",")
	row := []string{c.id, c.title, c.column, get["group"], get["priority"], depends, get["created"], get["done"], c.body}
	for _, key := range extras {
		row = append(row, get[key])
	}
	return row, nil
}

// parseCards reads the cards of one file. A "## heading" sets the column (or, in
// the done file, every card is Archived); a "### ID Title" outside a code fence
// starts a card; lines in a fence are never headings.
func parseCards(r io.Reader, archived bool) ([]card, error) {
	var (
		cards   []card
		current *card
		lines   []string
		column  string
		inFence bool
		n       int
	)
	flush := func() error {
		if current == nil {
			return nil
		}
		c := *current
		c.fields, c.body = split(lines)
		cards = append(cards, c)
		current, lines = nil, nil
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && !strings.HasPrefix(strings.TrimSpace(line), "```") {
			if m := columnHeading.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "###") {
				flush()
				column = m[1]
				if archived {
					column = "Archived"
				}
				continue
			}
			if m := cardHeading.FindStringSubmatch(line); m != nil {
				flush()
				if column == "" {
					return nil, fmt.Errorf("card %s (line %d) comes before any column heading", m[1], n)
				}
				current = &card{id: m[1], title: m[2], column: column, line: n}
				continue
			}
		}
		if current != nil {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	return cards, nil
}

// split separates a card's lines into its field lines (the "- key: value" lines
// right under the heading) and its body (the rest, without the blank lines
// around it).
func split(lines []string) ([]field, string) {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	var fields []field
	for i < len(lines) {
		m := fieldLine.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		fields = append(fields, field{strings.ToLower(m[1]), m[2]})
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return fields, strings.TrimRight(strings.Join(lines[i:], "\n"), " \t\r\n")
}
