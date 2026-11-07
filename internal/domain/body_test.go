package domain

import (
	"testing"
	"time"
)

func TestNormalizeBody(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"   \n\n":                "",
		"notes":                  "notes",
		"notes\n":                "notes",
		"notes\n\n\n":            "notes",
		"\n\nlead":               "lead",
		"a\r\nb\r\n":             "a\nb",
		"    indented first":     "    indented first",
		"\n    indented first\n": "    indented first",
		"keeps  inner\n\nblanks": "keeps  inner\n\nblanks",
		"trailing spaces  \n":    "trailing spaces  ",
		"```\ncode\n```\n":       "```\ncode\n```",
	} {
		if got := NormalizeBody(in); got != want {
			t.Errorf("NormalizeBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddCardAndPatchNormalizeTheBody(t *testing.T) {
	b := NewBoard("N", "T")
	c, err := b.AddCard(NewCard{Title: "One", Body: "from stdin\r\n\n"}, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "from stdin" {
		t.Fatalf("body = %q", c.Body)
	}
	body := "\nedited\n\n"
	if err := b.UpdateCard(c.ID, CardPatch{Body: &body}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := b.Find(c.ID); got.Body != "edited" {
		t.Fatalf("patched body = %q", got.Body)
	}
}
