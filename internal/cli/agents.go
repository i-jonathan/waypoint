package cli

import (
	"fmt"

	"waypoint/docs"
)

const (
	usageAgents = "waypoint agents"
	usageHelp   = "waypoint help [format]"
)

// agentsSnippet is the text `waypoint agents` prints, for pasting into a
// project's AGENTS.md or CLAUDE.md.
const agentsSnippet = `## Task board

This project's board is managed by Waypoint. Prefer the CLI over editing the board file.

- See what to work on next: ` + "`waypoint ls --next --json`" + `
- Add a card: ` + "`waypoint add \"Title\" --group \"Group\" --priority P1`" + `
- Move a card: ` + "`waypoint move <ID> \"<Column>\"`" + `
- Change a field: ` + "`waypoint set <ID> key=value`" + `
- Finish a card: ` + "`waypoint done <ID>`" + `

If you edit the board file directly, follow the format printed by "waypoint help format"; a card heading without an id gets one automatically.
`

// cmdAgents prints the AGENTS.md snippet. It needs no project and no config.
func (r *runner) cmdAgents(args []string) int {
	pos, code, done := r.parse(usageAgents, args, nil)
	if done {
		return code
	}
	if len(pos) > 0 {
		return r.usageError("agents takes no arguments", usageAgents)
	}
	fmt.Fprint(r.env.Stdout, agentsSnippet)
	return exitOK
}

// cmdHelp prints the usage listing, or with the topic "format" the board file
// format guide. It needs no project and no config.
func (r *runner) cmdHelp(args []string) int {
	switch len(args) {
	case 0:
		fmt.Fprint(r.env.Stdout, usageText)
		return exitOK
	case 1:
		if args[0] == "format" {
			fmt.Fprint(r.env.Stdout, docs.BoardFormat)
			return exitOK
		}
		return r.usageError(fmt.Sprintf("unknown help topic %q", args[0]), usageHelp)
	}
	return r.usageError("help takes at most one topic", usageHelp)
}
