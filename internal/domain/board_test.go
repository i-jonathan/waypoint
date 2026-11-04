package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewBoardDefaults(t *testing.T) {
	b := NewBoard("Home", "H")
	names := []string{}
	for _, c := range b.Settings.Columns {
		names = append(names, c.Name)
	}
	assertEqual(t, names, []string{"Todo", "In progress", "Done"})
	done, _ := b.Column("Done")
	assertTrue(t, done.Done)
	todo, _ := b.Column("Todo")
	assertEqual(t, todo.MovesTo, []string{"In progress", "Done"})
	assertEqual(t, b.Settings.Priorities, []string{"P0", "P1", "P2"})
	assertEqual(t, b.Settings.Archive, ArchiveSettings{Mode: ArchiveManual, AfterDays: 14})
	assertEqual(t, b.Settings.NextID, 1)
	assertNoErr(t, b.Settings.Validate())
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Settings){
		"no columns":            func(s *Settings) { s.Columns = nil },
		"duplicate column":      func(s *Settings) { s.Columns = append(s.Columns, s.Columns[0]) },
		"moves_to unknown":      func(s *Settings) { s.Columns[0].MovesTo = []string{"Nope"} },
		"negative wip":          func(s *Settings) { s.Columns[0].WIP = -1 },
		"bad prefix":            func(s *Settings) { s.Prefix = "T-1" },
		"next_id zero":          func(s *Settings) { s.NextID = 0 },
		"bad archive mode":      func(s *Settings) { s.Archive.Mode = "sometimes" },
		"duplicate group":       func(s *Settings) { s.Groups = []Group{{Name: "A"}, {Name: "a"}} },
		"column leading space":  func(s *Settings) { s.Columns[0].Name = " Todo" },
		"column trailing space": func(s *Settings) { s.Columns[0].Name = "Todo\t" },
		"column line break":     func(s *Settings) { s.Columns[0].Name = "To\ndo" },
		"name line break":       func(s *Settings) { s.Name = "a\nb" },
		"group line break":      func(s *Settings) { s.Groups = []Group{{Name: "A\r\nB"}} },
		"priority line break":   func(s *Settings) { s.Priorities = []string{"P0", "P\n1"} },
	}
	for name, mutate := range cases {
		s := NewBoard("x", "T").Settings
		mutate(&s)
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestColumnIsExactMatch(t *testing.T) {
	b := NewBoard("x", "T")
	_, ok := b.Column("todo")
	assertTrue(t, !ok)
	c, ok := b.Column("In progress")
	assertTrue(t, ok)
	assertEqual(t, c.Name, "In progress")
}

func TestCardsInKeepsRankOrder(t *testing.T) {
	b := NewBoard("x", "T")
	b.Cards = []Card{
		{ID: "T-1", Column: "Todo"},
		{ID: "T-2", Column: "Done"},
		{ID: "T-3", Column: "Todo"},
	}
	var ids []CardID
	for _, c := range b.CardsIn("Todo") {
		ids = append(ids, c.ID)
	}
	assertEqual(t, ids, []CardID{"T-1", "T-3"})
	assertEqual(t, len(b.CardsIn("In progress")), 0)
}

func TestFind(t *testing.T) {
	b := NewBoard("x", "T")
	b.Cards = []Card{{ID: "T-1", Title: "live"}}
	b.Archived = []Card{{ID: "T-2", Title: "old"}}

	c, archived, ok := b.Find("T-1")
	assertTrue(t, ok)
	assertTrue(t, !archived)
	assertEqual(t, c.Title, "live")

	c, archived, ok = b.Find("T-2")
	assertTrue(t, ok)
	assertTrue(t, archived)
	assertEqual(t, c.Title, "old")

	_, _, ok = b.Find("T-9")
	assertTrue(t, !ok)
}

func TestDayTruncatesToUTCDate(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*3600)
	in := time.Date(2026, 11, 4, 2, 30, 0, 0, loc) // 2026-11-03 21:30 UTC
	assertEqual(t, Day(in), time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	assertTrue(t, Day(time.Time{}).Year() == 1)
}
