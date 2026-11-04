package markdown

import (
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"waypoint/docs"
	"waypoint/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := "testdata/" + name
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("output differs from %s.\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func mustParse(t *testing.T, src string) *domain.Board {
	t.Helper()
	b, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v\n%s", err, src)
	}
	return b
}

func TestFormatGolden(t *testing.T) {
	b := mustParse(t, readFixture(t, "basic.md"))
	golden(t, "basic.canonical.md", Format(b))
}

func TestRoundTripStable(t *testing.T) {
	inputs := map[string]string{
		"basic": readFixture(t, "basic.md"),
		"messy": "---\nwaypoint: 1\nname: m\nprefix: T\nnext_id: 3\n---\n\n## In progress\n### T-2 B\n- note: trailing   \n- done:\n\n\n\n- group: x\n## Todo\n### T-1 A\n- z: 1\n- a: 2\n\ntext\n",
	}
	for name, src := range inputs {
		first := Format(mustParse(t, src))
		second := Format(mustParse(t, string(first)))
		if string(first) != string(second) {
			t.Errorf("%s: second pass differs.\n--- first ---\n%s\n--- second ---\n%s", name, first, second)
		}
		if !strings.HasSuffix(string(first), "\n") || strings.HasSuffix(string(first), "\n\n") {
			t.Errorf("%s: file must end with exactly one newline: %q", name, first[max(0, len(first)-10):])
		}
	}
}

func TestBodyPreserved(t *testing.T) {
	body := "Trailing spaces   \n\ttabbed line\nunicode: café — 日本語\n\n```sh\n# comment\n## not a heading\n### T-9 not a card\n```\n\n## Heading\n\nlast line  "
	want := strings.Replace(body, "\n## Heading\n", "\n#### Heading\n", 1)

	b := domain.NewBoard("x", "T")
	b.Settings.NextID = 2
	b.Cards = []domain.Card{{ID: "T-1", Title: "One", Column: "Todo", Body: body}}
	out := Format(b)

	got := mustParse(t, string(out)).Cards[0].Body
	if got != want {
		t.Fatalf("body changed.\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	d := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cards := []domain.Card{
		{ID: "T-5", Title: "Newest", Created: d("2026-10-01"), Done: d("2026-10-20"), Priority: "P1", Body: "done well"},
		{ID: "T-6", Title: "Also October", Done: d("2026-10-02"), Depends: []domain.CardID{"T-1", "T-2"}},
		{ID: "T-3", Title: "Older", Done: d("2026-09-15"), Group: "G", Extra: []domain.Field{{Key: "k", Value: "v"}}},
	}
	out := string(FormatArchive(cards))
	if !strings.HasPrefix(out, "# Archive\n\n## 2026-10\n\n") {
		t.Fatalf("unexpected start:\n%s", out)
	}
	if strings.Index(out, "## 2026-10") > strings.Index(out, "## 2026-09") {
		t.Fatalf("sections must be newest first:\n%s", out)
	}
	back, err := ParseArchive([]byte(out), domain.Settings{})
	assertNoErr(t, err)
	assertEqual(t, back, cards)
	if again := string(FormatArchive(back)); again != out {
		t.Fatalf("archive format is not stable:\n%s\n---\n%s", out, again)
	}
	assertEqual(t, string(FormatArchive(nil)), "# Archive\n")
}

func TestFieldLikeBodyOnFieldlessCard(t *testing.T) {
	b := domain.NewBoard("x", "T")
	b.Settings.NextID = 3
	b.Cards = []domain.Card{
		{ID: "T-1", Title: "One", Column: "Todo", Body: "- group: sneaky\n- k: v"},
		{ID: "T-2", Title: "Two", Column: "Todo", Group: "G", Body: "- priority: P0"},
	}
	out := string(Format(b))
	if !strings.Contains(out, "### T-1 One\n\n\n- group: sneaky") {
		t.Fatalf("fieldless card needs two blank lines before a field-like body:\n%s", out)
	}
	if strings.Contains(out, "### T-2 Two\n\n\n") || !strings.Contains(out, "- group: G\n\n- priority: P0") {
		t.Fatalf("a card with fields needs only the usual blank line:\n%s", out)
	}
	back := mustParse(t, out)
	assertEqual(t, back.Cards, b.Cards)
}

func TestUnclosedFenceIsClosed(t *testing.T) {
	b := domain.NewBoard("x", "T")
	b.Settings.NextID = 4
	b.Cards = []domain.Card{
		{ID: "T-1", Title: "Open", Column: "Todo", Body: "text\n````go\ncode\n### T-9 Fake\n## Done"},
		{ID: "T-2", Title: "Next", Column: "Todo"},
		{ID: "T-3", Title: "Later", Column: "Done"},
	}
	out := Format(b)
	back := mustParse(t, string(out))
	assertEqual(t, len(back.Cards), 3)
	assertEqual(t, back.Cards[0].Body, "text\n````go\ncode\n### T-9 Fake\n## Done\n````")
	assertEqual(t, back.Cards[1].ID, domain.CardID("T-2"))
	assertEqual(t, back.Cards[2].Column, "Done")
	if again := Format(back); string(again) != string(out) {
		t.Fatalf("not stable:\n%s\n---\n%s", out, again)
	}
}

func TestFrontMatterAwkwardNames(t *testing.T) {
	names := []string{
		"a, b", "a: b", "R&D", "#hash", `say "hi"`, "it's", "1st", "007", "yes", "No", "null", "~", "true",
		"2026-10-01", "- dash", "[x]", "{y}", "@at", "%pct", "!bang", "*star", "|pipe", ">gt", "café 日本",
		"a #b", "trailing:", "12", "1e3", "0x1f",
	}
	s := domain.NewBoard("Name: with, awkward & #things", "T").Settings
	s.NextID = 7
	s.Columns = nil
	for i, n := range names {
		c := domain.Column{Name: n, Done: i == len(names)-1}
		if i > 0 {
			c.MovesTo = []string{names[i-1], names[0]}
			if names[i-1] == names[0] {
				c.MovesTo = c.MovesTo[:1]
			}
		}
		if i == 2 {
			c.WIP = 3
		}
		s.Columns = append(s.Columns, c)
	}
	s.Groups = nil
	for i, n := range names {
		s.Groups = append(s.Groups, domain.Group{Name: n + " g", Color: map[bool]string{true: "blue", false: ""}[i%2 == 0]})
		s.Groups = append(s.Groups, domain.Group{Name: n, Color: "#7c3aed"})
	}
	s.Priorities = []string{"yes", "P: 1", "#2"}
	s.Archive = domain.ArchiveSettings{Mode: domain.ArchiveAuto, AfterDays: 30}
	s.Appearance = domain.Appearance{Theme: "dark", Accent: "#7c3aed", Density: "null", Swimlane: "group, x", Textures: "yes"}
	b := &domain.Board{Settings: s, Cards: []domain.Card{{ID: "T-1", Title: "c", Column: names[3]}}}

	out := Format(b)
	back, err := Parse(out)
	if err != nil {
		t.Fatalf("parse failed: %v\n%s", err, out)
	}
	assertEqual(t, back.Settings, s)
	assertEqual(t, back.Cards, b.Cards)
	if again := Format(back); string(again) != string(out) {
		t.Fatalf("not stable:\n%s\n---\n%s", out, again)
	}
}

func TestFrontMatterLayout(t *testing.T) {
	b := domain.NewBoard("Plain", "T")
	out := string(Format(b))
	want := "---\nwaypoint: 1\nname: Plain\nprefix: T\nnext_id: 1\ncolumns:\n" +
		"  - {name: Todo, moves_to: [In progress, Done]}\n" +
		"  - {name: In progress, moves_to: [Todo, Done]}\n" +
		"  - {name: Done, moves_to: [In progress], done: true}\n---\n\n## Todo\n\n## In progress\n\n## Done\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	// Key order follows the spec, and non-default settings are written.
	b.Settings.Groups = []domain.Group{{Name: "G", Color: "blue"}}
	b.Settings.Priorities = []string{"P0"}
	b.Settings.Archive = domain.ArchiveSettings{Mode: domain.ArchiveOff, AfterDays: 3}
	b.Settings.Appearance = domain.Appearance{Accent: "#7c3aed"}
	out = string(Format(b))
	last := -1
	for _, key := range []string{"waypoint:", "\nname:", "\nprefix:", "\nnext_id:", "\ncolumns:", "\ngroups:", "\npriorities:", "\narchive:", "\nappearance:"} {
		i := strings.Index(out, key)
		if i <= last {
			t.Fatalf("key %q missing or out of order in:\n%s", key, out)
		}
		last = i
	}
	assertEqual(t, mustParse(t, out).Settings, b.Settings)

	// An empty priority list is not the default and must survive.
	b.Settings.Priorities = []string{}
	back := mustParse(t, string(Format(b)))
	assertEqual(t, len(back.Settings.Priorities), 0)
}

func TestNoticesAreNotPersisted(t *testing.T) {
	b := mustParse(t, readFixture(t, "basic.md"))
	want := Format(b)
	b.Notices = []string{"line 3: something"}
	if got := Format(b); string(got) != string(want) {
		t.Fatalf("notices changed the output:\n%s", got)
	}
}

func TestExtraFieldValuesAreTrimmed(t *testing.T) {
	b := domain.NewBoard("x", "T")
	b.Settings.NextID = 2
	b.Cards = []domain.Card{{ID: "T-1", Title: "One", Column: "Todo", Group: "G  ", Extra: []domain.Field{{Key: "note", Value: " lead and trail  "}, {Key: "empty", Value: "  "}}}}
	out := Format(b)
	if strings.Contains(string(out), " \n") {
		t.Fatalf("trailing space written:\n%q", out)
	}
	back := mustParse(t, string(out))
	assertEqual(t, back.Cards[0].Extra, []domain.Field{{Key: "note", Value: " lead and trail"}, {Key: "empty", Value: ""}})
	if again := Format(back); string(again) != string(out) {
		t.Fatal("not stable")
	}
}

func TestRoundTripAwkwardBodies(t *testing.T) {
	bodies := []struct {
		name, body, want string
	}{
		{"field-like first line", "- key: v\nmore", "- key: v\nmore"},
		{"known field-like first line", "- group: sneaky", "- group: sneaky"},
		{"empty-value field-like", "- key:", "- key:"},
		{"column heading outside fence", "intro\n## Done\nafter", "intro\n#### Done\nafter"},
		{"column heading first line", "## Done", "#### Done"},
		{"card heading", "### Heading\ntext\n### T-9 Fake card", "#### Heading\ntext\n#### T-9 Fake card"},
		{"h1 and bare hashes", "# One\n##\n###", "#### One\n####\n####"},
		{"only a fence opener", "```", "```\n```"},
		{"opener then blanks", "```\n\n\n", "```\n```"},
		{"tilde opener", "~~~~", "~~~~\n~~~~"},
		{"mixed fences closed", "~~~\n```\n## Done\n~~~\n```", "~~~\n```\n## Done\n~~~\n```\n```"},
		{"tilde inside backticks", "```\n~~~\n### T-9 Fake\n## Done", "```\n~~~\n### T-9 Fake\n## Done\n```"},
		{"backticks inside tildes closed", "~~~\n```\n## Done\n~~~", "~~~\n```\n## Done\n~~~"},
		{"long opener", "`````md\n```\n## Done", "`````md\n```\n## Done\n`````"},
		{"indented opener", "  ```\n## Done", "  ```\n## Done\n```"},
		{"crlf", "line1\r\nline2\r\n## Done\r\n- k: v\r\n", "line1\nline2\n#### Done\n- k: v"},
		{"trailing spaces", "text   ", "text   "},
		{"trailing blank spaces line", "text\n   \n  ", "text"},
		{"empty", "", ""},
		{"whitespace only", " \n\t\n", ""},
		{"unicode", "héllo — 日本語 \U0001F600\n## ✓ done", "héllo — 日本語 \U0001F600\n#### ✓ done"},
		{"front matter lookalike", "---\nwaypoint: 2\n---", "---\nwaypoint: 2\n---"},
		{"leading blank lines", "\n\n- group: x", "- group: x"},
	}
	for _, withFields := range []bool{false, true} {
		for _, tc := range bodies {
			t.Run(tc.name, func(t *testing.T) {
				b := domain.NewBoard("x", "T")
				b.Settings.NextID = 4
				first := domain.Card{ID: "T-1", Title: "Nasty", Column: "Todo", Body: tc.body}
				if withFields {
					first.Group, first.Priority = "G", "P1"
					first.Extra = []domain.Field{{Key: "k", Value: "v"}}
				}
				b.Cards = []domain.Card{
					first,
					{ID: "T-2", Title: "Normal", Column: "Todo", Body: "fine"},
					{ID: "T-3", Title: "Later", Column: "Done", Body: tc.body},
				}
				out := Format(b)
				back, err := Parse(out)
				if err != nil {
					t.Fatalf("parse failed: %v\n%s", err, out)
				}
				ids := func(b *domain.Board) []string {
					var s []string
					for _, c := range b.Cards {
						s = append(s, c.Column+"/"+string(c.ID))
					}
					return s
				}
				assertEqual(t, ids(back), ids(b))
				assertEqual(t, back.Cards[0].Body, tc.want)
				assertEqual(t, back.Cards[1].Body, "fine")
				assertEqual(t, back.Cards[2].Body, tc.want)
				assertEqual(t, back.Cards[0].Group, first.Group)
				assertEqual(t, back.Cards[0].Extra, first.Extra)
				if again := Format(back); string(again) != string(out) {
					t.Fatalf("not stable.\n--- first ---\n%s\n--- second ---\n%s", out, again)
				}
			})
		}
	}
}

// TestDocExampleParses keeps the example in docs/board-format.md valid.
func TestDocExampleParses(t *testing.T) {
	_, rest, ok := strings.Cut(docs.BoardFormat, "````markdown\n")
	if !ok {
		t.Fatal("example not found in board-format.md")
	}
	example, _, _ := strings.Cut(rest, "````")
	b := mustParse(t, example)
	assertEqual(t, len(b.Cards), 4)
	assertEqual(t, b.Cards[1].ID, domain.CardID("T-112"))
}

// A card added by hand under a done heading has no done date. Archiving it must
// still give a file that reads back.
func TestArchiveUndatedDoneCardRoundTrips(t *testing.T) {
	src := "---\nwaypoint: 1\nname: x\nprefix: T\nnext_id: 3\n---\n\n## Todo\n\n## In progress\n\n## Done\n\n### T-1 Hand added\n\n### T-2 Dated\n\n- done: 2026-10-01\n"
	b := mustParse(t, src)
	today := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if err := b.Archive(b.DoneCards(), today); err != nil {
		t.Fatal(err)
	}
	back, err := ParseArchive(FormatArchive(b.Archived), b.Settings)
	if err != nil {
		t.Fatalf("archive does not read back: %v\n%s", err, FormatArchive(b.Archived))
	}
	assertEqual(t, len(back), 2)
	assertEqual(t, back[0].ID, domain.CardID("T-1"))
	assertEqual(t, back[0].Done, today)
}
