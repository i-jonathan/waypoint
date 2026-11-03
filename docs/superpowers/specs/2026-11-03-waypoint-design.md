# Waypoint — design

Date: 2026-11-03 · Status: draft for approval

Items marked **(builder's call)** were decided by Claude rather than by the owner and are open to
change during review. Everything else was stated by the owner.

## 1. Intent

Waypoint is a fast, 100% local tracker for "things I want to do" on **any kind of project**: a
codebase, a house renovation, a course, a side business. Cards are plain to-dos, not issues or
tickets, and nothing in the product assumes software. Software-flavoured features (agent access,
living next to a git repo) are optional extras. It starts as a kanban board.

Why it exists: GitHub Projects forces every card to be a draft or an issue, Jira and Asana are
bloated and online, and the owner's current system (a hand-edited `BOARD.md` plus `DONE.md` in
the pharmacy repo) works as data but can't be *seen*. With dozens of cards it's hard to plan:
**what's next, what's blocked, and how work groups into larger pieces** (a simpler Jira epic).

Success: the owner opens Waypoint on the pharmacy project, sees the board, and can answer those
three questions at a glance. Their coding agents read and update the same board without breaking
it.

## 2. Goals, non-goals, anti-goals

**Goals**
- A kanban web app served by a local Go binary, with many projects in one app.
- Storage chosen per project: a **markdown file** or an **SQLite database**, at a location the
  user chooses (any directory, e.g. inside the project's folder, or Waypoint's own data directory).
  A project can be **converted between markdown and SQLite in both directions** at any time (§5.4).
- Waypoint defines its own documented markdown format. **CSV is the structured import/export path.**
- Configurable columns, with per-column rules for which columns a card may move to, plus a WIP limit
  and "counts as done".
- Done handling as a per-project setting: done cards stay visible, or get archived (manually or
  automatically after N days). The archive stays browsable.
- Groups (simple epics), "next up", and "blocked" as first-class concepts.
- A proper interface for coding agents.
- Reasonable configurability: global settings, with some settings overridable per project (for
  example a global dark theme with a different accent colour per project).
- Backend in Go with clean architecture. Simple beats complex.

**Non-goals:** user accounts, permissions, multi-user collaboration, comments, sprints, time
tracking, estimates, Gantt or timeline views, notifications, cloud sync, mobile apps, importing
arbitrary markdown formats.

**Anti-goals** (failure even if it technically works):
- It feels slow: noticeable lag on load, drag, or save with a few hundred cards.
- It needs setup before you can add a card.
- It locks data into a format a human can't read or edit.
- It silently overwrites a change made outside the UI (by the user's editor or an agent).
- It grows toward Jira.
- Emoji in the UI: use icons.

## 3. Concepts

| Concept | Definition |
|---|---|
| **Project** | A board plus its settings. It has a name, an id prefix (e.g. `T`), a storage type and a storage path. |
| **Column** | Name, order, optional WIP limit, list of columns it may move to, and a "counts as done" flag. |
| **Card** | `id` (prefix + number, never reused), title, column, rank (position within the column), optional group, optional priority, `depends` (list of card ids), `created`, `done` (date, set on entering a done column), free markdown body, and any extra key/value fields (preserved, displayed, filterable). |
| **Group** | Name and colour. A card belongs to zero or one group **(builder's call)**. |
| **Next up** | The top card of the first non-done column, in rank order. You set the order by dragging; priority never reorders cards automatically **(builder's call)**. |
| **Blocked** | A card with at least one `depends` id that is not in a done column (or archived). Unknown ids count as blocking and are flagged as "missing". |
| **Archive** | Done cards moved out of the board, grouped by month of `done`, browsable and searchable, and restorable to a column. |

**Default columns for a new project:** Todo → In progress → Done.
- Todo may move to In progress and Done.
- In progress may move to Todo and Done.
- Done may move to In progress, and counts as done.
- There are no WIP limits by default.

**Rules**
- A move that the source column's `moves_to` doesn't allow is refused.
- A move into a column that is already at its WIP limit is refused.
- Both rules are enforced in the domain layer, so the UI, CLI and MCP all behave the same.
- Reordering within a column is always allowed.
- Exceeding a WIP limit through an external file edit is shown as a warning, not an error.
- Entering a done column sets `done` to today, and leaving one clears it **(builder's call)**.
- Auto-archive runs when a project opens and after each write. It moves cards whose `done` is older
  than `after_days` **(builder's call)**.
- Priority is an ordered list per project, defaulting to `P0, P1, P2` and editable in settings. Its
  only effects are the chip, filtering and swimlanes **(builder's call)**.
- Renaming a column rewrites the cards in it. Deleting a column is only allowed when it's empty.

**Projects:** "New project" either creates a new board file or database at a chosen path, or
*opens* an existing Waypoint markdown or SQLite file and registers it. Removing a project from the
registry never deletes its data.

## 4. User interface

The layout was chosen from mockups (option C):

- **Board view (default):** columns across the page, cards stacked in rank order, drag to move or
  reorder. Column headers show the count, and the WIP status when a limit is set (e.g. `1/2`).
- **Swimlane toggle:** group by *none / group / priority*. Each lane is a collapsible row across the
  columns, and a group lane header shows its progress ("1 of 4 done").
- **Left sidebar:**
  - The project switcher.
  - Groups (headed "Routes", see the design language) with card counts. Clicking a group filters the board to it.
  - Quick views for *Next up* and *Blocked*.
  - A link to *Archive*.
- **Card face:** id, title, the group (route line plus name), a priority chip, checklist progress
  (`☑ 1/4` drawn as an icon), and a red "Blocked by T-017" line with an icon when blocked.
- **Card panel:** a side panel that opens on click. It edits:
  - the title, column, group, priority, depends-on (with id autocomplete), and extra fields;
  - the markdown body, rendered with clickable `- [ ]` checklists;
  - the created and done dates, shown read-only.
- **Filter bar:** free text plus tokens such as `p0`, `blocked`, `group:security`, and
  `field:value` for extra fields.
- **Keyboard:** `n` new card, `/` filter, `Esc` close panel, and arrow-key movement of the focused
  card between columns **(builder's call; keep it minimal)**.
- **Quick add:** typing a title and pressing Enter creates the card at the bottom of the first
  column. Pasting several lines creates one card per line, which supports dumping in a planning
  session.
- **Settings:** a global settings page and a per-project settings page. The project page holds
  columns and rules (the table from the mockup: reorder, rename, WIP limit, moves-to checkboxes,
  counts-as-done), groups, done handling, and appearance overrides.
- **Design language: "night map"** (locked by the owner after three directions and three rounds of
  changes; the reference mockup is `docs/design/design-language.html`). The base is the
  "cartographic" direction, with the boldness of "wayfinding" and the tactile cards of "index cards".
  - **Canvas.**
    - Dark: green-black `#0f1714`. Light: pale survey green `#e9eee8`.
    - Two low-contrast textures sit on the canvas only, never behind text blocks: a dot grid (18px)
      and faint contour rings. Settings can switch them off.
  - **Colour.**
    - One project accent, plus group colours. Nothing else is coloured, except danger red for
      blocked.
    - The accent marks: the active nav item (filled), the primary button, the next-up card, the
      in-progress WIP count, focus rings, and the P0 text.
    - There are 8 curated accents, each with tuned dark and light values, plus any custom hex.
    - Every accent has a matching text colour on top of it, checked for 4.5:1 contrast.
  - **Type.**
    - **Archivo** condensed (width about 70%, weight 800, uppercase) for "signs" only: the logo,
      column headers (white-on-ink or ink-on-paper blocks), and the NEXT tag.
    - **Geist** for everything you read.
    - **Geist Mono** for ids, counts, priority and filter syntax.
    - All fonts are bundled in the binary, so nothing loads from the network.
  - **Cards (index-card style).**
    - Each card has a 1.5px ink outline and a hard 3px offset edge, with no blur. In dark mode the
      ink is a soft pale green.
    - The next-up card takes the accent for its outline and edge, rotates by -0.6deg, and carries a
      bold "NEXT" tag on its top edge.
    - Done cards lose the edge, fade back, and their title is struck through, with a check icon
      before the id.
    - Blocked cards show a red "Blocked by T-017" line with an icon.
  - **Groups.**
    - On the card: a short coloured route line, then the group name.
    - In the sidebar: the same line markers, with counts like "1/4".
    - **Naming:** the concept is called "group" everywhere: settings, filters (`group:`), the CLI,
      the file format. Only the sidebar section heading reads "Routes", with a tooltip and
      accessible name of "Groups". It's a flavour label, never a term the user has to learn.
  - **Shape.** 10px radius for controls, 12px for cards, 7px for column signs, 4px for chips. A 4px
    spacing grid.
  - **Icons.** Phosphor (regular weight, with bold for emphasis such as NEXT and blocked), never
    emoji. Icon-only buttons get an `aria-label` and a tooltip.
  - **Motion.** 120–180ms, only for:
    - picking up and dropping a card (it lifts by growing its offset edge);
    - the panel slide;
    - collapsing a swimlane.
    
    All of it is disabled under `prefers-reduced-motion`.
  - **Accessibility.**
    - Blocked status is an icon plus text, not red alone.
    - A group always shows its name next to its colour.
    - Every drag has a keyboard and menu alternative: `m` opens "move to…" and arrow keys move the
      focused card.
    - Focus is always visible.
    - Light and dark themes are designed together.
- **Live updates:** when the underlying file or database changes outside the app, the open board
  refreshes within about a second, without losing an open card panel's unsaved text (see §7).

## 5. Storage

One Go interface (`Repository`) has two adapters. The rest of the app doesn't know which one is in
use.

### 5.1 Markdown adapter

- A project is **one board file** at a user-chosen path, e.g. `~/Documents/Projects/pharmacy/BOARD.md`.
- The archive is a sibling file `<name>.archive.md`, e.g. `BOARD.archive.md`, with one `##` heading
  per month (newest first) and the cards beneath it.
- Settings live in YAML front matter, so a board file is self-contained and can be committed to git.

```markdown
---
waypoint: 1
name: Pharmacy POS
prefix: T
next_id: 112
columns:
  - { name: Todo, moves_to: [In progress, Done] }
  - { name: In progress, wip: 2, moves_to: [Todo, Done] }
  - { name: Done, done: true, moves_to: [In progress] }
groups:
  - { name: Security hardening, color: blue }
  - { name: Purchasing & receiving, color: amber }
archive: { mode: auto, after_days: 14 }   # mode: off | manual | auto
appearance: { accent: "#7c3aed" }          # optional per-project overrides (§6)
---

## Todo

### T-019 Authenticate the /ws WebSocket

- group: Security hardening
- priority: P0
- depends: T-017
- created: 2026-10-26

`/ws` bypasses the middleware stack… (free markdown, checklists allowed)

- [ ] step
```

**Parsing rules (the documented format)**
- `## <Column>` starts a column, and its name must match a column in the front matter. Columns
  present in the front matter but missing from the body are empty.
- `### <ID> <Title>` starts a card. The `- key: value` lines directly under the heading, up to the
  first blank line, are fields. The rest, up to the next `##` or `###` heading outside a fenced code
  block, is the body, kept verbatim.
- Body headings must be `####` or deeper. On write, Waypoint demotes any shallower heading inside a
  body.
- Known fields are `group`, `priority`, `depends` (comma-separated), `created` and `done`. Unknown
  fields are kept in order and round-tripped.
- A card's rank is its position in the file.
- **On write, Waypoint emits canonical formatting** (field order, blank lines). It does not promise
  to preserve hand formatting outside card bodies. Bodies are preserved byte for byte.
- A file that fails to parse is never overwritten. The UI shows the error with its line number, and
  the board becomes read-only until the file is fixed.

### 5.2 SQLite adapter

- A project is one `.db` file at a user-chosen path.
- Tables: `project` (one row, settings as JSON), `columns`, `groups`, `cards` (with an `archived_at`
  column instead of a separate archive), and `card_fields` (key/value for extra fields).
- Migrations are embedded and run on open.
- The driver is pure Go (`modernc.org/sqlite`) **(builder's call)**, so building needs no C
  toolchain.

### 5.3 Concurrent edits (agents and editors write too)

Waypoint must never overwrite a change it hasn't seen.

- **Writes are operations, not snapshots.** Every change, whether from the UI, the CLI or MCP, is a
  semantic operation such as *move T-019 to "In progress" at index 0*, *set field priority=P1 on
  T-020*, or *create card*.
- To apply one, the app layer:
  1. takes the project's write lock (an OS file lock on `<board>.lock` for markdown, a transaction
     for SQLite);
  2. **re-reads current state from disk**;
  3. applies the operation;
  4. writes the result atomically (temp file plus rename for markdown);
  5. releases the lock.
  
  Because each operation is re-applied to fresh state, a concurrent external edit to a different
  card is never lost.
- **Text edits are version-checked.** The card panel sends the card's content hash along with title
  or body edits. If that card changed on disk since the panel loaded it, the server answers `409`,
  and the UI shows "This card changed outside Waypoint" with both versions so the user can pick or
  merge.
- **Change detection:** the server watches the board file, the archive file, or the db file with
  `fsnotify` and pushes a "changed" event to the browser over Server-Sent Events. The browser
  refetches.
- Agents editing the markdown directly can't take Waypoint's lock. Atomic rename on Waypoint's side,
  plus the re-read before every write, keeps the race window to milliseconds. Agents are encouraged
  to use the CLI or MCP (§8), which do take the lock.

### 5.4 Converting between storage types

- **How to run it:** Project settings → Storage → "Convert to SQLite / Markdown", or
  `waypoint project convert NAME --to sqlite|markdown --path FILE`.
- **What it does:**
  1. Loads the full project (settings, columns, groups, cards, archive) through the current adapter.
  2. Writes it through the target adapter to the new path.
  3. Re-reads the result and checks that it matches: same cards, ids, ranks, bodies and settings.
  4. Only then points the registry at the new storage.
- **The old file is kept**, renamed to `<name>.bak-YYYYMMDD` and never deleted automatically, so a
  conversion is always reversible.
- Conversion is lossless in both directions. That's a hard requirement, so every concept in §3 has
  to be representable in both adapters. The shared contract test suite includes a round-trip
  markdown → SQLite → markdown check.

### 5.5 Other formats (builder's recommendation)

**Storage formats stay at two.**
- Markdown is human-first: readable, editable, and diffs well in git.
- SQLite is scale-first: handles thousands of cards and can be queried.
- A third storage format (a JSON or YAML file, say) would add a full adapter, a lock and a watcher
  for almost no gain. Markdown already covers "a file I can read", and SQLite covers "a database".

**Interchange formats** are import and export only, and much cheaper:

| Format | Scope | Why |
|---|---|---|
| **CSV** | cards only | The structured import path, and it opens in a spreadsheet. |
| **JSON** | the whole project (settings, columns, groups, cards, archive) | Lossless backup, a scripting and agent-friendly snapshot, and the intermediate form for conversion. |
| **Markdown export** | read-only snapshot | Share a board as a document. It's the same as the board file format. |

Not planned: Trello, Jira or GitHub importers, since the anti-goal is drifting toward Jira. If one
becomes necessary, it can go through CSV.

## 6. Configuration

- **Global config** **(builder's call on location and format):** `~/.config/waypoint/config.yaml`.
  YAML is used everywhere, matching the front matter. It holds:
  - `port` (default 4747) and `open_browser` (default true);
  - `data_dir` (default `~/.local/share/waypoint/`, where projects go if no location is chosen);
  - the project registry (name and storage path for each project);
  - default appearance: theme (light, dark or system), accent, and density.
- **Per-project settings** live with the project's data (front matter or the `project` row):
  columns, rules, groups, id prefix, archive mode, and **appearance overrides**.
- **Overridable per project:** theme, accent colour, density, and default swimlane mode.
- **Global only:** port, browser opening, data directory, and the registry.
- **Resolution order:** project override, then global, then built-in default. The settings UI shows
  which level each value comes from and offers "reset to global".
- A missing config file is created with defaults on first run, so there is zero setup.

## 7. Architecture (Go, clean architecture, kept small)

```
cmd/waypoint/           main: wires config → repos → app → adapters (serve | cli | mcp)
internal/domain/        Project, Column, Card, Group, rules (CanMove, IsBlocked, NextUp), errors. No I/O.
internal/app/           use cases: CreateCard, MoveCard, UpdateCard, ArchiveDone, ImportCSV, ExportCSV,
                        ListCards(filter)… Defines the Repository and Clock interfaces.
internal/storage/markdown/   Repository via board file (parser and writer, lock, atomic write)
internal/storage/sqlite/     Repository via SQLite
internal/config/        global config and registry; settings resolution
internal/http/          JSON API, SSE events, embedded static UI (go:embed)
internal/cli/           agent and human CLI commands
internal/mcp/           MCP stdio server (tools map 1:1 onto app use cases)
web/                    Svelte 5 + TypeScript + Vite; built into internal/http/dist
```

- Dependencies point inward: adapters depend on `app`, and `app` depends on `domain`.
- `domain` imports nothing outside the standard library.
- Libraries are kept minimal **(builder's call)**: the standard library `net/http` router (Go 1.22+
  patterns), `gopkg.in/yaml.v3`, `modernc.org/sqlite`, `fsnotify`, and the official MCP Go SDK.
- The frontend is Svelte with TypeScript and Vite **(builder's call)**. It's built once and embedded,
  so Waypoint ships as **one binary**. `waypoint` with no arguments runs `serve` and opens the
  browser. The server binds to `127.0.0.1` only.
- **HTTP/2 (owner's requirement):** the server enables HTTP/1.1, HTTP/2 over TLS, and h2c. Browsers
  only use HTTP/2 over HTTPS, so Waypoint generates a local certificate authority. Running
  `waypoint trust` once installs it, after which `serve` uses HTTPS and HTTP/2. Until then it serves
  plain HTTP, so there's still no required setup. The `tls: auto|on|off` key in the config controls
  this.
- **Local-only safety:** the server rejects requests with a `Host` other than localhost or 127.0.0.1,
  and every write needs an `X-Waypoint: 1` header. Together these block DNS rebinding and
  cross-site requests from other web pages.
- **Testing:** the domain and app layers are unit-tested against an in-memory repository. Both
  storage adapters run one shared contract test suite. The markdown parser has golden-file round-trip
  tests, including fenced code, odd headings, unknown fields, and a parse-error case.

## 8. Agent interface

The owner wants a proper agent interface: MCP, or something lighter if better suited.
**Decision (builder's call): both, as thin adapters over the same use cases, with the CLI as the
primary interface.**

- **The CLI** is the lighter option. Every agent that can run a shell can use it, and it needs no
  server running and no configuration:
  - `waypoint ls [--column X] [--group G] [--blocked] [--next]`
  - `waypoint show T-019`
  - `waypoint add "Title" [--column X] [--group G] [--priority P0] [--depends T-1,T-2] [--body -]`
  - `waypoint move T-019 "In progress" [--top|--index N]`
  - `waypoint set T-019 priority=P1 group="Security hardening"`
  - `waypoint check T-017 2` (ticks checklist item 2)
  - `waypoint done T-019`
  - `waypoint archive`
  - `waypoint import cards.csv|project.json`
  - `waypoint export --format csv|json|md`
  - `waypoint project add|ls|rm|convert`
  
  Every command accepts `--json` for machine-readable output and `--project NAME`. Without
  `--project`, the CLI picks the registered project whose storage path lies under the current
  directory. The CLI uses the same lock and rules as the server, and the server sees CLI changes
  through its file watcher.
- **MCP:** `waypoint mcp` runs an MCP server over stdio for agents configured that way. Its tools are
  `list_cards`, `get_card`, `create_card`, `move_card`, `update_card`, `next_up` and
  `list_blocked`.
- **`waypoint agents`** prints a short paragraph to paste into a project's `AGENTS.md` or
  `CLAUDE.md`, explaining how to use the CLI.

## 9. Import and export (CSV shown; JSON is the full project snapshot, see §5.5)

- Columns: `id,title,column,group,priority,depends,created,done,body`, plus any extra headers, which
  become extra fields.
- On import:
  - An empty `id` gets a new id.
  - An existing id updates that card.
  - Rows keep file order as their rank within each column.
  - Unknown column or group names are created, after a confirmation in the UI or with a `--create`
    flag on the CLI.
- Export writes the same shape, including archived cards with `column=Archived`.
- **Pharmacy migration:** a one-off conversion of the current `BOARD.md`/`DONE.md` into CSV
  (`feature` → `group`, columns In Progress / Up Next / Backlog → configurable), done as a separate
  task once Waypoint works. It isn't a product feature.

## 10. Scope for the first version

The first version includes everything above. Suggested build order: domain and markdown storage,
then the app layer and CLI, then the HTTP API and board UI, then settings, the archive and SQLite,
then MCP and CSV.

Deliberately left out: multiple boards per project, card attachments, a history or undo log, global
search across projects, and any import other than CSV.

## 11. Left to the builder

Visual details not fixed by §4 or the reference mockup, API route shapes, CSS approach, exact keyboard map, colour
palette for groups, test tooling, and packaging (Homebrew and similar come later, if at all).
