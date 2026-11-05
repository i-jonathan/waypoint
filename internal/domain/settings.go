package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// SettingsChange is a request to replace a board's settings. ColumnRenames,
// GroupRenames and PriorityRenames map an old name to its new name, so that
// cards follow a rename instead of being treated as belonging to a removed
// column, group or priority. All are
// read against the names the board has now, all at once, so a swap (A to B and
// B to A) or a chain (A to B and B to C) works.
type SettingsChange struct {
	Settings      Settings
	ColumnRenames map[string]string // old → new
	GroupRenames  map[string]string // old → new

	// PriorityRenames follows the rules of GroupRenames: names match without
	// regard to case, and a priority that is gone and not renamed is cleared
	// from the cards that had it, live and archived.
	PriorityRenames map[string]string // old → new
}

var accentHexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var accentPresets = []string{"orange", "cobalt", "teal", "rose", "green", "amber", "sky", "violet"}

// CheckAppearanceField reports whether value is one the interface knows for the
// appearance field theme, accent, density, swimlane or textures, wrapping
// ErrInvalid if not. An empty value is always fine: it inherits.
func CheckAppearanceField(field, value string) error {
	if value == "" {
		return nil
	}
	var allowed []string
	switch field {
	case "theme":
		allowed = []string{"system", "light", "dark"}
	case "accent":
		if slices.Contains(accentPresets, value) || accentHexRE.MatchString(value) {
			return nil
		}
		return fmt.Errorf("%w: appearance accent %q must be one of %s or #rrggbb", ErrInvalid, value, strings.Join(accentPresets, ", "))
	case "density":
		allowed = []string{"comfortable", "compact"}
	case "swimlane":
		allowed = []string{"none", "group", "priority"}
	case "textures":
		allowed = []string{"on", "off"}
	}
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("%w: appearance %s %q must be one of %s", ErrInvalid, field, value, strings.Join(allowed, ", "))
}

// ValidateAppearance reports the first appearance value that is not one the
// interface knows, wrapping ErrInvalid. Settings.Validate does not call this, so
// a typo in a hand-edited file cannot lock a board; ApplySettings does.
func ValidateAppearance(a Appearance) error {
	for _, f := range []struct{ field, value string }{
		{"theme", a.Theme}, {"accent", a.Accent}, {"density", a.Density}, {"swimlane", a.Swimlane}, {"textures", a.Textures},
	} {
		if err := CheckAppearanceField(f.field, f.value); err != nil {
			return err
		}
	}
	return nil
}

// ApplySettings replaces the board's settings with ch.Settings and moves the
// cards along with the renames. Prefix and NextID cannot change: the incoming
// values are ignored. A column that is absent and not renamed must hold no
// cards. A group or priority that is absent and not renamed is cleared from the
// cards that had it, live and archived. Cards keep their rank within a column, and when
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
	if err := checkNameSpacing(s); err != nil {
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
	priorityMap, err := nameMapping("priority", b.Settings.Priorities, s.Priorities, ch.PriorityRenames)
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
		next.Cards[i].Group = mapName(next.Cards[i].Group, groupMap)
		next.Cards[i].Priority = mapName(next.Cards[i].Priority, priorityMap)
	}
	for i := range next.Archived {
		next.Archived[i].Column = colMap[next.Archived[i].Column] // "" and unknown names map to ""
		next.Archived[i].Group = mapName(next.Archived[i].Group, groupMap)
		next.Archived[i].Priority = mapName(next.Archived[i].Priority, priorityMap)
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

// groupMapping is columnMapping for groups; see nameMapping.
func groupMapping(cur, next Settings, renames map[string]string) (map[string]string, error) {
	names := func(s Settings) []string {
		out := make([]string, len(s.Groups))
		for i, g := range s.Groups {
			out[i] = g.Name
		}
		return out
	}
	return nameMapping("group", names(cur), names(next), renames)
}

// nameMapping is columnMapping for a list of names that match without regard
// to case, as groups and priorities do everywhere else. The map is keyed by
// lower-cased old name and holds the new settings' spelling. A name that is
// gone and not renamed maps to "". kind ("group", "priority") words the errors.
func nameMapping(kind string, cur, next []string, renames map[string]string) (map[string]string, error) {
	spelling := func(list []string, name string) (string, bool) {
		for _, n := range list {
			if strings.EqualFold(n, name) {
				return n, true
			}
		}
		return "", false
	}
	renamed := make(map[string]string, len(renames)) // lower-cased old name → new spelling
	for from, to := range renames {
		old, ok := spelling(cur, from)
		if !ok {
			return nil, fmt.Errorf("%w: cannot rename %s %q: the board has no such %s", ErrInvalid, kind, from, kind)
		}
		canon, ok := spelling(next, to)
		if !ok {
			return nil, fmt.Errorf("%w: %s %q is renamed to %q, which the new settings do not have", ErrInvalid, kind, from, to)
		}
		renamed[strings.ToLower(old)] = canon
	}
	out := make(map[string]string, len(cur))
	from := make(map[string]string, len(cur)) // lower-cased new name → old name
	for _, name := range cur {
		key := strings.ToLower(name)
		to, ok := renamed[key]
		if !ok {
			to, _ = spelling(next, name) // "" when the name is gone
		}
		if to != "" {
			if prev, dup := from[strings.ToLower(to)]; dup {
				return nil, fmt.Errorf("%w: %ss %q and %q would both become %q", ErrInvalid, kind, prev, name, to)
			}
			from[strings.ToLower(to)] = name
		}
		out[key] = to
	}
	return out, nil
}

// mapName returns the group or priority a card has afterwards. A name the board
// never had (a hand-edited file can hold one) is left alone; Warnings reports it.
func mapName(name string, m map[string]string) string {
	if to, ok := m[strings.ToLower(name)]; ok {
		return to
	}
	return name
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

// checkNameSpacing refuses group and priority names with outer spaces, which a card's field
// can't keep. It runs on a save and not in Validate, so a board that already has such a name
// in its file still opens and can be fixed.
func checkNameSpacing(s Settings) error {
	for _, g := range s.Groups {
		if g.Name != strings.TrimSpace(g.Name) {
			return fmt.Errorf("%w: group name %q has leading or trailing spaces", ErrInvalid, g.Name)
		}
	}
	for _, p := range s.Priorities {
		if p != strings.TrimSpace(p) {
			return fmt.Errorf("%w: priority name %q has leading or trailing spaces", ErrInvalid, p)
		}
	}
	return nil
}
