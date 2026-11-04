package markdown

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"waypoint/internal/domain"
)

// formatVersion is the only front matter version this parser reads.
const formatVersion = 1

// frontMatter is the YAML shape of the board settings (spec section 5.1).
// Pointer and nil-able fields tell "absent" (take the NewBoard default) from
// an explicit empty value.
type frontMatter struct {
	Waypoint   any          `yaml:"waypoint"`
	Name       string       `yaml:"name"`
	Prefix     *string      `yaml:"prefix"`
	NextID     *int         `yaml:"next_id"`
	Columns    []fmColumn   `yaml:"columns"`
	Groups     []fmGroup    `yaml:"groups"`
	Priorities []string     `yaml:"priorities"`
	Archive    *fmArchive   `yaml:"archive"`
	Appearance fmAppearance `yaml:"appearance"`
}

type fmColumn struct {
	Name    string   `yaml:"name"`
	WIP     int      `yaml:"wip"`
	MovesTo []string `yaml:"moves_to"`
	Done    bool     `yaml:"done"`
}

type fmGroup struct {
	Name  string `yaml:"name"`
	Color string `yaml:"color"`
}

type fmArchive struct {
	Mode      string `yaml:"mode"`
	AfterDays *int   `yaml:"after_days"`
}

type fmAppearance struct {
	Theme    string `yaml:"theme"`
	Accent   string `yaml:"accent"`
	Density  string `yaml:"density"`
	Swimlane string `yaml:"swimlane"`
	Textures string `yaml:"textures"`
}

// splitFrontMatter returns the YAML text between a first line "---" and the
// next "---" line, and the index of the closing line. A first line that is not
// "---" gives ErrNotABoard.
func splitFrontMatter(lines []string) (yamlText string, closing int, err error) {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", 0, ErrNotABoard
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n"), i, nil
		}
	}
	return "", 0, &ParseError{Line: 1, Msg: "front matter is not closed (missing second ---)"}
}

// yamlLineRE finds the line number in a yaml.v3 error message.
var yamlLineRE = regexp.MustCompile(`line (\d+): (.*)`)

// yamlError turns a yaml.v3 error into a ParseError positioned in the file.
// The YAML text starts on file line 2.
func yamlError(err error) *ParseError {
	msg := err.Error()
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		msg = te.Errors[0]
	}
	msg = strings.TrimPrefix(msg, "yaml: ")
	line := 1
	if m := yamlLineRE.FindStringSubmatch(msg); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n + 1
			msg = m[2]
		}
	}
	return &ParseError{Line: line, Msg: "invalid front matter: " + msg}
}

// settingsFromYAML decodes front matter into board settings on top of the
// NewBoard defaults and validates them. Unknown keys are an error, so a typo
// such as "colums" is not silently dropped.
func settingsFromYAML(yamlText string) (domain.Settings, error) {
	// Other tools use front matter too. Only a file with a waypoint key is a
	// board, and only a board is held to the strict key check below.
	var probe struct {
		Waypoint any `yaml:"waypoint"`
	}
	if err := yaml.Unmarshal([]byte(yamlText), &probe); err != nil {
		return domain.Settings{}, yamlError(err)
	}
	if probe.Waypoint == nil {
		return domain.Settings{}, ErrNotABoard
	}
	var fm frontMatter
	dec := yaml.NewDecoder(strings.NewReader(yamlText))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil && !errors.Is(err, io.EOF) {
		return domain.Settings{}, yamlError(err)
	}
	if v, ok := fm.Waypoint.(int); !ok || v != formatVersion {
		return domain.Settings{}, &ParseError{Line: keyLine(yamlText, "waypoint"), Msg: fmt.Sprintf("unsupported format version %v", fm.Waypoint)}
	}

	prefix := "T"
	if fm.Prefix != nil {
		prefix = *fm.Prefix
	}
	s := domain.NewBoard(fm.Name, prefix).Settings
	if fm.NextID != nil {
		s.NextID = *fm.NextID
	}
	if fm.Columns != nil {
		s.Columns = make([]domain.Column, len(fm.Columns))
		for i, c := range fm.Columns {
			s.Columns[i] = domain.Column{Name: c.Name, WIP: c.WIP, MovesTo: c.MovesTo, Done: c.Done}
		}
	}
	if fm.Groups != nil {
		s.Groups = make([]domain.Group, len(fm.Groups))
		for i, g := range fm.Groups {
			s.Groups[i] = domain.Group{Name: g.Name, Color: g.Color}
		}
	}
	if fm.Priorities != nil {
		s.Priorities = fm.Priorities
	}
	if fm.Archive != nil {
		if fm.Archive.Mode != "" {
			s.Archive.Mode = domain.ArchiveMode(fm.Archive.Mode)
		}
		if fm.Archive.AfterDays != nil {
			s.Archive.AfterDays = *fm.Archive.AfterDays
		}
	}
	s.Appearance = domain.Appearance(fm.Appearance)

	if err := s.Validate(); err != nil {
		return domain.Settings{}, &ParseError{Line: 1, Msg: err.Error()}
	}
	return s, nil
}

// keyLine returns the file line of a top-level YAML key, or 2 (the first YAML
// line) when it cannot be found.
func keyLine(yamlText, key string) int {
	for i, l := range strings.Split(yamlText, "\n") {
		if strings.HasPrefix(l, key+":") {
			return i + 2
		}
	}
	return 2
}
