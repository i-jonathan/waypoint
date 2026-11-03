package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// NewCard describes a card to add. Column "" means the first column.
type NewCard struct {
	Title, Column, Group, Priority, Body string
	Depends                              []CardID
	Extra                                []Field
}

// CardPatch is a partial card update. Nil fields are left alone. An empty
// Group or Priority clears it. In Set, an empty value deletes the key.
type CardPatch struct {
	Title, Group, Priority, Body *string
	Depends                      *[]CardID
	Set                          map[string]string
}

// knownFields are written by the markdown format itself, so they cannot be
// used as extra field keys.
var knownFields = []string{"group", "priority", "depends", "created", "done"}

// wipError reports a full column. Its message is the bare sentence the CLI
// prints, and it unwraps to ErrWIPLimit.
type wipError struct{ msg string }

func (e wipError) Error() string { return e.msg }
func (e wipError) Unwrap() error { return ErrWIPLimit }

// AddCard appends a new card at the bottom of its column and assigns it the
// next id. On error the board is unchanged.
func (b *Board) AddCard(n NewCard, today time.Time) (Card, error) {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		return Card{}, fmt.Errorf("%w: title is empty", ErrInvalid)
	}
	column := n.Column
	if column == "" {
		if len(b.Settings.Columns) == 0 {
			return Card{}, fmt.Errorf("%w: board has no columns", ErrUnknownColumn)
		}
		column = b.Settings.Columns[0].Name
	}
	col, ok := b.Column(column)
	if !ok {
		return Card{}, fmt.Errorf("%w: %q", ErrUnknownColumn, column)
	}
	group, err := b.canonicalGroup(n.Group)
	if err != nil {
		return Card{}, err
	}
	priority, err := b.canonicalPriority(n.Priority)
	if err != nil {
		return Card{}, err
	}
	id := CardID(fmt.Sprintf("%s-%d", b.Settings.Prefix, b.Settings.NextID))
	if slices.Contains(n.Depends, id) {
		return Card{}, fmt.Errorf("%w: %s cannot depend on itself", ErrInvalid, id)
	}
	for _, f := range n.Extra {
		if err := validateExtraKey(f.Key); err != nil {
			return Card{}, err
		}
	}

	card := Card{
		ID:       id,
		Title:    title,
		Column:   col.Name,
		Group:    group,
		Priority: priority,
		Depends:  slices.Clone(n.Depends),
		Created:  Day(today),
		Body:     n.Body,
		Extra:    slices.Clone(n.Extra),
	}
	if col.Done {
		card.Done = Day(today)
	}
	b.Settings.NextID++
	b.Cards = slices.Insert(b.Cards, b.bottomOf(col.Name), card)
	return card, nil
}

// MoveCard moves a live card to a column and position. An index below zero or
// at or past the end of the target column means the bottom. Moving within the
// same column only reorders and is always allowed. On error the board is
// unchanged.
func (b *Board) MoveCard(id CardID, to string, index int, today time.Time) error {
	from := b.indexOf(id)
	if from < 0 {
		return fmt.Errorf("%w: card %s", ErrNotFound, id)
	}
	dest, ok := b.Column(to)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownColumn, to)
	}
	card := b.Cards[from]
	if card.Column != dest.Name {
		src, _ := b.Column(card.Column)
		if !slices.Contains(src.MovesTo, dest.Name) {
			return fmt.Errorf("%w: %s cannot move to %s", ErrMoveNotAllowed, card.Column, dest.Name)
		}
		if n := len(b.CardsIn(dest.Name)); dest.WIP > 0 && n >= dest.WIP {
			return wipError{fmt.Sprintf("%s is at its WIP limit (%d/%d)", dest.Name, n, dest.WIP)}
		}
		switch {
		case !dest.Done:
			card.Done = time.Time{}
		case !src.Done:
			card.Done = Day(today)
		}
		card.Column = dest.Name
	}

	b.Cards = slices.Delete(b.Cards, from, from+1)
	pos := b.bottomOf(dest.Name)
	if index >= 0 {
		seen := 0
		for i, c := range b.Cards {
			if c.Column != dest.Name {
				continue
			}
			if seen == index {
				pos = i
				break
			}
			seen++
		}
	}
	b.Cards = slices.Insert(b.Cards, pos, card)
	return nil
}

// DeleteCard removes a card from the board or the archive. NextID is left
// alone so ids are never reused.
func (b *Board) DeleteCard(id CardID) error {
	if i := b.indexOf(id); i >= 0 {
		b.Cards = slices.Delete(b.Cards, i, i+1)
		return nil
	}
	for i, c := range b.Archived {
		if c.ID == id {
			b.Archived = slices.Delete(b.Archived, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("%w: card %s", ErrNotFound, id)
}

// UpdateCard applies a patch to a card on the board or in the archive. The
// whole patch is validated first, so a failing patch changes nothing.
func (b *Board) UpdateCard(id CardID, p CardPatch) error {
	var target *Card
	if i := b.indexOf(id); i >= 0 {
		target = &b.Cards[i]
	} else {
		for i := range b.Archived {
			if b.Archived[i].ID == id {
				target = &b.Archived[i]
			}
		}
	}
	if target == nil {
		return fmt.Errorf("%w: card %s", ErrNotFound, id)
	}

	next := *target // edited copy; its slices still alias the board until replaced
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			return fmt.Errorf("%w: title is empty", ErrInvalid)
		}
		next.Title = title
	}
	if p.Group != nil {
		group, err := b.canonicalGroup(*p.Group)
		if err != nil {
			return err
		}
		next.Group = group
	}
	if p.Priority != nil {
		priority, err := b.canonicalPriority(*p.Priority)
		if err != nil {
			return err
		}
		next.Priority = priority
	}
	if p.Body != nil {
		next.Body = *p.Body
	}
	if p.Depends != nil {
		if slices.Contains(*p.Depends, id) {
			return fmt.Errorf("%w: %s cannot depend on itself", ErrInvalid, id)
		}
		next.Depends = slices.Clone(*p.Depends)
	}
	if len(p.Set) > 0 {
		extra := slices.Clone(next.Extra)
		// Sorted so the order new keys are appended in is deterministic.
		keys := make([]string, 0, len(p.Set))
		for k := range p.Set {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if err := validateExtraKey(k); err != nil {
				return err
			}
			v := p.Set[k]
			at := slices.IndexFunc(extra, func(f Field) bool { return f.Key == k })
			switch {
			case v == "" && at >= 0:
				extra = slices.Delete(extra, at, at+1)
			case v == "":
			case at >= 0:
				extra[at].Value = v
			default:
				extra = append(extra, Field{Key: k, Value: v})
			}
		}
		next.Extra = extra
	}
	*target = next
	return nil
}

// indexOf returns the position of a live card, or -1.
func (b *Board) indexOf(id CardID) int {
	return slices.IndexFunc(b.Cards, func(c Card) bool { return c.ID == id })
}

// bottomOf returns the slice position that places a card at the bottom of the
// named column while keeping Cards sorted by column order: after the last card
// in that column or any earlier one.
func (b *Board) bottomOf(column string) int {
	order := make(map[string]int, len(b.Settings.Columns))
	for i, c := range b.Settings.Columns {
		order[c.Name] = i
	}
	limit := order[column]
	pos := 0
	for i, c := range b.Cards {
		if o, ok := order[c.Column]; ok && o <= limit {
			pos = i + 1
		}
	}
	return pos
}

// canonicalGroup returns the configured spelling of a group, matching without
// regard to case. "" means no group.
func (b *Board) canonicalGroup(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	for _, g := range b.Settings.Groups {
		if strings.EqualFold(g.Name, name) {
			return g.Name, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownGroup, name)
}

// canonicalPriority returns the configured spelling of a priority, matching
// without regard to case. "" means no priority.
func (b *Board) canonicalPriority(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	for _, p := range b.Settings.Priorities {
		if strings.EqualFold(p, name) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownPriority, name)
}

// validateExtraKey rejects keys the markdown format cannot round-trip.
func validateExtraKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, ":\r\n") {
		return fmt.Errorf("%w: field name %q", ErrInvalid, key)
	}
	if slices.Contains(knownFields, strings.ToLower(key)) {
		return fmt.Errorf("%w: %q is a built-in field", ErrInvalid, key)
	}
	return nil
}
