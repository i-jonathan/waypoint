# Waypoint SQLite, Convert, Import/Export, MCP (Plan 3 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** New projects are stored in SQLite by default (markdown stays a choice), a project converts
between the two losslessly, boards import and export (JSON, CSV, markdown), agents get an MCP
server, and the pharmacy board is migrated in.

**Architecture:** A second `app.Repository` adapter, split so Plan 4 (Postgres/MySQL) can reuse it:
`internal/storage/sqlstore` holds the portable board-to-rows logic over `database/sql`, and
`internal/storage/sqlite` holds what is SQLite-only (driver, file, pragmas, exclusive lock).
Conversion, import and export are `app`-level use cases over `domain.Board`, with CLI, HTTP and MCP as
thin adapters, so all three behave the same.

**Tech Stack:** Go 1.27; `modernc.org/sqlite` (pure Go, no cgo) and `database/sql`;
`encoding/csv` and `encoding/json` from the standard library; the official MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk`). Check the current SDK and driver versions on the day
Task 1 and Task 10 start, and pin them in `go.mod`. Frontend changes use the existing Svelte 5 setup.

**Spec:** `docs/superpowers/specs/2026-11-03-waypoint-design.md` (§5.2 SQLite, §5.3 concurrent edits,
§5.4 converting, §5.5 and §9 import/export, §8 agent interface). **Depends on:** Plans 1 and 2 merged
(PR #1 is; the `feat/web` PR must be merged first). Names used below (`app.Repository`, `app.Service`,
`domain.Board`, `config.ProjectRef`, `storage.Open`/`Create`, `projects.Add`, `watch.Target`) already
exist.

**Decisions from the owner (2026-11-05):** SQLite is the default storage for new projects; markdown
stays supported and existing markdown boards keep working. Choosing Postgres/MySQL (local or remote)
is Plan 4, one person's data only. Plan 3 must not bake in SQLite-only assumptions beyond the small
`sqlite` package.

## Global Constraints

- Go 1.27; the SQLite driver is `modernc.org/sqlite` (pure Go): building needs no C toolchain.
- `internal/domain` imports only the standard library. Dependencies point inward (adapters → `app` → `domain`).
- Dates are stored as UTC and written with `domain.StoredDate` (`YYYY-MM-DD` for a date-only value, a UTC timestamp otherwise); parse with the same rules the markdown adapter uses. Display stays the local date.
- Every change is an operation re-applied to fresh state under the project lock; a stored change Waypoint has not seen is never overwritten.
- A project is a registry entry (`config.ProjectRef`: name, storage, absolute path). Removing one never deletes data. Conversion never deletes the old file.
- A file that cannot be read as a Waypoint board is never written to.
- Tests use a temp `WAYPOINT_CONFIG` and `HOME`. Never run `waypoint trust` or touch the trust store.
- No emoji in the UI; icon-only buttons need an `aria-label` and tooltip. Writes over HTTP need the `X-Waypoint: 1` header (existing middleware).
- Milestones stay small: one behaviour, about 300-500 changed lines with tests. After each task, stop and report what changed, where to look and how to try it.

## Review Focus

Input classes the spec implies but no obvious task test covers; each has a test in the task named.

1. A `.db` file that is not a Waypoint board (another app's SQLite file, an empty file, a newer `schema_version`) must give a clear error and stay untouched (Task 1, Task 3).
2. A database that another process holds locked, or that is read-only, must wait then fail with a plain message, never hang or corrupt (Task 2).
3. Converting when the target path already exists, is on another volume, or the backup name for today is taken must refuse or pick a free name, leaving the registry and both files as they were (Task 5).
4. CSV from real spreadsheets: UTF-8 BOM, CRLF, quoted newlines in `body`, ragged rows, duplicate ids, an id that is already archived (Task 8).
5. Text that stresses both adapters: very long bodies, unicode and `&` in group names, a body with `### T-1` inside a code fence, lowercase ids typed by hand (Tasks 1, 5, 7, 8).

## File Structure

| Path | Responsibility |
|---|---|
| `internal/storage/sqlstore/` | Portable `app.Repository` logic over `database/sql`: schema SQL, `Load`, `Save`, revision check. Knows a `Dialect` (placeholder style, DDL type names). No driver import. |
| `internal/storage/sqlite/` | `New`/`Create`, driver import, pragmas (WAL, busy timeout, foreign keys), `Lock` (pinned connection with `BEGIN IMMEDIATE`), migrations runner, file sniffing. |
| `internal/storage/open.go` | `Open`/`Create` learn `"sqlite"`; `Sniff(path)` tells markdown from SQLite by content. |
| `internal/projects/` | `Add` takes a storage choice; default SQLite; `Convert`. |
| `internal/app/transfer.go` | `Snapshot`, `ImportJSON`, `ImportCSV`, `ExportCSV`, `ExportMarkdown` use cases. |
| `internal/cli/` | `project convert`, `import`, `export`, `mcp`, `--storage`. |
| `internal/mcp/` | MCP stdio server, tools mapped onto `app.Service`. |
| `internal/http/` | Convert, import and export routes. |
| `scripts/migrate-pharmacy/` | One-off BOARD.md/DONE.md → CSV tool. Not a product feature. |

---

### Task 1: SQLite repository, Create/Load/Save

**Files:**
- Create: `internal/storage/sqlstore/{schema.go,load.go,save.go,dialect.go,store_test.go}`, `internal/storage/sqlite/{sqlite.go,migrate.go,migrations/001_init.sql,sqlite_test.go}`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `app.Repository`, `domain.Board`, `storagetest.RunContract`.
- Produces:
  - `sqlite.Create(path string, s domain.Settings) (*sqlite.Repo, error)`: creates the file (the parent directory must exist; `storage.Create` makes it), runs migrations, writes the settings; an existing file gives `os.ErrExist`.
  - `sqlite.New(path string) *sqlite.Repo`: lazy; opens on first use.
  - `(*Repo).Load/Save/Path` implementing `app.Repository` (`Lock` arrives in Task 2; until then it returns `errors.ErrUnsupported`, and the contract's lock test is skipped behind a flag removed in Task 2).
  - `sqlstore.Dialect` interface `{ Rebind(q string) string; Types() ColumnTypes }` and `sqlstore.Store{DB *sql.DB, D Dialect}` with `Load(ctx, q Querier)` / `Save(ctx, tx *sql.Tx, b *domain.Board, seenRevision int64) (newRevision int64, err error)`.

Schema (portable column types only, `?` placeholders, no engine-specific SQL in `sqlstore`):
`project(id INTEGER PK CHECK id=1, schema_version INT, revision INT, settings TEXT)`;
`columns(position INT PK, name TEXT, wip INT, done INT, moves_to TEXT)` (JSON list);
`groups(position INT PK, name TEXT, color TEXT)`;
`cards(id TEXT PK, position INT, column_name TEXT NULL, title, group_name, priority, depends TEXT, created TEXT, done TEXT, body TEXT, archived_at TEXT NULL)`;
`card_fields(card_id TEXT, position INT, key TEXT, value TEXT, PK(card_id, position))`.
`settings` holds name, prefix, next_id, priorities, archive, appearance as JSON. Live cards are
ordered by `position` with `archived_at IS NULL`; archived cards keep their slice order in `position`
and `archived_at` holds the card's `done` stamp, else `created`, else the Unix epoch.

- [ ] **Step 1: Write the contract hook.** In `sqlite_test.go`, `TestSQLiteRepoContract` calls `storagetest.RunContract` with a `newRepo` that does `sqlite.Create(filepath.Join(t.TempDir(), "b.db"), domain.NewBoard("Contract", "T").Settings)`.
- [ ] **Step 2: Run to see it fail.** Run: `go test ./internal/storage/sqlite/ -run Contract -v`. Expected: build failure (package missing).
- [ ] **Step 3: Add the driver.** Run `go get modernc.org/sqlite@<current>`; commit nothing yet.
- [ ] **Step 4: Implement the `sqlstore` schema, `Load` and `Save`.** `Load` reads settings, columns, groups and both card sets in one read transaction, so they agree. `Save` replaces all rows in one transaction (delete then insert; a few hundred cards is fast) and bumps `revision`. It returns `app.ErrChangedDuringWrite` when `project.revision` differs from `seenRevision`.
- [ ] **Step 5: Implement `sqlite.Repo`.** `New`, `Create`, `Path`, `Load`, `Save`; remember the revision seen at `Load`; a `Save` with no earlier `Load` skips the check (same rule as the markdown adapter). Open with WAL, `busy_timeout=5000`, `foreign_keys=on`.
- [ ] **Step 6: Implement migrations.** `migrate.go` applies embedded `migrations/NNN_*.sql` in order and records `project.schema_version`. A file with a table set that is not Waypoint's, or a `schema_version` higher than the binary knows, returns `domain.ErrInvalid` ("not a Waypoint database" / "made by a newer Waypoint") and is not written.
- [ ] **Step 7: Add tests beyond the contract** in `sqlite_test.go`:
  - `TestCreateRefusesExistingFile`: `errors.Is(err, os.ErrExist)`, file bytes unchanged.
  - `TestOpenForeignDatabaseUntouched`: a SQLite file holding `CREATE TABLE notes(x)` makes `Load` fail with `domain.ErrInvalid`, and the file's sha256 is the same after.
  - `TestOpenNewerSchemaRefused`: `project.schema_version = 999` is refused and not migrated.
  - `TestLongAndUnicodeText`: a 1 MB body, group `Café & crème`, and a body containing a fenced `### T-1` round-trip equal.
- [ ] **Step 8: Run.** Run: `go test ./internal/storage/... -v`. Expected: PASS (lock test skipped).
- [ ] **Step 9: Commit.** `git add go.mod go.sum internal/storage && git commit -m "feat(storage): sqlite repository"`

**Review:** own review (new persistence code; the save path decides data safety).

---

### Task 2: SQLite lock, change checks and concurrency

**Files:**
- Modify: `internal/storage/sqlite/sqlite.go`, `internal/storage/storagetest/contract.go` (no change if the lock test already exists)
- Create: `internal/storage/sqlite/lock.go`, `internal/storage/sqlite/race_test.go`

**Interfaces:**
- Consumes: Task 1's `Repo`.
- Produces: `(*Repo).Lock(ctx) (unlock func() error, err error)` per `app.Repository`: pins one connection and runs `BEGIN IMMEDIATE`, so other processes' writers wait. While locked, `Load` and `Save` on that `Repo` use the pinned transaction; `unlock` commits it (nothing else changed) or rolls it back. Waiting stops at `ctx` with the plain error `the project database is busy: another program has it locked`.

- [ ] **Step 1: Write failing tests** in `race_test.go`:
  - `TestLockExcludesAnotherRepo`: two `Repo` values on one file; the second `Lock` with a 200 ms context fails with the busy message while the first holds it, and succeeds after unlock.
  - `TestSaveWithoutLockDetectsOtherWriter`: A `Load`s, B saves, A `Save`s → `errors.Is(err, app.ErrChangedDuringWrite)` and B's cards are intact.
  - `TestParallelServicesLoseNothing`: 8 goroutines, each with its own `app.Service` on the same file, add 25 cards each; afterwards 200 distinct ids exist (mirror `internal/storage/markdown/race_test.go`).
  - `TestReadOnlyFileGivesPlainError`: `chmod 0444` the file; `Save` fails with a message containing "read-only", not a raw driver string.
  - `TestReadsDoNotBlockOnWriter`: while A holds `Lock`, B's `Load` (no lock) returns the last committed board within 1 s (WAL).
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/storage/sqlite/ -run 'Lock|Detects|Parallel|ReadOnly|DoNotBlock' -v`. Expected: FAIL (Lock unsupported).
- [ ] **Step 3: Implement `lock.go`** as above. Map `SQLITE_BUSY`/`SQLITE_READONLY` driver errors to the plain messages; wrap unknown errors with the path.
- [ ] **Step 4: Remove the lock-test skip flag from Task 1** so the contract's `LockIsExclusive` runs.
- [ ] **Step 5: Run.** Run: `go test ./internal/storage/... -race -count=3`. Expected: PASS.
- [ ] **Step 6: Commit.** `git commit -am "feat(storage): sqlite lock and change detection"`

**Review:** own review (locking and conflicts; Review Focus 2).

---

### Task 3: Make SQLite a registrable storage

**Files:**
- Modify: `internal/storage/open.go`, `internal/projects/projects.go`, `internal/http/projects.go`, `internal/http/errors.go`, `internal/watch/fingerprint.go`, `internal/cli/serve.go` (only if targets change), `internal/config/registry.go`
- Test: `internal/storage/open_test.go`, `internal/projects/projects_test.go`, `internal/watch/poll_test.go`, `internal/http/api_test.go`

**Interfaces:**
- Consumes: `sqlite.New/Create`.
- Produces:
  - `storage.Sniff(path string) (kind string, err error)`: `"sqlite"` when the file starts with `SQLite format 3\x00`, `"markdown"` otherwise, `os.ErrNotExist` passed through. `config.StorageFor` stays the guess for a file that does not exist yet.
  - `storage.Open/Create` handle `"sqlite"`; `storage.ErrSQLite` is deleted.
  - `projects.Add` opens an existing file by `Sniff` (an existing `.db` is registered as SQLite whatever its name; a markdown file named `.db` is an error that says so).
  - `watch` fingerprints a SQLite project by its `.db` and `.db-wal` files, and ignores `-shm`.

- [ ] **Step 1: Write failing tests.**
  - `TestSniff`: a real SQLite file gives `"sqlite"`, a markdown board gives `"markdown"`, an empty file gives `domain.ErrInvalid`, a missing file gives `os.ErrNotExist`.
  - `TestAddSQLiteProject`: `projects.Add` with `Path: <tmp>/x.db` creates the file, registry entry has `Storage: "sqlite"`.
  - `TestAddExistingDatabaseOpens`: pre-created via `sqlite.Create`, `Add` registers it without rewriting (sha256 same).
  - `TestAddForeignDatabaseRefused`: Review Focus 1, config unchanged.
  - `TestWatchNoticesSQLiteWrite`: a write through `app.Service` publishes within 2 s (extend the existing poll test pattern).
  - `TestCreateProjectAPIAcceptsDb`: `POST /api/projects` with `path` ending `.db` returns 201; `.txt` still returns 400 with the updated message "path must end in .md, .markdown or .db".
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/storage/ ./internal/projects/ ./internal/watch/ ./internal/http/ -v`.
- [ ] **Step 3: Implement the changes above.** `checkArchive` (markdown sibling-file collisions) is skipped for SQLite projects. Keep the HTTP `existing` flag behaviour.
- [ ] **Step 4: Run all Go tests.** Run: `go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(projects): register and watch sqlite projects"`

---

### Task 4: SQLite becomes the default

**Files:**
- Modify: `internal/projects/projects.go` (`AddOptions.Storage`), `internal/cli/project.go` (`--storage`), `internal/http/projects.go`, `web/src/components/NewProjectDialog.svelte`, `web/src/lib/api.ts`, `internal/cli/agents.go`, `README.md`, `docs/board-format.md`
- Test: `internal/projects/projects_test.go`, `internal/cli/project_test.go`, `internal/http/api_test.go`, `web/src/components/NewProjectDialog.test.ts`, `web/e2e/owner.spec.ts`

**Interfaces:**
- Produces:
  - `projects.AddOptions.Storage string` (`""` means sqlite, otherwise `"markdown"` or `"sqlite"`); a `Path` whose extension disagrees with an explicit `Storage` gives `domain.ErrInvalid`.
  - Default file name with no path: `<data_dir>/<slug>.db` (sqlite) or `.md` (markdown).
  - `waypoint init` makes `./waypoint.db` by default and `./BOARD.md` with `--storage markdown`; `project add NAME --storage markdown|sqlite`.
  - API body for `POST /api/projects` gains `"storage"`; omitted means sqlite.
  - New project dialog: a "Storage" choice (SQLite selected, Markdown file); the placeholder and extension check follow the choice. "Open existing file" accepts both.

- [ ] **Step 1: Write failing tests.**
  - Go: `TestAddDefaultsToSQLite` (no path → `<data_dir>/<slug>.db`, storage `sqlite`); `TestAddMarkdownStillWorks`; `TestStorageAndExtensionMustAgree`; `TestInitDefaultsToSQLiteFile`; `TestInitMarkdownFlag`; `TestAPIDefaultStorage`.
  - Frontend: `NewProjectDialog.test.ts` — default radio is SQLite; picking Markdown changes the accepted extension message to ".md or .markdown"; "Open existing file" accepts `.db`.
  - e2e: create a project with defaults and see the board load.
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/... && (cd web && npx vitest run NewProjectDialog)`.
- [ ] **Step 3: Implement.** Existing registry entries are unchanged. Update the `waypoint agents` paragraph so it does not claim the board is a Markdown file, and update README and `docs/board-format.md` with a short "Choosing a storage" section.
- [ ] **Step 4: Run everything.** Run: `make test`, then `make e2e`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat: sqlite is the default storage for new projects"`

---

### Task 5: Convert a project between storages

**Files:**
- Create: `internal/projects/convert.go`, `internal/projects/convert_test.go`
- Modify: `internal/cli/project.go`

**Interfaces:**
- Consumes: `storage.Open/Create`, `config.Update`, `domain.Board.Equal`.
- Produces:
  - `projects.Convert(ctx, cfgPath string, env func(string) string, name, to, newPath string) (config.ProjectRef, error)`.
  - CLI: `waypoint project convert NAME --to sqlite|markdown --path FILE` (`--path` optional; default is the same folder and stem with the new extension). `--json` prints the new `projectJSON`.

Behaviour (spec §5.4), all under the old project's lock and the config lock:
1. Load the full board (settings, cards, archive) through the old repository.
2. Refuse when `newPath` exists, equals the old path, or is an archive file, or the project is already that storage. Nothing is created.
3. Create the target, `Save`, then `Load` it fresh and require `Board.Equal` with the source (ids, ranks, bodies, extra fields, settings, archive).
4. Rename the old files (board, and `<stem>.archive.md` for markdown) to `<name>.bak-YYYYMMDD`; if that exists, `.bak-YYYYMMDD-2`, `-3`, … Update the registry entry last.
5. On any failure before step 4 completes, remove only the target file Convert itself created and leave the registry and old files as they were.

- [ ] **Step 1: Write failing tests** in `convert_test.go`:
  - `TestConvertMarkdownToSQLiteAndBack`: a board with every concept (groups, WIP, depends, extra fields, archive in two months, date-only and timestamp dates, a body with a fenced `### T-9`) converts md → sqlite → md and `Board.Equal` the original at each step (the §5.4 round-trip requirement).
  - `TestConvertKeepsBackup`: old board and archive exist as `*.bak-<date>`; registry now points at the new path with the new storage.
  - `TestConvertRefusesExistingTarget`, `TestConvertSameStorageRefused`: registry bytes and files unchanged.
  - `TestConvertBackupNameTaken`: a pre-existing `.bak-<today>` gets the `-2` suffix.
  - `TestConvertVerifyFailureRollsBack`: inject a repository wrapper that drops a card on `Load` (via an exported test seam `projects.openTarget` variable) → error, target file removed, registry and source untouched.
  - `TestConvertAcrossDirectories`: target in another temp dir works (rename failure of backup falls back to copy-then-remove is not needed: backups stay in the source folder).
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/projects/ -run Convert -v`.
- [ ] **Step 3: Implement `Convert`** per the list; use the clock injected through the same `Now` seam `projects` tests already use, or add one.
- [ ] **Step 4: Add the CLI command and tests** in `project_test.go`: success output "Converted NAME to sqlite at PATH (old files kept as …)", error exit codes via `errorCode`.
- [ ] **Step 5: Run.** Run: `go test ./internal/projects/ ./internal/cli/ -race`. Expected: PASS.
- [ ] **Step 6: Commit.** `git commit -am "feat(projects): convert between markdown and sqlite"`

**Review:** own review (data-loss risk; Review Focus 3).

---

### Task 6: Convert from the web UI

**Files:**
- Modify: `internal/http/projects.go`, `internal/http/api.go`, `web/src/components/ProjectSettings.svelte`, `web/src/lib/api.ts`
- Test: `internal/http/api_test.go`, `web/src/components/ProjectSettings.test.ts`, `web/e2e/owner.spec.ts`

**Interfaces:**
- Consumes: `projects.Convert`.
- Produces: `POST /api/projects/{name}/convert` body `{"to":"sqlite|markdown","path":"<abs, optional>"}` → 200 with `projectJSON`; publishes the `projects` event; `domain.ErrInvalid` → 400. The project settings page has a Storage section showing the current storage and path, and a "Convert to SQLite / Markdown" button with a confirm dialog naming the backup files, using the existing `ConfirmDialog`.

- [ ] **Step 1: Write failing tests:** API happy path (board still loads under the same name after), API refusal for an existing target (400, nothing changed), component test that the confirm dialog names the target and calls the API once, e2e converting and seeing the same cards.
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/http/ && (cd web && npx vitest run ProjectSettings)`.
- [ ] **Step 3: Implement the route and the section.** Open boards of the project refetch after the `projects` event; the UI keeps working while the registry entry's storage changes.
- [ ] **Step 4: Run.** Run: `make test && make e2e`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(web): convert a project's storage"`

**Midpoint review (after this task):** binary-level whole-branch review by a fresh reviewer (build, create projects in both storages, convert both ways, hammer with the CLI while the server runs). Fix findings, then continue.

---

### Task 7: JSON snapshot export and import

**Files:**
- Create: `internal/app/transfer.go`, `internal/app/transfer_test.go`, `internal/cli/transfer.go`, `internal/cli/transfer_test.go`

**Interfaces:**
- Consumes: `domain.Board`, `Service.apply`.
- Produces:
  - `type Snapshot struct { Format int; Settings domain.Settings; Cards []domain.Card; Archived []domain.Card }` with JSON tags; `Format` is `1`.
  - `(*Service).Snapshot(ctx) (Snapshot, error)` and `(*Service).ImportSnapshot(ctx, Snapshot, ImportOptions) (ImportResult, error)` where `ImportOptions{Replace bool}`; without `Replace` the project must be empty (no cards, no archive), otherwise `domain.ErrInvalid`. `Replace` swaps the whole board.
  - CLI: `waypoint export --format json [--out FILE]` (stdout by default), `waypoint import FILE.json [--replace]`. Format is chosen by `--format`, else the file extension.
  - JSON dates use `domain.StoredDate`; unknown top-level keys and a `Format` above 1 are refused.

- [ ] **Step 1: Write failing tests:**
  - `TestSnapshotRoundTrip`: export from a markdown project, import into a fresh SQLite project, `Board.Equal`.
  - `TestImportIntoNonEmptyRefused`, `TestImportReplace`.
  - `TestImportRejectsNewerFormat`, `TestImportValidatesSettings` (bad prefix, card in a column that does not exist, depends on a missing id is allowed and flagged missing as today).
  - `TestImportLowercaseIDs`: ids are normalised or refused with a clear message (pick: refused, naming the id).
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/app/ ./internal/cli/ -run 'Snapshot|Import|Export' -v`.
- [ ] **Step 3: Implement** the use cases (one `apply` call, validating before any change) and the CLI commands with `--json` on `import` printing counts.
- [ ] **Step 4: Run.** Run: `go test ./... -race`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat: json snapshot export and import"`

---

### Task 8: CSV import and export, markdown export

**Files:**
- Modify: `internal/app/transfer.go`, `internal/cli/transfer.go`
- Create: `internal/app/csv.go`, `internal/app/csv_test.go`, `internal/app/testdata/*.csv`

**Interfaces:**
- Produces:
  - `(*Service).ExportCSV(ctx, w io.Writer) error`: header `id,title,column,group,priority,depends,created,done,body` then extra-field headers (union, in first-seen order); archived cards have `column=Archived`; `depends` is comma-joined; dates via `domain.StoredDate`.
  - `(*Service).ImportCSV(ctx, r io.Reader, o CSVOptions) (ImportResult, error)` with `CSVOptions{Create bool}` and `ImportResult{Created, Updated []domain.CardID; NewColumns, NewGroups []string}`.
  - `(*Service).ExportMarkdown(ctx, w io.Writer) error`: the board in the markdown file format (read-only snapshot, archive appended as its own sections).
  - CLI: `export --format csv|md`, `import FILE.csv [--create]`.
- Rules (spec §9): empty `id` makes a new id; an existing id (live or archived) updates that card in place; rows keep file order as rank within each column; unknown columns or groups are an error listing them unless `Create`, in which case they are added (groups get the next palette colour); extra headers become extra fields; nothing is written if any row is invalid (all-or-nothing).

- [ ] **Step 1: Write failing tests**, with fixtures in `testdata/`:
  - `TestCSVRoundTrip`: export then import into an empty project equals the original board's cards.
  - `TestCSVSpreadsheetQuirks` (Review Focus 4): UTF-8 BOM, CRLF line endings, quoted newline and comma in `body`, trailing blank line, short rows → fields default empty, extra long row → error naming the line.
  - `TestCSVUpdatesByID` including an archived id (card stays archived, fields updated) and a duplicate id inside one file (error naming both lines).
  - `TestCSVUnknownColumnNeedsCreate`: error lists the names; with `Create` they exist afterwards in the file order given.
  - `TestCSVAllOrNothing`: a bad row 5 leaves the board byte-identical.
  - `TestCSVEmptyTitleRefused`, `TestCSVBadPriorityRefused`.
  - `TestMarkdownExportParses`: the exported text parses back with the markdown parser to an equal board.
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/app/ -run 'CSV|MarkdownExport' -v`.
- [ ] **Step 3: Implement** with `encoding/csv` (`LazyQuotes` off, `FieldsPerRecord = -1` and our own row checks so errors name a line). Strip a BOM before the header. Export writes `\n` line endings.
- [ ] **Step 4: Add CLI wiring and tests** for stdout/`--out`, `--create`, exit codes.
- [ ] **Step 5: Run.** Run: `go test ./... -race`. Expected: PASS.
- [ ] **Step 6: Commit.** `git commit -am "feat: csv import/export and markdown export"`

**Review:** own review for import (it can overwrite cards by id).

---

### Task 9: Import and export in the web UI

**Files:**
- Modify: `internal/http/api.go`, `internal/http/projects.go`, `web/src/components/ProjectSettings.svelte`, `web/src/lib/api.ts`
- Create: `web/src/components/ImportDialog.svelte`, `web/src/components/ImportDialog.test.ts`
- Test: `internal/http/api_test.go`, `web/e2e/owner.spec.ts`

**Interfaces:**
- Produces:
  - `GET /api/projects/{name}/export?format=csv|json|md` → file download with `Content-Disposition`.
  - `POST /api/projects/{name}/import?format=csv|json&create=0|1&dry_run=0|1` with the file as body (limit 10 MB). A dry run returns `ImportResult` without writing, so the UI can show "This adds columns X, Y and groups Z" and ask for confirmation before the real call.
  - Project settings gains an Import / export section: three export links and an import file picker feeding `ImportDialog`.

- [ ] **Step 1: Write failing tests:** API export content types and bodies; import dry run changes nothing; real import after dry run matches the dry run's result; oversize body gives 413; component test that unknown columns trigger the confirmation text and "Create and import" sends `create=1`.
- [ ] **Step 2: Run to see them fail.** Run: `go test ./internal/http/ && (cd web && npx vitest run ImportDialog)`.
- [ ] **Step 3: Implement.** `dry_run` is done by running the import against a clone and discarding it (no second code path).
- [ ] **Step 4: Run.** Run: `make test && make e2e`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(web): import and export"`

---

### Task 10: MCP server

**Files:**
- Create: `internal/mcp/server.go`, `internal/mcp/server_test.go`
- Modify: `internal/cli/cli.go` (command `mcp`), `internal/cli/agents.go`, `go.mod`

**Interfaces:**
- Consumes: `app.Service`, the CLI's project resolution (`--project`, `$WAYPOINT_PROJECT`, nearest registered board).
- Produces: `mcp.New(svc *app.Service) *sdk.Server` and `waypoint mcp [--project NAME]`, serving MCP over stdio. Tools (spec §8), each calling the matching `Service` method and returning the same JSON the CLI's `--json` prints: `list_cards{column?,group?,blocked?,query?}`, `get_card{id}`, `create_card{title,column?,group?,priority?,depends?,body?}`, `move_card{id,column,index?}`, `update_card{id,fields…,if_match?}`, `next_up{}`, `list_blocked{}`.

- [ ] **Step 1: Pick the SDK version** (latest stable at least two weeks old), `go get` it, and note the version in the commit message.
- [ ] **Step 2: Write failing tests** driving the server through the SDK's in-memory transport pair:
  - `TestToolsListed`: exactly the seven names.
  - `TestCreateThenMove` against a markdown and a SQLite service.
  - `TestMoveRefusedByRules`: a disallowed move returns a tool error whose text is the domain message, not a protocol error.
  - `TestUpdateConflict`: a stale `if_match` returns the conflict text with the current hash.
  - `TestUnknownCardIsToolError`.
- [ ] **Step 3: Run to see them fail.** Run: `go test ./internal/mcp/ -v`.
- [ ] **Step 4: Implement** the tools with input structs and short descriptions that tell an agent when to use the CLI instead. The process must write nothing but protocol messages to stdout (logs to stderr).
- [ ] **Step 5: Add `waypoint mcp`** and a CLI test that a one-request session over pipes answers `tools/list`. Extend `waypoint agents` text with the MCP config snippet.
- [ ] **Step 6: Run.** Run: `go test ./... -race`. Expected: PASS.
- [ ] **Step 7: Commit.** `git commit -am "feat: mcp server"`

---

### Task 11: Pharmacy migration (one-off)

**Files:**
- Create: `scripts/migrate-pharmacy/main.go`, `scripts/migrate-pharmacy/main_test.go`, `scripts/migrate-pharmacy/testdata/{BOARD.md,DONE.md,want.csv}`

**Interfaces:**
- Consumes: Task 8's CSV format.
- Produces: `go run ./scripts/migrate-pharmacy BOARD.md DONE.md > pharmacy.csv`. Read-only on its inputs. Mapping:
  - Columns `In Progress`, `Up Next`, `Backlog` keep their names; `DONE.md` cards get `column=Archived` and keep their `done` dates (the owner can still change this).
  - `feature` → `group`; `priority`, `depends`, `created` map directly; `merged` and `prs` become extra headers; the body is everything after the field lines, byte for byte.
  - The `<!-- next-id: T-112 -->` comment is printed to stderr as `next_id 112`, for `project add` to be given afterwards.
  - Ids already in the file are kept; there is no id renumbering. The "Card format" example `T-000` in the how-to block is skipped.

- [ ] **Step 1: Write failing test** `TestMigrateSample`: a trimmed fixture (the how-to block with its fenced `T-000` example, three cards with `feature`, `depends`, `merged`, one with a checklist and a fenced code block, an empty column placeholder line `_Nothing in progress…_`, a `DONE.md` month section) produces exactly `testdata/want.csv`.
- [ ] **Step 2: Run to see it fail.** Run: `go test ./scripts/migrate-pharmacy/ -v`.
- [ ] **Step 3: Implement** the parser (headings `##` column, `###` card, `- key: value` lines until the first blank line, `## 2026-10` month headings inside `DONE.md`).
- [ ] **Step 4: Dry run on a copy.** Run the tool on copies of the real files (in the scratchpad, never inside the pharmacy repo), then import into a throwaway project:
  `WAYPOINT_CONFIG=<tmp> waypoint project add pharmacy-test --path <tmp>/p.db && waypoint import pharmacy.csv --create --project pharmacy-test`.
  Expected: card count equals BOARD.md `###` cards plus DONE.md `###` cards (minus the `T-000` example); `waypoint ls --blocked` and `--next` give sensible answers.
- [ ] **Step 5: Stop and show the owner** the dry-run result. Registering the real project, setting its `next_id`, and pointing the pharmacy repo's `AGENTS.md`/`CLAUDE.md` at `waypoint` are the owner's call and happen only on their go-ahead.
- [ ] **Step 6: Commit.** `git add scripts && git commit -m "chore: one-off pharmacy board migration tool"`

---

### Task 12: Docs, progress and final review

**Files:**
- Modify: `docs/PROGRESS.md`, `README.md`, `docs/board-format.md`

- [ ] **Step 1: Update docs.** README: storage choice, convert, import/export, MCP setup. `docs/PROGRESS.md`: Plan 3 state, new known limits (for example, a raw SQL editor that bypasses Waypoint's lock is outside the protections), and Plan 4 as the next step.
- [ ] **Step 2: Run everything.** Run: `make test && make e2e && go vet ./...`. Expected: PASS.
- [ ] **Step 3: Final binary-level whole-branch review** by a fresh reviewer on the most capable model: build the binary, run both storages, convert both ways while the server is open in a browser, import the migrated pharmacy CSV, drive the MCP server, and attack the Review Focus list. Fix findings; no re-review of small fixes unless they touch saving, locking or conflicts.
- [ ] **Step 4: Commit** `git commit -am "docs: plan 3 state"` and hand the branch to the owner for a guided review.

## Order and stops

Tasks 1-2 are the riskiest (storage correctness): stop after each for review. Task 3-4 change what
users see by default: stop after Task 4 so the owner can try a new default project. Tasks 5-6 end at
the midpoint review. Tasks 7-9 can each be tried from the CLI or UI. Task 10 is independent of 5-9 and
can move earlier if agents are blocked on it. Task 11 needs Task 8.

## Self-review notes

- Spec coverage: §5.2 (Tasks 1-3), §5.3 for SQLite (Task 2), §5.4 (Tasks 5-6), §5.5 and §9 (Tasks 7-9), §8 import/export/convert/mcp (Tasks 5, 7-8, 10), pharmacy migration §9 (Task 11), owner's default change (Task 4).
- Plan 4 headroom: only `internal/storage/sqlite` and the `Sniff` file test assume a local file. `ProjectRef` gains a connection field in Plan 4.
- Left out on purpose: user accounts and multi-user features, a third storage format, Trello/Jira importers.
