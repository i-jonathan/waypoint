# Waypoint

Waypoint is a local kanban tracker for any kind of project: a home renovation, a thesis, a product launch, a software release. Your whole board is one file: a SQLite database by default, or a Markdown file that you can read, edit by hand, and keep in version control. A command line tool lets you, or an AI agent working for you, add cards, move them between columns, and see what to work on next. Cards can depend on each other, so Waypoint can tell you which ones are ready and which are still blocked.

Waypoint comes as one program with two faces: a command line for you and your agents, and a web UI that shows the same board in your browser and updates as the file changes. SQLite storage and the MCP server are not in this version yet.

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

`init` creates a `waypoint.db` in the current folder and registers it as a project. (`waypoint init --storage markdown` makes a `BOARD.md` instead; see [Choosing a storage](#choosing-a-storage).) `ls` shows the cards:

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

Every command accepts `--json` for scripts and `--project NAME` to act on a project other than the one in the current folder. Without either, Waypoint uses `$WAYPOINT_PROJECT` if it is set, then the project whose board is in the current folder or a folder above it, and otherwise stops with an error rather than guess. A Waypoint board named `waypoint.db` or `BOARD.md` in the current folder that is not registered is an error, so run `waypoint init` there first (add `--storage markdown` for a `BOARD.md`). Any other file or folder with those names is ignored.

## The web UI

Run `waypoint` with no arguments (or `waypoint serve`) and the UI opens in your browser. It lists every registered project in the sidebar, shows the board with drag and drop, a filter, swimlanes, a card panel for editing, "Next up", "Blocked" and "Archive" views, and settings. It follows the files: if you or an agent change the board while the page is open, the board updates within a second or two with no reload.

```
waypoint serve [--port N] [--no-open]
```

- `--port N` sets the port for this run. Otherwise the `port` in the config is used, and that defaults to 4747. Ports 80 and 443 are refused.
- `--no-open` keeps Waypoint from opening a browser. The `open_browser` key in the config turns the opening off for good.

Waypoint listens on `127.0.0.1` only, so other machines cannot reach it. It also answers only to the host names `localhost` and `127.0.0.1`, and a change made through the API must carry a header that a web page on another site cannot send. Press Ctrl+C in the terminal to stop it.

### A project is a registry entry

A project is an entry in the config file that points to a board file. It is not a folder and it does not need a git repository. The file can be called anything and live anywhere. `waypoint project rm` drops the entry and keeps the file. In the UI, "New project" can either create a new board file or open an existing one, which registers it.

### HTTPS, HTTP/2 and `waypoint trust`

The `tls` key in the config sets how the server is reached:

| Mode | What happens |
| --- | --- |
| `auto` (the default) | HTTPS if you have run `waypoint trust`, otherwise plain HTTP. |
| `on` | Always HTTPS. Your browser warns until you have run `waypoint trust`. |
| `off` | Always plain HTTP. |

Browsers only use HTTP/2 over HTTPS, so "waypoint trust" installs a local certificate authority that only this machine trusts. The UI works without it, over plain HTTP, and `waypoint serve` says so once with a tip. HTTP/2 mostly helps with many small requests at once, so on a small board you will not notice a difference.

Things to know about `waypoint trust`:

- On macOS it adds the certificate authority to your login keychain, and macOS asks for your password to allow that. Waypoint never sees the password.
- The certificate only works for `localhost` and `127.0.0.1`. It cannot vouch for any other site, even if its key were stolen.
- The files live in the `tls` folder inside the data directory (`~/.local/share/waypoint` by default). Delete that folder to start over, and run `waypoint trust` again.
- On Linux it prints the commands to run by hand, because they differ between systems. Run them, then create the marker file the last line of the output names.

Restart `waypoint serve` after trusting, so that it starts serving HTTPS.

### Keyboard

You can do everything on the board without a mouse. The short version: arrows move between cards, Shift with an arrow moves the card, `m` opens a move menu, Enter opens the card, `n` adds a card and `/` goes to the filter. The full map is in [docs/keyboard.md](docs/keyboard.md).

### Settings

Appearance (theme, accent colour, density, swimlanes, textures) can be set in three places. For each field, the first of these that has a value wins:

1. the project's own override, in the `appearance` key of the board file's header (or in the project's settings page),
2. the global setting, in the `appearance` key of the config file (or in the UI's settings page),
3. the built-in default: theme `system`, accent `orange`, density `comfortable`, swimlanes `none`, textures `on`.

A field left empty, or with a value Waypoint does not know, falls through to the next level. In a project's settings, a field you have not overridden says whether it comes from the app settings or is built in.

## Choosing a storage

Each project is stored in one of two ways, and new projects use SQLite unless you say otherwise.

- **SQLite** (`waypoint.db`, or a `.db` file you choose): one file that stays fast with many cards. Edit it through Waypoint, the web UI or the CLI, not by hand.
- **Markdown** (`BOARD.md`, or a `.md` file you choose): a file you can read and edit in any editor, and that diffs well in git.

Pick one with `--storage sqlite|markdown` on `waypoint init` and `waypoint project add`, or with the Storage choice in the New project dialog. Opening an existing file works out its kind from its content. Existing Markdown boards keep working as they are.

To change your mind later, run `waypoint project convert NAME --to sqlite` (or `--to markdown`, optionally with `--path FILE`). Waypoint writes the board and its archive to the new file, reads it back and checks that every card, id, order, body and setting matches, and only then points the project at it. The old files are renamed to `<name>.bak-YYYYMMDD` and never deleted, so a conversion can always be undone. If a card could not be stored exactly as written, the conversion stops and tells you which card, with nothing changed.

## The Markdown board file

A Markdown board, such as `BOARD.md`, is plain Markdown. The columns and settings sit in a short header, and each card is a heading with text under it. You can edit the file in any editor. A card heading without an id gets one the next time Waypoint reads the file. Dates are stored as UTC timestamps such as `2026-11-04T23:52:10Z` and shown in your local time; a plain date such as `2026-10-26` also works and stays as written. Run `waypoint help format` for the full description of the format.

While it writes, Waypoint keeps a small `.lock` file beside the board, for example `BOARD.md.lock`. It is safe to ignore and safe to delete when nothing is running; add `*.lock` to your `.gitignore` to keep it out of version control, or `BOARD.md.lock` if the repository tracks other lock files such as `Cargo.lock`.

## Configuration

Waypoint keeps a registry of your projects in a small config file. It is found in this order:

1. the path in `$WAYPOINT_CONFIG`,
2. `$XDG_CONFIG_HOME/waypoint/config.yaml`,
3. `~/.config/waypoint/config.yaml`.

The keys are `port` (default 4747), `open_browser` (default true), `tls` (`auto`, `on` or `off`), `data_dir`, `appearance` and `projects`. Waypoint rewrites this file when you change a setting or register a project, whether from the command line or the UI, so comments in it are not kept. If the file is a symlink, the link stays and the file it points to is updated.

The file is created the first time something is saved and is never needed just to read a board. Boards created with `waypoint project add` and no `--path` are stored under `~/.local/share/waypoint`.

## Working with AI agents

Run `waypoint agents` and paste the output into your project's `AGENTS.md` or `CLAUDE.md`. It tells an agent to use the CLI, for example `waypoint ls --next --json` to find the next task, and to follow the board format if it edits the file directly.

## What is coming

- Converting a project from the web UI, and import and export.
- An MCP server, so agents can use the board as a tool.
