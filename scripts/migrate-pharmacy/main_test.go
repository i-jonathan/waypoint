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
