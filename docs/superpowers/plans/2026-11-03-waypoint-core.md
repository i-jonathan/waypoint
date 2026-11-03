# Waypoint Core (Plan 1 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single `waypoint` Go binary whose CLI creates and edits kanban boards stored in the
Waypoint markdown format. It enforces column rules, never overwrites unseen external edits, and
is usable by people and coding agents (`--json`).

**Architecture:** Clean architecture with four layers.
- `internal/domain`: pure types and rules, no I/O.
- `internal/app`: use cases, plus the `Repository` and `Clock` ports.
- Adapters:
  - `internal/storage/markdown`: board file parse and format, lock, atomic write.
  - `internal/config`: global config and project registry.
  - `internal/cli`: the commands.
- `cmd/waypoint`: wires everything together.

Every write goes through `app.Service.apply`: lock, re-read, mutate, save, unlock.

**Tech Stack:** Go (go.mod `go 1.24`), `gopkg.in/yaml.v3`, `github.com/gofrs/flock`, and the
standard library (`flag`, `encoding/json`, `crypto/sha256`). No CLI framework.

**Spec:** `docs/superpowers/specs/2026-11-03-waypoint-design.md` (§3 Concepts, §5.1, §5.3, §6, §7,
§8 CLI). Executors read both documents.

**Roadmap:**
- Plan 2 (`waypoint-web`): HTTP API, SSE, fsnotify watcher, Svelte UI in the "night map" design,
  appearance resolution, and `waypoint` defaulting to `serve`.
- Plan 3 (`waypoint-formats`): SQLite adapter, `project convert`, CSV/JSON/MD import and export,
  `waypoint mcp`, and the one-off pharmacy migration.

Both later plans are written after this one lands, against its real interfaces.

## Global Constraints

- Module path `waypoint`; `go 1.24` in go.mod; `domain` imports only the standard library.
- Dependencies point inward: `cli`/`storage`/`config` → `app` → `domain`. `app` never imports an
  adapter.
- Default columns: `Todo` (moves_to: In progress, Done), `In progress` (moves_to: Todo, Done),
  `Done` (done: true, moves_to: In progress). No WIP limits.
- Default priorities: `P0, P1, P2`. Default archive: `{mode: manual, after_days: 14}`. Default id
  prefix: `T`.
- Card ids are `<prefix>-<n>`, assigned from `next_id`, never reused, even after deletion.
- Dates are written `YYYY-MM-DD`. Entering a done column sets `done` to today, and leaving one
  clears it.
- Known card fields, written in this order: `group`, `priority`, `depends` (comma plus space
  separated), `created`, `done`. Unknown fields follow in their original order.
- Markdown write is canonical outside bodies. **Bodies are preserved byte for byte**, except that
  body headings shallower than `####` outside fenced code are demoted to `####`.
- **A file that fails to parse is never written.** Errors carry a 1-based line number.
- Writes use a temp file in the same directory plus `os.Rename`. The lock file is
  `<board path>.lock`.
- The archive file for `X.md` is `X.archive.md`, holding a `# Archive` title and one `## YYYY-MM`
  section per month, newest first.
- Global config: `$WAYPOINT_CONFIG` if set, else `$XDG_CONFIG_HOME/waypoint/config.yaml`, else
  `~/.config/waypoint/config.yaml`.
  - Default `port: 4747`, `open_browser: true`, `data_dir: ~/.local/share/waypoint`.
  - A missing config is created on first write and never required for a read.
- The CLI never prints emoji. Every command accepts `--json` and `--project NAME`.
- No em dash characters in any user-facing CLI output.

## Review Focus

1. **Duplicate card ids in a hand-edited file** (an agent copy-pastes a card). Expect a parse error
   naming both lines, not a silent merge. Test in Task 6.
2. **A `##` heading that isn't a configured column** (a typo like `## In Progres`). Expect a parse
   error with the line number, file untouched. Test in Task 6.
3. **`waypoint project add` pointed at an ordinary markdown file** (no front matter, e.g. the old
   pharmacy `BOARD.md`). Expect "not a Waypoint board (missing front matter)" and the file left
   unchanged. Test in Task 11.
4. **CRLF line endings** from a Windows editor. Expect it to parse the same as LF, and Waypoint to
   write LF. Test in Task 6.
5. **A card that depends on itself, or a dependency cycle.** Expect self-dependency to be rejected
   on set, and `Blockers` to terminate on a cycle. Tests in Tasks 2 and 3.

---

## File Structure

```
go.mod, Makefile, README.md
cmd/waypoint/main.go                  wiring: config → registry → storage.Open → app.Service → cli.Run
internal/domain/board.go              Board, Settings, Column, Group, Card, Field, defaults, Validate
internal/domain/errors.go             sentinel errors
internal/domain/cards.go              AddCard, MoveCard, UpdateCard (CardPatch), DeleteCard
internal/domain/query.go              Blockers, NextUp, Warnings, CardHash
internal/domain/checklist.go          Checklist, ToggleCheck
internal/domain/archive.go            DueForArchive, Archive, Restore
internal/domain/filter.go             ParseFilter, Filter.Match
internal/storage/markdown/frontmatter.go   YAML front matter <-> domain.Settings
internal/storage/markdown/parse.go    Parse(board bytes), ParseArchive
internal/storage/markdown/format.go   Format(board), FormatArchive
internal/storage/markdown/repo.go     Repo: Load, Save, Lock, Path; Create
internal/storage/markdown/testdata/*.md   golden files
internal/storage/storagetest/contract.go  RunContract(t, factory): shared adapter suite
internal/storage/open.go              Open(ref config.ProjectRef) (app.Repository, error)
internal/app/ports.go                 Repository, Clock
internal/app/service.go               Service + use cases + CardView
internal/config/config.go             Config, ProjectRef, Load, Save, DefaultPath
internal/config/registry.go           Add, Remove, Find, FindByDir
internal/cli/cli.go                   Run(ctx, args, Env) int; flag helpers; output helpers
internal/cli/project.go               init, project add|ls|rm
internal/cli/cards.go                 ls, show, add, move, set, check, done, archive, restore, rm
internal/cli/agents.go                agents
docs/board-format.md                  the documented markdown format
```

---

### Task 1: Module scaffold and domain types

**Files:**
- Create: `go.mod`, `Makefile`, `internal/domain/board.go`, `internal/domain/errors.go`
- Test: `internal/domain/board_test.go`

**Interfaces:**
- Produces:
```go
package domain
type CardID string
type Column struct { Name string; WIP int; MovesTo []string; Done bool }
type Group struct { Name, Color string }
type Field struct { Key, Value string }
type Card struct {
    ID CardID; Title, Column, Group, Priority string
    Depends []CardID
    Created, Done time.Time // zero = unset; date-only semantics (UTC midnight)
    Body string
    Extra []Field // unknown fields, original order
}
type ArchiveMode string // "off" | "manual" | "auto"
const ( ArchiveOff ArchiveMode = "off"; ArchiveManual = "manual"; ArchiveAuto = "auto" )
type ArchiveSettings struct { Mode ArchiveMode; AfterDays int }
type Appearance struct { Theme, Accent, Density, Swimlane string } // "" = inherit global
type Settings struct {
    Name, Prefix string; NextID int
    Columns []Column; Groups []Group; Priorities []string
    Archive ArchiveSettings; Appearance Appearance
}
type Board struct { Settings Settings; Cards []Card; Archived []Card } // Cards order = rank order
func NewBoard(name, prefix string) *Board          // defaults from Global Constraints, NextID 1
func (s Settings) Validate() error                  // see test
func (b *Board) Column(name string) (Column, bool)  // exact match
func (b *Board) CardsIn(column string) []Card       // in rank order
func (b *Board) Find(id CardID) (c Card, archived bool, ok bool)
func Day(t time.Time) time.Time                      // truncate to UTC date
```
- Errors in `errors.go`: `ErrNotFound`, `ErrUnknownColumn`, `ErrUnknownGroup`,
  `ErrUnknownPriority`, `ErrMoveNotAllowed`, `ErrWIPLimit`, `ErrInvalid`, `ErrConflict`. Callers
  wrap them with `fmt.Errorf("%w: ...")`.

- [ ] **Step 1:** `go mod init waypoint`, set `go 1.24`. Create a `Makefile` with targets `test`
  (`go test ./...`), `build` (`go build -o bin/waypoint ./cmd/waypoint`) and `vet`.
- [ ] **Step 2: Write the failing tests**

```go
func TestNewBoardDefaults(t *testing.T) {
    b := NewBoard("Home", "H")
    names := []string{}
    for _, c := range b.Settings.Columns { names = append(names, c.Name) }
    assertEqual(t, names, []string{"Todo", "In progress", "Done"})
    done, _ := b.Column("Done")
    assertTrue(t, done.Done)
    todo, _ := b.Column("Todo")
    assertEqual(t, todo.MovesTo, []string{"In progress", "Done"})
    assertEqual(t, b.Settings.Priorities, []string{"P0", "P1", "P2"})
    assertEqual(t, b.Settings.Archive, ArchiveSettings{Mode: ArchiveManual, AfterDays: 14})
    assertEqual(t, b.Settings.NextID, 1)
    assertNoErr(t, b.Settings.Validate())
}

func TestValidateRejects(t *testing.T) {
    cases := map[string]func(*Settings){
        "no columns":        func(s *Settings) { s.Columns = nil },
        "duplicate column":  func(s *Settings) { s.Columns = append(s.Columns, s.Columns[0]) },
        "moves_to unknown":  func(s *Settings) { s.Columns[0].MovesTo = []string{"Nope"} },
        "negative wip":      func(s *Settings) { s.Columns[0].WIP = -1 },
        "bad prefix":        func(s *Settings) { s.Prefix = "T-1" },     // must match ^[A-Za-z][A-Za-z0-9]*$
        "next_id zero":      func(s *Settings) { s.NextID = 0 },
        "bad archive mode":  func(s *Settings) { s.Archive.Mode = "sometimes" },
        "duplicate group":   func(s *Settings) { s.Groups = []Group{{Name: "A"}, {Name: "a"}} }, // case-insensitive
    }
    for name, mutate := range cases {
        s := NewBoard("x", "T").Settings
        mutate(&s)
        if err := s.Validate(); !errors.Is(err, ErrInvalid) { t.Errorf("%s: got %v", name, err) }
    }
}
```
(Put small `assertEqual`/`assertTrue`/`assertNoErr` helpers using `reflect.DeepEqual` in
`internal/domain/helpers_test.go`. No testing library.)

- [ ] **Step 3:** Run `go test ./internal/domain/`. Expect a FAIL (undefined).
- [ ] **Step 4:** Implement the types, `NewBoard`, `Validate`, `Column`, `CardsIn`, `Find` and `Day`.
- [ ] **Step 5:** Run `go test ./internal/domain/`. Expect a PASS.
- [ ] **Step 6:** Commit with `feat(domain): board types, defaults and settings validation`.

---

### Task 2: Card operations and move rules

**Files:**
- Create: `internal/domain/cards.go`
- Test: `internal/domain/cards_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces:
```go
type NewCard struct { Title, Column, Group, Priority, Body string; Depends []CardID; Extra []Field }
func (b *Board) AddCard(n NewCard, today time.Time) (Card, error)
    // Column "" = first column. Appends at the bottom of the column. Sets ID = Prefix-NextID and
    // NextID++. Sets Created = Day(today), and Done = Day(today) if the column is a done column.
func (b *Board) MoveCard(id CardID, to string, index int, today time.Time) error
    // index < 0 or >= len = bottom. Same column = reorder only, always allowed.
func (b *Board) DeleteCard(id CardID) error   // board or archive; NextID unchanged
type CardPatch struct {
    Title, Group, Priority, Body *string
    Depends *[]CardID
    Set map[string]string // extra fields; "" value deletes the key
}
func (b *Board) UpdateCard(id CardID, p CardPatch) error
```

- [ ] **Step 1: Write the failing tests** (`today := time.Date(2026,11,3,15,0,0,0,time.UTC)`)
  - `TestAddCardAssignsIDAndDefaults`: on `NewBoard("x","T")` with `NextID=7`, `AddCard({Title:"A"})`
    gives `ID=="T-7"`, `Column=="Todo"`, `Created==2026-11-03`, and `NextID==8`. It's appended after
    existing Todo cards.
  - `TestAddCardUnknownColumnOrGroupOrPriority`: errors are `errors.Is` `ErrUnknownColumn`,
    `ErrUnknownGroup` and `ErrUnknownPriority`. Group and priority match case-insensitively and are
    stored with their canonical spelling.
  - `TestAddCardRejectsEmptyTitle`: whitespace-only title gives `ErrInvalid`.
  - `TestMoveAllowedSetsAndClearsDone`: Todo → Done sets `Done=2026-11-03`. Done → In progress
    clears it.
  - `TestMoveNotAllowed`: with Todo's `MovesTo=["In progress"]`, Todo → Done gives
    `ErrMoveNotAllowed` and the board is unchanged.
  - `TestMoveWIPLimit`: In progress has `WIP=1` and one card. Moving a second card in gives
    `ErrWIPLimit`. Reordering inside In progress still succeeds.
  - `TestMoveIndex`: Todo is [A,B,C]. `Move(C,"Todo",0)` gives [C,A,B]. `Move(A,"Todo",-1)` gives
    [C,B,A]. Moving to In progress at index 5 lands at the bottom.
  - `TestDeleteDoesNotReuseIDs`: delete T-1 and add a card. The new card is T-2 when `NextID` was 2.
  - `TestUpdatePatch`: set the title, priority `p1` (stored as `P1`), body, and `Set{"owner":"me"}`,
    then `Set{"owner":""}` removes it. Extra field order is preserved for untouched keys.
  - `TestUpdateRejectsSelfDependency` **(Review Focus 5)**: `Depends=&[]CardID{own id}` gives
    `ErrInvalid`.
- [ ] **Step 2:** Run `go test ./internal/domain/ -run 'Card|Move|Delete|Update'`. Expect a FAIL.
- [ ] **Step 3:** Implement. A move works by removing the card from `b.Cards`, then inserting it at
  the slice position of the `index`-th card of the target column. For the bottom case, insert it
  after the target column's last card, or, if the column is empty, after the last card of any
  earlier column in `Settings.Columns` order. That keeps `Cards` sorted by column order.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(domain): add, move, update and delete cards with column rules`.

---

### Task 3: Queries: blocked, next up, warnings, checklist, hash

**Files:**
- Create: `internal/domain/query.go`, `internal/domain/checklist.go`
- Test: `internal/domain/query_test.go`, `internal/domain/checklist_test.go`

**Interfaces:**
- Produces:
```go
type Blocker struct { ID CardID; Missing bool }
func (b *Board) Blockers(id CardID) []Blocker
    // each Depends entry not in a done column and not archived; unknown ids are Missing
func (b *Board) NextUp() (Card, bool)   // top card of first non-done column that has cards
func (b *Board) Warnings() []string     // e.g. `In progress is over its WIP limit (3/2)`
func CardHash(c Card) string            // hex sha256 over ID, Title, Column, Group, Priority, Depends, Body, Extra
type ChecklistItem struct { Text string; Checked bool }
func Checklist(body string) []ChecklistItem           // `- [ ]`, `- [x]`, `- [X]`, `* [ ]`; ignores fenced code
func (b *Board) ToggleCheck(id CardID, n int) error   // 1-based; ErrNotFound if n out of range
```

- [ ] **Step 1: Write the failing tests**
  - `TestBlockers`: A depends on [B, C, Z]. B is in Done and C is in Todo, so the result is
    `[{C,false},{Z,true}]`. Once C is archived, the result is `[{Z,true}]`.
  - `TestBlockersCycleTerminates` **(Review Focus 5)**: A depends on B and B depends on A. Both
    `Blockers` calls return within the test, with one blocker each. Blockers are direct only, never
    transitive.
  - `TestNextUp`: with Todo empty and In progress holding [X], the result is X. With every column
    empty except Done, it returns `ok=false`.
  - `TestWarnings`: In progress has WIP 2 and 3 cards, so the result is exactly
    `["In progress is over its WIP limit (3/2)"]`.
  - `TestCardHashStable`: equal cards give an equal hash, and changing the body changes it.
  - `TestChecklistIgnoresFences`: the body has 2 items plus one `- [ ] x` inside a fenced block,
    which gives 2 items.
  - `TestToggleCheck`: toggling item 2 rewrites only that line (`- [ ]` → `- [x]`), and the rest of
    the body is byte-identical. Toggling `- [X]` gives `- [ ]`. `n=3` gives `ErrNotFound`.
- [ ] **Step 2:** Run `go test ./internal/domain/ -run 'Block|Next|Warn|Hash|Check'`. Expect a FAIL.
- [ ] **Step 3:** Implement. Track fences with an exported helper,
  `func ScanLines(body string, fn func(i int, line string, inFence bool))`, which treats a line
  starting with three backticks or `~~~` as a fence toggle. It lives in `checklist.go` and is reused
  by Task 7.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(domain): blockers, next up, WIP warnings, checklist, card hash`.

---

### Task 4: Archive

**Files:**
- Create: `internal/domain/archive.go`
- Test: `internal/domain/archive_test.go`

**Interfaces:**
- Produces:
```go
func (b *Board) DueForArchive(today time.Time) []CardID
    // mode auto only: cards in done columns with Done set and Day(today)-Done >= AfterDays days
func (b *Board) Archive(ids []CardID) error   // moves to b.Archived (prepended, newest first); only cards in done columns, else ErrInvalid
func (b *Board) DoneCards() []CardID          // all cards currently in done columns
func (b *Board) Restore(id CardID, to string, today time.Time) error // to "" = first column; appended at bottom; done date set/cleared per target
```

- [ ] **Step 1: Write the failing tests**
  - `TestDueForArchiveAuto`: with `after_days 14`, cards done on 2026-10-20 and 2026-10-25 and
    today 2026-11-03, only the first is due (14 days ≥ 14).
  - `TestDueForArchiveManualOrOff`: the result is empty in both modes.
  - `TestArchiveRejectsNotDone`: archiving a Todo card gives `ErrInvalid`.
  - `TestRestore`: archiving and then restoring to "" puts the card at the bottom of Todo with
    `Done` cleared. `Find` reports `archived=false`.
- [ ] **Step 2:** Run `go test ./internal/domain/ -run Archive\|Restore`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(domain): archive and restore done cards`.

---

### Task 5: Filter

**Files:**
- Create: `internal/domain/filter.go`
- Test: `internal/domain/filter_test.go`

**Interfaces:**
- Produces:
```go
type Filter struct{ /* parsed terms */ }
func ParseFilter(q string) Filter
func (f Filter) Match(b *Board, c Card) bool
func (f Filter) Empty() bool
```
- Token rules: whitespace-separated tokens, with `"double quotes"` grouping a phrase. All tokens
  must match (AND). Every comparison ignores case.

| Token | Matches |
|---|---|
| a configured priority name (`p0`) | `Priority` equals it |
| `blocked` | `len(Blockers)>0` |
| `next` | the card is `NextUp` |
| `group:x` / `column:x` | the group or column name contains `x` |
| `key:value` (any other key) | an extra field `key` contains `value` |
| anything else | substring of ID, title or body |

- [ ] **Step 1: Write the failing tests** as a table test `TestFilterMatch` over a 4-card board,
  covering every row above. Include these cases:
  - `group:"security hard"` (quoted value)
  - `p0 blocked` (AND)
  - `t-019` (id substring)
  - an empty query, which matches all cards
- [ ] **Step 2:** Run `go test ./internal/domain/ -run Filter`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(domain): filter query language`.

---

### Task 6: Markdown parse

**Files:**
- Create: `internal/storage/markdown/frontmatter.go`, `internal/storage/markdown/parse.go`
- Test: `internal/storage/markdown/parse_test.go`, `internal/storage/markdown/testdata/basic.md`

**Interfaces:**
- Consumes: domain types and `Settings.Validate`.
- Produces:
```go
package markdown
type ParseError struct { Line int; Msg string }   // Error(): "line 12: <msg>"
func Parse(data []byte) (*domain.Board, error)     // board file; Archived left empty
func ParseArchive(data []byte, s domain.Settings) ([]domain.Card, error)
var ErrNotABoard = errors.New("not a Waypoint board (missing front matter)")
```
- Front matter is YAML between a first line `---` and the next `---`. It maps to Settings using
  the keys from spec §5.1 (`waypoint`, `name`, `prefix`, `next_id`, `columns[{name,wip,moves_to,done}]`,
  `groups[{name,color}]`, `priorities`, `archive{mode,after_days}`, `appearance{theme,accent,density,swimlane}`).
  Missing keys take `NewBoard` defaults. `waypoint` must be `1`; anything else gives a ParseError
  `unsupported format version N`.
- A card heading without an id (`### Fix the gate`) is accepted. It gets the next id in memory
  (`NextID++`) and `Parse` reports it through
  `func ParseAssigned(data []byte) (*domain.Board, []domain.CardID, error)`. `Parse` calls this
  and drops the list. Task 9 uses the list to persist the new ids.

- [ ] **Step 1: Write `testdata/basic.md`.** It's the spec §5.1 example extended with:
  - a Done card with a `done:` field;
  - an unknown field `- estimate: small`;
  - a body containing fenced code that includes a `### not a card` line;
  - a `#### Notes` heading inside a body;
  - an id-less card `### Call the plumber`.
- [ ] **Step 2: Write the failing tests**
  - `TestParseBasic`: checks the settings values, card count, each card's column, the order within
    Todo, `Depends`, the `Extra` order, a body that contains the fenced `### not a card` verbatim,
    and the id-less card getting `T-<next_id>` with `NextID` bumped.
  - `TestParseCRLF` **(Review Focus 4)**: replacing `\n` with `\r\n` in basic.md gives an identical
    board (`reflect.DeepEqual`).
  - `TestParseDuplicateID` **(Review Focus 1)**: the error is a `*ParseError` whose message contains
    `duplicate card id T-19` and both line numbers.
  - `TestParseUnknownColumn` **(Review Focus 2)**: `## In Progres` gives a `*ParseError` with that
    line number and the message `unknown column "In Progres"`.
  - `TestParseNoFrontMatter` **(Review Focus 3)**: plain markdown gives `errors.Is(err, ErrNotABoard)`.
  - `TestParseCardBeforeColumn`: a `###` before any `##` gives a ParseError.
  - `TestParseBadDate`: `- created: 26/10/2026` gives a ParseError on that line.
  - `TestParseArchive`: a `# Archive` file with `## 2026-10` and two cards gives two cards with
    their `Done` dates.
- [ ] **Step 3:** Run `go test ./internal/storage/markdown/`. Expect a FAIL.
- [ ] **Step 4:** Implement it as a line scanner (normalise `\r\n` first), using the rules below.
  Run `Settings.Validate` and wrap any failure as a ParseError on line 1.
  - **Fences:** toggle on lines starting with three backticks or `~~~`, and don't recognise
    headings while inside a fence.
  - **Card heading:** `### ` followed by an optional id matching `^[A-Za-z][A-Za-z0-9]*-\d+$` and
    then the title.
  - **Fields:** after a heading, skip blank lines, then read consecutive lines matching
    `^- ([a-z][a-z0-9_-]*): (.*)$`. The first non-matching line ends the fields.
  - **Body:** the rest up to the next heading, with leading and trailing blank lines trimmed.
  - **Column:** set from the enclosing `##`. Any text between a `##` and its first `###` is ignored.
- [ ] **Step 5:** Run the same command. Expect a PASS.
- [ ] **Step 6:** Commit with `feat(markdown): parse board and archive files`.

---

### Task 7: Markdown format and round-trip

**Files:**
- Create: `internal/storage/markdown/format.go`, `docs/board-format.md`, `docs/embed.go`
  (`package docs`, `//go:embed board-format.md`, `var BoardFormat string`)
- Test: `internal/storage/markdown/format_test.go`, `testdata/basic.canonical.md`

**Interfaces:**
- Produces: `func Format(b *domain.Board) []byte` and `func FormatArchive(cards []domain.Card) []byte`.
- Canonical layout:
  - front matter (keys in the spec §5.1 order, columns and groups in YAML flow style as in the
    spec, `appearance` and `archive` omitted when they equal the defaults);
  - a blank line;
  - for every configured column, even empty ones: `## Name`, a blank line, then each card;
  - each card: `### ID Title`, a blank line, fields, a blank line, the body (if any), a blank line.
  
  The file ends with exactly one `\n`.

- [ ] **Step 1: Write the failing tests**
  - `TestFormatGolden`: `Format(Parse(basic.md))` equals `testdata/basic.canonical.md`. Generate it
    once with `-update` (add a `var update = flag.Bool("update", false, "")` golden helper), then
    review it by hand against the layout rules before committing.
  - `TestRoundTripStable`: `Format(Parse(Format(Parse(x))))` equals `Format(Parse(x))`.
  - `TestBodyPreserved`: a body with trailing spaces, tabs, unicode, `#`s inside fences and a
    `## Heading` outside fences comes back byte-identical, except that `## Heading` becomes
    `#### Heading`.
  - `TestArchiveRoundTrip`: archived cards done in 2026-09 and 2026-10 give sections in the order
    `## 2026-10` then `## 2026-09`, and parsing back gives equal cards.
- [ ] **Step 2:** Run `go test ./internal/storage/markdown/ -run 'Format|Round|Body|ArchiveRound'`.
  Expect a FAIL.
- [ ] **Step 3:** Implement, demoting body headings with `domain.ScanLines` from Task 3.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Write `docs/board-format.md`, documenting the format (front matter keys, headings,
  fields, body rules, the id-less card shortcut, the archive file) with one example. The audience
  is people and agents editing by hand.
- [ ] **Step 6:** Commit with `feat(markdown): canonical writer, archive writer, format docs`.

---

### Task 8: Markdown repository and storage contract suite

**Files:**
- Create: `internal/app/ports.go`, `internal/storage/markdown/repo.go`,
  `internal/storage/storagetest/contract.go`
- Test: `internal/storage/markdown/repo_test.go`

**Interfaces:**
- Produces in `app`:
```go
type Repository interface {
    Load(ctx context.Context) (*domain.Board, []domain.CardID, error) // second = ids assigned during load
    Save(ctx context.Context, b *domain.Board) error
    Lock(ctx context.Context) (unlock func() error, err error)        // exclusive, cross-process
    Path() string
}
type Clock func() time.Time
```
- Produces in `markdown`: `func New(path string) *Repo`, and
  `func Create(path string, s domain.Settings) (*Repo, error)`. `Create` refuses an existing file
  with `os.ErrExist` and writes `Format(&Board{Settings:s})`.
  - **Load:** reads the board and, if present, `X.archive.md`.
  - **Save:** writes the board atomically. It writes the archive file only when `Archived` is
    non-empty or the file already exists.
- Produces in `storagetest`:
  `func RunContract(t *testing.T, newRepo func(t *testing.T) app.Repository)`, where `newRepo`
  returns a repo for a freshly created `NewBoard("Contract","T")`.

- [ ] **Step 1: Write `RunContract`** with these subtests:
  - `SaveLoadRoundTrip`: settings, cards, ranks, bodies, extras and archived cards survive.
  - `IDsNeverReused`.
  - `LockIsExclusive`: a second `Lock` with a 100ms context deadline fails while the first is held.
- [ ] **Step 2: Write `repo_test.go`**, which calls `RunContract` with a factory using
  `t.TempDir()`, plus:
  - `TestSaveIsAtomic`: after `Save`, no temp files are left in the directory.
  - `TestCreateRefusesExisting`.
  - `TestNoArchiveFileWhenEmpty`.
  - `TestLoadReturnsParseErrorAndDoesNotWrite`: corrupt the file, then `Load` errors and the file
    bytes are unchanged.
- [ ] **Step 3:** Run `go test ./internal/storage/...`. Expect a FAIL.
- [ ] **Step 4:** Implement it with `github.com/gofrs/flock` (`TryLockContext` with a 50ms retry)
  on `path+".lock"`, and `os.CreateTemp(dir, ".waypoint-*")` + `Sync` + `Rename` for the write.
- [ ] **Step 5:** Run the same command. Expect a PASS.
- [ ] **Step 6:** Commit with `feat(markdown): file repository with lock and atomic writes; storage contract suite`.

---

### Task 9: App service

**Files:**
- Create: `internal/app/service.go`
- Test: `internal/app/service_test.go` (uses the markdown repo on a temp dir, so the race tests are
  real)

**Interfaces:**
- Consumes: `Repository`, `Clock`, and the domain ops.
- Produces:
```go
type Service struct{ /* repo Repository; now Clock */ }
func NewService(r Repository, now Clock) *Service
type CardView struct {
    domain.Card
    Archived bool; Blockers []domain.Blocker; Next bool
    Checklist struct{ Done, Total int }; Hash string
}
type BoardView struct { Settings domain.Settings; Cards []CardView; Warnings []string }
func (s *Service) Board(ctx) (BoardView, error)                       // runs persistIfAssigned
func (s *Service) List(ctx, query string, includeArchived bool) ([]CardView, error)
func (s *Service) Get(ctx, id domain.CardID) (CardView, error)
func (s *Service) CreateCard(ctx, n domain.NewCard) (CardView, error)
func (s *Service) MoveCard(ctx, id domain.CardID, to string, index int) (CardView, error)
func (s *Service) Done(ctx, id domain.CardID) (CardView, error)        // move to first done column, bottom
func (s *Service) UpdateCard(ctx, id domain.CardID, p domain.CardPatch, ifMatch string) (CardView, error)
func (s *Service) CheckItem(ctx, id domain.CardID, n int) (CardView, error)
func (s *Service) DeleteCard(ctx, id domain.CardID) error
func (s *Service) ArchiveDone(ctx) ([]domain.CardID, error)           // archives DoneCards(); refused with ErrInvalid when mode is off
func (s *Service) Restore(ctx, id domain.CardID, to string) (CardView, error)
type ConflictError struct { Current CardView }                        // Unwrap() == domain.ErrConflict
```
- `apply(ctx, fn func(*domain.Board) error)`:
  1. Lock.
  2. Load. If that fails, return the error without saving.
  3. Run `fn`. If it fails, return the error without saving.
  4. If the mode is auto, `Archive(DueForArchive(now))`.
  5. Save.
  6. Unlock, deferred.
- `persistIfAssigned` is used by the read paths. When `Load` reports assigned ids, it re-runs
  through `apply` with a no-op `fn`, so hand-added id-less cards get stable ids on first read.
  `Board`, `List` and `Get` call it.

- [ ] **Step 1: Write the failing tests**
  - `TestExternalEditNotLost`: create a board with A and B. Externally, append a card C to the file
    with `os.WriteFile` (re-format the parsed board plus C, simulating an editor). Then
    `MoveCard(A,"In progress",0)`. The file now has A in In progress *and* C in Todo.
  - `TestUpdateConflict`: `Get(A)` gives hash h. An external change edits A's body, then
    `UpdateCard(A, title, h)` returns a `*ConflictError` with `errors.Is(err, domain.ErrConflict)`,
    `Current.Body` is the external body, and the file is unchanged. An empty `ifMatch` skips the
    check.
  - `TestParseErrorBlocksWrites`: corrupt the file, `CreateCard` errors, and the bytes are
    unchanged.
  - `TestAutoArchiveOnWrite`: with mode auto, `after_days 14`, a card done 20 days ago and the
    clock fixed, any write moves it to Archived and creates the `.archive.md` file.
  - `TestArchiveDoneRefusedWhenOff`.
  - `TestIdlessCardPersistedOnRead`: write a file with `### Call the plumber`, call `Board()`, and
    the file now has `### T-<n> Call the plumber`. A second `Board()` shows the same id.
  - `TestDoneMovesToFirstDoneColumn`, and `TestDoneRespectsRules` (Todo with
    `MovesTo=["In progress"]` gives `ErrMoveNotAllowed`).
  - `TestCardViewFields`: `Next`, `Blockers`, `Checklist{1,4}` and `Hash` are populated.
- [ ] **Step 2:** Run `go test ./internal/app/`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(app): service with lock-reread-apply writes, conflicts, auto-archive`.

---

### Task 10: Config and project registry

**Files:**
- Create: `internal/config/config.go`, `internal/config/registry.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
```go
type ProjectRef struct { Name string `yaml:"name"`; Storage string `yaml:"storage"`; Path string `yaml:"path"` } // Storage "markdown"|"sqlite"; Path absolute
type Config struct {
    Port int `yaml:"port"`; OpenBrowser bool `yaml:"open_browser"`; DataDir string `yaml:"data_dir"`
    Appearance domain.Appearance `yaml:"appearance"`; Projects []ProjectRef `yaml:"projects"`
}
func DefaultPath(env func(string) string) (string, error) // rule in Global Constraints
func Load(path string) (*Config, error)   // missing file → defaults, no error, nothing written
func (c *Config) Save(path string) error  // mkdir -p, atomic write
func (c *Config) Add(p ProjectRef) error  // ErrInvalid on duplicate name (case-insensitive) or duplicate path; Path made absolute and cleaned
func (c *Config) Remove(name string) error
func (c *Config) Find(name string) (ProjectRef, bool)
func (c *Config) FindByDir(cwd string) (ProjectRef, bool)
    // deepest project whose filepath.Dir(Path) is cwd or an ancestor of cwd;
    // else the single project whose Path is under cwd; else false
func StorageFor(path string) string // ".db", ".sqlite", ".sqlite3" → "sqlite", else "markdown"
```
- `domain.Appearance` needs yaml tags. Add `yaml:"theme,omitempty"` etc. in Task 1's struct, a
  single-line edit to `internal/domain/board.go`. Struct tags are allowed in domain because they
  have no import cost.

- [ ] **Step 1: Write the failing tests**
  - `TestDefaultPath`: `WAYPOINT_CONFIG` wins, then `XDG_CONFIG_HOME/waypoint/config.yaml`, then
    `$HOME/.config/waypoint/config.yaml`.
  - `TestLoadMissingGivesDefaults`: the result has `Port 4747`, `OpenBrowser true`, and `DataDir`
    ending in `.local/share/waypoint`, and no file is created.
  - `TestSaveLoadRoundTrip`.
  - `TestAddDuplicate`.
  - `TestFindByDir`, with projects at `/p/pharmacy/BOARD.md` and `/p/pharmacy/sub/x.md`:
    - cwd `/p/pharmacy/backend` gives pharmacy;
    - cwd `/p/pharmacy/sub/deep` gives sub;
    - cwd `/p` gives false, because two candidates are under it.
  - `TestStorageFor`.
- [ ] **Step 2:** Run `go test ./internal/config/`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run the same command. Expect a PASS.
- [ ] **Step 5:** Commit with `feat(config): global config and project registry`.

---

### Task 11: CLI skeleton, storage factory, project commands

**Files:**
- Create: `internal/storage/open.go`, `internal/cli/cli.go`, `internal/cli/project.go`,
  `cmd/waypoint/main.go`
- Test: `internal/cli/project_test.go`, `internal/cli/helpers_test.go`

**Interfaces:**
- Produces:
```go
package storage
func Open(ref config.ProjectRef) (app.Repository, error)  // "sqlite" → error "sqlite storage arrives in a later version" (Plan 3)
func Create(ref config.ProjectRef, s domain.Settings) (app.Repository, error)

package cli
type Env struct {
    Stdin io.Reader; Stdout, Stderr io.Writer
    Getenv func(string) string; Cwd string; Now app.Clock
}
func Run(ctx context.Context, args []string, env Env) int  // exit codes: 0 ok, 1 error, 2 usage
```
- **Flag parsing:** each command builds a `flag.FlagSet` (`ContinueOnError`) and parses it with
  `parseInterspersed(fs, args) ([]string, error)`, which repeatedly calls `fs.Parse` and collects
  `fs.Arg(0)` until no args are left. That way `waypoint move T-1 "In progress" --top` works.
  Every command registers `--json` and `--project`.
- **Project resolution** (`resolveProject`):
  1. `--project`;
  2. `$WAYPOINT_PROJECT`;
  3. `FindByDir(env.Cwd)`;
  4. the only registered project, if exactly one exists;
  5. otherwise exit 1 with
     `no project here; use --project NAME or run "waypoint project add"`.
- **Errors:** printed to stderr as `waypoint: <message>`. With `--json`, they go to stdout as
  `{"error":"...","code":"not_found|unknown_column|move_not_allowed|wip_limit|conflict|invalid|parse"}`,
  where `parse` carries `"line":N`.
- **Commands:**
  - `waypoint project add NAME [--path FILE] [--prefix T]`:
    - If `--path` is omitted, it uses `DataDir/<slug(NAME)>.md`.
    - If the file exists, it opens and validates it through `Load` (refusing on error, with the
      file untouched) and registers it.
    - Otherwise it creates the file with `NewBoard(NAME, prefix)` and registers it.
    
    It prints `Added project NAME (markdown) at PATH`.
  - `waypoint project ls`: shows name, storage and path, marking the current project with `*`.
  - `waypoint project rm NAME`: unregisters only, and prints `Removed NAME from the registry; PATH was not deleted`.
  - `waypoint init [NAME]`: `project add` with `--path ./BOARD.md` in the cwd. NAME defaults to
    the base name of the cwd.

- [ ] **Step 1: Write the test helper** `runCLI(t, cwd string, args ...string) (stdout, stderr string, code int)`.
  It sets `WAYPOINT_CONFIG` to a temp file and fixes `Now` to 2026-11-03.
- [ ] **Step 2: Write the failing tests**
  - `TestProjectAddNew`: it creates the file, `project ls` lists it, and the config contains it.
  - `TestProjectAddExistingBoard`: adding a pre-made Waypoint file keeps its cards.
  - `TestProjectAddPlainMarkdown` **(Review Focus 3)**: pointing it at `# Board\n## Todo\n...` exits
    1, stderr contains `not a Waypoint board`, and the file bytes and config are unchanged.
  - `TestProjectRmKeepsFile`.
  - `TestInitInCwd`.
  - `TestResolveProjectByCwd`: two registered projects, with the cwd inside the second's directory.
  - `TestJSONErrorShape`: `waypoint project rm nope --json` gives `{"error":...,"code":"not_found"}`
    with exit 1.
- [ ] **Step 3:** Run `go test ./internal/cli/`. Expect a FAIL.
- [ ] **Step 4:** Implement it, with `main.go` calling
  `os.Exit(cli.Run(ctx, os.Args[1:], cli.Env{..., Now: time.Now}))`. Running with no args prints
  usage, and Plan 2 changes that to `serve`.
- [ ] **Step 5:** Run `go test ./internal/cli/`, then `make build` and
  `./bin/waypoint project ls`. Expect a PASS, and an empty list printed as `No projects yet. Run "waypoint project add NAME".`
- [ ] **Step 6:** Commit with `feat(cli): command skeleton, storage factory, project commands`.

---

### Task 12: CLI card commands

**Files:**
- Create: `internal/cli/cards.go`, `internal/cli/output.go`
- Test: `internal/cli/cards_test.go`

**Interfaces:**
- Consumes: `app.Service` (constructed per invocation from `storage.Open(ref)` and `env.Now`).
- Commands (spec §8):

| Command | Behaviour |
|---|---|
| `ls [QUERY...] [--column X] [--group G] [--blocked] [--next] [--archived]` | Flags are sugar appended to the query (`column:"X"` etc.), then `Service.List`. |
| `show ID` | Card with all fields, blockers, checklist and body. |
| `add TITLE [--column X] [--group G] [--priority P] [--depends T-1,T-2] [--body TEXT\|-]` | `-` reads stdin. Prints `Created T-12 in Todo`. |
| `move ID COLUMN [--top\|--index N]` | Bottom by default. Prints `Moved T-12 to In progress`. |
| `set ID key=value...` | `title`, `group`, `priority`, `depends` (comma list, empty clears), `body` (`-` = stdin). Other keys are extra fields, with an empty value deleting. `--if-match HASH`. |
| `check ID N` | Toggle checklist item N. |
| `done ID` | `Service.Done`. |
| `archive` | `Service.ArchiveDone`. Prints `Archived N cards`. |
| `restore ID [--column X]` | |
| `rm ID` | `Service.DeleteCard`. |

- **Human output for `ls`:** one line per card, column-aligned:
  `T-19  Todo         P0  Security hardening  Authenticate the /ws WebSocket  [NEXT]`.
  - A blocked card appends `[BLOCKED by T-17]`, with missing ids shown as `T-99?`.
  - `ls` prints board warnings to stderr.
- **JSON output:** `ls` prints `[]CardView`, and single-card commands print one `CardView`
  (`encoding/json`, with lower-case keys set via json tags on `CardView`). Dates are `"YYYY-MM-DD"`,
  or `null` when unset. Add a `MarshalJSON` on `CardView` for this.

- [ ] **Step 1: Write the failing tests.** Run them end to end through `runCLI` on a project made
  by `init`.
  - `TestAddLsShow`: add 3 cards, then `ls` lines and order match, the first shows `[NEXT]`, and
    `show T-1` includes the body.
  - `TestMoveTopAndRules`: `move T-3 Todo --top` makes `ls` start with T-3. With Todo's moves_to
    restricted (edit the front matter in the file), `move T-1 Done` exits 1 with stderr
    `waypoint: move not allowed: Todo cannot move to Done`.
  - `TestSetFieldsAndDepends`: `set T-2 priority=p1 owner=me depends=T-1`, then `ls --blocked`
    lists T-2 with `[BLOCKED by T-1]`. `set T-2 owner=` removes the field. `set T-2 depends=T-2`
    exits 1.
  - `TestBodyFromStdin`.
  - `TestCheck`.
  - `TestDoneArchiveRestore`: `done T-1`, `archive` prints `Archived 1 cards`, `ls` no longer shows
    it but `ls --archived` does, and `restore T-1` brings it back to Todo.
  - `TestJSONOutput`: `ls --json` parses as an array with keys `id,title,column,group,priority,depends,created,done,body,extra,archived,blockers,next,checklist,hash`.
  - `TestIfMatchConflict`: `set T-1 title=x --if-match deadbeef --json` gives code `conflict`,
    exit 1.
  - `TestQueryPassthrough`: `ls p0 group:security` matches the filter semantics from Task 5.
- [ ] **Step 2:** Run `go test ./internal/cli/ -run 'Add|Move|Set|Body|Check|Done|JSON|IfMatch|Query'`.
  Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. Expect everything to PASS.
- [ ] **Step 5:** Commit with `feat(cli): card commands with human and JSON output`.

---

### Task 13: `waypoint agents`, README, end-to-end smoke

**Files:**
- Create: `internal/cli/agents.go`, `README.md`
- Test: `internal/cli/agents_test.go`

**Interfaces:**
- `waypoint agents` prints a markdown snippet for pasting into `AGENTS.md`/`CLAUDE.md`. It must
  contain each of these lines exactly (test asserts substrings):
  - `This project's board is managed by Waypoint. Prefer the CLI over editing the board file.`
  - `waypoint ls --next --json`
  - `waypoint add "Title" --group "Group" --priority P1`
  - `waypoint move <ID> "<Column>"`
  - `waypoint set <ID> key=value`
  - `waypoint done <ID>`
  - `If you edit the board file directly, follow the format printed by "waypoint help format"; a card heading without an id gets one automatically.`

  Also add `waypoint help format`, which prints `docs.BoardFormat` (Task 7).

- [ ] **Step 1:** Write the failing test `TestAgentsSnippet` (substrings above) and
  `TestHelpFormat` (output starts with the doc's first heading).
- [ ] **Step 2:** Run `go test ./internal/cli/ -run 'Agents|HelpFormat'`. Expect a FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. Expect everything to PASS.
- [ ] **Step 5:** Write `README.md` covering what Waypoint is (one paragraph, any kind of project),
  install (`make build`), quick start (`waypoint init`, `add`, `ls`, `move`, `done`), the config
  location, agents, and what's coming in Plans 2 and 3.
- [ ] **Step 6: End-to-end smoke** in a temp dir with `WAYPOINT_CONFIG` set to a temp file:

```bash
make build && export WAYPOINT_CONFIG=$(mktemp -d)/c.yaml && cd $(mktemp -d) && \
B=$OLDPWD/bin/waypoint && $B init Demo && $B add "Measure every room" && \
$B add "Paint the hallway" --depends T-1 && $B ls && $B done T-1 && $B ls --json && cat BOARD.md
```
  Expected:
  - `ls` shows T-1 `[NEXT]` and T-2 `[BLOCKED by T-1]`;
  - after `done`, T-2 is no longer blocked;
  - `BOARD.md` matches the canonical layout with T-1 under `## Done` and a `- done: <today>` field.
- [ ] **Step 7:** Commit with `feat(cli): agents snippet, embedded format help, README`.

---

## Self-review notes (resolved inline)

- **Spec coverage, in this plan:** §3 (all rules, including the done date, WIP, moves_to, archive
  modes, id-less cards), §5.1, §5.3 (lock, re-read, atomic write, hash conflict), §6 global config
  and registry, §7 layers and tests (contract suite, golden files), and §8 CLI.
- **Deferred to Plan 2:** §4 UI, appearance resolution (§6 overrides), the fsnotify watcher and
  SSE, the column-settings ops (rename, add, remove column), and `serve`.
- **Deferred to Plan 3:** §5.2 SQLite (`RunContract` is ready for it), §5.4 convert, §5.5 and §9
  import/export, and §8 MCP.
- **Names checked across tasks:**
  - `ScanLines`: exported from domain in Task 3, used in Task 7.
  - `docs.BoardFormat`: Task 7, used in Task 13.
  - `ParseAssigned`: Task 6, used by `Repo.Load` (Task 8) to fill the second return value.
  - `CardView`: Task 9, used in Task 12.
  - `ConflictError`: Task 9, used in Task 12.
  - `storage.Open`/`Create`: Task 11.
