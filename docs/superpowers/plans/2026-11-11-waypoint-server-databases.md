# Waypoint Postgres and MySQL Storage (Plan 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A project can be stored in a Postgres or MySQL database (local Docker or remote), set up from the CLI and the web UI, convertible to and from the other storages without losing data or leaking the password.

**Architecture:** Two new `app.Repository` adapters, `internal/storage/postgres` and `internal/storage/mysql`, reuse the portable board-to-rows code in `internal/storage/sqlstore` through a larger `Dialect`. Connection details (not the password) live in `config.ProjectRef`; the password is read from an environment variable by a small `internal/dbconn` package that also owns the local/non-local check and error scrubbing. `projects.Add` and `projects.Convert` are generalised from "file" to "storage target"; the watcher polls a revision number for server projects.

**Tech Stack:** Go 1.27, `database/sql`, `github.com/jackc/pgx/v5` (stdlib driver), `github.com/go-sql-driver/mysql`; Docker for server tests; the existing Svelte 5 web UI. Check current driver versions on the day Task 3 and Task 9 start and pin them in `go.mod`.

**Spec:** `docs/superpowers/specs/2026-11-11-waypoint-server-databases-design.md`. **Depends on:** Plan 3 (PR #3, branch `feat/sqlite`) merged or this branch cut from it. Existing names used below: `app.Repository`, `config.ProjectRef`, `storage.Open/Create`, `projects.Add/Convert`, `sqlstore.Store/Dialect/Querier`, `watch.Target`, `domain.ErrInvalid`.

## Global Constraints

- Password: only from the environment, variable `WAYPOINT_DB_PASSWORD_<NAME>` (`<NAME>` = project name upper-cased, every character other than A-Z and 0-9 turned into `_`), then fallback `WAYPOINT_DB_PASSWORD`. If neither is set, connect without a password. Never prompt. Never written to the registry, logs, errors, `--json`, API responses, command lines or the UI.
- Local server = host `localhost`, `127.0.0.0/8`, `::1`, or a Unix socket path; anything else is non-local. The warning (designed for one person's data, not for sharing a board) is shown once at `init`, `project add` and `project convert`, never stored or repeated.
- TLS: local default is the driver's "prefer"; non-local default is `require` (encrypted, no certificate check); `--ssl` accepts `disable`, `prefer`, `require`, `verify-full`. `disable` on a non-local server prints that the password will cross the network unencrypted.
- One board per database. Postgres uses the `public` schema; MySQL uses the database. A conversion or new-board target must be empty (no tables, views or sequences in that schema). A retired database is never a target. `project add` on a database that already holds a Waypoint board adopts it after a schema-version check.
- Lock: row lock on the `project` row, retired flag re-checked after acquiring, caller's context honoured, 30 s cap with `ErrBusy` (`busy`, HTTP 503) when the caller sets no deadline, engine lock timeout below 30 s. Writes under the lock use a SAVEPOINT; the revision check gives `ErrChangedDuringWrite`.
- Dates stay text from `domain.StoredDate`. Card numbers fit `domain.MaxCardNumber` (2^31-1). Positions are stored explicitly.
- Postgres refuses a NUL character in any text value with `domain.ErrInvalid` naming the card or field. MySQL uses `utf8mb4` and binary collation (`utf8mb4_bin`) on identifier-like columns; bodies are `LONGTEXT`.
- Live updates for server projects: poll the revision about every 2 seconds; log once per outage; a missing password variable is logged once with the variable's name.
- Connect timeout 10 s (plan decision), so an unreachable server never hangs a command or the UI.
- Conversion rules from Plan 3 stay: lock the source, load, write the copy, verify it equals the original, switch the registry, then retire. Leaving a server renames nothing and deletes nothing. If the retire step fails after the switch the conversion counts as done and the message names the stale source and the statement to retire it.
- `go test ./...` passes on a machine with no database server (server tests skip). Tests use a temp `WAYPOINT_CONFIG` and `HOME`; never run `waypoint trust` or touch the trust store.
- Existing configs, Markdown and SQLite boards keep working unchanged. The config decodes strictly, so older binaries refuse server projects; no version bump.
- Commits are small, one behaviour each, and end with the attribution line the harness gives.

## Review Focus

Inputs the spec implies but no task's own happy-path tests would reach; each has a test in the owning task.

1. A password with `@ : / ? # % space ' "` and a database name with `-` and a leading digit connect correctly (Task 3, Task 9).
2. A project name that sanitises to an empty or colliding variable name (`日本語`, `a-b` vs `a_b`): refused at add with the reason (Task 1, Task 5).
3. A server that is down, wrong, or missing its password when `serve` starts or lists projects: that project shows an error within the connect timeout; other projects work; the watcher does not spam (Task 7, Task 8).
4. The server restarts or drops the connection mid-run: the next call recovers (Task 4).
5. Bodies of 1 MB, emoji, quotes, `\r\n`, and 6,000 cards survive conversion in and out (Task 6).

## File Structure

- `internal/dbconn/` (new): `Spec` helpers, password variable, local check, TLS defaults, error scrubbing, warning text.
- `internal/config/{config,registry}.go`: server fields on `ProjectRef`, validation.
- `internal/storage/sqlstore/`: `Dialect` grows a `ValidText` method (Task 3).
- `internal/storage/postgres/`, `internal/storage/mysql/` (new): `Create`, `New`, `Load`, `Save`, `Lock`, `Retire`, `Revision`, `Discard`, `Probe`, migrations.
- `internal/storage/servertest/` (new): test helpers that provide a fresh database or skip.
- `internal/storage/open.go`: engines in `Open`/`Create`.
- `internal/projects/{projects,convert}.go`: server targets, adopt, generalised conversion.
- `internal/watch/watch.go`, `internal/cli/{serve,project}.go`, `internal/http/{projects,transfer}.go`, `web/src/...`.

---

### Task 1: Server fields in the registry, password and local check

**Files:**
- Create: `internal/dbconn/dbconn.go`, `internal/dbconn/dbconn_test.go`
- Modify: `internal/config/config.go:28-32`, `internal/config/registry.go:19-40`, plus tests

**Interfaces:**
- Produces (`package dbconn`, imports only the standard library):
  - `func PasswordVar(project string) string` (`"pharmacy"` -> `WAYPOINT_DB_PASSWORD_PHARMACY`)
  - `func Password(getenv func(string) string, project string) string` (per-project, then the generic fallback, else `""`)
  - `func IsLocal(host string) bool`
  - `func DefaultPort(engine string) int` (5432, 3306)
  - `func ValidSSL(v string) bool`; `func DefaultSSL(host string) string` (`prefer` local, `require` non-local)
  - `func Scrub(err error, secrets ...string) error` (replaces every secret, and any `://user:pass@` pattern, in the message; keeps `errors.Is/As` working)
  - `const NonLocalWarning string`
- Produces (`config`): `ProjectRef` gains `Host string`, `Port int`, `Database string`, `User string`, `SSL string` (all `yaml:",omitempty"`, and `Path` becomes `yaml:",omitempty"`), and `func (p ProjectRef) IsServer() bool` (storage `postgres` or `mysql`). `Display() string` returns the path, or `host:port/database` for a server.

- [ ] **Step 1: Write failing tests** in `dbconn_test.go`: `TestPasswordVar` (`"my-proj"` -> `..._MY_PROJ`, `"日本語"` -> `..._` + `___`), `TestPassword` (project var wins; fallback used; unset gives `""`), `TestIsLocal` (`localhost`, `127.0.0.1`, `127.5.5.5`, `::1`, `/var/run/postgresql` true; `db.example.com`, `192.168.1.5`, `10.0.0.2` false), `TestScrubKeepsWrapping` (message loses the secret `p@ss:w/rd`, `errors.Is(err, io.EOF)` still true), `TestScrubURLPassword` (`postgres://u:secret@h/db` loses `secret` even when not listed). In `internal/config`: `TestServerEntryRoundTrip` (a postgres entry with no path saves and loads), `TestServerEntryNeedsHostUserDatabase`, `TestServerEntryRefusesCollidingPasswordVars` (`a-b` and `a_b`), `TestServerEntryRefusesEmptyVar` (a name with no A-Z0-9 at all), `TestFileEntryUnchanged`.
- [ ] **Step 2:** Run `go test ./internal/dbconn ./internal/config`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `dbconn` as listed, and in `registry.go` validate server entries: engine known, host/user/database non-empty, port 1-65535 (0 means default), `ssl` empty or valid, no `Path`; a file entry keeps the existing checks and must have no server fields. Path uniqueness applies only to file entries; project-name and password-variable uniqueness apply to all.
- [ ] **Step 4:** Run the same tests. Expected: PASS. Run `go test ./...`. Expected: PASS (nothing else should change yet; fix any code that now sees an empty `Path` only in Task 5).
- [ ] **Step 5: Commit** `feat: registry fields and password helpers for server projects`.

### Task 2: Server test harness

**Files:**
- Create: `internal/storage/servertest/servertest.go`, `internal/storage/servertest/servertest_test.go`
- Modify: `Makefile` (add `test-servers`), `docs/` none

**Interfaces:**
- Produces: `func Postgres(t *testing.T) config.ProjectRef` and `func MySQL(t *testing.T) config.ProjectRef`. Each reads `WAYPOINT_TEST_POSTGRES` / `WAYPOINT_TEST_MYSQL` (`host:port:user:password`), skips the test when unset, creates a uniquely named empty database, sets the password variable for the returned project name with `t.Setenv`, and drops the database in `t.Cleanup`. The returned `ProjectRef` has `Name` `"servertest"`, the engine, host, port, database and user. Also `func Docker(t *testing.T) bool` (true when `docker` is reachable).

- [ ] **Step 1:** Write `TestSkipsWithoutEnv` (with the variable unset the helper skips; assert via a sub-test run). The helper itself is exercised by Tasks 3 and 9.
- [ ] **Step 2:** Implement. The helper may use `pgx`/`mysql` directly for `CREATE DATABASE`/`DROP DATABASE` (add the drivers now; Task 3 and 9 reuse them). Database names are `waypoint_test_<8 random hex>`.
- [ ] **Step 3:** `make test-servers`: starts `postgres:17` and `mysql:8.4` on free ports with a throwaway password (`docker run --rm -d`), waits until ready, exports both variables, runs `go test ./... -race -count=1`, and stops the containers even on failure. If Docker is missing it prints so and exits 1.
- [ ] **Step 4:** Run `go test ./internal/storage/servertest` (skips cleanly) and `make test-servers` (runs, though no server test exists yet). Expected: PASS.
- [ ] **Step 5: Commit** `test: docker test servers and helpers`.

### Task 3: Postgres repository, schema, Load and Save

**Files:**
- Create: `internal/storage/postgres/{postgres,migrate}.go`, `internal/storage/postgres/migrations/001_init.sql`, `postgres_test.go`
- Modify: `internal/storage/sqlstore/store.go` (Dialect), `internal/storage/sqlstore/save.go` (call `ValidText`), `internal/storage/sqlite/sqlite.go` (dialect gets `ValidText`), `internal/storage/storagetest/contract.go` if a hook is needed

**Interfaces:**
- Consumes: Task 1 helpers, Task 2 `servertest.Postgres`, `sqlstore.Store`.
- Produces:
  - `sqlstore.Dialect` gains `ValidText(s string) error`; `Store.Save` calls it on every string it binds and wraps the error with the card id or field name. SQLite returns nil.
  - `func New(ref config.ProjectRef) *Repo` (password read with `dbconn.Password(os.Getenv, ref.Name)`; nothing is connected until a call)
  - `func Create(ctx context.Context, ref config.ProjectRef, s domain.Settings, createDatabase bool) (*Repo, error)`: optionally `CREATE DATABASE`, requires the schema to be empty, creates the six tables in one transaction, saves the empty board. Any failure leaves nothing.
  - `func (r *Repo) Load(ctx) (*domain.Board, []domain.CardID, error)`, `Save`, `Path() string` (returns `host:port/database`, no user or password), `Revision(ctx) (int64, error)`, `Discard(ctx) error` (drops only the six Waypoint tables), `Probe(ctx, ref) (ProbeResult, error)` with `ProbeResult{Reachable, DatabaseExists, Empty, HasBoard bool}`.
  - Tables as SQLite plus `waypoint_meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)` rows `schema_version` and `retired`; integers are `BIGINT`.
  - Validation on open: refuse missing Waypoint tables, foreign tables, a newer schema version, a retired database (message includes the SQL to clear it).

- [ ] **Step 1: Write failing tests.** Contract run: `TestContract` passing `servertest.Postgres` through `storagetest` (Load/Save round trips, conflicts). Also `TestCreateRefusesNonEmpty` (a pre-existing table makes `Create` return `domain.ErrInvalid` naming it, and the table is untouched), `TestCreateFailureLeavesNothing` (force a failure after the first table via a seam and assert no tables remain), `TestCreateDatabase` (the `createDatabase` flag makes a missing database; without it the error names the database), `TestOpenRefusesForeignDatabase`, `TestRefusesNewerSchema`, `TestRefusesRetired` (message contains `waypoint_meta`), `TestRejectsNUL` (title, body, field key, column name each give `ErrInvalid` naming the card/field and save nothing), `TestOddPassword` (password `p@ss:w/rd?#% '"`, db `1-odd-name`: connects and round-trips), `TestPasswordNeverInErrors` (wrong password `s3cr3t-marker` set in env; call Load, Create, Probe; assert the marker is absent from every returned error string), `TestRoundTripHardBoard` (1 MB body, emoji, `\r\n` normalised the same way as SQLite, case-only column names `Todo`/`todo` both kept).
- [ ] **Step 2:** Run `make test-servers` or `WAYPOINT_TEST_POSTGRES=... go test ./internal/storage/postgres`. Expected: FAIL (package missing).
- [ ] **Step 3: Implement.** Dialect `Rebind` turns each `?` into `$n`. Build the connection with `pgx.ConnConfig` fields (host, port, user, database, password, TLS mode), never a string with the password; run through `dbconn.Scrub` on every returned error (secrets: the password). Open with `sql.OpenDB(stdlib.GetConnector(cfg))`. `lock_timeout` and `statement_timeout` are not set here (Task 4). Apply the connect timeout from Global Constraints.
- [ ] **Step 4:** Run the tests with a server. Expected: PASS. Run `go test ./...` without one. Expected: PASS with skips.
- [ ] **Step 5: Commit** `feat: postgres repository`.

### Task 4: Postgres lock, retire and recovery

**Files:**
- Create: `internal/storage/postgres/lock.go`, `internal/storage/postgres/race_test.go`, `internal/storage/postgres/export_test.go`
- Modify: `internal/storage/postgres/postgres.go`

**Interfaces:**
- Consumes: Task 3 `Repo`.
- Produces: `func (r *Repo) Lock(ctx) (unlock func() error, err error)`, `func (r *Repo) Retire(ctx) error`, `var lockWaitLimit = 30 * time.Second` with `SetLockWaitLimit(d)` exported to tests, as in SQLite.

- [ ] **Step 1: Write failing tests:** `TestLockSerialisesWriters` (two goroutines, 50 saves each under `Lock`; none lost), `TestLockCapReturnsBusy` (a second `Lock` with a 200 ms cap while the first is held returns an error `errors.Is(err, app.ErrBusy)` or the equivalent code `busy`; the test hangs if the cap is removed), `TestRetiredWhileWaiting` (a writer blocked in `Lock` while `Retire` runs fails with the retired message, nothing saved), `TestChangedDuringWrite` (a save from a stale revision gives `ErrChangedDuringWrite`), `TestRecoversAfterConnectionKilled` (terminate the backend with `pg_terminate_backend` from another connection; the next `Load` succeeds), `TestUnlockOnContextCancel`.
- [ ] **Step 2:** Run with a server. Expected: FAIL.
- [ ] **Step 3: Implement** `Lock`: begin a transaction on a pinned connection, `SET LOCAL lock_timeout = '25s'`, `SELECT revision FROM project WHERE id = 1 FOR UPDATE`, re-read `waypoint_meta.retired`, then keep the transaction open until unlock; `Load`/`Save` inside use that connection and a SAVEPOINT. `Retire` sets `retired='true'`. Map lock-timeout and the 30 s context cap to the existing busy error. A connection error from a stale pooled connection is retried once on a fresh connection when no lock is held (this is what makes recovery work); never retry a write.
- [ ] **Step 4:** Run with `-race` and a server. Expected: PASS. Mutation check: set the cap to a huge value and confirm `TestLockCapReturnsBusy` fails, then restore.
- [ ] **Step 5: Commit** `feat: postgres locking, retire and reconnect`.

### Task 5: Setup from the CLI (Postgres): init, project add, adopt, warning

**Files:**
- Modify: `internal/storage/open.go`, `internal/projects/projects.go`, `internal/cli/project.go`, `internal/cli/cli.go` (flags), `internal/cli/agents.go` (snippet mentions the env variable), plus tests in `internal/projects`, `internal/cli`, `internal/storage`
- Fix code that assumes a file path: `projects.Validate`, `projects.Inspect`, `projects.RegisteredAt`, `project ls` and `project rm` output, JSON output (`projectJSON` gains `host`, `port`, `database`, `user`; `path` omitted for servers).

**Interfaces:**
- Consumes: Tasks 1, 3, 4.
- Produces:
  - `storage.Open(ref)` and `storage.Create(ref, s)` handle `postgres` (password from `os.Getenv`). `Create` for a server takes the create-database choice through a new `storage.CreateOptions{CreateDatabase bool}` argument (`Create(ref, s, opts)`; update callers).
  - `projects.AddOptions` gains `Server *ServerOptions` where `ServerOptions{Engine, Host string; Port int; User, Database, SSL string; CreateDatabase bool}` and a `Warn func(string)` hook (called with `dbconn.NonLocalWarning` for a non-local host, before connecting).
  - CLI: `waypoint init --storage postgres --host H --port N --user U --database D --ssl MODE --create-database` and the same on `project add`. Defaults: host `localhost`, port 5432, database = `slug(project name)`, `--user` required.

- [ ] **Step 1: Write failing tests:** `TestAddPostgresNewBoard` (registers, creates the board, entry has no `Path` and no password anywhere in the config bytes), `TestAddAdoptsExistingBoard`, `TestAddRefusesForeignDatabase` (non-empty, no Waypoint board: refused, untouched), `TestAddRefusesRetired`, `TestAddUnreachableDoesNotRegister` (port with nothing listening, returns within 12 s, config unchanged, message names host and port), `TestAddMissingPasswordMessage` (server wants a password, variable unset: the error contains `WAYPOINT_DB_PASSWORD_<NAME>` exactly), `TestAddWarnsOnceNonLocal` (host `203.0.113.1` fails to connect but `Warn` was called first; `localhost` never calls it), `TestAddRefusesCollidingPasswordVar` (Review Focus 2), `TestDisableSSLNonLocalNote`, CLI tests `TestInitPostgresFlags`, `TestProjectLsShowsServer`, `TestProjectRmKeepsDatabase`, `TestNoPasswordFlag` (`--password` is an unknown flag), `TestPasswordNotInOutput` for `--json` and human output.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3: Implement.** In `projects.Add`: validate and default the server options, call `Warn`, `Probe`, then create or adopt under the config lock exactly as the file path flow does (create/open before the config write, rollback with `Discard` if the config write fails after a create). `SSL` empty becomes `dbconn.DefaultSSL(host)`. Messages: unreachable, wrong password, missing database (suggest `--create-database`), missing privilege.
- [ ] **Step 4:** Run `go test ./... -race` with and without a server. Expected: PASS.
- [ ] **Step 5: Commit** `feat: set up projects on postgres from the cli`.

### Task 6: Conversion between any two storages

**Files:**
- Modify: `internal/projects/convert.go`, `internal/cli/project.go` (convert flags), `internal/http/projects.go` (call site), tests in `internal/projects`
- Test: `internal/projects/convert_server_test.go`

**Interfaces:**
- Consumes: Tasks 3-5.
- Produces: `type Target struct{ Storage, Path string; Server *ServerOptions }` and `func Convert(ctx, cfgPath string, env func(string) string, name string, to Target) (ConvertResult, error)`. `ConvertResult` gains `Retired string` (the server source that was marked retired, else empty). Every existing call site moves to `Target{Storage: to, Path: newPath}`. CLI: `project convert NAME --to postgres|sqlite|markdown [--path FILE | --host --port --user --database --ssl --create-database]`.

- [ ] **Step 1: Write failing tests:** `TestConvertSQLiteToPostgresAndBack`, `TestConvertMarkdownToPostgres`, `TestConvertPostgresToMarkdown` (and bodies normalised as today), `TestConvertTargetMustBeEmpty` (non-empty or retired database refused, untouched), `TestConvertFromServerRetiresAndKeeps` (source database still holds all rows, flagged retired, `Backups` empty, nothing dropped), `TestConvertVerifyFailureCleansUp` (a seam makes the copy differ: tables created by the run are dropped, registry unchanged, source unlocked), `TestConvertWritersDuringConversion` (8 writers hammering the source via `Lock`/`Save`, none of the acknowledged writes lost; mutation: disable the retire re-check and see it fail), `TestConvertCrashSteps` (`afterCopy` and `afterRegistry` seams: registry always points at a loadable board), `TestRetireFailsAfterSwitch` (conversion reported done, message names the stale source and the statement to run), `TestConvertHardBoards` (Review Focus 5: 6,000 cards, 1 MB body, emoji, quotes in both directions with a deep equality check), `TestConvertNULToPostgresNamesCard`, `TestConvertPasswordNeverInOutput`.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3: Implement.** `writeCopy` for a server target calls `storage.Create`; `removeBoardFiles` becomes `discard(ref)` which calls `Discard` for servers and the file removal for files. The source side: files are backed up as now; servers use the existing `retirer` path and skip `backUp`. Keep the lock-until-switched order. The "target changed while converting" check compares the whole source entry.
- [ ] **Step 4:** Run `go test ./internal/projects ./internal/cli -race` with a server. Expected: PASS.
- [ ] **Step 5: Commit** `feat: convert between any two storages`.

### Task 7: Watching server projects

**Files:**
- Modify: `internal/watch/watch.go`, `internal/cli/serve.go:136-155`; tests in `internal/watch`, `internal/cli`

**Interfaces:**
- Consumes: `Repo.Revision`.
- Produces: `watch.Target` gains `Revision func(ctx context.Context) (int64, error)`; when set, `Path` is ignored and the watcher polls it every `ServerPoll = 2 * time.Second` (an `Options.ServerPoll` override for tests), publishing the project when the value changes. Errors: log once at the first failure of an outage (message never contains the password), back off up to 30 s, log recovery once, publish after recovery.

- [ ] **Step 1: Write failing tests** with a fake `Revision`: `TestServerTargetPublishesOnChange`, `TestServerTargetErrorLoggedOnce` (20 failing polls produce one log line), `TestServerTargetRecovers`, `TestServerTargetMissingPasswordLoggedOnce`, `TestRemovedProjectStopsPolling`, and a server test `TestServeWatchesPostgres` (a save from another process publishes within 5 s).
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3: Implement** the polling goroutine per target inside the existing watcher; `watchTargets` in `serve.go` builds the `Revision` closure from a repo kept open for the process (one small pool per project, replaced when the registry entry changes, closed on shutdown).
- [ ] **Step 4:** Run `go test ./internal/watch ./internal/cli -race`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: web ui follows server projects`.

### Task 8: HTTP and web UI for server projects

**Files:**
- Modify: `internal/http/projects.go`, `internal/http/api.go` (route), `web/src/lib/{api,types}.ts`, `web/src/components/{NewProjectDialog,ProjectSettings}.svelte`; tests `internal/http/*_test.go`, `web/tests/*.test.ts`, `web/e2e/owner.spec.ts`
- Create: `web/src/components/ServerFields.svelte`

**Interfaces:**
- Consumes: Tasks 5 and 6.
- Produces: `POST /api/db/test` with `{engine, host, port, user, database, ssl, project}` (no password field; an unknown key is a 422) returning `{reachable, non_local, database_exists, empty, has_board, password_var, error}`. Create-project and convert endpoints accept the same server fields. Project JSON gains `host`, `port`, `database`, `user`, `ssl` and omits `path` for servers. `ServerFields.svelte` renders engine, host, port, user, database, SSL, "create the database", the exact password variable name, a Test connection button and, for non-local, the warning with a required "I understand" tick.

- [ ] **Step 1: Write failing tests:** Go `TestDBTestEndpointNoPasswordField`, `TestDBTestNonLocalFlag`, `TestCreateServerProject`, `TestConvertToPostgresOverHTTP`, `TestResponsesNeverContainPassword` (marker password set, failure paths included), `TestUnreachableServerProjectListsWithError` (Review Focus 3: `GET /api/projects` returns within the connect timeout, the broken project carries an `error`, others are fine). Vitest: dialog shows the variable name, shows the warning only for non-local hosts, Create disabled until Test connection passes and the warning is ticked, no password input exists. e2e (skipped without `WAYPOINT_TEST_POSTGRES`): create a Postgres project, add a card, convert to SQLite and back.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3: Implement.** Add `unreachable`/`needs_password` as error codes in `internal/http/errors.go` (status 503 for unreachable, 422 for the missing variable).
- [ ] **Step 4:** Run `go test ./internal/http -race`, `cd web && npm test && npx svelte-check`, `make e2e` (with a server). Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): postgres projects in the ui`.

*Midpoint review here (fresh binary-level review of Tasks 1-8, with a password-leak audit). Fix findings before MySQL.*

### Task 9: MySQL repository

**Files:**
- Create: `internal/storage/mysql/{mysql,lock,migrate}.go`, `migrations/001_init.sql`, tests `mysql_test.go`, `race_test.go`
- Modify: `internal/storage/servertest` (if MySQL needs setup details)

**Interfaces:**
- Consumes: the same `Repo` shape as Task 3 and 4: `New`, `Create(ctx, ref, s, createDatabase)`, `Load`, `Save`, `Lock`, `Retire`, `Revision`, `Discard`, `Probe`, `Path`.
- Produces: the schema with `utf8mb4` and `utf8mb4_bin` on `id`, `column_name`, `group_name`, `field_key`, `name` columns (`VARCHAR(255)`, so a name longer than 255 characters fails conversion naming it), bodies `LONGTEXT`, `waypoint_meta`; `Rebind` leaves `?`; `ValidText` returns nil; `Lock` uses `START TRANSACTION`, `SELECT ... FOR UPDATE`, `innodb_lock_wait_timeout` below 30 s. A failed `Create` drops the tables it made (MySQL DDL is not transactional).

- [ ] **Step 1: Write failing tests:** the same set as Tasks 3 and 4 against `servertest.MySQL`, minus `TestRejectsNUL` (MySQL stores NUL: add `TestStoresNULRoundTrip`), plus `TestCaseSensitiveKeys` (`Todo` and `todo` columns and field keys `Key`/`key` stay distinct), `TestTrailingSpacesKept` (a title and column name with trailing spaces round-trip), `TestLongNameRefused` (256-character column name fails with `ErrInvalid` naming it), `TestMultibyteKeysEmoji`.
- [ ] **Step 2:** Run with a MySQL server. Expected: FAIL.
- [ ] **Step 3: Implement** with `mysql.Config` struct fields (never a DSN string with the password), `parseTime` off, `loc` irrelevant, `multiStatements` off, `tls` from `ssl`; scrub errors with `dbconn.Scrub`.
- [ ] **Step 4:** Run with a server and `-race`. Expected: PASS. Mutation check: drop the binary collation and confirm `TestCaseSensitiveKeys` fails, then restore.
- [ ] **Step 5: Commit** `feat: mysql repository`.

### Task 10: MySQL setup, conversion and UI

**Files:**
- Modify: `internal/storage/open.go`, `internal/projects/{projects,convert}.go`, `internal/cli/project.go`, `internal/http/projects.go`, `web/src/components/ServerFields.svelte`; tests

**Interfaces:**
- Consumes: Tasks 5-9. Produces: `mysql` accepted wherever `postgres` is (engine lists, flags, validation, default port 3306, adopt, probe).

- [ ] **Step 1: Write failing tests:** parameterise the Task 5 setup tests, the Task 6 conversion tests (including writers during conversion and hard boards) and the Task 7 watcher test over both engines; add `TestConvertMySQLToPostgres` (server to server in both directions) and a Vitest test that the engine choice changes the default port.
- [ ] **Step 2:** Run. Expected: FAIL for `mysql`.
- [ ] **Step 3: Implement** the wiring; add no new logic where the engines share behaviour.
- [ ] **Step 4:** Run `make test-servers`, `cd web && npm test`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: mysql setup, conversion and ui`.

### Task 11: Docs and final review

**Files:**
- Modify: `README.md` (Choosing a storage, a "Using a database server" section: Docker example, the password variable, the warning, TLS, converting; MCP example with `env`), `docs/PROGRESS.md`, `internal/cli/agents.go` (snippet), `Makefile` help text

- [ ] **Step 1:** Write the README section with a copy-paste local Docker run for Postgres and MySQL, the exact `WAYPOINT_DB_PASSWORD_<NAME>` rule with an example for each shell, a note that the variable must be set for `waypoint serve` and in an agent's MCP config `env`, the one-board-per-database and not-for-sharing limits, and the recovery steps (clear the retired flag, stale source after a failed retire).
- [ ] **Step 2:** Update `docs/PROGRESS.md`: Plan 4 state, known limits from the spec, next steps.
- [ ] **Step 3:** Run everything: `go vet ./...`, `make test-servers`, `cd web && npm test && npx svelte-check`, `make e2e` with a server.
- [ ] **Step 4:** Dispatch a fresh whole-branch review of Plan 4 changes (binary-level: build, run against real Postgres and MySQL containers, attack locking, conversion and the password). Fix Critical and Important findings, then re-run Step 3.
- [ ] **Step 5:** Update the PR body (or open the Plan 4 PR against `feat/sqlite`'s successor) and commit `docs: plan 4 state, readme and known limits`.
