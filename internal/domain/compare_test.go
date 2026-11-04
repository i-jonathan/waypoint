package domain

import (
	"testing"
	"time"
)

func TestSameContentIgnoresColumnAndDone(t *testing.T) {
	a := Card{ID: "T-1", Title: "A", Column: "Todo"}
	b := Card{ID: "T-1", Title: "A", Column: "Done", Done: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}
	if !a.SameContent(b) {
		t.Fatal("cards differing only in Column and Done are not the same content")
	}
}

func TestSameContentTreatsNilAndEmptyListsAlike(t *testing.T) {
	a := Card{Depends: nil, Extra: nil}
	b := Card{Depends: []CardID{}, Extra: []Field{}}
	if !a.SameContent(b) {
		t.Fatal("nil and empty lists differ")
	}
}

// fullBoard has at least one value in every field that Equal compares.
func fullBoard() *Board {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	b := NewBoard("Full", "T")
	b.Settings.Groups = []Group{{Name: "G", Color: "blue"}}
	b.Cards = []Card{
		{ID: "T-1", Title: "One", Column: "Todo", Created: day, Depends: []CardID{"T-2"}, Extra: []Field{{Key: "note", Value: "x"}}},
		{ID: "T-2", Title: "Two", Column: "Todo"},
	}
	b.Archived = []Card{{ID: "T-3", Title: "Three", Done: day, Depends: []CardID{"T-1"}}}
	b.Notices = []string{"a notice"}
	return b
}

func TestCloneEqualsTheOriginal(t *testing.T) {
	b := fullBoard()
	if !b.Clone().Equal(b) {
		t.Fatal("a clone differs from its original")
	}
}

func TestEqualIgnoresNotices(t *testing.T) {
	b := fullBoard()
	c := b.Clone()
	c.Notices = nil
	if !c.Equal(b) {
		t.Fatal("boards differing only in Notices are not equal")
	}
}

// Each change runs on a clone, so this proves both that Equal sees the change
// and that the clone shares no memory with the original.
func TestEqualSeesEveryChangeAndCloneSharesNothing(t *testing.T) {
	changes := map[string]func(*Board){
		"name":        func(b *Board) { b.Settings.Name = "x" },
		"next id":     func(b *Board) { b.Settings.NextID = 9 },
		"column":      func(b *Board) { b.Settings.Columns[0].WIP = 2 },
		"moves to":    func(b *Board) { b.Settings.Columns[0].MovesTo[0] = "x" },
		"group":       func(b *Board) { b.Settings.Groups[0].Color = "red" },
		"priority":    func(b *Board) { b.Settings.Priorities[0] = "x" },
		"archive":     func(b *Board) { b.Settings.Archive.AfterDays = 1 },
		"appearance":  func(b *Board) { b.Settings.Appearance.Theme = "x" },
		"card":        func(b *Board) { b.Cards[0].Title = "x" },
		"card column": func(b *Board) { b.Cards[0].Column = "Done" },
		"card done":   func(b *Board) { b.Cards[0].Done = b.Cards[0].Created },
		"card order":  func(b *Board) { b.Cards[0], b.Cards[1] = b.Cards[1], b.Cards[0] },
		"depends":     func(b *Board) { b.Cards[0].Depends[0] = "T-9" },
		"extra":       func(b *Board) { b.Cards[0].Extra[0].Value = "y" },
		"archived":    func(b *Board) { b.Archived[0].Depends[0] = "T-9" },
		"removed":     func(b *Board) { b.Cards = b.Cards[:1] },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			b := fullBoard()
			c := b.Clone()
			change(c)
			if c.Equal(b) {
				t.Fatal("Equal missed the change")
			}
			if !b.Equal(fullBoard()) {
				t.Fatal("changing the clone changed the original")
			}
		})
	}
}
