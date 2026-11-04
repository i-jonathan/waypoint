# Waypoint Web (Plan 2 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `waypoint` (no args) starts a local HTTP/2 server and opens the board UI in the "night
map" design: board and swimlanes, drag with keyboard alternatives, card panel, filter, archive,
settings with per-project overrides. Open boards refresh live when the file changes on disk.

**Architecture:** This plan adds three adapters and a frontend on top of Plan 1's `app.Service`:
- `internal/http`: JSON API, SSE broker, security middleware, embedded UI;
- `internal/watch`: an fsnotify watcher feeding the broker;
- `internal/server`: TLS, protocols and listener.

Settings edits are one new domain op, `ApplySettings`. The frontend is a Svelte 5 single-page app
in `web/`. Vite builds it into `internal/http/dist`, which `go:embed` picks up. The server is the
single source of truth: the UI sends operations (move, patch, check), never whole boards, and
refetches on SSE `changed`.

**Tech Stack:**
- **Backend:** Go 1.27 (`net/http` with `http.Protocols`), `github.com/fsnotify/fsnotify`, and
  stdlib `crypto/x509` for the local CA.
- **Frontend:** versions checked on 2026-11-03, use `^` ranges.

| Package | Version |
|---|---|
| svelte | 5.57 |
| vite | 8.3 |
| @sveltejs/vite-plugin-svelte | 7.3 |
| typescript | ^6 (svelte-check 4.7 peer range is `^5 \|\| ^6`, so not 7) |
| svelte-check | 4.7 |
| vitest | 5.1 |
| @testing-library/svelte | 5.4 |
| jsdom | 30 |
| @playwright/test | 1.64 |
| marked | 18 |
| dompurify | 3.4 |
| phosphor-svelte | 3.1 |
| @fontsource-variable/archivo | 5.3 |
| @fontsource-variable/geist | 5.3 |
| @fontsource-variable/geist-mono | 5.3 |

  There's no CSS framework, no router library and no state library. Svelte runes in `.svelte.ts`
  modules cover state.

**Spec:** `docs/superpowers/specs/2026-11-03-waypoint-design.md` (§4 UI and design language, §5.3
live updates, §6 config and overrides, §7). The visual reference is `docs/design/design-language.html`.

**Depends on:** Plan 1 (`docs/superpowers/plans/2026-11-03-waypoint-core.md`) merged. The names used
here (`app.Service`, `app.CardView`, `app.BoardView`, `app.ConflictError`, `domain.*`,
`config.Config`, `storage.Open`) are defined there.

**Carried over from Plan 1's final review:**
- Add JSON tags to `app.BoardView` and `domain.Settings`, and move `errorCode` out of `internal/cli`
  so the HTTP layer shares it (Task 4).
- Registry writes go through `config.Update`. Validate registry entries on load, and keep a symlinked
  config file a symlink on save (Task 4).
- "When a project opens" for auto-archive means the server loading a board; CLI reads never archive.
- Dates arrive as UTC timestamps or date-only values. The UI shows the local date, and date-only
  values verbatim.

**Review process (owner's decision):** by risk. No re-review of small fixes unless they touch saving,
locking or conflict handling. Tasks 4, 5, 8, 10 and 11 get their own review; the rest are reviewed
in groups. A whole-branch review through the real binary runs after Task 5 and at the end, each
with a re-review.

## Global Constraints

- **HTTP/2:** the server sets `http.Protocols` with HTTP1, HTTP2 and UnencryptedHTTP2 all enabled.
  Browsers only negotiate HTTP/2 over TLS, so:
  - **`tls: auto`** (the default config key) serves HTTPS on `127.0.0.1:<port>` with a
    Waypoint-generated local CA. HTTP/2 is negotiated through ALPN.
  - **Trust:** until the user runs `waypoint trust` once, the CA isn't trusted. In that state,
    `auto` serves plain HTTP (HTTP/1.1, plus h2c for non-browser clients) and prints exactly once
    per run:
    `Tip: run "waypoint trust" once to serve over HTTPS with HTTP/2.`
    This keeps the zero-setup rule.
  - **`tls: on`** always serves HTTPS, and **`tls: off`** never does.
- **Bind:** `127.0.0.1` only. Requests whose `Host` isn't `localhost:<port>` or `127.0.0.1:<port>`
  get a 421 (DNS-rebinding guard). Mutating methods require the header `X-Waypoint: 1`, otherwise
  403. Browsers can't send a custom header cross-origin without a preflight, and the server never
  answers preflights.
- **One SSE connection per tab** (`/api/events`) carries events for all projects. That matters
  because HTTP/1.1 caps a browser at 6 connections per origin.
- **API errors** use the CLI's JSON shape: `{"error","code","line"?}`.

| Code | HTTP status |
|---|---|
| `not_found` | 404 |
| `conflict` | 409, with the body adding `"current": CardView` |
| `parse` | 423 (board read-only until fixed) |
| `move_not_allowed`, `wip_limit`, `invalid`, `unknown_*` | 422 |

- **Design tokens** are copied from `docs/design/design-language.html`:

| Token | Dark | Light |
|---|---|---|
| bg | `#0f1714` | `#e9eee8` |
| card | `#18241f` | `#fbfcfa` |
| line | `#2b3b34` | `#cdd6cd` |
| text | `#e9eee6` | `#16201b` |
| muted | `#97a79e` | `#56645c` |
| edge (card ink) | `#aebdb3` | `#16201b` |
| red | `#ff6b5b` | `#c8321e` |

  - Radii: controls 10px, cards 12px, column signs 7px, chips 4px.
  - The card edge is a `1.5px` border plus a `3px 3px 0` shadow in the edge colour.
  - The next-up card uses the accent for its border and a `4px 4px 0` shadow, rotated by `-0.6deg`.
- **Accent presets** (`dark` / `light`):

| Preset | Dark | Light |
|---|---|---|
| orange | `#ff7a3d` | `#e8601c` |
| cobalt | `#7d9bff` | `#2f5bff` |
| teal | `#2dd4bf` | `#0d9488` |
| rose | `#ff7a9a` | `#d0365e` |
| green | `#6ee7a0` | `#15803d` |
| amber | `#f5c451` | `#a16207` |
| sky | `#7cc8ff` | `#0369a1` |
| violet | `#b89cff` | `#6d28d9` |

  A custom `#rrggbb` value is used for both themes. The text colour on top of an accent is computed
  (`#0f1714` or `#ffffff`, whichever has the higher contrast) and must reach 4.5:1.
- **Built-in appearance defaults:** theme `system`, accent `orange`, density `comfortable`
  (alternative: `compact`), swimlane `none` (alternatives: `group`, `priority`), textures `on`.
- **Type:**
  - Archivo variable (width axis about 68–75%, weight 800, uppercase) **only** for the logo, column
    signs and the NEXT tag.
  - Geist for everything else.
  - Geist Mono for ids, counts, priority and filter syntax.
  - Sizes 11, 12, 13, 14, 15 and 22px.
- **Icons:** Phosphor only, never emoji. Icon-only buttons have an `aria-label` and a `title`.
- **Copy:** no em dash or en dash characters in any UI string. The groups sidebar heading reads
  "Routes" with `aria-label="Groups"` and `title="Groups"`; everywhere else the word is "group".
- **Motion:** 120–180ms, only for card lift and drop, the panel slide and swimlane collapse. All of
  it is off under `prefers-reduced-motion: reduce`.
- **Performance budget:** with 300 cards, the board renders within 300ms of the board response,
  and a drag frame takes no more than 16ms. Checked in Task 15.

## Review Focus

1. **The file goes unparseable while the board is open** (someone saves a half-edited file). Expect
   the board to stay visible, read-only, with a banner showing `line N: <msg>`, and to recover by
   itself on the next good save. Test in Task 4 (API 423) and Task 15 (e2e).
2. **A card edited externally while the panel has unsaved text.** The SSE refetch must not replace
   the draft. Saving gives a 409 conflict dialog with both versions. Test in Task 11.
3. **A rapid double drop, or a drag onto a column at its WIP limit.** Expect the optimistic move to
   roll back, the card back in its original position, and a toast with the server's message. Test
   in Task 10.
4. **A registered project whose file was deleted or moved.** Expect the project switcher to show it
   with "File not found at PATH", the other projects to keep working, and no server crash. Test in
   Task 4.
5. **The browser tab left open across a server restart.** Expect SSE to reconnect with backoff and
   a "Reconnecting" indicator, then a refetch on reconnect. Test in Task 9.

---

## File Structure

```
internal/domain/settings.go           ApplySettings (column/group renames, removal rules)
internal/config/appearance.go         Resolve(global, project) → ResolvedAppearance
internal/config/config.go             (modify) add TLS string `yaml:"tls"` default "auto"
internal/http/api.go                  Handler(deps) http.Handler; routes
internal/http/errors.go               writeError: domain/app errors → status + JSON
internal/http/middleware.go           host check, X-Waypoint check, logging
internal/http/events.go               Broker (pub/sub) + /api/events SSE handler
internal/http/static.go               embedded dist, SPA fallback to index.html
internal/http/dist/placeholder.html   committed; served when the UI wasn't built
internal/watch/watch.go               Watcher: fsnotify on project dirs → Broker
internal/server/tls.go                local CA + leaf cert in data_dir/tls; Trusted() bool
internal/server/server.go             Run(ctx, cfg, handler) with Protocols and TLS mode
internal/cli/serve.go                 serve [--port] [--no-open]; default command
internal/cli/trust.go                 trust (installs CA in OS trust store)
web/package.json, vite.config.ts, tsconfig.json, svelte.config.js, playwright.config.ts
web/index.html
web/src/main.ts, App.svelte
web/src/styles/tokens.css, base.css
web/src/lib/api.ts                    typed client, X-Waypoint header, ApiError
web/src/lib/types.ts                  mirrors CardView/BoardView/Settings JSON
web/src/lib/router.svelte.ts          History API router: /, /p/:project, /p/:project/:view, /p/:project/card/:id, /settings, /p/:project/settings
web/src/lib/board.svelte.ts           board store: load, optimistic ops, rollback, refetch
web/src/lib/events.svelte.ts          SSE subscription with backoff
web/src/lib/accent.ts                 presets, resolveAccent(theme, value), accentInk(hex)
web/src/lib/keys.ts                   global shortcuts
web/src/lib/dnd.ts                    pointer-event drag controller
web/src/components/Sidebar.svelte, TopBar.svelte, Board.svelte, Column.svelte, Card.svelte,
  Swimlanes.svelte, CardPanel.svelte, MarkdownBody.svelte, MoveMenu.svelte, QuickAdd.svelte,
  Toast.svelte, ParseBanner.svelte, ConflictDialog.svelte, ArchiveView.svelte, ListView.svelte,
  GlobalSettings.svelte, ProjectSettings.svelte, ColumnsEditor.svelte, NewProjectDialog.svelte
web/tests/*.test.ts                   Vitest component and unit tests
web/e2e/*.spec.ts                     Playwright against the built binary
```

---

### Task 1: Domain `ApplySettings`

**Files:**
- Create: `internal/domain/settings.go`
- Test: `internal/domain/settings_test.go`
- Modify: `internal/app/service.go` (add `UpdateSettings`)

**Interfaces:**
- Produces:
```go
type SettingsChange struct {
    Settings domain.Settings
    ColumnRenames map[string]string // old → new
    GroupRenames  map[string]string
}
func (b *Board) ApplySettings(ch SettingsChange) error
func (s *app.Service) UpdateSettings(ctx context.Context, ch domain.SettingsChange) (app.BoardView, error)
```
- **Rules:**
  - `Prefix` and `NextID` can't change: the incoming values are ignored and the current ones kept.
  - Renames rewrite `Card.Column` / `Card.Group` on both board and archived cards.
  - A column that's absent and not renamed must have no cards, otherwise
    `ErrInvalid: column "X" still has 3 cards`.
  - A group that's absent and not renamed is cleared from its cards.
  - The result must pass `Validate`.
  - Card ranks within each column are kept. When columns are reordered, `Cards` is re-sorted
    stably by the new column order.

- [ ] **Step 1: Write the failing tests:**
  - `TestRenameColumnRewritesCards`
  - `TestRemoveNonEmptyColumnRefused`
  - `TestRemoveEmptyColumn`
  - `TestReorderColumnsKeepsRanks`
  - `TestRenameGroup`
  - `TestRemoveGroupClearsCards`
  - `TestPrefixImmutable`
  - `TestInvalidSettingsRejected` (moves_to pointing at a removed column gives `ErrInvalid` and
    leaves the board unchanged)
  - `TestServiceUpdateSettingsPersists` (in `internal/app`)
- [ ] **Step 2:** Run `go test ./internal/domain/ ./internal/app/ -run Settings`. Expect a FAIL.
- [ ] **Step 3:** Implement it on a copy of the board, swapping it in only on success.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(domain): apply settings with column and group renames`.

---

### Task 2: Appearance resolution and TLS config key

**Files:**
- Create: `internal/config/appearance.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/appearance_test.go`

**Interfaces:**
- Produces:
```go
type ResolvedAppearance struct {
    Theme, Accent, Density, Swimlane, Textures string
    Source map[string]string `json:"source"` // field → "project" | "global" | "default"
}
func Resolve(global, project domain.Appearance) ResolvedAppearance
func ValidateAppearance(a domain.Appearance) error
    // theme ∈ {"",system,light,dark}; accent ∈ {"", 8 preset names, #rrggbb}; density ∈ {"",comfortable,compact};
    // swimlane ∈ {"",none,group,priority}; textures ∈ {"",on,off}; else domain.ErrInvalid
```
- `Config` gets the field `TLS string \`yaml:"tls"\``, defaulting to `"auto"` (accepted values:
  `auto`, `on`, `off`).

- [ ] **Step 1: Write the failing tests:**
  - `TestResolvePrecedence`: project accent `cobalt` and global accent `teal` give `cobalt` with
    source `project`. An unset theme everywhere gives `system` with source `default`.
  - `TestValidateAppearance`
  - `TestTLSDefault`
- [ ] **Step 2:** Run `go test ./internal/config/`. Expect a FAIL.
- [ ] **Step 3:** Implement. Also call `ValidateAppearance` from `Settings.Validate`'s caller in
  `UpdateSettings`, since domain can't import config, and from the global-settings API (Task 4).
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(config): appearance resolution and tls setting`.

---

### Task 3: Event broker and file watcher

**Files:**
- Create: `internal/http/events.go`, `internal/watch/watch.go`
- Test: `internal/http/events_test.go`, `internal/watch/watch_test.go`

**Interfaces:**
- Produces:
```go
package http
type Event struct { Type string `json:"type"`; Project string `json:"project"` } // Type: "changed" | "projects"
type Broker struct{}
func NewBroker() *Broker
func (b *Broker) Publish(e Event)
func (b *Broker) Subscribe() (ch <-chan Event, cancel func()) // buffered 16; a slow subscriber drops events, never blocks Publish
func (b *Broker) ServeSSE(w http.ResponseWriter, r *http.Request)
    // headers: text/event-stream, no-cache; writes "event: <type>\ndata: <json>\n\n"; ": ping\n\n" every 25s; flushes each write

package watch
type Target struct { Project, Path string }
func Run(ctx context.Context, targets func() []Target, publish func(project string)) error
    // watches each target's directory (not the file: atomic renames replace inodes);
    // events whose base name is the board file, its .archive.md, or (sqlite, Plan 3) its -wal file
    // map to the project; debounce 150ms per project; re-reads targets() on a 2s tick to pick up
    // registry changes
```

- [ ] **Step 1: Write the failing tests:**
  - `TestBrokerFanOut`: two subscribers both receive the event.
  - `TestBrokerSlowSubscriberDoesNotBlock`: 100 publishes to an unread subscriber return within
    50ms.
  - `TestSSEFormat`: uses `httptest`, publishes, and reads `event: changed\ndata: {"type":"changed","project":"Demo"}\n\n`.
  - `TestWatchAtomicRename`: write a temp file and rename it over the board, which publishes once
    within 500ms.
  - `TestWatchDebounce`: 5 writes in 50ms give one publish.
  - `TestWatchIgnoresOtherFiles`.
- [ ] **Step 2:** Run `go test ./internal/http/ ./internal/watch/`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(http,watch): SSE broker and debounced file watcher`.

---

### Task 4: JSON API and middleware

**Files:**
- Create: `internal/http/api.go`, `internal/http/errors.go`, `internal/http/middleware.go`
- Test: `internal/http/api_test.go`

**Interfaces:**
- Consumes: `app.Service`, `config.Config` (with a path for saving), `storage.Open`/`Create`,
  `Broker`, and `config.Resolve`.
- Produces: `func Handler(d Deps) http.Handler`, where
  `type Deps struct { ConfigPath string; Now app.Clock; Broker *Broker; Port int; UI fs.FS }`.
  The handler re-reads the config per request (it's a small file, and CLI edits must show up).
- **Routes** (`{p}` is the project name, URL-escaped):

| Method and path | Body | Response |
|---|---|---|
| `GET /api/projects` | | `[{name,storage,path,accent,error?}]`. `error` is `"File not found at PATH"` or a parse message (Review Focus 4). |
| `POST /api/projects` | `{name,path?,prefix?}` | Same semantics as `waypoint project add`. Publishes `projects`. |
| `DELETE /api/projects/{p}` | | Unregisters only. Publishes `projects`. |
| `GET /api/settings` / `PUT /api/settings` | `{port,open_browser,tls,appearance}` | Global config. A port or TLS change takes effect on restart, and the response includes `"restart_required":true`. |
| `GET /api/projects/{p}/board?q=` | | `{board: BoardView (cards filtered by q), appearance: ResolvedAppearance, readonly: false}`. A parse error gives 423 with `line`. |
| `GET /api/projects/{p}/archive?q=` | | `[]CardView` (archived only) |
| `POST /api/projects/{p}/cards` | `NewCard` | `CardView` (201) |
| `PATCH /api/projects/{p}/cards/{id}` | `{title?,group?,priority?,depends?,body?,set?,if_match?}` | `CardView`, or 409 with `current` |
| `POST /api/projects/{p}/cards/{id}/move` | `{column,index}` | `CardView` |
| `POST /api/projects/{p}/cards/{id}/check` | `{n}` | `CardView` |
| `POST /api/projects/{p}/cards/{id}/restore` | `{column?}` | `CardView` |
| `DELETE /api/projects/{p}/cards/{id}` | | 204 |
| `POST /api/projects/{p}/archive` | | `{archived:[ids]}` |
| `PUT /api/projects/{p}/settings` | `{settings,column_renames,group_renames}` | `{board,appearance}` |
| `GET /api/events` | | SSE |

  Every successful write also publishes `changed` immediately, without waiting for the watcher.
- **Middleware order:** host check (421) → `X-Waypoint` check on POST/PUT/PATCH/DELETE (403) →
  handler. Errors pass through `writeError` (statuses from Global Constraints).

- [ ] **Step 1: Write the failing tests** (an `httptest.Server` on a temp config with one project
  and a fixed clock):
  - `TestBoardRoundTrip`: create, move and patch through the API, then GET board reflects the
    changes and the file on disk contains them.
  - `TestFilterQuery`: `?q=p0` returns only P0 cards.
  - `TestConflict409`: the body has `code:"conflict"` and `current.body` equals the external edit.
  - `TestParse423`: corrupt the file, then GET board gives 423 with `line` and POST card gives 423.
  - `TestMissingProjectFile` **(Review Focus 4)**: delete one project's file. `/api/projects` lists
    it with `error` starting `File not found at`, and a second project's board still gives 200.
  - `TestHostCheck`: `Host: evil.example:4747` gives 421.
  - `TestMutationNeedsHeader`: POST without `X-Waypoint` gives 403.
  - `TestWritePublishesChanged`: a subscriber receives `changed` for the project.
  - `TestRuleErrors422`: a WIP-limit move gives 422 `wip_limit` with the domain message.
  - `TestSettingsRestartRequired`.
- [ ] **Step 2:** Run `go test ./internal/http/`. Expect a FAIL.
- [ ] **Step 3:** Implement it with the `net/http` ServeMux method and path patterns. Use
  `encoding/json` with `DisallowUnknownFields` for request bodies.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(http): JSON API with host and CSRF guards`.

---

### Task 5: Server, TLS, HTTP/2, `serve` and `trust`

**Files:**
- Create: `internal/server/tls.go`, `internal/server/server.go`, `internal/http/static.go`,
  `internal/http/dist/placeholder.html`, `internal/cli/serve.go`, `internal/cli/trust.go`
- Modify: `internal/cli/cli.go` (no args runs `serve`), `.gitignore` (ignore
  `internal/http/dist/*` except `placeholder.html`)
- Test: `internal/server/server_test.go`, `internal/server/tls_test.go`, `internal/http/static_test.go`

**Interfaces:**
- Produces:
```go
package server
type Certs struct { Dir string }                       // data_dir/tls
func (c Certs) Ensure(now time.Time) (tls.Certificate, error)
    // creates ca.pem/ca.key (ECDSA P-256, 10 years, CN "Waypoint Local CA") once;
    // leaf for localhost,127.0.0.1,::1 valid 397 days, re-issued when < 30 days remain; files 0600
func (c Certs) Trusted() bool                          // marker file data_dir/tls/trusted exists
func (c Certs) Install() error                         // darwin: `security add-trusted-cert -r trustRoot -k ~/Library/Keychains/login.keychain-db ca.pem`;
                                                       // linux: returns an error listing manual commands (update-ca-certificates, certutil for NSS); writes marker on success
type Options struct { Port int; TLS string; Certs Certs; Handler http.Handler; Ready func(url string) }
func Run(ctx context.Context, o Options) error
    // Protocols{HTTP1, HTTP2, UnencryptedHTTP2}; listen 127.0.0.1:Port;
    // https when TLS=="on" || (TLS=="auto" && Certs.Trusted()); Ready gets "https://localhost:4747/" or "http://localhost:4747/";
    // graceful Shutdown on ctx done (5s)
```
- **`static.go`:** `func UI(dist fs.FS) http.Handler`.
  - It serves files from the embedded `dist`, and paths without an extension fall back to
    `index.html`.
  - If `index.html` is absent, it serves `placeholder.html`, which says
    `The Waypoint UI was not built. Run "make web".`
  - Hashed assets under `/assets/` get `Cache-Control: public, max-age=31536000, immutable`, and
    `index.html` gets `no-cache`.
- **`waypoint serve [--port N] [--no-open]`:**
  - It starts the watcher, broker, API and UI, prints `Waypoint running at <url>`, and opens the
    browser (`open` on darwin, `xdg-open` on linux) unless `--no-open` is passed or
    `open_browser: false` is set.
  - If the port is busy, it exits 1 with `port 4747 is in use; set "port" in CONFIG or pass --port`.
- **`waypoint trust`:** runs `Certs.Ensure`, then `Certs.Install`, then prints
  `Trusted the Waypoint local CA. Restart "waypoint serve" to use HTTPS and HTTP/2.`

- [ ] **Step 1: Write the failing tests:**
  - `TestEnsureCreatesAndReuses`: a second call doesn't regenerate the CA, and a clock moved 380
    days ahead re-issues the leaf with the same CA.
  - `TestServeH2OverTLS`: `Run` with TLS `on` and a client whose root pool contains the CA, with
    `ForceAttemptHTTP2`, gives `resp.ProtoMajor == 2`.
  - `TestServeH2C`: TLS `off`, with a client `Transport{Protocols: UnencryptedHTTP2 only}`, gives
    `ProtoMajor == 2`.
  - `TestServeHTTP1Fallback`: TLS `off` with a plain client gives `ProtoMajor == 1`.
  - `TestAutoUntrustedServesHTTP`.
  - `TestSPAFallback`: `/p/Demo/card/T-1` serves index.html.
  - `TestPlaceholderWhenUnbuilt`.
  - `TestPortInUse`.
- [ ] **Step 2:** Run `go test ./internal/server/ ./internal/http/ -run 'Ensure|Serve|SPA|Placeholder|Port'`.
  Expect a FAIL.
- [ ] **Step 3:** Implement. `Install` isn't unit-tested, since it touches the OS trust store.
  Guard it behind a `var runCommand = exec.Command` seam and test that the right argv is built.
- [ ] **Step 4:** Run the same command, plus `make build && ./bin/waypoint serve --no-open --port 4799`
  in the background, then `curl -s --http2-prior-knowledge -o /dev/null -w '%{http_version}' http://127.0.0.1:4799/api/projects -H 'Host: 127.0.0.1:4799'`.
  Expect PASS, and `2`.
- [ ] **Step 5:** Commit with `feat(server): HTTP/2 server with local TLS, serve and trust commands`.

---

### Task 6: Frontend scaffold, tokens, fonts, build wiring

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/svelte.config.js`,
  `web/index.html`, `web/src/main.ts`, `web/src/App.svelte`, `web/src/styles/tokens.css`,
  `web/src/styles/base.css`, `web/src/lib/accent.ts`
- Modify: `Makefile`: `web` (`cd web && npm ci && npm run build`), `build` depends on `web`,
  `test` adds `cd web && npm test`.
- Test: `web/tests/accent.test.ts`

**Interfaces:**
- Produces:
  - `vite.config.ts`: `build.outDir: '../internal/http/dist'`, `emptyOutDir: true`. The dev server
    on 5173 proxies `/api` to `http://127.0.0.1:4747`, sets the `X-Waypoint` header, and needs SSE
    proxied with `ws:false`.
  - `accent.ts`:
    - `PRESETS: Record<string,{dark:string;light:string}>`, using the Global Constraints table;
    - `resolveAccent(value: string, theme: 'dark'|'light'): string`;
    - `accentInk(hex: string): '#0f1714'|'#ffffff'`;
    - `contrast(a: string, b: string): number` (WCAG relative luminance).
  - `tokens.css`: the theme tokens from Global Constraints, defined on `:root[data-theme=dark]` and
    `:root[data-theme=light]`. `--acc`/`--acc-ink` are set at runtime by `App.svelte` from the
    resolved appearance. `[data-textures=off]` removes the two canvas background images.
    `[data-density=compact]` reduces card padding from 10/12px to 6/10px and the column gap from
    14px to 10px.
  - `base.css`:
    - `@import '@fontsource-variable/archivo/wdth.css'` (if the package lacks a `wdth.css` entry,
      use its full-axes CSS; check `ls node_modules/@fontsource-variable/archivo`);
    - geist and geist-mono;
    - a `.sign` utility (Archivo `font-stretch: 68%`, 800, uppercase);
    - the canvas background (dot grid at 18px plus contour rings) from the reference mockup;
    - a reduced-motion block that sets `transition: none` and `animation: none` on everything.
  - `App.svelte` renders `Waypoint` in `.sign` on the canvas, so the build is visible end to end.

- [ ] **Step 1:** Scaffold with the versions in the Tech Stack table. Scripts:
  - `dev`: `vite`
  - `build`: `svelte-check && vite build`
  - `test`: `vitest run`
  - `e2e`: `playwright test`
- [ ] **Step 2: Write the failing tests**:
  - `accentInk` returns a colour with `contrast >= 4.5` against every preset in both themes, and
    against `#ffff00` and `#000080`.
  - `resolveAccent('cobalt','light') === '#2f5bff'`.
  - `resolveAccent('#123456','dark') === '#123456'`.
  - An unknown value falls back to orange.
- [ ] **Step 3:** Run `cd web && npm test`. Expect a FAIL.
- [ ] **Step 4:** Implement.
- [ ] **Step 5:** Run `npm test`, then `make build && ./bin/waypoint serve --port 4799`. Expect a
  PASS, and the browser shows "WAYPOINT" in condensed Archivo on the dotted, contoured canvas.
- [ ] **Step 6:** Commit with `feat(web): Svelte scaffold, design tokens, fonts, embedded build`.

---

### Task 7: API client, types, router

**Files:**
- Create: `web/src/lib/api.ts`, `web/src/lib/types.ts`, `web/src/lib/router.svelte.ts`
- Test: `web/tests/api.test.ts`, `web/tests/router.test.ts`

**Interfaces:**
- Produces:
  - `types.ts`: `Card`, `CardView`, `BoardView`, `Settings`, `Column`, `Group`, `Appearance`,
    `ResolvedAppearance`, `ProjectInfo`, `Blocker`. The field names match Plan 1's JSON tags
    exactly: `id,title,column,group,priority,depends,created,done,body,extra,archived,blockers,next,checklist,hash`.
  - `api.ts`:
    - `class ApiError extends Error { status: number; code: string; line?: number; current?: CardView }`;
    - a `const api` with one method per route in Task 4, e.g.
      `board(p: string, q?: string): Promise<{board: BoardView; appearance: ResolvedAppearance}>`,
      `move(p, id, column, index): Promise<CardView>`, `patch(p, id, body, ifMatch?)`, and so on.
      Every request sends `X-Waypoint: 1`.
  - `router.svelte.ts`: `route` (a `$state` holding `{name: 'home'|'board'|'next'|'blocked'|'archive'|'card'|'settings'|'projectSettings', project?: string, id?: string}`),
    `navigate(path: string)`, `href(route)`. It listens to `popstate`. The card route renders the
    board with the panel open.
- [ ] **Step 1: Write the failing tests:**
  - `api` sends the header and throws `ApiError` with `code` and `current` on 409 (mock `fetch`).
  - The router parses each path pattern and round-trips `href`. `/p/Home%20reno/card/H-4` decodes
    the project name.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): typed API client and router`.

---

### Task 8: Board store and live updates

**Files:**
- Create: `web/src/lib/board.svelte.ts`, `web/src/lib/events.svelte.ts`
- Test: `web/tests/board.test.ts`, `web/tests/events.test.ts`

**Interfaces:**
- Produces:
```ts
// board.svelte.ts
export function createBoardStore(api, project: string): {
  readonly view: BoardView | null; readonly appearance: ResolvedAppearance | null;
  readonly parseError: { line?: number; message: string } | null;
  readonly query: string; setQuery(q: string): void;            // debounced 120ms refetch
  refresh(): Promise<void>;
  move(id: string, column: string, index: number): Promise<void>; // optimistic; rollback + toast on error
  create(n: NewCard): Promise<CardView>;
  patch(id: string, body: Patch, ifMatch?: string): Promise<CardView>; // throws ApiError on 409 (panel handles)
  check(id: string, n: number): Promise<void>;                  // optimistic toggle
  remove(id: string): Promise<void>; archiveDone(): Promise<string[]>;
};
// events.svelte.ts
export function connectEvents(onEvent: (e: {type: string; project: string}) => void): {
  readonly status: 'open' | 'reconnecting'; close(): void;
}  // EventSource('/api/events'); on error: close, retry after 0.5s, 1s, 2s, 4s, max 10s; on reopen emit a synthetic {type:'changed', project:'*'}
export const toasts: { items: Toast[]; push(msg: string, kind?: 'error'|'info'): void }
```
- **Ordering:** the store keeps a monotonically increasing request counter, so a refetch response
  older than the latest local op is dropped.

- [ ] **Step 1: Write the failing tests:**
  - `move` reorders `view.cards` synchronously. A rejected API call restores the exact previous
    order and pushes the server's error message as a toast.
  - Two rapid moves where the first fails roll back only the first (Review Focus 3).
  - A 423 sets `parseError` and keeps the previous `view`.
  - A later successful refresh clears `parseError`.
  - Stale responses are ignored.
  - `connectEvents` retries with backoff and emits the synthetic changed event on reconnect (a fake
    `EventSource` with fake timers).
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): board store with optimistic ops and SSE`.

---

### Task 9: App shell: sidebar, top bar, theme application

**Files:**
- Create: `Sidebar.svelte`, `TopBar.svelte`, `Toast.svelte`, `ParseBanner.svelte`
- Modify: `App.svelte`
- Test: `web/tests/shell.test.ts`

**Interfaces:**
- **App.svelte:**
  - It loads `/api/projects`, opens the route's project (or the first one, or an empty state with
    a New project button), and creates the board store.
  - It subscribes to events, refreshing when `project` matches or is `*`, and reloading projects on
    `projects`.
  - It sets `data-theme` (resolving `system` through `matchMedia`, live), `data-density`,
    `data-textures`, `--acc` and `--acc-ink` on `document.documentElement`.
- **Sidebar** (width 214px), with content as in the reference mockup:
  - the logo (map-pin icon in the accent, "WAYPOINT" in `.sign`);
  - the project switcher (a button with the project name in mono that opens a list; each item
    shows the project's accent swatch and its `error` text if any, plus "New project" and "Open
    existing file");
  - nav: Board, Next up, Blocked (count), Archive. The active item is filled with the accent and
    uses the accent ink colour.
  - the "Routes" section (`aria-label="Groups"`). Each group shows a 16x4px route line in its
    colour, its name, and `done/total`. Clicking it toggles `group:"Name"` in the query.
  - Settings, at the bottom.
  - A status line shows "Reconnecting" with a spinner icon when the events status is
    `reconnecting` (Review Focus 5).
- **TopBar:**
  - a filter input (placeholder `Filter`, with a mono hint `p0 blocked group:security` until
    focused, and `/` focusing it);
  - a view toggle with two icon buttons, `columns` and `rows`, labelled "Board" and "Swimlanes". In
    swimlane mode, a select offers Group or Priority;
  - the "New card" button (accent fill, plus icon, `N` key hint).
- **ParseBanner:** a red-edged strip above the board reading
  `This board can't be read: line 12: <msg>. Fix the file and Waypoint will reload it.` While it's
  shown, the board is read-only and the drag, add and edit controls are disabled.

- [ ] **Step 1: Write the failing tests** (Testing Library with a mocked api and store):
  - Clicking a group in Routes sets the query to `group:"Security"`, and clicking again clears it.
  - The Routes heading has the accessible name "Groups".
  - The parse banner shows the line and disables "New card".
  - The theme attribute follows `system` via a mocked `matchMedia` change.
  - `--acc-ink` is set to `accentInk(accent)`.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): app shell, sidebar, top bar, theming`.

---

### Task 10: Board, cards, swimlanes, drag and keyboard moves

**Files:**
- Create: `Board.svelte`, `Column.svelte`, `Card.svelte`, `Swimlanes.svelte`, `MoveMenu.svelte`,
  `web/src/lib/dnd.ts`, `web/src/lib/keys.ts`
- Test: `web/tests/board-ui.test.ts`, `web/tests/dnd.test.ts`

**Interfaces:**
- **Column:**
  - The header is a `.sign` block in the `head` colour with the name, plus the count, or `n/WIP`
    in accent mono when a WIP limit is set.
  - Cards are listed in rank order, followed by an "Add card" row.
- **Card face** (exactly as in the reference mockup):
  - a mono id row with a priority chip (P0 drawn as `head` background with accent text);
  - the title (Geist 600 14px);
  - a meta row with the route line plus group name, and checklist progress
    (`check-square` icon, `1/4`);
  - a blocked line (`prohibit` bold icon, `Blocked by T-17`, with missing ids as `T-99 (missing)`);
  - for the next-up card: the accent outline and edge, `-0.6deg` rotation, and a "NEXT" tag;
  - for done cards: no edge, a 60% card background, the title struck through, and a check icon
    before the id.
  
  A card is a `button`-role element with
  `aria-label="<id> <title>, <column>[, blocked by ...][, next up]"`.
- **Swimlanes:**
  - Rows by group (plus an "No group" lane) or by priority (plus "No priority"), each across the
    same columns.
  - Lane headers show the name and "1 of 4 done", and collapse with a caret. The collapsed state
    is remembered per project in `localStorage`.
  - In group swimlanes, dropping into another lane changes the card's group: a `patch` with
    `group`, then a `move`.
- **dnd.ts:** `createDrag(opts: { canDrop(cardId, column): string | null /* reason or null */; onDrop(cardId, column, index): void })`.
  - **Pointer events:** a 4px movement threshold before the drag starts, and the card lifts by
    animating its shadow offset from 3px to 6px.
  - **Drop targets:** a placeholder slot shows the drop index. Columns where `canDrop` returns a
    reason are dimmed and show the reason in a tooltip.
  - **Scrolling:** auto-scrolls near the edges, and `Escape` cancels.
  - **Rule check:** `canDrop` checks `moves_to` and WIP from the store's settings. The server
    remains the authority.
- **Keyboard (keys.ts):**
  - Arrow keys move focus between cards (up and down within a column, left and right across
    columns).
  - `Shift+Arrow` moves the focused card: within the column for up and down, and to the adjacent
    allowed column for left and right.
  - `m` opens MoveMenu, a list of allowed columns plus "Top of column" and "Bottom of column".
  - `Enter` opens the panel, `n` focuses quick add in the first column, `/` focuses the filter,
    and `Esc` closes the menu, panel or drag.

- [ ] **Step 1: Write the failing tests:**
  - The card renders the NEXT tag and blocked text, and its aria-label matches the format.
  - A done card has a `line-through` title (computed style via a class assertion).
  - `canDrop` returns `"Todo cannot move to Done"` for a disallowed column and
    `"In progress is at its WIP limit (2/2)"` at the limit.
  - A simulated pointer drag (pointerdown, move 10px, over a column, pointerup) calls `onDrop` with
    the computed index. Movement under 4px doesn't start a drag.
  - `Shift+ArrowRight` calls `store.move` to the next allowed column.
  - `m` opens the menu, listing only allowed columns.
  - In group swimlanes, dropping across lanes calls `patch` with the new group.
  - **(Review Focus 3)** A drop onto a WIP-full column that the client thought allowed gets a 422
    from the server: the card returns to its origin and a toast shows the server's message.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement. Motion uses CSS transitions on `transform` and `box-shadow` only.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): board, cards, swimlanes, drag and keyboard moves`.

---

### Task 11: Card panel, markdown body, conflicts, quick add

**Files:**
- Create: `CardPanel.svelte`, `MarkdownBody.svelte`, `ConflictDialog.svelte`, `QuickAdd.svelte`
- Test: `web/tests/panel.test.ts`

**Interfaces:**
- **CardPanel:**
  - A right-side panel, 440px wide (full width under 720px), sliding in over 160ms, at the route
    `/p/:p/card/:id`.
  - **Fields:**
    - title (inline input);
    - column (select limited to allowed moves);
    - group (select, plus "No group");
    - priority (select, plus "None");
    - depends (token input with id autocomplete from the board, invalid ids marked);
    - extra fields (key/value rows with add and remove).
  - Created and done dates are shown read-only.
  - **Body:** a rendered view (MarkdownBody) with an "Edit" toggle to a monospace textarea.
- **Drafts and saving:**
  - The panel holds a **local draft**. Store refreshes update the non-dirty fields only, and never
    overwrite a dirty draft (Review Focus 2).
  - Saving happens on `Cmd/Ctrl+Enter`, on the Save button, or on blur of title/body when dirty.
    It sends `patch` with `if_match = hash at the time the draft started`.
  - On a 409, ConflictDialog opens: "This card changed outside Waypoint", with "Yours" and "On
    disk" side by side and two buttons:
    - **Keep mine** re-sends with `if_match = current.hash`;
    - **Use theirs** discards the draft.
  - Closing with a dirty draft asks "Discard unsaved changes?".
- **MarkdownBody:** `marked` renders, then `DOMPurify.sanitize`. Task-list checkboxes are rendered
  enabled, and clicking the nth one calls `store.check(id, n)`. Numbering matches
  `domain.Checklist`: checkboxes inside code fences aren't counted, which marked already
  guarantees. Links open in a new tab with `rel="noopener"`.
- **QuickAdd:** sits at the bottom of each column. Enter creates a card. A paste of several lines
  creates one card per non-empty line, in order, after a confirm `Create 5 cards?`.
- **Delete:** in the panel's overflow menu, with a confirm dialog naming the id and title.

- [ ] **Step 1: Write the failing tests:**
  - Editing the title and pressing `Cmd+Enter` calls `patch` with `if_match` set to the original
    hash.
  - **(Review Focus 2)** With a dirty body, a store refresh delivering a changed card keeps the
    draft text. Saving then gets a 409, the dialog shows both bodies, and "Keep mine" re-sends with
    the new hash.
  - Clicking the 2nd checkbox calls `check(id, 2)`.
  - A `<script>` in the body is not rendered.
  - Pasting `"a\nb\n\nc"` into QuickAdd confirms and creates 3 cards in order.
  - Closing a dirty panel prompts.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): card panel, markdown body, conflict handling, quick add`.

---

### Task 12: Next up, Blocked and Archive views

**Files:**
- Create: `ListView.svelte`, `ArchiveView.svelte`
- Test: `web/tests/views.test.ts`

**Interfaces:**
- **Next up:** the next-up card large at the top, then the next 9 cards of the first non-done
  column, with an "Open" action.
- **Blocked:** every blocked card grouped by its first blocker, each blocker rendered as a link that
  opens that card.
- **ArchiveView:** a search input (`?q=` against `api.archive`), cards grouped by month of `done`
  under `.sign` month headings (`OCTOBER 2026`), and a "Restore" action (to the first column, with
  an optional column select). An "Archive done cards" button runs `archiveDone`. It's hidden when
  the archive mode is `off`, and shows a toast `Archived N cards`.
- **Empty states**, one sentence plus an action:
  - `Nothing is blocked.`
  - `No archived cards yet.`
  - `No cards yet. Add one with N.`

- [ ] **Step 1: Write the failing tests:** the blocked grouping, archive month grouping and order
  (newest first), restore calls the api, the empty states, and the archive button hidden in `off`
  mode.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): next up, blocked and archive views`.

---

### Task 13: Settings: global, project, columns editor, new project

**Files:**
- Create: `GlobalSettings.svelte`, `ProjectSettings.svelte`, `ColumnsEditor.svelte`,
  `NewProjectDialog.svelte`
- Test: `web/tests/settings.test.ts`

**Interfaces:**
- **GlobalSettings** (`/settings`):
  - theme (System, Dark, Light), accent (8 swatches plus a custom hex input), density, textures,
    and default swimlane;
  - port, open browser, and TLS (Auto, On, Off). A save that returns `restart_required` shows
    `Restart "waypoint serve" to apply.`
- **ProjectSettings** (`/p/:p/settings`):
  - **Appearance:** the same appearance controls. Each shows a "From global" badge when its source
    isn't `project`, plus a "Reset to global" link, which sends that field as `""`.
  - **Columns:** ColumnsEditor.
  - **Groups:** name and colour (the 8 preset colours plus hex), with add, rename and remove.
  - **Priorities:** an ordered list with add, rename and remove.
  - **Archive mode:** Off, Manual, or Auto after N days.
  - **Storage:** the path and type, read-only. The convert button arrives in Plan 3.
  - Save sends one `PUT settings` with the renames collected from the edits.
- **ColumnsEditor:** the table from the spec. Each row has a drag handle (plus up and down buttons
  as the keyboard alternative), name (inline rename, recorded in `column_renames`), WIP limit
  (number, empty means none), "Can move to" checkboxes for every other column, "Counts as done",
  and remove (disabled with the tooltip `Move its 3 cards out first` when the column isn't empty).
  "Add column" appends `New column` with no moves_to.
- **NewProjectDialog** has two tabs:
  - **Create:** name, optional location (a text path with the hint "Leave empty to keep it in
    Waypoint's folder"), and prefix (default `T`, validated `^[A-Za-z][A-Za-z0-9]*$`).
  - **Open existing file:** a path.
  
  Errors from the API are shown inline under the field, e.g. "not a Waypoint board (missing front
  matter)". On success it navigates to the new board.

- [ ] **Step 1: Write the failing tests:**
  - Renaming a column and saving sends `column_renames: {"Todo":"Backlog"}`.
  - The remove button is disabled for a non-empty column.
  - "Reset to global" sends `accent: ""`.
  - The "From global" badge reflects `source`.
  - The prefix validation message.
  - An API error is shown under the path field.
  - The up and down buttons reorder columns.
- [ ] **Step 2:** Run `npm test`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `npm test`. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(web): global and project settings, columns editor, new project`.

---

### Task 14: README and docs for the web UI

**Files:**
- Modify: `README.md` (serve, `trust` and HTTP/2, keyboard shortcuts, settings precedence)
- Create: `docs/keyboard.md`

- [ ] **Step 1:** Document:
  - `waypoint` / `serve` flags;
  - the TLS modes, with the one-line explanation:
    `Browsers only use HTTP/2 over HTTPS, so "waypoint trust" installs a local certificate authority that only this machine trusts`;
  - the keyboard map from Task 10;
  - the override precedence (project, then global, then default).
- [ ] **Step 2:** Commit with `docs: web UI, HTTP/2 and keyboard reference`.

---

### Task 15: End-to-end tests and performance check

**Files:**
- Create: `web/playwright.config.ts`, `web/e2e/board.spec.ts`, `web/e2e/live.spec.ts`,
  `web/e2e/perf.spec.ts`, `web/e2e/fixtures.ts`

**Interfaces:**
- **Fixture:** builds once (`make build` in `globalSetup`). Per test it creates a temp
  `WAYPOINT_CONFIG` and project via `bin/waypoint project add`, then starts
  `bin/waypoint serve --no-open --port <free>` with `tls: off`, and gives the test `baseURL` and
  the board file path.
- Run in Chromium, plus `reducedMotion: 'reduce'` for one project config.

- [ ] **Step 1: Write the specs:**
  - `board.spec`: create a card with N, then type and press Enter. Drag it to In progress. The file
    on disk has it under `## In progress`. Mark it done with `Shift+ArrowRight`, and the title is
    struck through. Filter with `p0` narrows the cards.
  - `live.spec`:
    - Append a card to the file with Node `fs`, and it appears within 1.5s with no reload.
    - **(Review Focus 1)** Write garbage to the file: the banner shows `line`, and New card is
      disabled. Restore the file, and the banner disappears within 1.5s.
    - **(Review Focus 5)** Kill and restart the server on the same port: the sidebar shows
      "Reconnecting", then the board refreshes.
  - `perf.spec`: a board with 300 generated cards. Measure from response to the last card painted
    (`performance.mark` in the store plus a `requestAnimationFrame` after render) at no more than
    300ms. A drag over 3 columns records long tasks with `PerformanceObserver`, and none is over
    50ms.
- [ ] **Step 2:** Run `cd web && npm run e2e`. Expect everything to PASS. Fix the owning task's code
  for any failure, not the test.
- [ ] **Step 3:** Run `make test`. Expect the Go and Vitest suites to pass.
- [ ] **Step 4:** Commit with `test(web): end-to-end, live update and performance checks`.

---

## Self-review notes (resolved inline)

- **Spec coverage:**
  - §4: board (Task 10), swimlanes (10), sidebar (9), card face (10), panel (11), filter (8, 9),
    keyboard (10), quick add and paste (11), settings (13), visuals (6, 10), live updates (3, 8).
  - §5.3: 409 UI (11) and the watcher (3).
  - §6: overrides and resolution (2, 13).
  - HTTP/2: Task 5.
- **Deferred to Plan 3:** convert, import/export (CSV/JSON/MD), MCP, SQLite (the watcher already
  accepts `-wal` names), and the pharmacy migration.
- **Names checked across tasks:**
  - `createBoardStore`: Task 8, used in 9–13.
  - `connectEvents`: 8 → 9.
  - `accentInk` / `resolveAccent`: 6 → 9, 13.
  - `api.*` methods: 7 → 8, 12, 13.
  - `Broker`: 3 → 4, 5.
  - `ResolvedAppearance`: 2 → 4 → `types.ts` in 7.
  - `SettingsChange`: 1 → 4.
