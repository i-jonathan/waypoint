package markdown

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"waypoint/internal/domain"
)

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// lineOf returns the 1-based line number of the first line of src that
// contains substr.
func lineOf(t *testing.T, src, substr string) int {
	t.Helper()
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, substr) {
			return i + 1
		}
	}
	t.Fatalf("%q not found", substr)
	return 0
}

// parseErr asserts that parsing src fails with a *ParseError and returns it.
func parseErr(t *testing.T, src string) *ParseError {
	t.Helper()
	_, err := Parse([]byte(src))
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("got error %v (%T), want *ParseError", err, err)
	}
	return pe
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// board builds a small board file with the default columns around cards.
func board(cards string) string {
	return "---\nwaypoint: 1\nname: T\nprefix: T\nnext_id: 5\n---\n\n" + cards
}

func ids(cards []domain.Card) []domain.CardID {
	var out []domain.CardID
	for _, c := range cards {
		out = append(out, c.ID)
	}
	return out
}

func TestParseBasic(t *testing.T) {
	b, assigned, err := ParseAssigned([]byte(readFixture(t, "basic.md")))
	assertNoErr(t, err)

	s := b.Settings
	assertEqual(t, s.Name, "Pharmacy POS")
	assertEqual(t, s.Prefix, "T")
	assertEqual(t, s.NextID, 113) // the id-less card took 112
	assertEqual(t, s.Columns, []domain.Column{
		{Name: "Todo", MovesTo: []string{"In progress", "Done"}},
		{Name: "In progress", WIP: 2, MovesTo: []string{"Todo", "Done"}},
		{Name: "Done", Done: true, MovesTo: []string{"In progress"}},
	})
	assertEqual(t, s.Groups, []domain.Group{
		{Name: "Security hardening", Color: "blue"},
		{Name: "Purchasing & receiving", Color: "amber"},
	})
	assertEqual(t, s.Priorities, []string{"P0", "P1", "P2"}) // default
	assertEqual(t, s.Archive, domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 14})
	assertEqual(t, s.Appearance, domain.Appearance{Accent: "#7c3aed"})

	assertEqual(t, len(b.Cards), 5)
	assertEqual(t, ids(b.CardsIn("Todo")), []domain.CardID{"T-019", "T-020", "T-112"})
	assertEqual(t, ids(b.CardsIn("In progress")), []domain.CardID{"T-017"})
	assertEqual(t, ids(b.CardsIn("Done")), []domain.CardID{"T-010"})
	assertEqual(t, assigned, []domain.CardID{"T-112"})

	c19, _, _ := b.Find("T-019")
	assertEqual(t, c19.Title, "Authenticate the /ws WebSocket")
	assertEqual(t, c19.Group, "Security hardening")
	assertEqual(t, c19.Priority, "P0")
	assertEqual(t, c19.Depends, []domain.CardID{"T-017", "T-018"})
	assertEqual(t, c19.Created, date(2026, 10, 26))
	assertEqual(t, c19.Body, "`/ws` bypasses the middleware stack… (free markdown, checklists allowed)\n\n- [ ] step one\n- [x] step two")

	c20, _, _ := b.Find("T-020")
	assertEqual(t, c20.Extra, []domain.Field{{Key: "estimate", Value: "small"}, {Key: "reviewer", Value: "dana"}})
	assertEqual(t, c20.Body, "Sketch of the query:\n\n```sql\n### not a card\nSELECT * FROM stock;\n```\n\n#### Notes\n\nKeep the supplier list small.")

	c10, _, _ := b.Find("T-010")
	assertEqual(t, c10.Done, date(2026, 10, 15))

	plumber, _, _ := b.Find("T-112")
	assertEqual(t, plumber.Title, "Call the plumber")
	assertEqual(t, plumber.Column, "Todo")
	assertEqual(t, plumber.Body, "")
	assertEqual(t, len(b.Archived), 0)
}

func TestParseCRLF(t *testing.T) {
	src := readFixture(t, "basic.md")
	want, err := Parse([]byte(src))
	assertNoErr(t, err)
	got, err := Parse([]byte(strings.ReplaceAll(src, "\n", "\r\n")))
	assertNoErr(t, err)
	assertEqual(t, got, want)
}

func TestParseBOM(t *testing.T) {
	src := readFixture(t, "basic.md")
	want, err := Parse([]byte(src))
	assertNoErr(t, err)
	got, err := Parse([]byte("\xef\xbb\xbf" + src))
	assertNoErr(t, err)
	assertEqual(t, got, want)
}

func TestParseDuplicateID(t *testing.T) {
	src := board("## Todo\n\n### T-19 One\n\n## Done\n\n### T-19 Two\n")
	pe := parseErr(t, src)
	first := lineOf(t, src, "T-19 One")
	second := lineOf(t, src, "T-19 Two")
	assertEqual(t, pe.Line, second)
	assertEqual(t, pe.Msg, "duplicate card id T-19 (first on line "+strconv.Itoa(first)+")")
}

func TestParseUnknownColumn(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n\n## In Progres\n\n### T-2 Two\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "## In Progres"))
	assertEqual(t, pe.Msg, `unknown column "In Progres"`)
	assertEqual(t, pe.Error(), "line "+strconv.Itoa(pe.Line)+`: unknown column "In Progres"`)
}

func TestParseNoFrontMatter(t *testing.T) {
	for name, src := range map[string]string{
		"plain markdown":  "# Notes\n\nJust some text.\n",
		"empty":           "",
		"no waypoint key": "---\ntitle: a blog post\n---\n\nHello\n",
	} {
		_, err := Parse([]byte(src))
		if !errors.Is(err, ErrNotABoard) {
			t.Errorf("%s: got %v, want ErrNotABoard", name, err)
		}
	}
}

func TestParseCardBeforeColumn(t *testing.T) {
	src := board("### T-1 Orphan\n\n## Todo\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "T-1 Orphan"))
}

func TestParseBadDate(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n- created: 26/10/2026\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "26/10/2026"))
	for _, field := range []string{"done", "created"} {
		src := board("## Todo\n\n### T-1 One\n- " + field + ": 2026-02-30\n")
		assertEqual(t, parseErr(t, src).Line, lineOf(t, src, "2026-02-30"))
	}
}

func TestParseFrontMatterErrors(t *testing.T) {
	cases := map[string]struct {
		src  string
		line int
		msg  string
	}{
		"unsupported version": {"---\nwaypoint: 2\n---\n", 2, "unsupported format version 2"},
		"invalid yaml":        {"---\nwaypoint: 1\nname: a: b\n---\n", 3, ""},
		"wrong type":          {"---\nwaypoint: 1\nnext_id: lots\n---\n", 3, ""},
		"unknown key":         {"---\nwaypoint: 1\ncolums: []\n---\n", 3, ""},
		"unclosed":            {"---\nwaypoint: 1\n", 1, "front matter is not closed (missing second ---)"},
		"bad prefix":          {"---\nwaypoint: 1\nprefix: 9x\n---\n", 1, ""},
		"duplicate column":    {"---\nwaypoint: 1\ncolumns: [{name: A}, {name: A}]\n---\n", 1, ""},
	}
	for name, c := range cases {
		pe := parseErr(t, c.src)
		if pe.Line != c.line {
			t.Errorf("%s: line %d, want %d (%v)", name, pe.Line, c.line, pe)
		}
		if c.msg != "" && pe.Msg != c.msg {
			t.Errorf("%s: msg %q, want %q", name, pe.Msg, c.msg)
		}
	}
}

func TestParseDefaults(t *testing.T) {
	b, err := Parse([]byte("---\nwaypoint: 1\nname: Mini\n---\n"))
	assertNoErr(t, err)
	want := domain.NewBoard("Mini", "T")
	assertEqual(t, b, want)
}

func TestParseKeepsExplicitEmptyLists(t *testing.T) {
	b, err := Parse([]byte("---\nwaypoint: 1\nprefix: K\npriorities: []\ngroups: []\n---\n"))
	assertNoErr(t, err)
	assertEqual(t, len(b.Settings.Priorities), 0)
	assertEqual(t, b.Settings.Prefix, "K")
}

func TestParseDuplicateField(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n- priority: P0\n- group: A\n- priority: P1\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "priority: P1"))
	assertEqual(t, pe.Msg, `duplicate field "priority"`)

	src = board("## Todo\n\n### T-1 One\n- estimate: 1\n- estimate: 2\n")
	assertEqual(t, parseErr(t, src).Msg, `duplicate field "estimate"`)
}

func TestParseFieldRules(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n\n- group: Nope   \n- priority: P9\n- note:  two  spaces: and a colon \n- empty:\n- Upper: Case\n- more: ignored, in the body now\n")
	b, err := Parse([]byte(src))
	assertNoErr(t, err)
	c := b.Cards[0]
	// unknown group and priority are kept verbatim, with trailing space trimmed
	assertEqual(t, c.Group, "Nope")
	assertEqual(t, c.Priority, "P9")
	assertEqual(t, c.Extra, []domain.Field{{Key: "note", Value: " two  spaces: and a colon"}, {Key: "empty", Value: ""}})
	// a line that is not a valid field ends the field block
	assertEqual(t, c.Body, "- Upper: Case\n- more: ignored, in the body now")
	assertEqual(t, len(b.Warnings()) > 0, true)
}

func TestParseBodyAfterBlankLineIsNotFields(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n- priority: P0\n\n- estimate: this is body text\n")
	b, err := Parse([]byte(src))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Extra, []domain.Field(nil))
	assertEqual(t, b.Cards[0].Body, "- estimate: this is body text")
}

func TestParseDepends(t *testing.T) {
	b, err := Parse([]byte(board("## Todo\n\n### T-1 One\n- depends: T-2,T-3 ,  T-4,\n")))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Depends, []domain.CardID{"T-2", "T-3", "T-4"})

	src := board("## Todo\n\n### T-1 One\n- depends: T-2 T-3\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "depends"))
}

func TestParseHeadingsInFencesAndBodies(t *testing.T) {
	body := "~~~\n## Todo\n### T-9 fake\n~~~\n\n````\n```\n### still fake\n````\n\n#### deep\n"
	b, err := Parse([]byte(board("## Todo\n\n### T-1 One\n\n" + body)))
	assertNoErr(t, err)
	assertEqual(t, len(b.Cards), 1)
	assertEqual(t, b.Cards[0].Body, strings.TrimSpace(body))
}

func TestParseBodyTrimsOnlyBlankLines(t *testing.T) {
	b, err := Parse([]byte(board("## Todo\n\n### T-1 One\n- priority: P0\n\n\n   \n    indented code  \n\nlast line  \n\n\n### T-2 Two\n")))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Body, "    indented code  \n\nlast line  ")
}

func TestParseUnclosedFenceSwallowingHeadings(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n\n```\ncode\n\n### T-2 Two\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "### T-2 Two"))
	if !strings.Contains(pe.Msg, "never closed") {
		t.Fatalf("msg %q", pe.Msg)
	}

	// An unclosed fence that hides no heading is fine.
	b, err := Parse([]byte(board("## Todo\n\n### T-1 One\n\n```\ncode\n")))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Body, "```\ncode")
}

func TestParseSortsByColumnThenFileOrder(t *testing.T) {
	src := board("## Done\n\n### T-3 C\n\n## Todo\n\n### T-1 A\n\n## Done\n\n### T-4 D\n\n## Todo\n\n### T-2 B\n")
	b, err := Parse([]byte(src))
	assertNoErr(t, err)
	assertEqual(t, ids(b.Cards), []domain.CardID{"T-1", "T-2", "T-3", "T-4"})
}

func TestParseIDAssignment(t *testing.T) {
	// An explicit id at or above next_id bumps it before id-less cards get theirs.
	src := "---\nwaypoint: 1\nprefix: T\nnext_id: 112\n---\n\n## Todo\n\n### First idless\n\n### T-200 Hand added\n\n### Second idless\n\n### X-900 Other prefix\n\n### T-5 Low\n"
	b, assigned, err := ParseAssigned([]byte(src))
	assertNoErr(t, err)
	assertEqual(t, assigned, []domain.CardID{"T-201", "T-202", "T-203"})
	assertEqual(t, b.Settings.NextID, 204)
	assertEqual(t, ids(b.Cards), []domain.CardID{"T-201", "T-200", "T-202", "T-203", "T-5"})
	assertEqual(t, b.Cards[3].Title, "X-900 Other prefix")
}

func TestParseCardHeadingErrors(t *testing.T) {
	src := board("## Todo\n\n### T-1\n")
	pe := parseErr(t, src)
	assertEqual(t, pe.Line, lineOf(t, src, "### T-1"))
	src = board("## Todo\n\n###\n")
	assertEqual(t, parseErr(t, src).Line, lineOf(t, src, "###"))
}

func TestParseHeadingWithTab(t *testing.T) {
	b, err := Parse([]byte(board("## Todo\n\n###\tT-1\tTabbed title\n")))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].ID, domain.CardID("T-1"))
	assertEqual(t, b.Cards[0].Title, "Tabbed title")
}

func TestParseTitleKeepsInnerText(t *testing.T) {
	b, err := Parse([]byte(board("## Todo\n\n### T-1   Fix: the   gate  \n")))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Title, "Fix: the   gate")
}

func TestParseArchive(t *testing.T) {
	src := "# Archive\n\n## 2026-10\n\n### T-9 Old thing\n- priority: P1\n- created: 2026-09-20\n- done: 2026-10-12\n\nBody **here**.\n\n### T-8 Older\n- done: 2026-10-02\n\n## 2026-09\n\n### T-3 Oldest\n- done: 2026-09-01\n"
	cards, err := ParseArchive([]byte(src), domain.NewBoard("x", "T").Settings)
	assertNoErr(t, err)
	assertEqual(t, ids(cards), []domain.CardID{"T-9", "T-8", "T-3"})
	assertEqual(t, cards[0].Column, "")
	assertEqual(t, cards[0].Done, date(2026, 10, 12))
	assertEqual(t, cards[0].Body, "Body **here**.")
	assertEqual(t, cards[1].Done, date(2026, 10, 2))
	assertEqual(t, cards[2].Done, date(2026, 9, 1))
}

func TestParseArchiveEmpty(t *testing.T) {
	for _, src := range []string{"", "# Archive\n"} {
		cards, err := ParseArchive([]byte(src), domain.NewBoard("x", "T").Settings)
		assertNoErr(t, err)
		assertEqual(t, len(cards), 0)
	}
}

func TestParseArchiveErrors(t *testing.T) {
	cases := map[string]struct{ src, at string }{
		"no done date":  {"# Archive\n\n## 2026-10\n\n### T-9 Old\n- created: 2026-09-01\n", "### T-9"},
		"bad month":     {"# Archive\n\n## October\n\n### T-9 Old\n- done: 2026-10-01\n", "## October"},
		"month 13":      {"# Archive\n\n## 2026-13\n\n### T-9 Old\n- done: 2026-10-01\n", "## 2026-13"},
		"no id":         {"# Archive\n\n## 2026-10\n\n### Old\n- done: 2026-10-01\n", "### Old"},
		"before month":  {"# Archive\n\n### T-9 Old\n- done: 2026-10-01\n", "### T-9"},
		"duplicate ids": {"# Archive\n\n## 2026-10\n\n### T-9 A\n- done: 2026-10-01\n\n### T-9 B\n- done: 2026-10-01\n", "### T-9 B"},
	}
	for name, c := range cases {
		_, err := ParseArchive([]byte(c.src), domain.NewBoard("x", "T").Settings)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: got %v, want ParseError", name, err)
			continue
		}
		if want := lineOf(t, c.src, c.at); pe.Line != want {
			t.Errorf("%s: line %d, want %d (%v)", name, pe.Line, want, pe)
		}
	}
}

func TestParseTextOutsideCards(t *testing.T) {
	const msg = "text outside a card would be lost: move it into a card or delete it"
	cases := map[string]struct{ src, at string }{
		"h1 before first column": {board("# My board\n\n## Todo\n\n### T-1 One\n"), "# My board"},
		"intro text":             {board("Some intro.\n\n## Todo\n"), "Some intro."},
		"note under column":      {board("## Todo\n\nA note.\n\n### T-1 One\n"), "A note."},
		"note in empty column":   {board("## Todo\n\n### T-1 One\n\n## Done\n\nTrailing note\n"), "Trailing note"},
		"fence outside card":     {board("## Todo\n\n```\ncode\n```\n\n### T-1 One\n"), "```"},
		"heading ends card body": {board("## Todo\n\n### T-1 One\n\nbody\n\n## Done\n\nmore text\n"), "more text"},
	}
	for name, c := range cases {
		_, err := Parse([]byte(c.src))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: got %v, want ParseError", name, err)
			continue
		}
		if want := lineOf(t, c.src, c.at); pe.Line != want || pe.Msg != msg {
			t.Errorf("%s: got line %d %q, want line %d", name, pe.Line, pe.Msg, want)
		}
	}
	// Blank lines are fine, and so is a board with no cards.
	_, err := Parse([]byte(board("\n\n## Todo\n\n   \n\n## Done\n\n")))
	assertNoErr(t, err)
}

func TestParseArchiveTextOutsideCards(t *testing.T) {
	s := domain.NewBoard("x", "T").Settings
	for name, src := range map[string]string{
		"other title":   "# History\n\n## 2026-10\n",
		"intro":         "# Archive\n\nOld stuff.\n\n## 2026-10\n",
		"note in month": "# Archive\n\n## 2026-10\n\nA note.\n\n### T-9 Old\n- done: 2026-10-01\n",
	} {
		_, err := ParseArchive([]byte(src), s)
		var pe *ParseError
		if !errors.As(err, &pe) || !strings.HasPrefix(pe.Msg, "text outside a card") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	cards, err := ParseArchive([]byte("\n# Archive\n\n## 2026-10\n\n### T-9 Old\n- done: 2026-10-01\n"), s)
	assertNoErr(t, err)
	assertEqual(t, len(cards), 1)
}

func TestParseNoticeForCardHeadingInCodeBlock(t *testing.T) {
	// T-1 has a stray fence; T-2 and T-3 are swallowed into its body until the
	// lone fence line ends the block, so nothing is unclosed and nothing errors.
	src := board("## Todo\n\n### T-1 One\n\n```\nstray\n\n### T-2 Two\n\n### T-3 Three\n\n```\n\n## Done\n\n### T-4 Four\n")
	b, err := Parse([]byte(src))
	assertNoErr(t, err)
	assertEqual(t, ids(b.Cards), []domain.CardID{"T-1", "T-4"})
	opened := lineOf(t, src, "```")
	assertEqual(t, b.Notices, []string{
		"line " + strconv.Itoa(lineOf(t, src, "T-2 Two")) + ": this looks like a card heading inside a code block opened on line " + strconv.Itoa(opened) + "; if the block should have ended earlier, close it",
		"line " + strconv.Itoa(lineOf(t, src, "T-3 Three")) + ": this looks like a card heading inside a code block opened on line " + strconv.Itoa(opened) + "; if the block should have ended earlier, close it",
	})
	assertEqual(t, b.Warnings(), b.Notices)

	// The same holds with CRLF line endings.
	b2, err := Parse([]byte(strings.ReplaceAll(src, "\n", "\r\n")))
	assertNoErr(t, err)
	assertEqual(t, b2, b)
}

func TestParseNoticeForColumnHeadingInCodeBlock(t *testing.T) {
	src := board("## Todo\n\n### T-1 One\n\n```md\n## Done\n## Elsewhere\n```\n")
	b, err := Parse([]byte(src))
	assertNoErr(t, err)
	assertEqual(t, len(b.Notices), 1)
	if !strings.HasPrefix(b.Notices[0], "line "+strconv.Itoa(lineOf(t, src, "## Done"))+": ") {
		t.Fatalf("notice %q", b.Notices[0])
	}
}

func TestParseNoNoticeForOrdinaryHeadingInCodeBlock(t *testing.T) {
	for _, line := range []string{"### not a card", "### X-1 other prefix", "### T-x not digits", "### T-3", "#### T-3 deeper", "  ### T-3 indented", "## Archive"} {
		b, err := Parse([]byte(board("## Todo\n\n### T-1 One\n\n```\n" + line + "\n```\n")))
		assertNoErr(t, err)
		if len(b.Notices) != 0 {
			t.Errorf("%q: unexpected notices %v", line, b.Notices)
		}
	}
}

func TestSecondBlankLineEndsFieldBlock(t *testing.T) {
	const head = "---\nwaypoint: 1\nname: x\nprefix: T\nnext_id: 5\n---\n\n## Todo\n\n"
	// One blank line may separate the heading from its fields.
	b, err := Parse([]byte(head + "### T-1 One\n\n- group: A\n\nbody\n"))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Group, "A")
	assertEqual(t, b.Cards[0].Body, "body")

	// A second blank line means the card has no fields, so a list item that
	// looks like a field is body text.
	b, err = Parse([]byte(head + "### T-1 One\n\n\n- group: A\n- note: b\n"))
	assertNoErr(t, err)
	assertEqual(t, b.Cards[0].Group, "")
	assertEqual(t, b.Cards[0].Extra, []domain.Field(nil))
	assertEqual(t, b.Cards[0].Body, "- group: A\n- note: b")
}

func TestHeadingFirstWordIsAnIDOnlyWithTheBoardsPrefix(t *testing.T) {
	const head = "---\nwaypoint: 1\nname: x\nprefix: T\nnext_id: 5\n---\n\n## Todo\n\n"
	cases := []struct {
		heading string
		id      domain.CardID
		title   string
		given   bool // the id was assigned, not read
	}{
		{"### COVID-19 booster", "T-5", "COVID-19 booster", true},
		{"### COVID-19", "T-5", "COVID-19", true},
		{"### t-1 lower case", "T-5", "t-1 lower case", true},
		{"### T-2 Mine", "T-2", "Mine", false},
		{"### TT-2 Longer prefix", "T-5", "TT-2 Longer prefix", true},
	}
	for _, c := range cases {
		t.Run(c.heading, func(t *testing.T) {
			b, assigned, err := ParseAssigned([]byte(head + c.heading + "\n"))
			assertNoErr(t, err)
			assertEqual(t, b.Cards[0].ID, c.id)
			assertEqual(t, b.Cards[0].Title, c.title)
			assertEqual(t, len(assigned) == 1, c.given)
		})
	}
}

func TestBareIDOfThisBoardStillNeedsATitle(t *testing.T) {
	_, err := Parse([]byte("---\nwaypoint: 1\nprefix: T\n---\n\n## Todo\n\n### T-3\n"))
	var pe *ParseError
	if !errors.As(err, &pe) || !strings.Contains(pe.Msg, "no title") {
		t.Fatalf("got %v", err)
	}
}

func TestArchiveKeepsAnIDOfAnyPrefix(t *testing.T) {
	src := "# Archive\n\n## 2026-10\n\n### OLD-7 From another board\n\n- done: 2026-10-01\n"
	cards, err := ParseArchive([]byte(src), domain.Settings{})
	assertNoErr(t, err)
	assertEqual(t, cards[0].ID, domain.CardID("OLD-7"))
	assertEqual(t, cards[0].Title, "From another board")

	_, err = ParseArchive([]byte("# Archive\n\n## 2026-10\n\n### No id here\n\n- done: 2026-10-01\n"), domain.Settings{})
	if err == nil || !strings.Contains(err.Error(), "has no id") {
		t.Fatalf("got %v", err)
	}
}

func TestDatesRoundTripThroughFormatAndParse(t *testing.T) {
	cases := []struct{ written, stored string }{
		{"2026-10-26", "2026-10-26"},
		{"2026-11-04T23:52:10Z", "2026-11-04T23:52:10Z"},
		{"2026-11-04T10:00:00+01:00", "2026-11-04T09:00:00Z"},
		{"2026-11-04T10:00:00-05:30", "2026-11-04T15:30:00Z"},
		{"2026-11-04T23:52:10.75Z", "2026-11-04T23:52:10Z"},
		{"2026-11-04T00:00:00Z", "2026-11-04"},
		{"2026-11-04T01:00:00+01:00", "2026-11-04"},
	}
	for _, c := range cases {
		t.Run(c.written, func(t *testing.T) {
			b, err := Parse([]byte(board("## Todo\n\n### T-1 Card\n\n- created: " + c.written + "\n")))
			assertNoErr(t, err)

			once := Format(b)
			if want := "- created: " + c.stored + "\n"; !strings.Contains(string(once), want) {
				t.Fatalf("%q not in\n%s", want, once)
			}
			again, err := Parse(once)
			assertNoErr(t, err)
			assertEqual(t, string(Format(again)), string(once))
		})
	}
}

func TestParseRejectsABadDateOnItsLine(t *testing.T) {
	for _, value := range []string{"2026-11-04T25:00:00Z", "2026-11-04 10:00", "2026-11-04T10:00:00", "soon"} {
		src := board("## Todo\n\n### T-1 Card\n\n- created: " + value + "\n")
		assertEqual(t, parseErr(t, src).Line, lineOf(t, src, "- created:"))
	}
}

func TestArchiveSectionIsTheMonthOfTheStoredUTCValue(t *testing.T) {
	done := time.Date(2026, 10, 31, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600)) // 2026-11-01 04:30 UTC
	got := string(FormatArchive([]domain.Card{{ID: "T-1", Title: "A", Done: domain.Stamp(done)}}))
	if !strings.Contains(got, "## 2026-11\n") || !strings.Contains(got, "- done: 2026-11-01T04:30:00Z\n") {
		t.Fatalf("got\n%s", got)
	}
}
