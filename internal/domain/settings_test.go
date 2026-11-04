package domain

import (
	"errors"
	"strings"
	"testing"
)

// settingsBoard is a board with groups, a few cards in each column and one
// archived card, so a settings change has something to rewrite.
func settingsBoard(t *testing.T) *Board {
	t.Helper()
	b := NewBoard("x", "T")
	b.Settings.Groups = []Group{{Name: "Bugs", Color: "red"}, {Name: "Chores", Color: "blue"}}
	mustAdd(t, b, NewCard{Title: "t1", Column: "Todo", Group: "Bugs"})
	mustAdd(t, b, NewCard{Title: "t2", Column: "Todo", Group: "Chores"})
	mustAdd(t, b, NewCard{Title: "p1", Column: "In progress", Group: "Bugs"})
	mustAdd(t, b, NewCard{Title: "p2", Column: "In progress"})
	mustAdd(t, b, NewCard{Title: "d1", Column: "Done"})
	mustAdd(t, b, NewCard{Title: "d2", Column: "Done", Group: "Chores"})
	// An archived card keeps the column it was archived from in memory.
	assertNoErr(t, b.Archive([]CardID{idOf(t, b, "d1")}, today))
	b.Archived[0].Group = "Bugs"
	return b
}

// change starts a SettingsChange from the board's current settings.
func change(b *Board) SettingsChange { return SettingsChange{Settings: b.Clone().Settings} }

// assertConsistent fails if a card sits in a column the settings do not have.
func assertConsistent(t *testing.T, b *Board) {
	t.Helper()
	for _, c := range b.Cards {
		if _, ok := b.Column(c.Column); !ok {
			t.Fatalf("card %s is in %q, which the settings do not have", c.ID, c.Column)
		}
	}
	for _, c := range b.Archived {
		if _, ok := b.Column(c.Column); c.Column != "" && !ok {
			t.Fatalf("archived card %s is in %q, which the settings do not have", c.ID, c.Column)
		}
	}
	assertNoErr(t, b.Settings.Validate())
}

func renameColumn(ch *SettingsChange, from, to string) {
	for i := range ch.Settings.Columns {
		if ch.Settings.Columns[i].Name == from {
			ch.Settings.Columns[i].Name = to
		}
		for j, m := range ch.Settings.Columns[i].MovesTo {
			if m == from {
				ch.Settings.Columns[i].MovesTo[j] = to
			}
		}
	}
	if ch.ColumnRenames == nil {
		ch.ColumnRenames = map[string]string{}
	}
	ch.ColumnRenames[from] = to
}

func TestRenameColumnRewritesCards(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	renameColumn(&ch, "In progress", "Doing")
	renameColumn(&ch, "Done", "Shipped")
	assertNoErr(t, b.ApplySettings(ch))

	assertEqual(t, titles(b, "Doing"), []string{"p1", "p2"})
	assertEqual(t, titles(b, "Shipped"), []string{"d2"})
	assertEqual(t, b.Archived[0].Column, "Shipped")
	assertEqual(t, len(b.CardsIn("In progress")), 0)
	assertConsistent(t, b)
}

func TestRenameColumnsSwapAndChain(t *testing.T) {
	b := settingsBoard(t)
	// Swap: the old "Todo" cards end up in "In progress" and the reverse.
	ch := change(b)
	ch.Settings.Columns[0].Name, ch.Settings.Columns[1].Name = "In progress", "Todo"
	ch.Settings.Columns[0].MovesTo = []string{"Todo", "Done"}
	ch.Settings.Columns[1].MovesTo = []string{"In progress", "Done"}
	ch.Settings.Columns[2].MovesTo = []string{"Todo"}
	ch.ColumnRenames = map[string]string{"Todo": "In progress", "In progress": "Todo"}
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, titles(b, "In progress"), []string{"t1", "t2"})
	assertEqual(t, titles(b, "Todo"), []string{"p1", "p2"})
	assertEqual(t, b.Settings.Columns[0].Name, "In progress")
	assertConsistent(t, b)

	// Chain: Todo -> Backlog and In progress -> Todo, from the original names.
	b = settingsBoard(t)
	ch = change(b)
	ch.Settings.Columns[0].Name, ch.Settings.Columns[1].Name = "Backlog", "Todo"
	ch.Settings.Columns[0].MovesTo = []string{"Todo", "Done"}
	ch.Settings.Columns[1].MovesTo = []string{"Backlog", "Done"}
	ch.Settings.Columns[2].MovesTo = []string{"Todo"}
	ch.ColumnRenames = map[string]string{"Todo": "Backlog", "In progress": "Todo"}
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, titles(b, "Backlog"), []string{"t1", "t2"})
	assertEqual(t, titles(b, "Todo"), []string{"p1", "p2"})
	assertConsistent(t, b)
}

func TestRenameValidation(t *testing.T) {
	cases := map[string]func(ch *SettingsChange){
		"old name does not exist":  func(ch *SettingsChange) { ch.ColumnRenames = map[string]string{"Nope": "Todo"} },
		"new name not in settings": func(ch *SettingsChange) { ch.ColumnRenames = map[string]string{"Todo": "Elsewhere"} },
		"two columns to one name": func(ch *SettingsChange) {
			ch.ColumnRenames = map[string]string{"Todo": "Done", "In progress": "Done"}
		},
		"renamed onto a kept column": func(ch *SettingsChange) { ch.ColumnRenames = map[string]string{"Todo": "In progress"} },
		"group old name unknown":     func(ch *SettingsChange) { ch.GroupRenames = map[string]string{"Nope": "Bugs"} },
		"group new name absent":      func(ch *SettingsChange) { ch.GroupRenames = map[string]string{"Bugs": "Elsewhere"} },
		"two groups to one name": func(ch *SettingsChange) {
			ch.GroupRenames = map[string]string{"Bugs": "Chores", "Chores": "chores"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := settingsBoard(t)
			before := b.Clone()
			ch := change(b)
			mutate(&ch)
			err := b.ApplySettings(ch)
			assertTrue(t, errors.Is(err, ErrInvalid))
			assertTrue(t, b.Equal(before))
		})
	}
}

func TestRemoveNonEmptyColumnRefused(t *testing.T) {
	b := settingsBoard(t)
	before := b.Clone()
	ch := change(b)
	ch.Settings.Columns = []Column{ch.Settings.Columns[0], ch.Settings.Columns[2]}
	ch.Settings.Columns[0].MovesTo = []string{"Done"}
	ch.Settings.Columns[1].MovesTo = []string{"Todo"}
	err := b.ApplySettings(ch)
	assertTrue(t, errors.Is(err, ErrInvalid))
	assertTrue(t, strings.Contains(err.Error(), `column "In progress" still has 2 cards`))
	assertTrue(t, b.Equal(before))
}

func TestRemoveEmptyColumn(t *testing.T) {
	b := settingsBoard(t)
	assertNoErr(t, b.DeleteCard(idOf(t, b, "p1")))
	assertNoErr(t, b.DeleteCard(idOf(t, b, "p2")))
	ch := change(b)
	ch.Settings.Columns = []Column{ch.Settings.Columns[0], ch.Settings.Columns[2]}
	ch.Settings.Columns[0].MovesTo = []string{"Done"}
	ch.Settings.Columns[1].MovesTo = []string{"Todo"}
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, len(b.Settings.Columns), 2)
	assertEqual(t, titles(b, "Todo"), []string{"t1", "t2"})
	assertConsistent(t, b)
}

func TestRemovedColumnOfAnArchivedCardLeavesNoStaleColumn(t *testing.T) {
	// d1 was archived from "Done" and the board has no other card there once
	// d2 is deleted, so Done can go. The archived card must not keep naming it,
	// or it would sit in a column the settings do not have.
	b := settingsBoard(t)
	assertNoErr(t, b.DeleteCard(idOf(t, b, "d2")))
	ch := change(b)
	ch.Settings.Columns = ch.Settings.Columns[:2]
	ch.Settings.Columns[0].MovesTo = []string{"In progress"}
	ch.Settings.Columns[1].MovesTo = []string{"Todo"}
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Archived[0].Column, "")
	assertConsistent(t, b)
}

func TestReorderColumnsKeepsRanks(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	cols := ch.Settings.Columns
	ch.Settings.Columns = []Column{cols[2], cols[0], cols[1]}
	assertNoErr(t, b.ApplySettings(ch))

	assertEqual(t, b.Settings.Columns[0].Name, "Done")
	var order []string
	for _, c := range b.Cards {
		order = append(order, c.Title)
	}
	assertEqual(t, order, []string{"d2", "t1", "t2", "p1", "p2"})
	assertConsistent(t, b)
}

func TestRenameGroup(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	ch.Settings.Groups[0].Name = "Defects"
	ch.GroupRenames = map[string]string{"Bugs": "Defects"}
	assertNoErr(t, b.ApplySettings(ch))

	assertEqual(t, b.Cards[0].Group, "Defects")
	assertEqual(t, b.Cards[1].Group, "Chores")
	assertEqual(t, b.Archived[0].Group, "Defects")
	assertEqual(t, b.Warnings(), []string{})
}

func TestRenameGroupsSwap(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	ch.Settings.Groups[0].Name, ch.Settings.Groups[1].Name = "Chores", "Bugs"
	ch.GroupRenames = map[string]string{"Bugs": "Chores", "Chores": "Bugs"}
	assertNoErr(t, b.ApplySettings(ch))

	assertEqual(t, b.Cards[0].Group, "Chores") // t1 was Bugs
	assertEqual(t, b.Cards[1].Group, "Bugs")   // t2 was Chores
	assertEqual(t, b.Archived[0].Group, "Chores")
}

func TestRemoveGroupClearsCards(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	ch.Settings.Groups = ch.Settings.Groups[1:] // drop Bugs
	assertNoErr(t, b.ApplySettings(ch))

	assertEqual(t, b.Cards[0].Group, "")
	assertEqual(t, b.Cards[1].Group, "Chores")
	assertEqual(t, b.Archived[0].Group, "")
	assertEqual(t, b.Warnings(), []string{})
}

func TestRemoveGroupLeavesAlreadyUnknownGroupsAlone(t *testing.T) {
	b := settingsBoard(t)
	b.Cards[0].Group = "Hand edited"
	ch := change(b)
	ch.Settings.Groups = ch.Settings.Groups[1:]
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Cards[0].Group, "Hand edited")
}

func TestPrefixImmutable(t *testing.T) {
	b := settingsBoard(t)
	next := b.Settings.NextID
	ch := change(b)
	ch.Settings.Prefix = "ZZ"
	ch.Settings.NextID = 99
	ch.Settings.Name = "Renamed"
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Settings.Prefix, "T")
	assertEqual(t, b.Settings.NextID, next)
	assertEqual(t, b.Settings.Name, "Renamed")

	// An invalid incoming prefix is ignored along with the rest of it.
	ch.Settings.Prefix = "9 bad"
	ch.Settings.NextID = 0
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Settings.Prefix, "T")
}

func TestInvalidSettingsRejected(t *testing.T) {
	b := settingsBoard(t)
	before := b.Clone()
	ch := change(b)
	// moves_to points at a column that is no longer there.
	ch.Settings.Columns = []Column{ch.Settings.Columns[0], ch.Settings.Columns[2]}
	err := b.ApplySettings(ch)
	assertTrue(t, errors.Is(err, ErrInvalid))
	assertTrue(t, b.Equal(before))

	// A moves_to that still uses a renamed column's old name is the caller's job.
	ch = change(b)
	ch.Settings.Columns[1].Name = "Doing"
	ch.ColumnRenames = map[string]string{"In progress": "Doing"}
	assertTrue(t, errors.Is(b.ApplySettings(ch), ErrInvalid))
	assertTrue(t, b.Equal(before))
}

func TestApplySettingsDoesNotAliasTheChange(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	assertNoErr(t, b.ApplySettings(ch))
	ch.Settings.Columns[0].MovesTo[0] = "mutated"
	ch.Settings.Groups[0].Name = "mutated"
	assertEqual(t, b.Settings.Columns[0].MovesTo[0], "In progress")
	assertEqual(t, b.Settings.Groups[0].Name, "Bugs")
}

func TestLoweringWIPBelowCountIsAWarning(t *testing.T) {
	b := settingsBoard(t)
	ch := change(b)
	ch.Settings.Columns[0].WIP = 1 // Todo holds two cards
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Warnings(), []string{"Todo is over its WIP limit (2/1)"})
}

func TestValidateAppearance(t *testing.T) {
	good := []Appearance{
		{},
		{Theme: "system", Accent: "orange", Density: "comfortable", Swimlane: "none", Textures: "on"},
		{Theme: "light", Accent: "#0a1B2c", Density: "compact", Swimlane: "group", Textures: "off"},
		{Theme: "dark", Accent: "violet", Swimlane: "priority"},
	}
	for _, a := range good {
		assertNoErr(t, ValidateAppearance(a))
	}
	bad := []Appearance{
		{Theme: "blue"}, {Accent: "purple"}, {Accent: "#fff"}, {Accent: "#12345g"}, {Accent: "#1234567"},
		{Accent: "Orange"}, {Density: "tight"}, {Swimlane: "column"}, {Textures: "yes"},
	}
	for _, a := range bad {
		assertTrue(t, errors.Is(ValidateAppearance(a), ErrInvalid))
	}
}

func TestApplySettingsChecksAppearanceButValidateDoesNot(t *testing.T) {
	b := settingsBoard(t)
	before := b.Clone()
	ch := change(b)
	ch.Settings.Appearance.Accent = "chartreuse"
	assertTrue(t, errors.Is(b.ApplySettings(ch), ErrInvalid))
	assertTrue(t, b.Equal(before))

	// A hand-edited typo must not stop the board from loading or saving.
	assertNoErr(t, ch.Settings.Validate())

	ch.Settings.Appearance = Appearance{Accent: "teal", Density: "compact"}
	assertNoErr(t, b.ApplySettings(ch))
	assertEqual(t, b.Settings.Appearance, Appearance{Accent: "teal", Density: "compact"})
}

func TestApplySettingsLeavesNoCardInAMissingColumn(t *testing.T) {
	// Every accepted change, whatever its shape, leaves the board consistent.
	changes := map[string]func(b *Board, ch *SettingsChange){
		"rename":  func(b *Board, ch *SettingsChange) { renameColumn(ch, "Todo", "Backlog") },
		"reorder": func(b *Board, ch *SettingsChange) { c := ch.Settings.Columns; c[0], c[2] = c[2], c[0] },
		"identity": func(b *Board, ch *SettingsChange) {
		},
		"drop empty": func(b *Board, ch *SettingsChange) {
			for _, id := range b.CardsIn("In progress") {
				assertNoErr(t, b.DeleteCard(id.ID))
			}
			ch.Settings.Columns = []Column{ch.Settings.Columns[0], ch.Settings.Columns[2]}
			ch.Settings.Columns[0].MovesTo = []string{"Done"}
			ch.Settings.Columns[1].MovesTo = []string{"Todo"}
		},
	}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			b := settingsBoard(t)
			ch := change(b)
			mutate(b, &ch)
			assertNoErr(t, b.ApplySettings(ch))
			assertConsistent(t, b)
		})
	}
}
