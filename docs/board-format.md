# Waypoint board format

A Waypoint project is one Markdown file, usually named `BOARD.md`. It holds the
board settings in YAML front matter and every card as a heading with text under
it. You can edit it by hand, in any editor, or let an agent edit it. Waypoint
reads it again whenever it changes.

When Waypoint saves the board it rewrites the file in a standard layout. Your
card bodies are kept as they are, but spacing and field order outside bodies is
normalised. Archived cards live in a second file next to the board, described at
the end. An operation that changes nothing, such as archiving when no card is
done, leaves the file exactly as it is.

Waypoint also keeps a `BOARD.md.lock` file beside the board while it writes. It
holds nothing and is safe to ignore. To keep it out of version control add
`*.lock` to `.gitignore`, or `BOARD.md.lock` if the repository tracks other lock
files such as `Cargo.lock`.

## Example

````markdown
---
waypoint: 1
name: Pharmacy POS
prefix: T
next_id: 112
columns:
  - {name: Todo, moves_to: [In progress, Done]}
  - {name: In progress, wip: 2, moves_to: [Todo, Done]}
  - {name: Done, moves_to: [In progress], done: true}
groups:
  - {name: Security hardening, color: blue}
  - {name: Purchasing & receiving, color: amber}
archive: {mode: auto, after_days: 14}
---

## Todo

### T-019 Authenticate the /ws WebSocket

- group: Security hardening
- priority: P0
- depends: T-017
- created: 2026-10-26

`/ws` bypasses the middleware stack. Reuse the session check.

- [ ] add the check
- [ ] test a stale token

### Call the plumber

## In progress

### T-017 Rate limiting

- priority: P1
- created: 2026-10-20

## Done

### T-010 Ship the login screen

- priority: P2
- created: 2026-10-01
- done: 2026-10-15
````

## Front matter

The file must start with a line `---`, then YAML, then a second `---` line.
Unknown keys are an error, so a typo such as `colums` is reported instead of
ignored.

**Comments in the front matter are not kept.** A YAML `# comment` is read and
then dropped the next time Waypoint saves a change. Keep notes in a card instead.

| Key | Meaning |
| --- | --- |
| `waypoint` | Format version. Always `1`. Required: a file without it is not a board. |
| `name` | The project name. |
| `prefix` | Card id prefix: a letter, then letters or digits. Default `T`. |
| `next_id` | The number the next new card gets. Ids are never reused. Raised automatically if a card has a higher id. |
| `columns` | The columns, left to right. Each has `name`, and optionally `wip` (limit, 0 or absent for none), `moves_to` (columns a card may move to) and `done: true` (a done column). At least one column is required. |
| `groups` | Optional named groups, each with `name` and optionally `color`. |
| `priorities` | The priority names, highest first. Default `[P0, P1, P2]`. Left out when it is the default. |
| `archive` | `mode` (`off`, `manual` or `auto`) and `after_days`. Default `{mode: manual, after_days: 14}`. Left out when it is the default. |
| `appearance` | Optional per-project display overrides: `theme`, `accent`, `density`, `swimlane`, `textures`. |

Waypoint writes the keys in this order and quotes names that need it (commas,
colons, `#`, words like `yes` or `null`). By hand, quote such names yourself.

## Columns

`## Name` starts a column. The name must match a column in the front matter.
Columns in the front matter with no `##` heading are empty. Sections may appear
in any order in a hand-edited file; Waypoint puts them in column order when it
saves.

## Cards

`### ID Title` starts a card, for example `### T-019 Authenticate the /ws
WebSocket`. A card belongs to the nearest `##` column above it, and its position
in the file is its rank.

**Cards without an id.** A heading with no id, such as `### Call the plumber`,
is a new card. Waypoint gives it the next id from `next_id` the next time it
reads the file, and writes the id back. This is the quickest way to add a card
by hand.

Ids look like `T-19`: the board's prefix, a dash and a number. A duplicate id is
an error.

**Only this board's prefix makes an id.** On a board with prefix `T`, the
headings `### COVID-19 booster` and `### COVID-19` are cards without an id, titled
`COVID-19 booster` and `COVID-19`. This means that changing `prefix` by hand
turns the existing `### T-1 Title` headings into new cards titled `T-1 Title`,
each with a new id. The archive file is different: see below.

### Fields

The lines `- key: value` directly under the heading are the card's fields. A
blank line is allowed between the heading and the first field, and the fields end
at the first line that is not a field.

Known fields, always written in this order:

| Field | Value |
| --- | --- |
| `group` | A group name. |
| `priority` | A priority name. |
| `depends` | Card ids separated by commas: `T-17, T-18`. |
| `created` | A date, `YYYY-MM-DD`. |
| `done` | A date, `YYYY-MM-DD`. Set when a card enters a done column and cleared when it leaves. |

Any other `- key: value` line is kept as an extra field, in its original order.
Extra field names are lowercase letters, digits, `_` and `-`, starting with a
letter. Each field may appear once. Trailing spaces in a value are dropped.

A list item in the body such as `- Note: see below` is not a field, because
`Note` is not a valid field name.

**Two blank lines.** If a card has no fields and its text starts with a line that
looks like a field (`- estimate: small`), Waypoint writes two blank lines after
the heading. The second blank line tells the reader that this card has no
fields and that line is text. When editing by hand, do the same.

### Body

Everything after the fields, up to the next `##` or `###` heading outside a code
block, is the card's body: free Markdown, task lists (`- [ ] step`) included.

- Headings inside a body must be `####` or deeper. When Waypoint saves, a
  `#`, `##` or `###` heading in a body becomes `####`.
- A heading inside a fenced code block (` ``` ` or `~~~`) is just code and is
  left alone. A fence opened with backticks closes with backticks, and one opened
  with tildes closes with tildes.
- Apart from that, bodies are kept exactly as written, including trailing spaces
  and tabs. Blank lines at the start and end of a body are dropped.
- **An unclosed code fence is closed on save.** If a body ends inside a code
  fence, Waypoint adds a closing fence line at the end of that body, so the
  fence cannot swallow the cards below it.

## Errors and warnings

A file that cannot be read is never overwritten. Waypoint shows the error with
its line number and holds the board read-only until you fix the file.

- **Text outside a card is an error.** Anything other than blank lines that is
  not in front matter, a `##` column heading or a card is rejected, because
  saving the file would delete it. That includes text between a column heading
  and its first card, and a card heading before any column.
- A `##` heading that names no column, a duplicate field, a bad date, a bad id in
  `depends`, a duplicate card id, and a code fence that is never closed while
  headings follow it are errors too.
- **The same id on the board and in the archive is an error** if the two cards
  differ in anything but their column and `done` date, once both are normalised
  as a save would. Delete the copy you don't want, or if they are separate cards,
  give one a new id. If they match, this is what an interrupted save leaves
  behind: Waypoint keeps the board copy, warns, and removes the archive copy on
  the next save.
- **A code block containing a card-like heading raises a warning.** If a fenced
  block contains a line such as `### T-12 Title` or `## Done`, the board still
  loads, and Waypoint warns with the line numbers, because the usual cause is a
  fence that was never closed where the block was meant to end.

## Archive file

The archive for `BOARD.md` is `BOARD.archive.md` in the same directory; if the
board is a symlink, that is the directory of the real file. It starts with
`# Archive`, then one `## YYYY-MM` section per month of the cards' `done` dates,
newest month first, with the cards under it in the same format as above. Every archived card needs an id, of any prefix, and a `done` date, and has
no column. A card whose prefix is not the board's cannot be restored to it.

```markdown
# Archive

## 2026-10

### T-010 Ship the login screen

- priority: P2
- created: 2026-10-01
- done: 2026-10-15

## 2026-09

### T-003 Set up CI

- done: 2026-09-12
```
