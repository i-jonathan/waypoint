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
