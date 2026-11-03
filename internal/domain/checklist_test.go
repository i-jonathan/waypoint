package domain

import (
	"errors"
	"testing"
)

type scanned struct {
	Line    string
	InFence bool
}

func scan(body string) []scanned {
	out := []scanned{}
	ScanLines(body, func(i int, line string, inFence bool) {
		if i != len(out) {
			panic("line index out of sequence")
		}
		out = append(out, scanned{line, inFence})
	})
	return out
}

func TestScanLines(t *testing.T) {
	got := scan("a\n```go\nb\n```\nc")
	assertEqual(t, got, []scanned{
		{"a", false}, {"```go", true}, {"b", true}, {"```", true}, {"c", false},
	})
}

func TestScanLinesFenceKindsDoNotMix(t *testing.T) {
	got := scan("~~~\n```\nx\n~~~\ny\n```\nz\n~~~\nw\n```\nv")
	want := []scanned{
		{"~~~", true}, {"```", true}, {"x", true}, {"~~~", true}, {"y", false},
		{"```", true}, {"z", true}, {"~~~", true}, {"w", true}, {"```", true}, {"v", false},
	}
	assertEqual(t, got, want)
}

func TestScanLinesFenceLengthAndInfo(t *testing.T) {
	// A closing fence must be at least as long as the opener and carry no info string.
	got := scan("````\n```\n```go\n````\nafter")
	assertEqual(t, got, []scanned{
		{"````", true}, {"```", true}, {"```go", true}, {"````", true}, {"after", false},
	})
}

func TestScanLinesUnclosedFenceRunsToEnd(t *testing.T) {
	got := scan("```\na\nb")
	assertEqual(t, got, []scanned{{"```", true}, {"a", true}, {"b", true}})
}

func TestScanLinesEmptyBody(t *testing.T) {
	assertEqual(t, scan(""), []scanned{{"", false}})
}

const checklistBody = "Intro\n- [ ] one\n* [x] two\n```\n- [ ] x\n```\n- [X] three\nplain - [ ] not an item\n"

func TestChecklistIgnoresFences(t *testing.T) {
	body := "- [ ] first\n```\n- [ ] x\n```\n- [x] second\n"
	assertEqual(t, Checklist(body), []ChecklistItem{
		{Text: "first"}, {Text: "second", Checked: true},
	})
}

func TestChecklistForms(t *testing.T) {
	assertEqual(t, Checklist(checklistBody), []ChecklistItem{
		{Text: "one"}, {Text: "two", Checked: true}, {Text: "three", Checked: true},
	})
	assertEqual(t, len(Checklist("")), 0)
	assertEqual(t, len(Checklist("- [] nope\n- [y] nope\n-[ ] nope\n")), 0)
	assertEqual(t, Checklist("  - [ ] nested\r\n"), []ChecklistItem{{Text: "nested"}})
}

func TestToggleCheck(t *testing.T) {
	b := NewBoard("x", "T")
	body := "Intro\n- [ ] one\n- [ ] two  \n```\n- [ ] x\n```\ntail\n"
	c := mustAdd(t, b, NewCard{Title: "A", Body: body})

	assertNoErr(t, b.ToggleCheck(c.ID, 2))
	assertEqual(t, b.Cards[0].Body, "Intro\n- [ ] one\n- [x] two  \n```\n- [ ] x\n```\ntail\n")

	assertNoErr(t, b.ToggleCheck(c.ID, 2))
	assertEqual(t, b.Cards[0].Body, body)

	assertNoErr(t, b.ToggleCheck(c.ID, 1))
	assertEqual(t, b.Cards[0].Body, "Intro\n- [x] one\n- [ ] two  \n```\n- [ ] x\n```\ntail\n")
}

func TestToggleCheckUppercaseBecomesUnchecked(t *testing.T) {
	b := NewBoard("x", "T")
	c := mustAdd(t, b, NewCard{Title: "A", Body: "* [X] done\r\n- [ ] next"})
	assertNoErr(t, b.ToggleCheck(c.ID, 1))
	assertEqual(t, b.Cards[0].Body, "* [ ] done\r\n- [ ] next")
}

func TestToggleCheckOutOfRange(t *testing.T) {
	b := NewBoard("x", "T")
	body := "- [ ] a\n- [x] b\n```\n- [ ] hidden\n```\n"
	c := mustAdd(t, b, NewCard{Title: "A", Body: body})
	for _, n := range []int{3, 0, -1} {
		err := b.ToggleCheck(c.ID, n)
		assertTrue(t, errors.Is(err, ErrNotFound))
		assertEqual(t, b.Cards[0].Body, body)
	}
	assertTrue(t, errors.Is(b.ToggleCheck("T-99", 1), ErrNotFound))
}
