package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// DueForArchive lists the cards that auto archiving would move: in a done
// column, with a done date, at least Archive.AfterDays whole UTC calendar days
// old. It is empty unless the archive mode is auto.
func (b *Board) DueForArchive(today time.Time) []CardID {
	if b.Settings.Archive.Mode != ArchiveAuto {
		return nil
	}
	now := Day(today)
	var due []CardID
	for _, c := range b.Cards {
		if col, ok := b.Column(c.Column); !ok || !col.Done || c.Done.IsZero() {
			continue
		}
		if days := int(now.Sub(Day(c.Done)) / (24 * time.Hour)); days >= b.Settings.Archive.AfterDays {
			due = append(due, c.ID)
		}
	}
	return due
}

// DoneCards lists every card currently in a done column, in board order.
func (b *Board) DoneCards() []CardID {
	var ids []CardID
	for _, c := range b.Cards {
		if col, ok := b.Column(c.Column); ok && col.Done {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// Archive moves cards from the board to the front of the archive, keeping the
// given order at the front so the archive stays newest first. Every id must be
// a card in a done column; otherwise nothing changes. A card with no done date
// is stamped now, because an archived card must be dated to be read back.
func (b *Board) Archive(ids []CardID, now time.Time) error {
	for _, id := range ids {
		i := b.indexOf(id)
		if i < 0 {
			return fmt.Errorf("%w: card %s", ErrNotFound, id)
		}
		if col, ok := b.Column(b.Cards[i].Column); !ok || !col.Done {
			return fmt.Errorf("%w: %s is in %q, not a done column", ErrInvalid, id, b.Cards[i].Column)
		}
	}
	var moved []Card
	for _, id := range ids {
		i := b.indexOf(id)
		if i < 0 { // listed twice
			continue
		}
		card := b.Cards[i]
		if card.Done.IsZero() {
			card.Done = Stamp(now)
		}
		moved = append(moved, card)
		b.Cards = slices.Delete(b.Cards, i, i+1)
	}
	b.Archived = slices.Concat(moved, b.Archived)
	return nil
}

// Restore moves an archived card back to the bottom of a column; "" means the
// first column. Its done date is set or cleared to match the column. Restoring
// is not a move between columns, so moves_to and WIP limits do not apply.
func (b *Board) Restore(id CardID, to string, now time.Time) error {
	at := slices.IndexFunc(b.Archived, func(c Card) bool { return c.ID == id })
	if at < 0 {
		return fmt.Errorf("%w: archived card %s", ErrNotFound, id)
	}
	if prefix, _, _ := strings.Cut(string(id), "-"); prefix != b.Settings.Prefix {
		return fmt.Errorf("%w: %s cannot be restored: its prefix does not match this board's prefix %q", ErrInvalid, id, b.Settings.Prefix)
	}
	if to == "" {
		if len(b.Settings.Columns) == 0 {
			return fmt.Errorf("%w: board has no columns", ErrUnknownColumn)
		}
		to = b.Settings.Columns[0].Name
	}
	col, ok := b.Column(to)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownColumn, to)
	}
	card := b.Archived[at]
	card.Column = col.Name
	card.Done = time.Time{}
	if col.Done {
		card.Done = Stamp(now)
	}
	b.Archived = slices.Delete(b.Archived, at, at+1)
	b.Cards = slices.Insert(b.Cards, b.bottomOf(col.Name), card)
	return nil
}
