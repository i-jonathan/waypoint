package domain

import "testing"

func TestBlockers(t *testing.T) {
	b := NewBoard("x", "T")
	bID := mustAdd(t, b, NewCard{Title: "B", Column: "Done"}).ID
	cID := mustAdd(t, b, NewCard{Title: "C"}).ID
	a := mustAdd(t, b, NewCard{Title: "A", Depends: []CardID{bID, cID, "T-99"}})

	assertEqual(t, b.Blockers(a.ID), []Blocker{{ID: cID}, {ID: "T-99", Missing: true}})

	// Archiving C clears it. Archived cards leave b.Cards.
	for i, c := range b.Cards {
		if c.ID == cID {
			b.Archived = append(b.Archived, c)
			b.Cards = append(b.Cards[:i], b.Cards[i+1:]...)
			break
		}
	}
	assertEqual(t, b.Blockers(a.ID), []Blocker{{ID: "T-99", Missing: true}})
}

func TestBlockersUnknownCardAndNoDepends(t *testing.T) {
	b := NewBoard("x", "T")
	a := mustAdd(t, b, NewCard{Title: "A"})
	assertEqual(t, len(b.Blockers(a.ID)), 0)
	assertEqual(t, len(b.Blockers("T-42")), 0)
}

func TestBlockersCycleTerminates(t *testing.T) {
	b := NewBoard("x", "T")
	a := mustAdd(t, b, NewCard{Title: "A"})
	bb := mustAdd(t, b, NewCard{Title: "B", Depends: []CardID{a.ID}})
	assertNoErr(t, b.UpdateCard(a.ID, CardPatch{Depends: ptr([]CardID{bb.ID})}))

	assertEqual(t, b.Blockers(a.ID), []Blocker{{ID: bb.ID}})
	assertEqual(t, b.Blockers(bb.ID), []Blocker{{ID: a.ID}})
}

func TestNextUp(t *testing.T) {
	b := NewBoard("x", "T")
	mustAdd(t, b, NewCard{Title: "Finished", Column: "Done"})
	_, ok := b.NextUp()
	assertTrue(t, !ok)

	x := mustAdd(t, b, NewCard{Title: "X", Column: "In progress"})
	mustAdd(t, b, NewCard{Title: "Y", Column: "In progress"})
	got, ok := b.NextUp()
	assertTrue(t, ok)
	assertEqual(t, got.ID, x.ID)

	first := mustAdd(t, b, NewCard{Title: "First", Column: "Todo"})
	got, ok = b.NextUp()
	assertTrue(t, ok)
	assertEqual(t, got.ID, first.ID)
}

func TestNextUpEmptyBoard(t *testing.T) {
	_, ok := NewBoard("x", "T").NextUp()
	assertTrue(t, !ok)
}

func TestWarnings(t *testing.T) {
	b := NewBoard("x", "T")
	assertEqual(t, b.Warnings(), []string{})

	b.Settings.Columns[1].WIP = 2
	for _, title := range []string{"A", "B", "C"} {
		mustAdd(t, b, NewCard{Title: title, Column: "In progress"})
	}
	// Direct board edits (as a hand-edited file would produce) can exceed WIP.
	assertEqual(t, b.Warnings(), []string{"In progress is over its WIP limit (3/2)"})

	b.Settings.Columns[1].WIP = 3
	assertEqual(t, b.Warnings(), []string{})
	b.Settings.Columns[1].WIP = 0
	assertEqual(t, b.Warnings(), []string{})
}

func TestWarningsOrder(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Web"}}
	b.Settings.Columns[0].WIP = 1
	b.Settings.Columns[1].WIP = 1
	mustAdd(t, b, NewCard{Title: "A"})
	mustAdd(t, b, NewCard{Title: "B"})
	mustAdd(t, b, NewCard{Title: "C", Column: "In progress"})
	mustAdd(t, b, NewCard{Title: "D", Column: "In progress"})
	b.Cards[0].Group = "Nope"
	b.Cards[1].Priority = "P9"
	b.Cards[3].Group = "Nope"
	b.Cards[3].Priority = "P9"

	assertEqual(t, b.Warnings(), []string{
		"Todo is over its WIP limit (2/1)",
		"In progress is over its WIP limit (2/1)",
		`T-1 has unknown group "Nope"`,
		`T-2 has unknown priority "P9"`,
		`T-4 has unknown group "Nope"`,
		`T-4 has unknown priority "P9"`,
	})
}

func TestWarningsAppendsNotices(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Web"}}
	mustAdd(t, b, NewCard{Title: "A"})
	b.Cards[0].Group = "Nope"
	b.Notices = []string{"line 9: look here"}
	assertEqual(t, b.Warnings(), []string{`T-1 has unknown group "Nope"`, "line 9: look here"})
}

func TestWarningsUnknownSoftData(t *testing.T) {
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Web"}}
	mustAdd(t, b, NewCard{Title: "A"})
	mustAdd(t, b, NewCard{Title: "B"})
	b.Cards[0].Group = "Mobile"
	b.Cards[1].Priority = "P9"
	assertEqual(t, b.Warnings(), []string{
		`T-1 has unknown group "Mobile"`,
		`T-2 has unknown priority "P9"`,
	})

	// Case-insensitive matches are fine, and empty values are not problems.
	b.Cards[0].Group = "web"
	b.Cards[1].Priority = "p1"
	assertEqual(t, b.Warnings(), []string{})
}

func TestCardHashStable(t *testing.T) {
	c := Card{ID: "T-1", Title: "A", Column: "Todo", Group: "g", Priority: "P1",
		Depends: []CardID{"T-2"}, Body: "hi", Extra: []Field{{"k", "v"}}}
	d := c
	assertEqual(t, CardHash(c), CardHash(d))
	assertEqual(t, len(CardHash(c)), 64)
	d.Body = "hi!"
	assertTrue(t, CardHash(c) != CardHash(d))
}

func TestCardHashIgnoresDates(t *testing.T) {
	c := Card{ID: "T-1", Title: "A"}
	d := c
	d.Created = today
	d.Done = today
	assertEqual(t, CardHash(c), CardHash(d))
}

func TestCardHashFieldBoundaries(t *testing.T) {
	differ := func(name string, a, b Card) {
		t.Helper()
		if CardHash(a) == CardHash(b) {
			t.Errorf("%s: hashes should differ", name)
		}
	}
	differ("title/column", Card{Title: "ab", Column: "c"}, Card{Title: "a", Column: "bc"})
	differ("group/priority", Card{Group: "ab", Priority: "c"}, Card{Group: "a", Priority: "bc"})
	differ("depends split", Card{Depends: []CardID{"a", "bc"}}, Card{Depends: []CardID{"ab", "c"}})
	differ("depends vs body", Card{Depends: []CardID{"x"}}, Card{Body: "x"})
	differ("extra key/value", Card{Extra: []Field{{"ab", "c"}}}, Card{Extra: []Field{{"a", "bc"}}})
	differ("extra order", Card{Extra: []Field{{"a", "1"}, {"b", "2"}}}, Card{Extra: []Field{{"b", "2"}, {"a", "1"}}})
	differ("extra split", Card{Extra: []Field{{"a", "b"}, {"c", "d"}}}, Card{Extra: []Field{{"a", "b"}}, Body: "cd"})
	differ("id/title", Card{ID: "ab", Title: "c"}, Card{ID: "a", Title: "bc"})
}
