# Waypoint: Postgres and MySQL storage (Plan 4 design)

Date: 2026-11-11. Builds on `2026-11-03-waypoint-design.md` and Plan 3 (SQLite, convert, import/export, MCP).

## Intent

Today a project is stored in a Markdown file or a SQLite file. The owner trusts Postgres more for larger projects and wants its reliability, usually from a Docker container on the same laptop. This adds Postgres and MySQL as two more storage choices per project, next to the two that exist. Everything else about Waypoint (CLI, web UI, MCP, import and export, agents) keeps working the same on all of them.

## Goals

- A project can be stored in a Postgres or MySQL database. Setup asks for engine, host, port, username and database, plus a "create the database if it does not exist" choice.
- The password is never stored by Waypoint. It comes from an environment variable, and the error names the variable when it is missing or wrong.
- Conversion works between any two of Markdown, SQLite, Postgres and MySQL, in both directions, with the checks Plan 3 already does (write the copy, read it back, compare every card, id, order, body and setting, only then switch the project).
- The web UI updates when the board changes in a server database, as it does for files.
- A non-local server gets a one-time warning at setup: Waypoint is designed for one person's data, not for several people sharing a board. Using it anyway is allowed.

## Non-goals

- Several people editing one board at once, accounts, permissions, or conflict handling beyond what exists.
- Several boards in one database (see Decisions: one board per database).
- SSH tunnels, cloud IAM login, a keychain or any password store, a proxy.
- Running or installing the database. Waypoint connects to one that exists.
- Changing the board model or the Markdown format.

## Anti-goals (a failure even if it works)

1. **Lost data.** Any card, id, order, body, date or setting that is lost or altered by saving, loading or converting. A failed or half-finished operation must leave the original intact.
2. **A leaked password.** It must not appear in the registry file, logs, error messages, `--json` output, API responses, command lines or the web UI.
3. **An annoying setup.** Few steps, clear messages, no repeated warnings, no jargon.

## Constraints

- Existing config files, Markdown boards and SQLite boards keep working with no migration.
- Use `app.Repository` (Load, Save, Lock, Path) unchanged where possible. Board-to-rows logic stays in `internal/storage/sqlstore`, which already takes a `Dialect` (currently only `Rebind`) and leaves transactions to the caller. Each engine gets its own package beside `internal/storage/sqlite`.
- One person, usually one laptop, but also not only macOS: nothing may depend on a platform keychain.
- Go standard `database/sql`. Drivers: `github.com/jackc/pgx/v5/stdlib` for Postgres and `github.com/go-sql-driver/mysql` for MySQL (builder's call if there is a strong reason to differ; both are pure Go).
- The repo's checks stay green without Docker: `go test ./...` must pass on a machine with no database server.

## Decisions (the owner's, as they decided them)

- Both Postgres and MySQL are supported. Postgres first.
- Connection is the standard host, port, username, password, database. A checkbox creates the database.
- Credentials: ask for the non-secret parts; tell the user to set environment variables for the secret part.
- A warning for a non-local server, saying it is not designed for multiple people. Using it is still allowed.
- Conversion between any two storages works the same way.

## Design

### Registry entry

`config.ProjectRef` gains server fields and keeps `Path` for files:

```yaml
projects:
  - name: pharmacy
    storage: postgres        # markdown | sqlite | postgres | mysql
    host: localhost
    port: 5432
    database: waypoint_pharmacy
    user: waypoint
    ssl: prefer              # engine-specific values, see Security
```

`Path` is empty for a server project. Code that assumes every project has a file path must be found and fixed (watching, the unregistered-board check, `project ls`, `project rm`'s "keeps the file" wording, export defaults, the agents snippet, the web UI's path display). The config is decoded strictly (`KnownFields(true)`), so an older Waypoint refuses a config that holds a server project, with its normal config-file error. That is acceptable; no version bump is needed.

### The password

- Read from `WAYPOINT_DB_PASSWORD_<NAME>`, where `<NAME>` is the project name upper-cased with every character other than A-Z and 0-9 turned into `_`. If two project names would give the same variable, `project add` refuses the second and says why.
- *My call:* if the variable is unset, Waypoint tries to connect without a password, since a local Docker server may allow it. If the server asks for one, the error says `set WAYPOINT_DB_PASSWORD_PHARMACY` (the exact name). It never prompts.
- *My call:* a fallback `WAYPOINT_DB_PASSWORD` (no project name) is used when the per-project variable is unset, for people with one server. Documented once.
- The CLI has no `--password` flag, the API takes no password, and the web form shows the variable name instead of asking. The registry never holds it.
- The variable must be set wherever Waypoint runs: the shell, `waypoint serve`, and an agent's MCP config (`env` in the config JSON). The README says so and the MCP example shows it.
- Every error from a driver is wrapped so that a connection string or password cannot appear in it, in logs, or in an API response. A test sets a distinctive password and asserts it is absent from every output of every failure path.

### Non-local warning

A server is **local** when the host is `localhost`, `127.0.0.0/8`, `::1`, or a Unix socket path. Anything else is non-local, including a Docker host reached by its LAN address.

- Shown once, at `init`, `project add` and `project convert` (CLI: printed before connecting; web: a notice in the dialog that the user must tick or click past). It says that Waypoint is designed for one person's data, not for sharing a board between people, and that the connection should be to a server they control.
- Not shown on later commands, not stored, not repeated by `serve`.
- Non-local also changes the default for encryption (see Security).

### Schema, version and the retire mark

- Same five tables as SQLite (`project`, `board_columns`, `board_groups`, `cards`, `card_fields`) plus a small `waypoint_meta` table holding the schema version and a `retired` flag. This replaces `PRAGMA user_version`.
- The revision counter in the `project` row is kept (it drives `ErrChangedDuringWrite` and the watcher).
- Opening checks the schema version: refuse a newer one, refuse a database with no Waypoint tables or foreign tables, refuse a retired one with a message saying how to undo it (clear the flag with SQL, shown in the message).
- Migrations run on create, as in Plan 3. A newer version is refused.

### Locking and writes

- `Lock` opens a transaction and takes a row lock on the `project` row (`SELECT ... FOR UPDATE`), re-checks the retired flag after acquiring it (exactly the Plan 3 rule, so a writer waiting during a conversion fails loudly), and honours the caller's context. Without a caller deadline it gives up after the same 30 seconds as SQLite and returns `ErrBusy` (`busy`, HTTP 503).
- Writes inside the lock use the same SAVEPOINT approach; the revision check gives `ErrChangedDuringWrite`.
- Postgres sets `lock_timeout`; MySQL sets `innodb_lock_wait_timeout`, both below the 30 second cap, so waiting never hangs on a stuck server.
- Postgres DDL is transactional; MySQL DDL is not. Creating the schema on MySQL can leave half a schema on failure. Creation therefore cleans up the tables it created in that run on failure, and the "target must be empty" rule means cleanup can never touch someone else's tables.
- Repository lifetime: connecting to a server costs more than opening a file. Builder's call whether a `Repo` keeps a small pool per process (cached by project, discarded when the registry entry changes) or connects per call. Whichever is chosen, the process must exit cleanly and not leak connections, and a server restart mid-run must recover on the next call.

### Dialect differences the builder must cover

- Placeholders: `$1` for Postgres, `?` for MySQL (extend `Dialect.Rebind`; the shared statements stay written with `?`).
- Text keys and sorting: MySQL's default collation is case-insensitive and ignores trailing spaces, which would merge column names or field keys that differ only in case. Identifier-like columns use a binary collation (`utf8mb4_bin`) and MySQL uses `utf8mb4` throughout, so emoji and any text survive. Plan 3 allows case-only duplicate columns in SQLite, so this must match. Key columns need bounded `VARCHAR` length on MySQL; bodies are `TEXT`/`LONGTEXT` (MySQL `TEXT` is 64 KB, too small).
- Postgres `TEXT` cannot hold a NUL (`\x00`) character. A card containing one must fail conversion, naming the card, with nothing changed, and must be refused on save with a clear message. (Markdown cannot hold it faithfully either, so this is consistent.)
- Dates stay stored the way Plan 3 stores them (as text through `domain.StoredDate`); do not switch to native timestamp types, so values compare equal after a round trip.
- Integer width: card numbers are capped at `domain.MaxCardNumber` (2^31-1); use a column type that holds it on both engines.
- Both engines must return the same ordering for the same board. Positions are stored explicitly, never inferred from insertion order.

### Creating the database

The "create the database" choice connects to the maintenance database (`postgres` for Postgres; no database for MySQL), runs `CREATE DATABASE` with the right encoding (UTF-8), then connects to it. If the account lacks the privilege, the error says so and suggests creating the database by hand, without changing anything. An existing database is used as it is (and must be empty for a new board or a conversion target). Database names are validated and quoted properly; they are never pasted into SQL unescaped.

### Conversion

`projects.Convert` is generalised from "file to file" to "any storage to any storage". The steps and safety rules from Plan 3 stay: lock the source, load, write the copy, verify it equals the original, switch the registry, then retire the source. What changes:

- **Target a server:** must be empty (no tables at all, *my call*; the simplest rule that can never overwrite or mix with other data). Not empty is a refusal that names what was found. The copy is written, read back and compared before the registry changes. On any failure after tables were created, the tables created by this run are dropped and the project is unchanged.
- **Source a server:** nothing is renamed or deleted. The source database is marked retired (the flag above) after the registry has switched, and left in place. Waypoint never drops a database or its data.
- **Source a file:** unchanged: files are renamed to `.bak-DATE`.
- A crash at any point leaves either the old registry pointing at a working source, or the new one pointing at a verified copy (the same order as Plan 3). Writers that wait for the source lock while it is converted fail loudly rather than saving into a retired board.
- CLI: `waypoint project convert NAME --to postgres|mysql|sqlite|markdown`, with `--host --port --user --database --create-database` for servers and `--path` for files. Web: Convert in project settings, with the same fields.

### Live updates

The watcher today fingerprints files. A server project has no file, so `watch.Target` gains a kind. For a server project the watcher polls the revision number (a one-row query) about every 2 seconds and publishes when it changes. On a connection failure it backs off, logs once per outage (never the password), and recovers by itself. A missing password variable is logged once with the variable's name, not every poll.

### CLI and web setup

- `waypoint init` and `waypoint project add` gain `--storage postgres|mysql` with `--host`, `--port`, `--user`, `--database`, `--create-database`, and `--ssl`. Defaults: host `localhost`, port 5432 or 3306, `--user` required, `--database` defaults to a name made from the project name (builder's call).
- The setup command connects first (a real test), prints the warning if non-local, then registers. It never registers a project it could not connect to or could not initialise.
- Web: the New project dialog gains the server options, showing the exact password variable to set and a "Test connection" step. Convert in settings gets the same. The API takes the same fields without a password.
- `waypoint project ls` shows `host/database` for a server project instead of a path.

### Security

- Encryption: for a local server the default is whatever the driver does when the server offers TLS (Postgres `prefer`; MySQL `preferred`). For a non-local server the default is to require TLS. A user can set it lower explicitly (`--ssl disable`), and then the setup says plainly that the password would cross the network unencrypted. Certificate verification choices are exposed as the engines' standard values; keep the list short.
- The password is only ever put in memory from the environment and passed to the driver as a structured config value, not built into a string that could be logged.
- No SQL is built from user text by concatenation except identifiers that are validated and quoted (database name).

## Testing

- The shared `storagetest` contract (Load, Save, Lock, conflicts, round trips) runs against every engine, including Postgres and MySQL.
- The Plan 3 hard tests are repeated for servers: writers during a conversion (none lost), a crash at each conversion step, the lock cap (hangs if the cap is removed), and case-sensitive column and field names on MySQL.
- A password-leak test per the Password section.
- Server tests need a server. A `make test-servers` target starts Postgres and MySQL in Docker, sets `WAYPOINT_TEST_POSTGRES` and `WAYPOINT_TEST_MYSQL` (connection details, password via the same variables), runs the tests and stops the containers. Without those variables the server tests skip, so `go test ./...` still passes everywhere. The final review must run them for real.
- Web: Vitest for the new dialog fields and the warning; Playwright e2e for setting up a Postgres project if a server is available (skipped otherwise).

## Milestones (suggested order; each small enough to review by hand)

1. Registry fields, the password helper, error scrubbing, the local/non-local check (no database yet).
2. Postgres repository: schema, Load/Save/Lock, contract tests (needs Docker).
3. Setup from the CLI: `init` / `project add` for Postgres, connection test, create-database, warning.
4. Conversion to and from Postgres, with the hard tests.
5. Watcher polling and the web UI setup and convert.
6. MySQL: dialect, collation, schema, contract tests.
7. MySQL setup and conversion (reuses 3 and 4).
8. README, `docs/PROGRESS.md`, agents snippet and MCP env example; final whole-branch review including a password-leak audit.

Reviews follow the existing risk tiers: own review for locking, conversion and password handling; a midpoint and a final binary-level review.

## Calls made after the builder check (mine, not the owner's)

- **Schema and emptiness.** Postgres uses the `public` schema; MySQL uses the database. "Empty" means no tables, views or sequences in that schema. Other Postgres schemas are ignored. Cleanup drops only the tables this run created, by name, and on Postgres the whole creation is one transaction.
- **Retired databases.** A retired database is never a target for `init`, `project add` or conversion: it is not empty and it is not a live board. To go back to it, the user clears the flag by hand (the error message gives the SQL), as with SQLite backups today.
- **Registry switched, retire failed.** The conversion counts as done. Both copies are then live, so the command says so, names the stale source and prints the exact statement to retire it. A test covers a failing retire after the switch. The same rule applies to a file source.
- **Re-adding a server project.** `project add` on a database that already holds a Waypoint board adopts it after a schema-version check (this is how `project rm` followed by `add` works, matching "rm keeps the data"). An empty database gets a new board.
- **NUL characters.** Checked on every text value (titles, bodies, keys, names, settings) through one shared check in `sqlstore`, applied for Postgres only; the error is `domain.ErrInvalid` naming the card or field. MySQL stores them.
- **TLS.** `--ssl` takes `disable`, `prefer`, `require`, `verify-full` (mapped to MySQL's equivalents). Non-local defaults to `require`: encrypted, with no certificate check, so a self-signed LAN server works at once. `verify-full` is documented for people who want protection from an impostor server.
- **Password changes.** The environment is read when a connection is made, so a changed variable takes effect after `serve` restarts.

## Left to the builder

Driver versions; pool or per-call connections; exact SSL flag values; default database name; how an old Waypoint reads a config with server projects; wording of messages; how the Makefile starts the containers.

## Known limits to document

- One board per database. Several projects need several databases (or one server with several databases).
- Not for sharing a board between people.
- A MySQL conversion that crashes in the middle may leave tables Waypoint created, which are safe to drop; Waypoint cleans up when it can.
- Moving between storages is a copy: a backup of a server project is the server's own dump or the SQLite/Markdown copy.
