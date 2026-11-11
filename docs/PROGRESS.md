# Waypoint progress

Read this first when resuming. Spec: `docs/superpowers/specs/2026-11-03-waypoint-design.md`. Plans: `docs/superpowers/plans/`. Detailed ledger with every ruling: `.superpowers/sdd/2026-11-03-waypoint-web/progress.md` (local, gitignored).

## State

- **Plan 1 (core, CLI, markdown storage):** merged to `main` (PR #1).
- **Plan 2 (web UI):** merged to `main` (PR #2).
- **Plan 3 (SQLite, convert, import/export, MCP, pharmacy migration):** on branch `feat/sqlite`, PR #3 open, waiting for the owner's review.
  - Plan: `docs/superpowers/plans/2026-11-05-waypoint-sqlite-and-agents.md`. All 12 tasks built.
  - SQLite is the default storage for new projects (`waypoint init` makes `waypoint.db`; `--storage markdown` makes `BOARD.md`).
  - `waypoint project convert` and Convert in project settings move a project between storages; the old files are kept as `.bak-DATE`.
  - `waypoint export` and `import` handle JSON (whole project), CSV (cards) and a Markdown export, from the CLI and from project settings.
  - `waypoint mcp` serves the seven tools over stdio.
  - `scripts/migrate-pharmacy` turns the pharmacy `BOARD.md` and `DONE.md` into a CSV. A dry run on copies gave 99 cards, 14 groups and a counter of 116. The real import has not been done: it waits for the owner.
  - A midpoint binary-level review found and fixed lost writes, hangs and an unsafe registry order in conversion. The final review is recorded in the PR.

## Next steps

1. With the owner: review PR #3, riskiest first: `internal/storage/sqlite/` (lock and save), `internal/projects/convert.go`, then `internal/app/transfer.go`, `internal/app/csv.go` and `internal/domain/validate.go`.
2. Merge the PR when the owner decides.
3. The real pharmacy migration, when the owner says: add the project, set its columns (In Progress, Up Next, Backlog, Done), run the tool on the real files, then `waypoint import --create`. Check the counter reads 116.
4. **Plan 4 (proposed):** choose the SQL server per project: SQLite, or Postgres or MySQL, local or remote. One person's data only, even on a remote server. Plan 3 keeps the board-to-rows code in `internal/storage/sqlstore`, free of SQLite, for this.

## How we work

- Build in the main session. Subagents only where a fresh context helps, such as code and security reviews.
- Keep milestones small enough to review by hand: one behaviour, about 300-500 changed lines with tests. Stop at each one with what changed, where to look and how to try it.

## Decisions to keep

- Plan 3: SQLite is the default storage for new projects. Markdown stays a supported choice, and existing markdown boards keep working.
- A project is a registry entry (name and board file path), not a folder or git repo. Commands use the nearest registered board in the current folder or a parent.
- Dates are stored as UTC timestamps and shown as the local date. Hand-written date-only values stay valid.
- Reviews go by risk: own review for risky tasks, groups for the rest, no re-review of small fixes unless they touch saving, locking or conflicts, and binary-level whole-branch reviews at the midpoint and end.
- Never run `waypoint trust` or touch the trust store in tests. Tests use temp `WAYPOINT_CONFIG` and `HOME`.

## Known limits

- On plain HTTP, more than five visible Waypoint windows at once can stall writes. Hidden tabs release their connection, and HTTPS has no such limit.
- A read-only board with a hand-added card without an id can't be read until its id is assigned. Temp files from a crash stay until deleted by hand.
- A hand edit during a Waypoint write can rarely be lost (0-2 of about 230 under stress). Agents should use the CLI or API, which take the lock.
- The constrained local CA hasn't been tried in a real browser; check this on the first `waypoint trust`.
- A SQLite database edited by a program that does not take Waypoint's lock (for example `sqlite3`) is outside the protections: Waypoint's lock and conflict checks only cover Waypoint's own writers.
- Converting a SQLite project whose card bodies hold a `#`, `##` or `###` heading to Markdown is refused, naming the card: the Markdown format would read it as structure. Edit the card, then convert.
- A SQLite backup made by a conversion is marked retired. To use it again, clear the mark with `sqlite3 FILE "PRAGMA user_version = 1"`.
- A read of a read-only SQLite database can leave a read-only `-shm` file beside it. A markdown conversion leaves a `.lock` file beside the old board.
- Migrations run only when a SQLite database is created; whoever adds migration 002 must run them on open too.
- A CSV import can move cards between columns without the column's move rules or WIP limit, as a hand edit can. A card the file mentions moves to the end of its column.
- `waypoint mcp` resolves the project once, when it starts. After the project is converted, restart it. A line that is not JSON ends the session without a reply.
- A snapshot or CSV with a card id that has another prefix than the board (`X-3` on a `T` board) imports into SQLite but cannot be converted to Markdown, and the refusal's wording is unclear.
- Two columns whose names differ only in case can exist in SQLite, but a Markdown file cannot tell them apart. Avoid them.
- A CSV export writes cells that start with `=`, `+`, `-` or `@` as they are, so a spreadsheet may read one as a formula. Nothing is escaped, to keep the export lossless.
- The web import dialog checks a file and then imports it; a change made to the project between the two can make the result differ from the preview.
- Conversion to a different disk is covered by a test across folders only.
