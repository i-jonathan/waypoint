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
