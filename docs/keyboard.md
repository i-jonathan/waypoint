# Keyboard reference

Everything on the board can be done from the keyboard. This page lists the keys as they work in the web UI. Keys with a letter also work with Caps Lock on, and none of them fire while you are typing in a field.

## Anywhere on a board view

| Key | What it does |
| --- | --- |
| `n` | New card. Opens quick add in the first column. From "Next up" or "Blocked" it goes to the board first. Does nothing while the board is read-only. |
| `/` | Moves to the filter box. |
| `Esc` in the filter | Clears the filter. With the filter already empty, leaves the box. |

Filter words: a priority name such as `p0`, `blocked`, `next`, `group:name`, `column:name`, `field:value` for an extra field, and plain words that match the id, title or text. Every word must match, and quotes group words, as in `group:"Security hardening"`.

## On a card

Move focus to a card with Tab (the board is one stop in the tab order), then:

| Key | What it does |
| --- | --- |
| `ArrowUp`, `ArrowDown` | Focus the card above or below, in the same column. |
| `ArrowLeft`, `ArrowRight` | Focus a card in the neighbouring column, at about the same height. Empty columns are skipped. With swimlanes on, it prefers a card in the same lane. |
| `Shift+ArrowUp`, `Shift+ArrowDown` | Move the card one place up or down among the cards you can see. |
| `Shift+ArrowLeft`, `Shift+ArrowRight` | Move the card to the next column in that direction that accepts it, at the same height as far as the column goes. Columns the card is not allowed to move to are skipped. At the edge, a spoken message says why nothing moved. |
| `m` | Opens the move menu for the card. |
| `Enter` or `Space` | Opens the card in the panel. |

In "Next up", "Blocked" and "Archive", cards are not in the tab order. Each has an Open button, and `Enter` or `Space` on a focused card opens it too.

Moves announce themselves to screen readers, for example "T-3 moved to In progress". The board is read-only while the file has an error, and then the move keys do nothing.

### The move menu (`m`)

| Key | What it does |
| --- | --- |
| `ArrowDown`, `ArrowUp` | Next or previous item, wrapping round. |
| `Home`, `End` | First or last item. |
| `Enter` | Does the item: move to a column (to the bottom of it), move to another swimlane when swimlanes are on, or to the top or bottom of the current column. |
| `Esc` | Closes the menu and returns focus to the card. |

Only columns the card may move to are listed.

### Dragging

With a mouse or touch, press on a card and drag it to a column (or, with swimlanes, to a lane). `Esc` during a drag puts the card back. A drag with a filter on leaves the cards the filter hides in their places.

## Quick add

| Key | What it does |
| --- | --- |
| `Enter` | Adds the card and keeps the box open for the next one. |
| `Esc` | Closes quick add. |
| Paste several lines | Asks before making one card per line. |

## The card panel

| Key | What it does |
| --- | --- |
| `Ctrl+Enter` or `Cmd+Enter` | Saves the card. |
| `Esc` | Closes the panel. If you have unsaved changes it asks first. |

## Menus and dialogs

| Where | Keys |
| --- | --- |
| Project switcher | `ArrowDown` on the button opens it. Then `ArrowUp`, `ArrowDown`, `Home`, `End` to choose, `Enter` to open a project, `Esc` to close. |
| New project dialog | `ArrowLeft`, `ArrowRight`, `Home`, `End` on the tabs switch between Create and Open existing file. `Esc` closes the dialog. Focus stays inside it until it closes. |
| Sidebar on a narrow window | `Esc` closes it and returns focus to the menu button. |
| Reordering columns in settings | `Esc` during a drag puts the column back. |
