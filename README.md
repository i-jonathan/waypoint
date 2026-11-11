# Waypoint

Waypoint is a local kanban tracker for any kind of project: a home renovation, a thesis, a product launch, a software release. Your whole board is one file: a SQLite database by default, or a Markdown file that you can read, edit by hand, and keep in version control. A command line tool lets you, or an AI agent working for you, add cards, move them between columns, and see what to work on next. Cards can depend on each other, so Waypoint can tell you which ones are ready and which are still blocked.

Waypoint comes as one program with two faces: a command line for you and your agents, and a web UI that shows the same board in your browser and updates as the file changes.

## Install

Download the archive for your computer from the [latest release](https://github.com/i-jonathan/waypoint/releases/latest) (macOS, Linux or Windows; Intel or ARM), check it against `SHA256SUMS`, unpack it and copy `waypoint` (`waypoint.exe` on Windows) somewhere on your `PATH`. The Windows build is new and has had less use than the others. `waypoint version` says which version you have.

To build it yourself you need Go and Node installed. From a checkout of this repository:

```
make build
```

This builds `bin/waypoint`, with the web UI inside it. Copy it somewhere on your `PATH` to use it from anywhere. Maintainers cut a release with `make release VERSION=vX.Y.Z`, which writes the archives and `SHA256SUMS` to `release/`.

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

Each project is stored in one of three ways, and new projects use SQLite unless you say otherwise.

- **SQLite** (`waypoint.db`, or a `.db` file you choose): one file that stays fast with many cards. Edit it through Waypoint, the web UI or the CLI, not by hand.
- **Markdown** (`BOARD.md`, or a `.md` file you choose): a file you can read and edit in any editor, and that diffs well in git.
- **Postgres**: a database on a server, usually a Docker container on your own computer. See [Using a database server](#using-a-database-server).

Pick one with `--storage sqlite|markdown|postgres` on `waypoint init` and `waypoint project add`, or with the Storage choice in the New project dialog. Opening an existing file works out its kind from its content. Existing Markdown boards keep working as they are.

To change your mind later, run `waypoint project convert NAME --to sqlite` (or `--to markdown`, optionally with `--path FILE`, or `--to postgres` with the server options below), or use Convert in the project's settings under Storage. Any storage can be converted to any other. Waypoint writes the board and its archive to the new file, reads it back and checks that every card, id, order, body and setting matches, and only then points the project at it. The old files are renamed to `<name>.bak-YYYYMMDD` and never deleted, so a conversion can always be undone. If a card could not be stored exactly as written, the conversion stops and tells you which card, with nothing changed. A SQLite database that was converted away is marked as retired, so a program still pointed at it fails with a clear message instead of saving into a file the project has left. To go back to a SQLite backup, rename it into place, register it, and clear the mark with `sqlite3 FILE "PRAGMA user_version = 1"`.

## Using a database server

A project can live in a Postgres database instead of a file. Waypoint connects to a server that already exists; it does not install or run one. The usual case is a throwaway container on your own computer:

```sh
docker run -d --name waypoint-pg -e POSTGRES_PASSWORD=choose-a-password -p 127.0.0.1:5432:5432 postgres:17
```

Then set the password in the environment and add the project. `--create-database` makes the database if it is missing:

```sh
export WAYPOINT_DB_PASSWORD=choose-a-password          # bash and zsh
waypoint project add pharmacy --storage postgres --user postgres --create-database
# or, in the project's folder: waypoint init --storage postgres --user postgres --create-database
```

The other options are `--host` (default `localhost`), `--port` (5432), `--database` (default: the project name as a slug) and `--ssl disable|prefer|require|verify-full`. There is no `--password`: **the password is never stored by Waypoint** and never goes on a command line. It comes only from an environment variable, one of two:

- `WAYPOINT_DB_PASSWORD_<NAME>`, for one project. `<NAME>` is the project name in capitals with every character other than A-Z and 0-9 turned into `_`, so `pharmacy` uses `WAYPOINT_DB_PASSWORD_PHARMACY` and `my-app` uses `WAYPOINT_DB_PASSWORD_MY_APP`.
- `WAYPOINT_DB_PASSWORD`, for every server project that has no variable of its own. The per-project variable wins when both are set.

If neither is set, Waypoint connects without a password, which a local Docker server may allow. If the server asks for one, the message names both variables. In the shell that runs the command it looks like `export WAYPOINT_DB_PASSWORD=...` (bash, zsh), `set -x WAYPOINT_DB_PASSWORD ...` (fish) or `$env:WAYPOINT_DB_PASSWORD = '...'` (PowerShell). The variable has to be set wherever Waypoint runs: in the shell, for `waypoint serve`, and in an agent's MCP config:

```json
{"mcpServers":{"waypoint":{"command":"waypoint","args":["mcp","--project","pharmacy"],"env":{"WAYPOINT_DB_PASSWORD":"choose-a-password"}}}}
```

The registry keeps only the host, port, database, user and TLS mode. A project on a server has no folder, so commands find it by name: `--project pharmacy` or `WAYPOINT_PROJECT=pharmacy`. `waypoint project ls` shows `host:port/database`, and `waypoint project rm` leaves the database as it is.

**One board per database.** Waypoint uses the `public` schema of a Postgres database and it only makes a new board in a database that holds nothing at all. Several projects need several databases. Adding a project to a database that already holds a Waypoint board opens that board as it is.

**One person's data.** Waypoint is designed for one person, not for sharing a board between people. When the server is not on this computer (anything but `localhost`, `127.0.0.0/8`, `::1` or a Unix socket) Waypoint says so once, when you set the project up. TLS is on by default for such a server (`require`: encrypted, certificate not checked). `require` stops eavesdroppers but not an active attacker who poses as the server (Waypoint only ever answers a server with a password exchange that does not reveal the password); for a server across a network you do not fully trust use `--ssl verify-full`, which checks the certificate. Use `--ssl disable` only if you accept that the password and the board cross the network unencrypted.

**Converting.** `waypoint project convert NAME --to postgres --user U --database D` (and the Convert button in project settings) copies the board into an empty database, reads it back, checks it matches, and only then switches the project. Converting away from a server leaves the old database exactly as it is, with every row, and marks it retired so nothing writes to it by mistake; Waypoint never drops or empties a database, and a database it created for a conversion that then failed stays behind, empty. A retired database is never a target. To use one again, clear the mark: `UPDATE waypoint_meta SET value = 'false' WHERE key = 'retired'` . If the mark could not be set after the switch, the conversion still counts as done: the message names the old database and the statement to run (`UPDATE waypoint_meta SET value = 'true' ...`).

**One password per conversion.** A conversion reads the old board and writes the new one with the same password variable, so a source and a target that need different passwords cannot be converted directly. Export the project (`waypoint export`), add the target project with its own variable, and import.

**The web UI** follows a server project by checking its revision every couple of seconds. If the server is down it says so once and keeps trying.

**Postgres does not store a NUL character** in text; a card that contains one is refused, naming the card.

To run the whole test suite against real servers, `make test-servers` starts Postgres in Docker, runs the tests and removes the containers. Without a server those tests are skipped.

## The Markdown board file

A Markdown board, such as `BOARD.md`, is plain Markdown. The columns and settings sit in a short header, and each card is a heading with text under it. You can edit the file in any editor. A card heading without an id gets one the next time Waypoint reads the file. Dates are stored as UTC timestamps such as `2026-11-04T23:52:10Z` and shown in your local time; a plain date such as `2026-10-26` also works and stays as written. Run `waypoint help format` for the full description of the format.

While it writes, Waypoint keeps a small `.lock` file beside the board, for example `BOARD.md.lock`. It is safe to ignore and safe to delete when nothing is running; add `*.lock` to your `.gitignore` to keep it out of version control, or `BOARD.md.lock` if the repository tracks other lock files such as `Cargo.lock`.

## Export and import

`waypoint export` writes the whole project (settings, cards and archive) as JSON, to the screen or with `--out FILE`. It never overwrites a file that exists. `waypoint import FILE.json` puts that export into a project, which must be empty unless you add `--replace`. The file is checked before anything is changed: an unknown key, a newer format, a card in a column that does not exist, a repeated id or a bad date all stop the import with a message that names the card. Use it for backups, for moving a project between machines, or to hand a board to a script.

### CSV and Markdown

`waypoint export --format csv` writes one row per card, archived cards included (with `column` set to `Archived`), with the columns `id,title,column,group,priority,depends,created,done,body` and then one column for each extra field. `waypoint import FILE.csv` reads the same shape, including files saved from a spreadsheet (a byte order mark, Windows line endings, and short rows are fine). An empty `id` makes a new card; an `id` the project already has, live or archived, updates that card, and an archived card stays archived. Rows keep their file order within each column. A column or group the project does not have stops the import and is listed, unless you add `--create`. Import is all or nothing: any bad row stops it with its line number and the project is not touched. `waypoint export --format md` writes the project in the Markdown board format with the archive after it; it is a copy to read, not something to import. Without `--format`, the file extension of `--out` or of the import file chooses.

The web UI does the same from a project's settings, under Import and export: three download links, and an Import a file button. Before anything is changed, the import dialog checks the file and tells you what it would do: how many cards it adds or updates and which columns and groups it would create. Nothing happens until you confirm, and a file with a mistake is refused with the line to fix.

## Configuration

Waypoint keeps a registry of your projects in a small config file. It is found in this order:

1. the path in `$WAYPOINT_CONFIG`,
2. `$XDG_CONFIG_HOME/waypoint/config.yaml`,
3. `~/.config/waypoint/config.yaml`.

The keys are `port` (default 4747), `open_browser` (default true), `tls` (`auto`, `on` or `off`), `data_dir`, `appearance` and `projects`. Waypoint rewrites this file when you change a setting or register a project, whether from the command line or the UI, so comments in it are not kept. If the file is a symlink, the link stays and the file it points to is updated.

The file is created the first time something is saved and is never needed just to read a board. Boards created with `waypoint project add` and no `--path` are stored under `~/.local/share/waypoint`.

## Working with AI agents

Run `waypoint agents` and paste the output into your project's `AGENTS.md` or `CLAUDE.md`. It tells an agent to use the CLI, for example `waypoint ls --next --json` to find the next task, and to follow the board format if it edits the file directly.

## Running in Docker

To keep the web UI running in the background instead of in a terminal, build and start the image with Compose:

```sh
docker compose up -d
```

Then open http://localhost:4747. `docker compose down` stops it, and it restarts by itself after a reboot.

- Your config and boards are mounted at the same paths as on your computer (`~/.config/waypoint` and `~/.local/share/waypoint`), because the registry stores absolute paths. Boards kept elsewhere need their own `volumes` line at the same path.
- The port is published on `127.0.0.1` only, so only this computer can reach it. The host port must equal the container port (4747): Waypoint checks the Host header against it, and a different host port gets a 421. To change it, set `port` in the config.
- The image listens on all addresses inside the container (`WAYPOINT_BIND=0.0.0.0`). Outside Docker `waypoint serve` always listens on 127.0.0.1 unless you set that variable yourself.
- If the `waypoint` command on your computer and the container write the same SQLite board at once, Docker Desktop's file sharing can be slow to show changes; the web page may lag a few seconds.
- `waypoint mcp` is not served by the container yet. Agents still start it as a command.

## MCP server

For agents that speak the Model Context Protocol and have no shell, `waypoint mcp [--project NAME]` serves a board over standard input and output. It picks its project the same way the CLI does (`--project`, `$WAYPOINT_PROJECT`, then the nearest registered board), so start it from the project's folder. Add it to the agent's MCP config:

```json
{"mcpServers":{"waypoint":{"command":"waypoint","args":["mcp"]}}}
```

The tools are `list_cards`, `get_card`, `create_card`, `move_card`, `update_card`, `next_up` and `list_blocked`. Each returns the same JSON as the matching `--json` command. A refused move, a WIP limit, an unknown card or column, or an `if_match` that no longer matches comes back as a tool error with the reason (and, for `if_match`, the card as it is now), so the agent can adjust. Where a shell is available, the CLI is simpler and does more.

## License

Waypoint is released under the [Apache License 2.0](LICENSE).
