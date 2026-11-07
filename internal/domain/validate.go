package domain

import (
	"fmt"
	"strings"
)

// Validate checks a whole board that came from outside Waypoint's own
// operations (an import), so that nothing in it could be refused or changed by
// a storage format later. It checks the settings and then every card, live and
// archived:
//   - the id is well formed, unique across both lists, and spelled with exactly
//     the board's prefix when it matches that prefix ignoring case;
//   - the title is one line with no spaces at either end;
//   - a live card is in a column that exists, and a group or priority it names
//     exists with the same spelling;
//   - depends are well formed ids, and extra field keys are valid, unique and
//     have one-line, non-empty values.
//
// It does not enforce move rules or WIP limits: they govern moves, and a file
// edited by hand can break them too (Warnings reports that). Errors wrap ErrInvalid
// and name the card.
func (b *Board) Validate() error {
	if err := b.Settings.Validate(); err != nil {
		return err
	}
	seen := map[CardID]bool{}
	check := func(c Card, live bool) error {
		fail := func(format string, args ...any) error {
			return fmt.Errorf("%w: card %s: %s", ErrInvalid, c.ID, fmt.Sprintf(format, args...))
		}
		if !ValidCardID(string(c.ID)) {
			return fmt.Errorf("%w: %q is not a card id (expected a prefix, a dash and a number, like T-1)", ErrInvalid, c.ID)
		}
		if prefix, _, _ := strings.Cut(string(c.ID), "-"); prefix != b.Settings.Prefix && strings.EqualFold(prefix, b.Settings.Prefix) {
			return fail("the id must be spelled %s-<number> to match the board's prefix", b.Settings.Prefix)
		}
		if seen[c.ID] {
			return fail("the id is used by more than one card")
		}
		seen[c.ID] = true
		if clean, err := cleanTitle(c.Title); err != nil {
			return fail("%s", strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": "))
		} else if clean != c.Title {
			return fail("the title has spaces at the start or end")
		}
		if live {
			if _, ok := b.Column(c.Column); !ok {
				return fail("the column %q is not on the board", c.Column)
			}
		}
		if c.Group != "" && !hasGroup(b.Settings.Groups, c.Group) {
			return fail("the group %q is not one of the board's groups", c.Group)
		}
		if c.Priority != "" && !contains(b.Settings.Priorities, c.Priority) {
			return fail("the priority %q is not one of the board's priorities", c.Priority)
		}
		if err := validateDepends(c.Depends); err != nil {
			return fail("%s", strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": "))
		}
		keys := map[string]bool{}
		for _, f := range c.Extra {
			if err := validateExtra(f.Key, f.Value); err != nil {
				return fail("%s", strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": "))
			}
			if f.Value == "" || keys[f.Key] {
				return fail("the field %q is empty or repeated", f.Key)
			}
			keys[f.Key] = true
		}
		return nil
	}
	for _, c := range b.Cards {
		if err := check(c, true); err != nil {
			return err
		}
	}
	for _, c := range b.Archived {
		if err := check(c, false); err != nil {
			return err
		}
	}
	return nil
}

func hasGroup(groups []Group, name string) bool {
	for _, g := range groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
