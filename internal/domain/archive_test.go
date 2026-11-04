package domain

import (
	"errors"
	"testing"
	"time"
)

// addDone adds a card straight into Done and backdates its done date.
func addDone(t *testing.T, b *Board, title string, done time.Time) CardID {
	t.Helper()
	c := mustAdd(t, b, NewCard{Title: title, Column: "Done"})
	for i := range b.Cards {
		if b.Cards[i].ID == c.ID {
			b.Cards[i].Done = done
		}
	}
	return c.ID
}

func TestDueForArchiveAuto(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Archive = ArchiveSettings{Mode: ArchiveAuto, AfterDays: 14}
	old := addDone(t, b, "old", time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	addDone(t, b, "recent", time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	assertEqual(t, b.DueForArchive(today), []CardID{old})
}

func TestDueForArchiveIgnoresZeroDoneAndOtherColumns(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Archive = ArchiveSettings{Mode: ArchiveAuto, AfterDays: 0}
	addDone(t, b, "undated", time.Time{})
	mustAdd(t, b, NewCard{Title: "todo"})
	assertEqual(t, len(b.DueForArchive(today)), 0)
}

func TestDueForArchiveManualOrOff(t *testing.T) {
	for _, mode := range []ArchiveMode{ArchiveManual, ArchiveOff} {
		b := NewBoard("x", "T")
		b.Settings.Archive = ArchiveSettings{Mode: mode, AfterDays: 1}
		addDone(t, b, "old", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		assertEqual(t, len(b.DueForArchive(today)), 0)
	}
}

func TestDoneCards(t *testing.T) {
	b := NewBoard("x", "T")
	mustAdd(t, b, NewCard{Title: "todo"})
	a := addDone(t, b, "a", today)
	c := addDone(t, b, "c", today)
	assertEqual(t, b.DoneCards(), []CardID{a, c})
}

func TestArchiveRejectsNotDone(t *testing.T) {
	b := NewBoard("x", "T")
	todo := mustAdd(t, b, NewCard{Title: "todo"})
	done := addDone(t, b, "done", today)
	err := b.Archive([]CardID{done, todo.ID}, today)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	// All-or-nothing: the valid card must not have moved.
	assertEqual(t, len(b.Cards), 2)
	assertEqual(t, len(b.Archived), 0)
}

func TestArchiveUnknownIDIsNotFoundAndChangesNothing(t *testing.T) {
	b := NewBoard("x", "T")
	done := addDone(t, b, "done", today)
	err := b.Archive([]CardID{done, "T-99"}, today)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	assertEqual(t, len(b.Cards), 1)
	assertEqual(t, len(b.Archived), 0)
}

func TestArchivePrependsNewestFirst(t *testing.T) {
	b := NewBoard("x", "T")
	a := addDone(t, b, "a", today)
	c := addDone(t, b, "c", today)
	d := addDone(t, b, "d", today)
	assertNoErr(t, b.Archive([]CardID{a}, today))
	assertNoErr(t, b.Archive([]CardID{d, c, c}, today))
	var got []CardID
	for _, card := range b.Archived {
		got = append(got, card.ID)
	}
	assertEqual(t, got, []CardID{d, c, a})
	assertEqual(t, len(b.Cards), 0)
	_, archived, ok := b.Find(a)
	assertTrue(t, ok && archived)
}

func TestRestore(t *testing.T) {
	b := NewBoard("x", "T")
	mustAdd(t, b, NewCard{Title: "first"})
	id := addDone(t, b, "back", today)
	assertNoErr(t, b.Archive([]CardID{id}, today))
	assertNoErr(t, b.Restore(id, "", today))
	assertEqual(t, titles(b, "Todo"), []string{"first", "back"})
	card, archived, ok := b.Find(id)
	assertTrue(t, ok && !archived)
	assertTrue(t, card.Done.IsZero())
	assertEqual(t, card.Column, "Todo")
	assertEqual(t, len(b.Archived), 0)
}

func TestRestoreToDoneColumnSetsDoneAndKeepsOrder(t *testing.T) {
	b := NewBoard("x", "T")
	id := addDone(t, b, "back", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	assertNoErr(t, b.Archive([]CardID{id}, today))
	mustAdd(t, b, NewCard{Title: "later", Column: "In progress"})
	assertNoErr(t, b.Restore(id, "Done", today))
	card, _, _ := b.Find(id)
	assertEqual(t, card.Done, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	assertEqual(t, b.Cards[len(b.Cards)-1].ID, id)
	assertEqual(t, b.Cards[0].Title, "later")
}

func TestRestoreErrors(t *testing.T) {
	b := NewBoard("x", "T")
	live := mustAdd(t, b, NewCard{Title: "live"})
	if err := b.Restore(live.ID, "", today); !errors.Is(err, ErrNotFound) {
		t.Fatalf("live card: got %v, want ErrNotFound", err)
	}
	id := addDone(t, b, "done", today)
	assertNoErr(t, b.Archive([]CardID{id}, today))
	if err := b.Restore(id, "Nope", today); !errors.Is(err, ErrUnknownColumn) {
		t.Fatalf("got %v, want ErrUnknownColumn", err)
	}
	assertEqual(t, len(b.Archived), 1)
}

func TestArchiveStampsUndatedCards(t *testing.T) {
	b := NewBoard("x", "T")
	dated := addDone(t, b, "dated", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	undated := addDone(t, b, "undated", today)
	b.Cards[1].Done = time.Time{} // a hand-added card under Done has no date
	assertNoErr(t, b.Archive([]CardID{dated, undated}, today))
	assertEqual(t, b.Archived[0].Done, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	assertEqual(t, b.Archived[1].Done, Day(today))
}
