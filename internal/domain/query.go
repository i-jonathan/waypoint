package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// Blocker is a dependency that has not been finished. Missing means the card
// does not exist, live or archived.
type Blocker struct {
	ID      CardID
	Missing bool
}

// Blockers lists the direct dependencies of a card that are not yet done: each
// Depends entry that is neither in a done column nor archived. Unknown ids are
// reported as Missing, and an id listed twice is reported once. Dependencies
// are not followed transitively, so cycles are harmless. An unknown card has no
// blockers.
func (b *Board) Blockers(id CardID) []Blocker {
	card, _, ok := b.Find(id)
	if !ok {
		return nil
	}
	var out []Blocker
	seen := make(map[CardID]bool, len(card.Depends))
	for _, dep := range card.Depends {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		c, archived, found := b.Find(dep)
		switch {
		case !found:
			out = append(out, Blocker{ID: dep, Missing: true})
		case archived:
		default:
			if col, known := b.Column(c.Column); known && col.Done {
				continue
			}
			out = append(out, Blocker{ID: dep})
		}
	}
	return out
}

// NextUp returns the top card of the first non-done column that has cards.
func (b *Board) NextUp() (Card, bool) {
	for _, col := range b.Settings.Columns {
		if col.Done {
			continue
		}
		if cards := b.CardsIn(col.Name); len(cards) > 0 {
			return cards[0], true
		}
	}
	return Card{}, false
}

// Warnings lists board problems that do not block saving: columns over their
// WIP limit (in column order), then, for each card in rank order, a group or
// priority that the settings do not define (as a hand-edited file can hold),
// then the board's Notices.
func (b *Board) Warnings() []string {
	out := []string{}
	for _, col := range b.Settings.Columns {
		if n := len(b.CardsIn(col.Name)); col.WIP > 0 && n > col.WIP {
			out = append(out, fmt.Sprintf("%s is over its WIP limit (%d/%d)", col.Name, n, col.WIP))
		}
	}
	for _, c := range b.Cards {
		if c.Group != "" && !b.hasGroup(c.Group) {
			out = append(out, fmt.Sprintf("%s has unknown group %q", c.ID, c.Group))
		}
		if c.Priority != "" && !b.hasPriority(c.Priority) {
			out = append(out, fmt.Sprintf("%s has unknown priority %q", c.ID, c.Priority))
		}
	}
	return append(out, b.Notices...)
}

func (b *Board) hasGroup(name string) bool {
	_, err := b.canonicalGroup(name)
	return err == nil
}

func (b *Board) hasPriority(name string) bool {
	_, err := b.canonicalPriority(name)
	return err == nil
}

// CardHash returns a hex sha256 over the card's content fields: ID, Title,
// Column, Group, Priority, Depends, Body and Extra. Created and Done are not
// included. Every field is length-prefixed, so values cannot run together:
// ("ab", "c") and ("a", "bc") hash differently.
func CardHash(c Card) string {
	var buf []byte
	str := func(s string) {
		buf = binary.AppendUvarint(buf, uint64(len(s)))
		buf = append(buf, s...)
	}
	str(string(c.ID))
	str(c.Title)
	str(c.Column)
	str(c.Group)
	str(c.Priority)
	buf = binary.AppendUvarint(buf, uint64(len(c.Depends)))
	for _, d := range c.Depends {
		str(string(d))
	}
	str(c.Body)
	buf = binary.AppendUvarint(buf, uint64(len(c.Extra)))
	for _, f := range c.Extra {
		str(f.Key)
		str(f.Value)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}
