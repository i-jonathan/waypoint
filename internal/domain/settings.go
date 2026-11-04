package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// SettingsChange is a request to replace a board's settings. ColumnRenames and
// GroupRenames map an old name to its new name, so that cards follow a rename
// instead of being treated as belonging to a removed column or group. Both are
// read against the names the board has now, all at once, so a swap (A to B and
// B to A) or a chain (A to B and B to C) works.
type SettingsChange struct {
	Settings      Settings
	ColumnRenames map[string]string // old → new
	GroupRenames  map[string]string // old → new
}

var accentHexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var accentPresets = []string{"orange", "cobalt", "teal", "rose", "green", "amber", "sky", "violet"}

// ValidateAppearance reports the first appearance value that is not one the
// interface knows, wrapping ErrInvalid. An empty value is always fine: it
// inherits the global setting. Settings.Validate does not call this, so a typo
// in a hand-edited file cannot lock a board; ApplySettings does.
func ValidateAppearance(a Appearance) error {
	check := func(field, value string, allowed ...string) error {
		if value == "" || slices.Contains(allowed, value) {
			return nil
		}
		return fmt.Errorf("%w: appearance %s %q must be one of %s", ErrInvalid, field, value, strings.Join(allowed, ", "))
	}
	if err := check("theme", a.Theme, "system", "light", "dark"); err != nil {
		return err
	}
	if a.Accent != "" && !slices.Contains(accentPresets, a.Accent) && !accentHexRE.MatchString(a.Accent) {
		return fmt.Errorf("%w: appearance accent %q must be one of %s or #rrggbb", ErrInvalid, a.Accent, strings.Join(accentPresets, ", "))
	}
	if err := check("density", a.Density, "comfortable", "compact"); err != nil {
		return err
	}
	if err := check("swimlane", a.Swimlane, "none", "group", "priority"); err != nil {
		return err
	}
	return check("textures", a.Textures, "on", "off")
}

// ApplySettings replaces the board's settings with ch.Settings and moves the
// cards along with the renames. Prefix and NextID cannot change: the incoming
// values are ignored. A column that is absent and not renamed must hold no
// cards. A group that is absent and not renamed is cleared from the cards that
// had it, live and archived. Cards keep their rank within a column, and when
// the columns are reordered the cards are sorted stably into the new order.
// Lowering a WIP limit below the number of cards is allowed; Warnings reports
// it. moves_to entries must already use the new names: Validate rejects the
// old ones.
//
// The change is made on a copy and swapped in only on success, so on an error
// the board is exactly as it was. Afterwards every card is in a column the
// settings have; an archived card whose column is gone (the archive file does
// not store one) gets an empty column.
func (b *Board) ApplySettings(ch SettingsChange) error {
	next := b.Clone()
	s := cloneSettings(ch.Settings)
	s.Prefix, s.NextID = b.Settings.Prefix, b.Settings.NextID
	if err := s.Validate(); err != nil {
		return err
	}
	if err := ValidateAppearance(s.Appearance); err != nil {
		return err
	}
	next.Settings = s

	colMap, err := columnMapping(b.Settings, s, ch.ColumnRenames)
	if err != nil {
		return err
	}
	groupMap, err := groupMapping(b.Settings, s, ch.GroupRenames)
	if err != nil {
		return err
	}

	for _, c := range b.Cards { // a column that is gone and not renamed must be empty
		if _, ok := colMap[c.Column]; !ok {
			n := len(b.CardsIn(c.Column))
			return fmt.Errorf("%w: column %q still has %d %s", ErrInvalid, c.Column, n, plural(n, "card", "cards"))
		}
	}

	for i := range next.Cards {
		next.Cards[i].Column = colMap[next.Cards[i].Column]
		next.Cards[i].Group = mapGroup(next.Cards[i].Group, groupMap)
	}
	for i := range next.Archived {
		next.Archived[i].Column = colMap[next.Archived[i].Column] // "" and unknown names map to ""
		next.Archived[i].Group = mapGroup(next.Archived[i].Group, groupMap)
	}

	order := make(map[string]int, len(s.Columns))
	for i, c := range s.Columns {
		order[c.Name] = i
	}
	slices.SortStableFunc(next.Cards, func(a, b Card) int { return order[a.Column] - order[b.Column] })

	*b = *next
	return nil
}

// columnMapping maps every column name the board has now to the name it has
// afterwards. A name that is gone from the new settings and not renamed is left
// out. Renames are checked here.
func columnMapping(cur, next Settings, renames map[string]string) (map[string]string, error) {
	have := make(map[string]bool, len(cur.Columns))
	for _, c := range cur.Columns {
		have[c.Name] = true
	}
	want := make(map[string]bool, len(next.Columns))
	for _, c := range next.Columns {
		want[c.Name] = true
	}
	for from, to := range renames {
		if !have[from] {
			return nil, fmt.Errorf("%w: cannot rename column %q: the board has no such column", ErrInvalid, from)
		}
		if !want[to] {
			return nil, fmt.Errorf("%w: column %q is renamed to %q, which the new settings do not have", ErrInvalid, from, to)
		}
	}
	out := make(map[string]string, len(cur.Columns))
	from := make(map[string]string, len(cur.Columns)) // new name → old name
	for _, c := range cur.Columns {
		to, renamed := renames[c.Name]
		if !renamed {
			if !want[c.Name] {
				continue
			}
			to = c.Name
		}
		if prev, dup := from[to]; dup {
			return nil, fmt.Errorf("%w: columns %q and %q would both become %q", ErrInvalid, prev, c.Name, to)
		}
		from[to] = c.Name
		out[c.Name] = to
	}
	return out, nil
}

// groupMapping is columnMapping for groups. Names match without regard to case,
// as they do everywhere else, so the map is keyed by lower-cased old name and
// holds the new settings' spelling. A group that is gone and not renamed maps
// to "".
func groupMapping(cur, next Settings, renames map[string]string) (map[string]string, error) {
	spelling := func(s Settings, name string) (string, bool) {
		for _, g := range s.Groups {
			if strings.EqualFold(g.Name, name) {
				return g.Name, true
			}
		}
		return "", false
	}
	renamed := make(map[string]string, len(renames)) // lower-cased old name → new spelling
	for from, to := range renames {
		old, ok := spelling(cur, from)
		if !ok {
			return nil, fmt.Errorf("%w: cannot rename group %q: the board has no such group", ErrInvalid, from)
		}
		canon, ok := spelling(next, to)
		if !ok {
			return nil, fmt.Errorf("%w: group %q is renamed to %q, which the new settings do not have", ErrInvalid, from, to)
		}
		renamed[strings.ToLower(old)] = canon
	}
	out := make(map[string]string, len(cur.Groups))
	from := make(map[string]string, len(cur.Groups)) // lower-cased new name → old name
	for _, g := range cur.Groups {
		key := strings.ToLower(g.Name)
		to, ok := renamed[key]
		if !ok {
			to, _ = spelling(next, g.Name) // "" when the group is gone
		}
		if to != "" {
			if prev, dup := from[strings.ToLower(to)]; dup {
				return nil, fmt.Errorf("%w: groups %q and %q would both become %q", ErrInvalid, prev, g.Name, to)
			}
			from[strings.ToLower(to)] = g.Name
		}
		out[key] = to
	}
	return out, nil
}

// mapGroup returns the group a card has afterwards. A group the board never had
// (a hand-edited file can hold one) is left alone; Warnings reports it.
func mapGroup(group string, m map[string]string) string {
	if to, ok := m[strings.ToLower(group)]; ok {
		return to
	}
	return group
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// cloneSettings copies s so that nothing in it is shared with the caller.
func cloneSettings(s Settings) Settings {
	s.Columns = slices.Clone(s.Columns)
	for i := range s.Columns {
		s.Columns[i].MovesTo = slices.Clone(s.Columns[i].MovesTo)
	}
	s.Groups = slices.Clone(s.Groups)
	s.Priorities = slices.Clone(s.Priorities)
	return s
}
