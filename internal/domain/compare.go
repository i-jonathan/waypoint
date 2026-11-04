package domain

import "slices"

// SameContent reports whether two cards say the same thing: Title, Body, Group,
// Priority, Depends, Extra and Created all match. ID, Column and Done are not
// compared, because they change when a card moves between the board and the
// archive.
func (c Card) SameContent(o Card) bool {
	return c.Title == o.Title &&
		c.Body == o.Body &&
		c.Group == o.Group &&
		c.Priority == o.Priority &&
		slices.Equal(c.Depends, o.Depends) &&
		slices.Equal(c.Extra, o.Extra) &&
		c.Created.Equal(o.Created)
}

// Equal reports whether two cards match in every field.
func (c Card) Equal(o Card) bool {
	return c.ID == o.ID && c.Column == o.Column && c.Done.Equal(o.Done) && c.SameContent(o)
}

// Clone returns a deep copy of the board: changing the copy never changes b.
func (b *Board) Clone() *Board {
	c := *b
	c.Settings.Columns = slices.Clone(b.Settings.Columns)
	for i := range c.Settings.Columns {
		c.Settings.Columns[i].MovesTo = slices.Clone(c.Settings.Columns[i].MovesTo)
	}
	c.Settings.Groups = slices.Clone(b.Settings.Groups)
	c.Settings.Priorities = slices.Clone(b.Settings.Priorities)
	c.Cards = cloneCards(b.Cards)
	c.Archived = cloneCards(b.Archived)
	c.Notices = slices.Clone(b.Notices)
	return &c
}

func cloneCards(cards []Card) []Card {
	out := slices.Clone(cards)
	for i := range out {
		out[i].Depends = slices.Clone(out[i].Depends)
		out[i].Extra = slices.Clone(out[i].Extra)
	}
	return out
}

// Equal reports whether two boards hold the same settings and the same cards
// in the same order. Notices are not compared, because they are not stored.
func (b *Board) Equal(o *Board) bool {
	return b.Settings.equal(o.Settings) &&
		slices.EqualFunc(b.Cards, o.Cards, Card.Equal) &&
		slices.EqualFunc(b.Archived, o.Archived, Card.Equal)
}

func (s Settings) equal(o Settings) bool {
	return s.Name == o.Name &&
		s.Prefix == o.Prefix &&
		s.NextID == o.NextID &&
		slices.EqualFunc(s.Columns, o.Columns, Column.equal) &&
		slices.Equal(s.Groups, o.Groups) &&
		slices.Equal(s.Priorities, o.Priorities) &&
		s.Archive == o.Archive &&
		s.Appearance == o.Appearance
}

func (c Column) equal(o Column) bool {
	return c.Name == o.Name && c.WIP == o.WIP && c.Done == o.Done && slices.Equal(c.MovesTo, o.MovesTo)
}
