package domain

import (
	"errors"
	"testing"
	"time"
)

var today = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)

// mustAdd adds a card and fails the test on error.
func mustAdd(t *testing.T, b *Board, n NewCard) Card {
	t.Helper()
	c, err := b.AddCard(n, today)
	assertNoErr(t, err)
	return c
}

// titles lists the titles of the cards in a column, in rank order.
func titles(b *Board, column string) []string {
	out := []string{}
	for _, c := range b.CardsIn(column) {
		out = append(out, c.Title)
	}
	return out
}

func idOf(t *testing.T, b *Board, title string) CardID {
	t.Helper()
	for _, c := range b.Cards {
		if c.Title == title {
			return c.ID
		}
	}
	t.Fatalf("no card titled %q", title)
	return ""
}

func TestAddCardAssignsIDAndDefaults(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.NextID = 7
	first := mustAdd(t, b, NewCard{Title: "A"})
	assertEqual(t, first.ID, CardID("T-7"))
	assertEqual(t, first.Column, "Todo")
	assertEqual(t, first.Created, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	assertTrue(t, first.Done.IsZero())
	assertEqual(t, b.Settings.NextID, 8)

	second := mustAdd(t, b, NewCard{Title: "B", Column: "Todo"})
	assertEqual(t, second.ID, CardID("T-8"))
	assertEqual(t, titles(b, "Todo"), []string{"A", "B"})
	assertEqual(t, b.Cards[1].ID, CardID("T-8"))
}

func TestAddCardToDoneColumnSetsDone(t *testing.T) {
	b := NewBoard("x", "T")
	c := mustAdd(t, b, NewCard{Title: "A", Column: "Done"})
	assertEqual(t, c.Done, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
}

func TestAddCardUnknownColumnOrGroupOrPriority(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Backend", Color: "#fff"}}

	_, err := b.AddCard(NewCard{Title: "A", Column: "Nope"}, today)
	assertTrue(t, errors.Is(err, ErrUnknownColumn))
	_, err = b.AddCard(NewCard{Title: "A", Group: "Nope"}, today)
	assertTrue(t, errors.Is(err, ErrUnknownGroup))
	_, err = b.AddCard(NewCard{Title: "A", Priority: "P9"}, today)
	assertTrue(t, errors.Is(err, ErrUnknownPriority))
	assertEqual(t, b.Settings.NextID, 1)
	assertEqual(t, len(b.Cards), 0)

	c := mustAdd(t, b, NewCard{Title: "A", Group: "backend", Priority: "p1"})
	assertEqual(t, c.Group, "Backend")
	assertEqual(t, c.Priority, "P1")
}

func TestAddCardRejectsEmptyTitle(t *testing.T) {
	b := NewBoard("x", "T")
	_, err := b.AddCard(NewCard{Title: "  \t"}, today)
	assertTrue(t, errors.Is(err, ErrInvalid))
	assertEqual(t, b.Settings.NextID, 1)
}

func TestAddCardCopiesIncomingSlices(t *testing.T) {
	b := NewBoard("x", "T")
	deps := []CardID{"T-9"}
	extra := []Field{{Key: "owner", Value: "me"}}
	mustAdd(t, b, NewCard{Title: "A", Depends: deps, Extra: extra})
	deps[0] = "T-0"
	extra[0].Value = "you"
	assertEqual(t, b.Cards[0].Depends, []CardID{"T-9"})
	assertEqual(t, b.Cards[0].Extra, []Field{{Key: "owner", Value: "me"}})
}

func TestMoveAllowedSetsAndClearsDone(t *testing.T) {
	b := NewBoard("x", "T")
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	assertNoErr(t, b.MoveCard(id, "Done", -1, today))
	c, _, _ := b.Find(id)
	assertEqual(t, c.Column, "Done")
	assertEqual(t, c.Done, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))

	assertNoErr(t, b.MoveCard(id, "In progress", -1, today))
	c, _, _ = b.Find(id)
	assertEqual(t, c.Column, "In progress")
	assertTrue(t, c.Done.IsZero())
}

func TestMoveNotAllowed(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Columns[0].MovesTo = []string{"In progress"}
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	before := append([]Card(nil), b.Cards...)

	err := b.MoveCard(id, "Done", -1, today)
	assertTrue(t, errors.Is(err, ErrMoveNotAllowed))
	assertEqual(t, err.Error(), "move not allowed: Todo cannot move to Done")
	assertEqual(t, b.Cards, before)
}

func TestMoveUnknownCardOrColumn(t *testing.T) {
	b := NewBoard("x", "T")
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	assertTrue(t, errors.Is(b.MoveCard("T-99", "Done", -1, today), ErrNotFound))
	assertTrue(t, errors.Is(b.MoveCard(id, "Nope", -1, today), ErrUnknownColumn))
}

func TestMoveWIPLimit(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Columns[1].WIP = 1
	a := mustAdd(t, b, NewCard{Title: "A", Column: "In progress"}).ID
	c := mustAdd(t, b, NewCard{Title: "C"}).ID
	before := append([]Card(nil), b.Cards...)

	err := b.MoveCard(c, "In progress", -1, today)
	assertTrue(t, errors.Is(err, ErrWIPLimit))
	assertEqual(t, err.Error(), "In progress is at its WIP limit (1/1)")
	assertEqual(t, b.Cards, before)

	// Reordering inside the full column is always fine.
	assertNoErr(t, b.MoveCard(a, "In progress", 0, today))
}

func TestMoveIndex(t *testing.T) {
	b := NewBoard("x", "T")
	a := mustAdd(t, b, NewCard{Title: "A"}).ID
	mustAdd(t, b, NewCard{Title: "B"})
	c := mustAdd(t, b, NewCard{Title: "C"}).ID

	assertNoErr(t, b.MoveCard(c, "Todo", 0, today))
	assertEqual(t, titles(b, "Todo"), []string{"C", "A", "B"})
	assertNoErr(t, b.MoveCard(a, "Todo", -1, today))
	assertEqual(t, titles(b, "Todo"), []string{"C", "B", "A"})
	assertNoErr(t, b.MoveCard(c, "Todo", 2, today))
	assertEqual(t, titles(b, "Todo"), []string{"B", "A", "C"})

	assertNoErr(t, b.MoveCard(a, "In progress", 5, today))
	assertEqual(t, titles(b, "In progress"), []string{"A"})
}

func TestMoveKeepsCardsSortedByColumn(t *testing.T) {
	b := NewBoard("x", "T")
	mustAdd(t, b, NewCard{Title: "T1"})
	mustAdd(t, b, NewCard{Title: "D1", Column: "Done"})
	mustAdd(t, b, NewCard{Title: "T2"})
	p := mustAdd(t, b, NewCard{Title: "P1", Column: "In progress"}).ID
	x := mustAdd(t, b, NewCard{Title: "P2", Column: "In progress"}).ID

	// Into the middle of a populated column, then to the bottom of an empty one.
	assertNoErr(t, b.MoveCard(idOf(t, b, "T1"), "In progress", 1, today))
	assertEqual(t, titles(b, "In progress"), []string{"P1", "T1", "P2"})
	assertNoErr(t, b.MoveCard(p, "Todo", -1, today))
	assertNoErr(t, b.MoveCard(x, "Todo", -1, today))
	assertNoErr(t, b.MoveCard(idOf(t, b, "T1"), "Todo", -1, today))
	assertEqual(t, titles(b, "In progress"), []string{})
	assertNoErr(t, b.MoveCard(p, "In progress", -1, today))

	got := []string{}
	for _, c := range b.Cards {
		got = append(got, c.Title)
	}
	assertEqual(t, got, []string{"T2", "P2", "T1", "P1", "D1"})
	assertEqual(t, b.Cards[3].Column, "In progress")
}

func TestMoveDoesNotAliasCardsOnShift(t *testing.T) {
	b := NewBoard("x", "T")
	a := mustAdd(t, b, NewCard{Title: "A", Depends: []CardID{"T-9"}}).ID
	mustAdd(t, b, NewCard{Title: "B"})
	assertNoErr(t, b.MoveCard(a, "Todo", -1, today))
	assertEqual(t, titles(b, "Todo"), []string{"B", "A"})
	assertEqual(t, b.Cards[1].Depends, []CardID{"T-9"})
}

func TestDeleteDoesNotReuseIDs(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.NextID = 1
	first := mustAdd(t, b, NewCard{Title: "A"}).ID
	assertEqual(t, b.Settings.NextID, 2)
	assertNoErr(t, b.DeleteCard(first))
	assertEqual(t, len(b.Cards), 0)
	assertEqual(t, b.Settings.NextID, 2)
	assertEqual(t, mustAdd(t, b, NewCard{Title: "B"}).ID, CardID("T-2"))
}

func TestDeleteFromArchiveAndNotFound(t *testing.T) {
	b := NewBoard("x", "T")
	b.Archived = []Card{{ID: "T-1", Title: "Old"}, {ID: "T-2", Title: "Older"}}
	assertNoErr(t, b.DeleteCard("T-1"))
	assertEqual(t, len(b.Archived), 1)
	assertEqual(t, b.Archived[0].ID, CardID("T-2"))
	assertTrue(t, errors.Is(b.DeleteCard("T-1"), ErrNotFound))
}

func ptr[T any](v T) *T { return &v }

func TestUpdatePatch(t *testing.T) {
	b := NewBoard("x", "T")
	id := mustAdd(t, b, NewCard{Title: "A", Extra: []Field{{"a", "1"}, {"b", "2"}, {"c", "3"}}}).ID

	assertNoErr(t, b.UpdateCard(id, CardPatch{
		Title: ptr("New"), Priority: ptr("p1"), Body: ptr("hello\n"),
		Depends: ptr([]CardID{"T-5", "T-6"}),
		Set:     map[string]string{"owner": "me", "b": "20"},
	}))
	c, _, _ := b.Find(id)
	assertEqual(t, c.Title, "New")
	assertEqual(t, c.Priority, "P1")
	assertEqual(t, c.Body, "hello\n")
	assertEqual(t, c.Depends, []CardID{"T-5", "T-6"})
	assertEqual(t, c.Extra, []Field{{"a", "1"}, {"b", "20"}, {"c", "3"}, {"owner", "me"}})

	assertNoErr(t, b.UpdateCard(id, CardPatch{Set: map[string]string{"owner": "", "a": ""}}))
	c, _, _ = b.Find(id)
	assertEqual(t, c.Extra, []Field{{"b", "20"}, {"c", "3"}})

	// Clearing the priority and depends.
	assertNoErr(t, b.UpdateCard(id, CardPatch{Priority: ptr(""), Depends: ptr([]CardID{})}))
	c, _, _ = b.Find(id)
	assertEqual(t, c.Priority, "")
	assertEqual(t, len(c.Depends), 0)
}

func TestUpdateValidation(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Backend"}}
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	before := append([]Card(nil), b.Cards...)

	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Title: ptr(" ")}), ErrInvalid))
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Group: ptr("Nope")}), ErrUnknownGroup))
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Priority: ptr("P9")}), ErrUnknownPriority))
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Set: map[string]string{"group": "x"}}), ErrInvalid))
	assertTrue(t, errors.Is(b.UpdateCard("T-99", CardPatch{Title: ptr("x")}), ErrNotFound))
	// A failing patch changes nothing, even the valid parts of it.
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Title: ptr("ok"), Priority: ptr("P9")}), ErrUnknownPriority))
	assertEqual(t, b.Cards, before)

	assertNoErr(t, b.UpdateCard(id, CardPatch{Group: ptr("backend")}))
	assertEqual(t, b.Cards[0].Group, "Backend")
	assertNoErr(t, b.UpdateCard(id, CardPatch{Group: ptr("")}))
	assertEqual(t, b.Cards[0].Group, "")
}

func TestUpdateRejectsSelfDependency(t *testing.T) {
	b := NewBoard("x", "T")
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	err := b.UpdateCard(id, CardPatch{Depends: &[]CardID{id}})
	assertTrue(t, errors.Is(err, ErrInvalid))
	assertEqual(t, len(b.Cards[0].Depends), 0)

	// Unknown ids are allowed: they show up later as missing blockers.
	assertNoErr(t, b.UpdateCard(id, CardPatch{Depends: &[]CardID{"T-99"}}))
}

func TestUpdateCopiesPatchDepends(t *testing.T) {
	b := NewBoard("x", "T")
	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	deps := []CardID{"T-5"}
	assertNoErr(t, b.UpdateCard(id, CardPatch{Depends: &deps}))
	deps[0] = "T-0"
	assertEqual(t, b.Cards[0].Depends, []CardID{"T-5"})
}

func TestValidFieldKey(t *testing.T) {
	for _, k := range []string{"owner", "a", "my-key", "my_key", "x1"} {
		assertTrue(t, ValidFieldKey(k))
	}
	for _, k := range []string{"", "Owner", "my key", "a.b", "1x", "-x", " owner", "owner ", "a:b", "a\nb",
		"group", "priority", "depends", "created", "done"} {
		if ValidFieldKey(k) {
			t.Fatalf("ValidFieldKey(%q) = true, want false", k)
		}
	}
}

func TestExtraKeysAndValuesValidated(t *testing.T) {
	badKeys := []string{"Owner", "my key", "a.b", "1x", " owner", "group"}
	for _, k := range badKeys {
		b := NewBoard("x", "T")
		_, err := b.AddCard(NewCard{Title: "A", Extra: []Field{{Key: k, Value: "v"}}}, today)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("AddCard key %q: got %v, want ErrInvalid", k, err)
		}
		assertEqual(t, len(b.Cards), 0)
		assertEqual(t, b.Settings.NextID, 1)

		id := mustAdd(t, b, NewCard{Title: "A"}).ID
		err = b.UpdateCard(id, CardPatch{Set: map[string]string{k: "v"}})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("UpdateCard key %q: got %v, want ErrInvalid", k, err)
		}
		assertEqual(t, len(b.Cards[0].Extra), 0)
	}

	b := NewBoard("x", "T")
	bad := map[string][]Field{
		"newline value":  {{Key: "owner", Value: "a\nb"}},
		"carriage value": {{Key: "owner", Value: "a\rb"}},
		"empty value":    {{Key: "owner", Value: ""}},
		"duplicate key":  {{Key: "owner", Value: "a"}, {Key: "owner", Value: "b"}},
	}
	for name, extra := range bad {
		_, err := b.AddCard(NewCard{Title: "A", Extra: extra}, today)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("AddCard %s: got %v, want ErrInvalid", name, err)
		}
	}
	assertEqual(t, len(b.Cards), 0)

	id := mustAdd(t, b, NewCard{Title: "A"}).ID
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Set: map[string]string{"owner": "a\nb"}}), ErrInvalid))
	assertTrue(t, errors.Is(b.UpdateCard(id, CardPatch{Set: map[string]string{"owner": "a\rb"}}), ErrInvalid))
	assertEqual(t, len(b.Cards[0].Extra), 0)
}

func TestTitleMustBeSingleLine(t *testing.T) {
	b := NewBoard("x", "T")
	for _, title := range []string{"a\nb", "a\rb", "trailing\n"} {
		_, err := b.AddCard(NewCard{Title: title}, today)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("AddCard title %q: got %v, want ErrInvalid", title, err)
		}
		id := mustAdd(t, b, NewCard{Title: "ok"}).ID
		err = b.UpdateCard(id, CardPatch{Title: ptr(title)})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("UpdateCard title %q: got %v, want ErrInvalid", title, err)
		}
		assertNoErr(t, b.DeleteCard(id))
	}
	assertEqual(t, len(b.Cards), 0)

	// Bodies are multi-line by design.
	c := mustAdd(t, b, NewCard{Title: "A", Body: "line1\nline2\n"})
	assertEqual(t, c.Body, "line1\nline2\n")
}
