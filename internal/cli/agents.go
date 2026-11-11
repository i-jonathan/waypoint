package cli

import (
	"fmt"

	"waypoint/docs"
)

const (
	usageAgents = "waypoint agents"
	usageHelp   = "waypoint help [format] [--json] [--project NAME]"
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
- ` + "`waypoint ls --json`" + ` and ` + "`waypoint show <ID> --json`" + ` for reading all cards and one card.

If the board is a Markdown file such as BOARD.md and you edit it directly, follow the format printed by "waypoint help format"; a card heading without an id gets one automatically. Never edit a SQLite board (a .db file) directly: use the CLI.

Run waypoint from inside this project's folder, or pass --project NAME. "waypoint project ls" shows where the board is: a file's path, or host:port/database for a database server.

If the board is in a Postgres or MySQL database, its password is not stored anywhere: it must be in the environment of every program that runs waypoint, as WAYPOINT_DB_PASSWORD_<NAME> (this project; NAME is the project name in capitals, other characters as _) or WAYPOINT_DB_PASSWORD (all projects). For an MCP agent, put it in the server's "env" in the MCP config, for example {"mcpServers":{"waypoint":{"command":"waypoint","args":["mcp","--project","NAME"],"env":{"WAYPOINT_DB_PASSWORD":"..."}}}}.

Groups, priorities and columns must already exist on the board. On a Markdown board, read them from the file's front matter before using them.

If your agent speaks MCP and has no shell, run "waypoint mcp" as an MCP server instead; it offers the same board through seven tools. Add it to the agent's MCP config:

` + "```json" + `
{"mcpServers":{"waypoint":{"command":"waypoint","args":["mcp"]}}}
` + "```" + `
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
	pos, code, done := r.parse(usageHelp, args, nil)
	if done {
		return code
	}
	switch len(pos) {
	case 0:
		fmt.Fprint(r.env.Stdout, usageText)
		return exitOK
	case 1:
		if pos[0] == "format" {
			fmt.Fprint(r.env.Stdout, docs.BoardFormat)
			return exitOK
		}
		return r.usageError(fmt.Sprintf("unknown help topic %q", pos[0]), usageHelp)
	}
	return r.usageError("help takes at most one topic", usageHelp)
}
