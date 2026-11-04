// Package domain holds the Waypoint board model and its rules. It imports only
// the standard library.
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CardID identifies a card, in the form <prefix>-<n>.
type CardID string

// Column is a board column. WIP 0 means no limit. MovesTo lists the columns a
// card may move to from this one. Done marks a done column.
type Column struct {
	Name    string
	WIP     int
	MovesTo []string
	Done    bool
}

// Group is a named, colored card grouping.
type Group struct {
	Name, Color string
}

// Field is an unknown card field kept verbatim.
type Field struct {
	Key, Value string
}

// Card is a single board card. Created and Done are date-only (UTC midnight);
// the zero time means unset.
type Card struct {
	ID                             CardID
	Title, Column, Group, Priority string
	Depends                        []CardID
	Created, Done                  time.Time
	Body                           string
	Extra                          []Field // unknown fields, original order
}

// ArchiveMode controls how cards are archived.
type ArchiveMode string

const (
	ArchiveOff    ArchiveMode = "off"
	ArchiveManual ArchiveMode = "manual"
	ArchiveAuto   ArchiveMode = "auto"
)

// ArchiveSettings configures archiving.
type ArchiveSettings struct {
	Mode      ArchiveMode
	AfterDays int
}

// Appearance holds per-board display overrides. An empty value inherits the
// global setting.
type Appearance struct {
	Theme, Accent, Density, Swimlane, Textures string
}

// Settings is the board configuration.
type Settings struct {
	Name, Prefix string
	NextID       int
	Columns      []Column
	Groups       []Group
	Priorities   []string
	Archive      ArchiveSettings
	Appearance   Appearance
}

// Board is a board's settings and cards. Cards order is rank order.
type Board struct {
	Settings Settings
	Cards    []Card
	Archived []Card
	// Notices are findings from reading the board file, such as a card
	// heading that sits inside a code block. They are not persisted, and
	// Warnings reports them after its own.
	Notices []string
}

var (
	prefixRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	cardIDRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-\d+$`)
)

// ValidCardID reports whether id has the shape of a card id: a prefix, a dash
// and a number. It does not check that the card exists.
func ValidCardID(id string) bool { return cardIDRE.MatchString(id) }

// singleLine reports whether s has no line break.
func singleLine(s string) bool { return !strings.ContainsAny(s, "\r\n") }

// NewBoard returns a board with the default columns, priorities and archive
// settings, and NextID 1.
func NewBoard(name, prefix string) *Board {
	return &Board{Settings: Settings{
		Name:   name,
		Prefix: prefix,
		NextID: 1,
		Columns: []Column{
			{Name: "Todo", MovesTo: []string{"In progress", "Done"}},
			{Name: "In progress", MovesTo: []string{"Todo", "Done"}},
			{Name: "Done", MovesTo: []string{"In progress"}, Done: true},
		},
		Priorities: []string{"P0", "P1", "P2"},
		Archive:    ArchiveSettings{Mode: ArchiveManual, AfterDays: 14},
	}}
}

// Validate reports the first problem with the settings, wrapping ErrInvalid.
func (s Settings) Validate() error {
	if !singleLine(s.Name) {
		return fmt.Errorf("%w: project name must be a single line", ErrInvalid)
	}
	if !prefixRE.MatchString(s.Prefix) {
		return fmt.Errorf("%w: prefix %q must match %s", ErrInvalid, s.Prefix, prefixRE)
	}
	if s.NextID < 1 {
		return fmt.Errorf("%w: next_id must be at least 1, got %d", ErrInvalid, s.NextID)
	}
	if len(s.Columns) == 0 {
		return fmt.Errorf("%w: at least one column is required", ErrInvalid)
	}
	cols := make(map[string]bool, len(s.Columns))
	for _, c := range s.Columns {
		if c.Name == "" {
			return fmt.Errorf("%w: column name is empty", ErrInvalid)
		}
		if c.Name != strings.TrimSpace(c.Name) || !singleLine(c.Name) {
			return fmt.Errorf("%w: column name %q must be a single line without leading or trailing spaces", ErrInvalid, c.Name)
		}
		if cols[c.Name] {
			return fmt.Errorf("%w: duplicate column %q", ErrInvalid, c.Name)
		}
		cols[c.Name] = true
		if c.WIP < 0 {
			return fmt.Errorf("%w: column %q has negative wip %d", ErrInvalid, c.Name, c.WIP)
		}
	}
	for _, c := range s.Columns {
		for _, to := range c.MovesTo {
			if !cols[to] {
				return fmt.Errorf("%w: column %q moves_to unknown column %q", ErrInvalid, c.Name, to)
			}
		}
	}
	groups := make(map[string]bool, len(s.Groups))
	for _, g := range s.Groups {
		if !singleLine(g.Name) {
			return fmt.Errorf("%w: group name %q must be a single line", ErrInvalid, g.Name)
		}
		key := strings.ToLower(g.Name)
		if groups[key] {
			return fmt.Errorf("%w: duplicate group %q", ErrInvalid, g.Name)
		}
		groups[key] = true
	}
	for _, p := range s.Priorities {
		if !singleLine(p) {
			return fmt.Errorf("%w: priority name %q must be a single line", ErrInvalid, p)
		}
	}
	switch s.Archive.Mode {
	case ArchiveOff, ArchiveManual, ArchiveAuto:
	default:
		return fmt.Errorf("%w: archive mode %q must be off, manual or auto", ErrInvalid, s.Archive.Mode)
	}
	return nil
}

// Column returns the column with exactly the given name.
func (b *Board) Column(name string) (Column, bool) {
	for _, c := range b.Settings.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// CardsIn returns the cards in the named column, in rank order.
func (b *Board) CardsIn(column string) []Card {
	var out []Card
	for _, c := range b.Cards {
		if c.Column == column {
			out = append(out, c)
		}
	}
	return out
}

// Find looks a card up by id in the live cards, then in the archive.
func (b *Board) Find(id CardID) (c Card, archived bool, ok bool) {
	for _, c := range b.Cards {
		if c.ID == id {
			return c, false, true
		}
	}
	for _, c := range b.Archived {
		if c.ID == id {
			return c, true, true
		}
	}
	return Card{}, false, false
}

// Day truncates t to its UTC calendar date at 00:00 UTC.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
