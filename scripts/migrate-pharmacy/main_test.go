package main

import (
	"bytes"
	"encoding/csv"
	"os"
	"strings"
	"testing"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func records(t *testing.T, text string) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v\n%s", err, text)
	}
	return rows
}

func TestMigrateSample(t *testing.T) {
	var out bytes.Buffer
	res, err := convert(open(t, "BOARD.md"), open(t, "DONE.md"), &out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/want.csv")
	if err != nil {
		t.Fatal(err)
	}
	got, wantRows := records(t, out.String()), records(t, string(want))
	if len(got) != len(wantRows) {
		t.Fatalf("%d rows, want %d:\n%s", len(got), len(wantRows), out.String())
	}
	for i := range wantRows {
		if strings.Join(got[i], "\x00") != strings.Join(wantRows[i], "\x00") {
			t.Errorf("row %d:\n got %q\nwant %q", i, got[i], wantRows[i])
		}
	}
	if res.NextID != 12 || res.OnBoard != 3 || res.Done != 2 {
		t.Errorf("result = %+v, want next id 12, 3 on the board, 2 done", res)
	}
}

func TestTheFencedExampleCardIsNotACard(t *testing.T) {
	var out bytes.Buffer
	if _, err := convert(open(t, "BOARD.md"), open(t, "DONE.md"), &out); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-000", "T-99"} {
		if strings.Contains(out.String(), id+",") {
			t.Errorf("%s became a row", id)
		}
	}
}

func TestRefusals(t *testing.T) {
	const done = "# Done\n\n## 2026-10\n\n### T-1 Old\n\n- feature: F\n- done: 2026-10-01\n\nBody.\n"
	for name, c := range map[string]struct{ board, done, want string }{
		"card before any column":    {"# Board\n\n### T-1 Lost\n\n- priority: P0\n\nBody.\n", "", "T-1"},
		"id on both boards":         {"## Backlog\n\n### T-1 Twice\n\n- priority: P0\n\nx\n", done, "T-1"},
		"feature and group":         {"## Backlog\n\n### T-2 Both\n\n- feature: A\n- group: B\n\nx\n", "", "T-2"},
		"field named like a column": {"## Backlog\n\n### T-2 Odd\n\n- title: A\n\nx\n", "", "title"},
	} {
		var out bytes.Buffer
		_, err := convert(strings.NewReader(c.board), strings.NewReader(c.done), &out)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want it to mention %q", name, err, c.want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: wrote output despite the error", name)
		}
	}
}

func TestNoNextIDCommentIsReportedNotGuessed(t *testing.T) {
	var out bytes.Buffer
	res, err := convert(strings.NewReader("## Backlog\n\n### T-7 A\n\n- priority: P0\n\nx\n"), strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if res.NextID != 0 || res.MaxID != 7 {
		t.Errorf("result = %+v, want NextID 0 (none found) and MaxID 7", res)
	}
}

func TestOnlyFieldLinesRightUnderTheHeadingAreFields(t *testing.T) {
	var out bytes.Buffer
	board := "## Backlog\n\n### T-1 A\n\n- priority: P1\n- created: 2026-10-01\n\n- Note: this list item is body text\n- [ ] and a task\n"
	if _, err := convert(strings.NewReader(board), strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	rows := records(t, out.String())
	if got := rows[1][8]; got != "- Note: this list item is body text\n- [ ] and a task" {
		t.Errorf("body = %q", got)
	}
}

func convertText(board, done string) ([][]string, error) {
	var out bytes.Buffer
	_, err := convert(strings.NewReader(board), strings.NewReader(done), &out)
	if err != nil {
		return nil, err
	}
	return csv.NewReader(&out).ReadAll()
}

func ids(rows [][]string) string {
	var out []string
	for _, r := range rows[1:] {
		out = append(out, r[0]+"@"+r[2])
	}
	return strings.Join(out, " ")
}

func TestATabBetweenTheIdAndTheTitleStillMakesACard(t *testing.T) {
	rows, err := convertText("## Backlog\n\n### T-1\tWith a tab\n\n- priority: P1\n\nx\n\n###  T-2   Extra spaces  \n\n- priority: P1\n\ny\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rows); got != "T-1@Backlog T-2@Backlog" {
		t.Fatalf("cards = %q", got)
	}
	if rows[1][1] != "With a tab" || rows[2][1] != "Extra spaces" {
		t.Errorf("titles = %q, %q", rows[1][1], rows[2][1])
	}
}

func TestAHeadingThatIsNotAColumnOrACardStopsTheRun(t *testing.T) {
	for name, c := range map[string]struct{ board, done, want string }{
		"## inside a body":    {"## Backlog\n\n### T-1 A\n\n- priority: P1\n\nSee:\n\n## Notes\n\nmore\n\n### T-2 B\n\n- priority: P1\n\nx\n", "", "Notes"},
		"### that is no card": {"## Backlog\n\n### T-1 A\n\n- priority: P1\n\n### Not a card\n", "", "Not a card"},
		"done heading":        {"## Backlog\n\n### T-1 A\n\n- priority: P1\n\nx\n", "## Misc\n\n### T-2 B\n\n- done: 2026-10-01\n\nx\n", "Misc"},
	} {
		if _, err := convertText(c.board, c.done); err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "line") {
			t.Errorf("%s: error %v, want it to name %q and a line", name, err, c.want)
		}
	}
}

func TestFencesAreRecognisedTheWayMarkdownReadsThem(t *testing.T) {
	board := "## Backlog\n\n" +
		"### T-1 One line fence\n\n- priority: P1\n\nrun ```bash ls``` here\n\n" +
		"### T-2 Tilde fence\n\n- priority: P1\n\n~~~\n### T-98 not a card\n## Not a column\n```\nstill inside\n~~~\n\n" +
		"### T-3 Longer fence\n\n- priority: P1\n\n````\n```\n### T-97 not a card\n```\n````\n\n" +
		"### T-4 Last\n\n- priority: P1\n\nend\n"
	rows, err := convertText(board, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rows); got != "T-1@Backlog T-2@Backlog T-3@Backlog T-4@Backlog" {
		t.Fatalf("cards = %q", got)
	}
	if !strings.Contains(rows[2][8], "### T-98 not a card") || !strings.Contains(rows[3][8], "### T-97 not a card") {
		t.Errorf("text inside a fence was lost: %q / %q", rows[2][8], rows[3][8])
	}
}

func TestAFenceLeftOpenIsAnError(t *testing.T) {
	_, err := convertText("## Backlog\n\n### T-1 A\n\n- priority: P1\n\n```\nnever closed\n\n### T-2 B\n\n- priority: P1\n\nx\n", "")
	if err == nil || !strings.Contains(err.Error(), "fence") {
		t.Fatalf("error = %v, want it to say a code fence is still open", err)
	}
}

func TestEveryHeadingOutsideAFenceIsAccountedFor(t *testing.T) {
	// A CRLF file, and a card whose body is nothing but a list.
	rows, err := convertText("## Up Next\r\n\r\n### T-1 A\r\n\r\n- priority: P0\r\n- created: 2026-10-01\r\n\r\n- [ ] one\r\n- [x] two\r\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][8] != "- [ ] one\n- [x] two" || rows[1][4] != "P0" {
		t.Errorf("row = %q", rows[1])
	}
}
