# Waypoint

Waypoint is a local kanban tracker for any kind of project: a home renovation, a thesis, a product launch, a software release. Your whole board is one Markdown file that you can read, edit by hand, and keep in version control. A command line tool lets you, or an AI agent working for you, add cards, move them between columns, and see what to work on next. Cards can depend on each other, so Waypoint can tell you which ones are ready and which are still blocked.

This version is the command line and the Markdown board format. The web UI, SQLite storage and the MCP server are not in this version yet.

## Install

You need Go installed. From a checkout of this repository:

```
make build
```

This builds `bin/waypoint`. Copy it somewhere on your `PATH` to use it from anywhere.

## Quick start

Say you are renovating a house. In the folder for the project:

```
waypoint init "House renovation"
waypoint add "Measure every room"
waypoint add "Paint the hallway" --depends T-1
waypoint ls
```

`init` creates a `BOARD.md` in the current folder and registers it as a project. `ls` shows the cards:

```
T-1  Todo  Measure every room  [NEXT]
T-2  Todo  Paint the hallway  [BLOCKED by T-1]
```

Paint can wait until the measuring is done. Start the first card, then finish it:

```
waypoint move T-1 "In progress"
waypoint done T-1
waypoint ls
```

Now the hallway is no longer blocked. Run `waypoint --help` for every command. A few more that you will use often:

- `waypoint show T-2` shows one card with its notes.
- `waypoint set T-2 priority=P1` changes a field.
- `waypoint ls --next` shows the top card of the first active column.
- `waypoint archive` moves finished cards out of the way.

Every command accepts `--json` for scripts and `--project NAME` to act on a project other than the one in the current folder. Without either, Waypoint uses `$WAYPOINT_PROJECT` if it is set, then the project found from the current folder, and otherwise stops with an error rather than guess.

## The board file

`BOARD.md` is plain Markdown. The columns and settings sit in a short header, and each card is a heading with text under it. You can edit the file in any editor. A card heading without an id gets one the next time Waypoint reads the file. Run `waypoint help format` for the full description of the format.

While it writes, Waypoint keeps a small `BOARD.md.lock` file beside the board. It is safe to ignore and safe to delete when nothing is running; add `*.lock` to your `.gitignore` to keep it out of version control, or `BOARD.md.lock` if the repository tracks other lock files such as `Cargo.lock`.

## Configuration

Waypoint keeps a registry of your projects in a small config file. It is found in this order:

1. the path in `$WAYPOINT_CONFIG`,
2. `$XDG_CONFIG_HOME/waypoint/config.yaml`,
3. `~/.config/waypoint/config.yaml`.

The file is created the first time something is saved and is never needed just to read a board. Boards created with `waypoint project add` and no `--path` are stored under `~/.local/share/waypoint`.

## Working with AI agents

Run `waypoint agents` and paste the output into your project's `AGENTS.md` or `CLAUDE.md`. It tells an agent to use the CLI, for example `waypoint ls --next --json` to find the next task, and to follow the board format if it edits the file directly.

## What is coming

- A web UI for viewing and arranging the board in a browser.
- SQLite as a second storage option, with conversion between it and Markdown, and import and export.
- An MCP server, so agents can use the board as a tool.
