package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"waypoint/internal/app"
	"waypoint/internal/domain"
	"waypoint/internal/storage/markdown"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	noErr(t, err)
	return string(data)
}

func importCSV(e *env, text string, create bool) (app.CSVResult, error) {
	return e.svc.ImportCSV(context.Background(), strings.NewReader(text), app.CSVOptions{Create: create})
}

func mustInvalid(t *testing.T, err error, parts ...string) {
	t.Helper()
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("want an ErrInvalid error, got %v", err)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q should mention %q", err, p)
		}
	}
}

func TestCSVRoundTrip(t *testing.T) {
	src := richService(t)
	var buf bytes.Buffer
	noErr(t, src.svc.ExportCSV(context.Background(), &buf))
	text := buf.String()
	if !strings.HasPrefix(text, "id,title,column,group,priority,depends,created,done,body,estimate\n") {
		t.Fatalf("header: %q", text)
	}
	if strings.Contains(text, "\r") || !strings.Contains(text, ",Archived,") {
		t.Fatalf("want \\n endings and an Archived row:\n%s", text)
	}

	want := wholeBoard(t, src)
	dst := newEnv(t, func(s *domain.Settings) { *s = want.Settings; s.NextID = 1 })
	res, err := importCSV(dst, text, false)
	noErr(t, err)
	equal(t, len(res.Created), 3)
	got := wholeBoard(t, dst)
	same := func(a, b []domain.Card) bool { return slices.EqualFunc(a, b, domain.Card.Equal) }
	if !same(got.Cards, want.Cards) || !same(got.Archived, want.Archived) {
		t.Fatalf("cards differ:\n got %#v / %#v\nwant %#v / %#v", got.Cards, got.Archived, want.Cards, want.Archived)
	}
}

func TestCSVSpreadsheetQuirks(t *testing.T) {
	e := newEnv(t)
	res, err := importCSV(e, fixture(t, "quirks.csv"), false)
	noErr(t, err)
	equal(t, len(res.Created), 3)
	b := e.board()
	equal(t, len(b.Cards), 3)
	equal(t, b.Cards[0].Title, "Quoted, comma")
	equal(t, b.Cards[0].Body, "line one\nline two, with comma")
	equal(t, b.Cards[0].Priority, "P1")
	equal(t, b.Cards[1].Title, "Short row")
	equal(t, b.Cards[1].Column, "Todo") // an empty column means the first
	equal(t, b.Cards[1].Group, "")
	equal(t, b.Cards[2].Column, "In progress")
}

func TestCSVOverlongRowNamesTheLine(t *testing.T) {
	e := newEnv(t)
	_, err := importCSV(e, fixture(t, "toolong.csv"), false)
	mustInvalid(t, err, "line 3")
	equal(t, len(e.board().Cards), 0)
}

func TestCSVUpdatesByID(t *testing.T) {
	e := richService(t)
	before := wholeBoard(t, e)
	archivedID := before.Archived[0].ID
	text := "id,title,column,priority,body\n" +
		"T-1,First renamed,In progress,P2,new body\n" +
		string(archivedID) + ",Old news updated,Todo,,Changed\n" +
		",Brand new,Todo,,\n"
	res, err := importCSV(e, text, false)
	noErr(t, err)
	equal(t, res.Updated, []domain.CardID{"T-1", archivedID})
	equal(t, len(res.Created), 1)

	got := wholeBoard(t, e)
	c, archived, ok := got.Find("T-1")
	equal(t, ok, true)
	equal(t, archived, false)
	equal(t, c.Title, "First renamed")
	equal(t, c.Column, "In progress")
	equal(t, c.Group, "Platform") // a column the file lacks is left alone
	a, archived, ok := got.Find(archivedID)
	equal(t, ok, true)
	equal(t, archived, true) // stays archived
	equal(t, a.Title, "Old news updated")
	equal(t, a.Body, "Changed")
}

func TestCSVDuplicateIDNamesBothLines(t *testing.T) {
	e := newEnv(t)
	_, err := importCSV(e, fixture(t, "dupid.csv"), false)
	mustInvalid(t, err, "T-1", "line 2", "line 4")
}

func TestCSVUnknownColumnNeedsCreate(t *testing.T) {
	e := newEnv(t)
	_, err := importCSV(e, fixture(t, "unknown.csv"), false)
	mustInvalid(t, err, "Review", "QA", "Ops", "Web")
	equal(t, len(e.board().Cards), 0)

	res, err := importCSV(e, fixture(t, "unknown.csv"), true)
	noErr(t, err)
	equal(t, res.NewColumns, []string{"Review", "QA"})
	equal(t, res.NewGroups, []string{"Ops", "Web"})
	b := e.board()
	var cols, groups []string
	for _, c := range b.Settings.Columns {
		cols = append(cols, c.Name)
	}
	for _, g := range b.Settings.Groups {
		groups = append(groups, g.Name)
		if g.Color == "" {
			t.Fatalf("group %q has no colour", g.Name)
		}
	}
	equal(t, cols, []string{"Todo", "In progress", "Done", "Review", "QA"})
	equal(t, groups, []string{"Ops", "Web"})
	equal(t, b.Cards[0].Extra, []domain.Field{{Key: "estimate", Value: "small"}})
	noErr(t, b.Validate())
	// A new column is usable: a card can move out of it.
	_, err = e.svc.MoveCard(context.Background(), b.CardsIn("Review")[0].ID, "Todo", 0)
	noErr(t, err)
}

func TestCSVAllOrNothing(t *testing.T) {
	e := richService(t)
	before := e.file()
	archiveBefore, _ := os.ReadFile(markdown.ArchiveFile(e.path))
	_, err := importCSV(e, fixture(t, "badrow.csv"), false)
	mustInvalid(t, err, "line 6")
	equal(t, e.file(), before)
	archiveAfter, _ := os.ReadFile(markdown.ArchiveFile(e.path))
	equal(t, string(archiveAfter), string(archiveBefore))
}

func TestCSVEmptyTitleRefused(t *testing.T) {
	e := newEnv(t)
	_, err := importCSV(e, "title,column\n  ,Todo\n", false)
	mustInvalid(t, err, "line 2", "title")
}

func TestCSVBadPriorityRefused(t *testing.T) {
	e := newEnv(t)
	_, err := importCSV(e, "title,priority\nA,P9\n", true) // Create does not cover priorities
	mustInvalid(t, err, "line 2", "P9")
	equal(t, len(e.board().Cards), 0)
}

func TestCSVOtherRefusals(t *testing.T) {
	e := newEnv(t)
	for name, text := range map[string]string{
		"no title column":  "id,column\nT-1,Todo\n",
		"empty file":       "",
		"duplicate header": "title,title\nA,B\n",
		"bad date":         "title,created\nA,yesterday\n",
		"lowercase id":     "id,title\nt-1,A\n",
		"bad extra header": "title,Bad Key\nA,B\n",
		"bad depends":      "title,depends\nA,nope\n",
		"unterminated":     "title\n\"A\n",
	} {
		if _, err := importCSV(e, text, true); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	equal(t, len(e.board().Cards), 0)
}

func TestCSVRowOrderIsRank(t *testing.T) {
	e := newEnv(t)
	e.create("Existing", "Todo")
	_, err := importCSV(e, "title,column\nZ,Todo\nA,Todo\nM,In progress\n", false)
	noErr(t, err)
	var titles []string
	for _, c := range e.board().CardsIn("Todo") {
		titles = append(titles, c.Title)
	}
	equal(t, titles, []string{"Existing", "Z", "A"})
}

func TestMarkdownExportParses(t *testing.T) {
	e := richService(t)
	var buf bytes.Buffer
	noErr(t, e.svc.ExportMarkdown(context.Background(), &buf, app.MarkdownFormat{Board: markdown.Format, Archive: markdown.FormatArchive}))
	boardText, archiveText, found := strings.Cut(buf.String(), "\n# Archive\n")
	equal(t, found, true)

	want := wholeBoard(t, e)
	got, err := markdown.Parse([]byte(boardText))
	noErr(t, err)
	got.Archived, err = markdown.ParseArchive([]byte("# Archive\n"+archiveText), got.Settings)
	noErr(t, err)
	if !got.Equal(want) {
		t.Fatalf("parsed export differs:\n got %#v\nwant %#v", got, want)
	}
}

func TestCSVKeepsTheBodyExactlyAsWritten(t *testing.T) {
	e := newEnv(t)
	// Storage drops trailing blank lines, as it does for any body; the start must survive.
	text := "title,body\nKeep my spaces,\"    indented code first\n\n- [ ] step\n\"\n"
	_, err := importCSV(e, text, false)
	noErr(t, err)
	b := wholeBoard(t, e)
	equal(t, len(b.Cards), 1)
	equal(t, b.Cards[0].Body, "    indented code first\n\n- [ ] step")
}

func TestCSVArchivedMarkerIgnoresCase(t *testing.T) {
	e := newEnv(t)
	res, err := importCSV(e, "title,column,done\nOld,archived,2026-10-01\n", true)
	noErr(t, err)
	equal(t, len(res.NewColumns), 0)
	b := wholeBoard(t, e)
	equal(t, len(b.Cards), 0)
	equal(t, len(b.Archived), 1)
	for _, c := range b.Settings.Columns {
		if strings.EqualFold(c.Name, "archived") {
			t.Fatalf("a column %q was created from the archived marker", c.Name)
		}
	}
}

func TestCSVRefusesABoardWithAColumnCalledArchived(t *testing.T) {
	e := newEnv(t, func(s *domain.Settings) {
		s.Columns = append(s.Columns, domain.Column{Name: "Archived", MovesTo: []string{"Todo"}})
	})
	_, err := importCSV(e, "title\nOne\n", false)
	mustInvalid(t, err, "Archived", "rename")
	var buf bytes.Buffer
	mustInvalid(t, e.svc.ExportCSV(context.Background(), &buf), "Archived", "rename")
	equal(t, buf.Len(), 0)
}
