package markdown

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"waypoint/internal/domain"
)

// Format renders a board as its canonical Markdown file. Parse(Format(b)) gives
// back the same settings and cards. Bodies are written as they are, except that
// headings shallower than #### outside code fences are demoted, and a code
// fence left open at the end of a body is closed, so that a body can never
// swallow the cards after it. Board.Notices are not written.
func Format(b *domain.Board) []byte {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(frontMatterYAML(b.Settings))
	sb.WriteString("---\n\n")

	byColumn := make(map[string][]domain.Card, len(b.Settings.Columns))
	for _, c := range b.Cards {
		byColumn[c.Column] = append(byColumn[c.Column], c)
	}
	for _, col := range b.Settings.Columns {
		sb.WriteString("## ")
		sb.WriteString(col.Name)
		sb.WriteString("\n\n")
		for _, c := range byColumn[col.Name] {
			writeCard(&sb, c)
		}
	}
	return finish(&sb)
}

// FormatArchive renders archived cards as an archive file: a "# Archive" title
// and one "## YYYY-MM" section per month of the done date, newest month first.
// Cards keep their given order within a month.
func FormatArchive(cards []domain.Card) []byte {
	month := func(c domain.Card) string { return domain.Day(c.Done).Format("2006-01") }
	sorted := slices.Clone(cards)
	slices.SortStableFunc(sorted, func(a, b domain.Card) int { return strings.Compare(month(b), month(a)) })

	var sb strings.Builder
	sb.WriteString("# Archive\n\n")
	current := ""
	for _, c := range sorted {
		if m := month(c); m != current {
			sb.WriteString("## ")
			sb.WriteString(m)
			sb.WriteString("\n\n")
			current = m
		}
		writeCard(&sb, c)
	}
	return finish(&sb)
}

// finish ends the file with exactly one newline.
func finish(sb *strings.Builder) []byte {
	return []byte(strings.TrimRight(sb.String(), "\n") + "\n")
}

// writeCard writes one card: heading, fields, body, each followed by a blank
// line.
func writeCard(sb *strings.Builder, c domain.Card) {
	sb.WriteString("### ")
	sb.WriteString(string(c.ID))
	sb.WriteString(" ")
	sb.WriteString(oneLine(strings.TrimSpace(c.Title)))
	sb.WriteString("\n\n")
	fields := cardFieldLines(c)
	for _, f := range fields {
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	if len(fields) > 0 {
		sb.WriteString("\n")
	}
	body := formatBody(c.Body)
	if body == "" {
		return
	}
	// Without fields, a body that opens with a field-like line needs a second
	// blank line, or it would be read back as the card's fields.
	if len(fields) == 0 && startsLikeField(body) {
		sb.WriteString("\n")
	}
	sb.WriteString(body)
	sb.WriteString("\n\n")
}

// cardFieldLines returns the "- key: value" lines of a card in canonical order.
func cardFieldLines(c domain.Card) []string {
	var out []string
	add := func(key, value string) {
		value = strings.TrimRight(oneLine(value), " \t")
		if value == "" {
			out = append(out, "- "+key+":")
			return
		}
		out = append(out, "- "+key+": "+value)
	}
	if v := strings.TrimRight(oneLine(c.Group), " \t"); v != "" {
		add("group", v)
	}
	if v := strings.TrimRight(oneLine(c.Priority), " \t"); v != "" {
		add("priority", v)
	}
	if len(c.Depends) > 0 {
		ids := make([]string, len(c.Depends))
		for i, d := range c.Depends {
			ids[i] = string(d)
		}
		add("depends", strings.Join(ids, ", "))
	}
	if !c.Created.IsZero() {
		add("created", domain.StoredDate(c.Created))
	}
	if !c.Done.IsZero() {
		add("done", domain.StoredDate(c.Done))
	}
	for _, f := range c.Extra {
		add(f.Key, f.Value)
	}
	return out
}

// oneLine replaces line breaks with spaces, because a heading or field is a
// single line.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

// startsLikeField reports whether the first line of body would be read as a
// field line.
func startsLikeField(body string) bool {
	first, _, _ := strings.Cut(body, "\n")
	m := fieldRE.FindStringSubmatch(first)
	return m != nil && isFieldKey(m[1])
}

// shallowHeadingRE matches a body heading shallower than ####.
var shallowHeadingRE = regexp.MustCompile(`^#{1,3}([ \t].*)?$`)

// formatBody prepares a body for writing: line breaks become "\n", blank lines
// at both ends go (the reader drops them), headings shallower than #### outside
// code fences become ####, and a code fence still open at the end is closed.
func formatBody(body string) string {
	body = trimBlankLines(strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n"))
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	domain.ScanLines(body, func(i int, line string, inFence bool) {
		if m := shallowHeadingRE.FindStringSubmatch(line); !inFence && m != nil {
			lines[i] = "####" + m[1]
		}
	})

	// Scan with one more line, which cannot close a fence, to find out whether
	// the body ends inside one.
	opener := -1
	domain.ScanFences(strings.Join(lines, "\n")+"\nx", func(_ int, _ string, o int) { opener = o })
	if opener >= 0 {
		lines = append(lines, fenceCloser(lines[opener]))
	}
	return strings.Join(lines, "\n")
}

// fenceCloser returns a line that closes the fence opened by line.
func fenceCloser(opening string) string {
	s := strings.TrimLeft(opening, " ")
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return s[:n]
}

// frontMatterYAML renders the settings as YAML front matter text. Nodes are
// used so that yaml.v3 quotes every awkward name correctly in flow style.
func frontMatterYAML(s domain.Settings) string {
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	num := func(n int) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
	}
	flag := func(v bool) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
	}
	mapping := func(style yaml.Style) *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: style}
	}
	sequence := func(style yaml.Style, items ...string) *yaml.Node {
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: style}
		for _, it := range items {
			n.Content = append(n.Content, str(it))
		}
		return n
	}
	set := func(m *yaml.Node, key string, v *yaml.Node) { m.Content = append(m.Content, str(key), v) }

	doc := mapping(0)
	set(doc, "waypoint", num(formatVersion))
	set(doc, "name", str(s.Name))
	set(doc, "prefix", str(s.Prefix))
	set(doc, "next_id", num(s.NextID))

	columns := sequence(0)
	for _, c := range s.Columns {
		m := mapping(yaml.FlowStyle)
		set(m, "name", str(c.Name))
		if c.WIP != 0 {
			set(m, "wip", num(c.WIP))
		}
		if len(c.MovesTo) > 0 {
			set(m, "moves_to", sequence(yaml.FlowStyle, c.MovesTo...))
		}
		if c.Done {
			set(m, "done", flag(true))
		}
		columns.Content = append(columns.Content, m)
	}
	set(doc, "columns", columns)

	if len(s.Groups) > 0 {
		groups := sequence(0)
		for _, g := range s.Groups {
			m := mapping(yaml.FlowStyle)
			set(m, "name", str(g.Name))
			if g.Color != "" {
				set(m, "color", str(g.Color))
			}
			groups.Content = append(groups.Content, m)
		}
		set(doc, "groups", groups)
	}
	if !slices.Equal(s.Priorities, domain.NewBoard("", "T").Settings.Priorities) {
		set(doc, "priorities", sequence(yaml.FlowStyle, s.Priorities...))
	}
	if s.Archive != domain.NewBoard("", "T").Settings.Archive {
		m := mapping(yaml.FlowStyle)
		set(m, "mode", str(string(s.Archive.Mode)))
		set(m, "after_days", num(s.Archive.AfterDays))
		set(doc, "archive", m)
	}
	if s.Appearance != (domain.Appearance{}) {
		m := mapping(yaml.FlowStyle)
		for _, kv := range []struct{ k, v string }{
			{"theme", s.Appearance.Theme}, {"accent", s.Appearance.Accent}, {"density", s.Appearance.Density},
			{"swimlane", s.Appearance.Swimlane}, {"textures", s.Appearance.Textures},
		} {
			if kv.v != "" {
				set(m, kv.k, str(kv.v))
			}
		}
		set(doc, "appearance", m)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	// Encoding a node tree of strings, ints and bools cannot fail.
	_ = enc.Encode(doc)
	_ = enc.Close()
	return buf.String()
}
