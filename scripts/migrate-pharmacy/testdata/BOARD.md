# Board

Kanban board for the Pharmacy POS. Finished cards live in [DONE.md](DONE.md).

**In Progress** → **Up Next** → **Backlog** · **Done** → [DONE.md](DONE.md)

<!-- next-id: T-12 -->

<details>
<summary>How this board works</summary>

- Each `##` heading is a column; each `###` heading is a card.

Card format:

```markdown
### T-000 Short imperative title

- priority: P0 | P1 | P2
- feature: one of the features listed below
- depends: T-000, T-000   (optional)
```

Features: Security hardening, Notifications.

</details>

## In Progress

_Nothing in progress. Pull the top card from Up Next._

## Up Next

### T-3 Authenticate the /ws socket

- priority: P0
- feature: Security hardening
- depends: T-1, T-2
- created: 2026-10-26

`/ws` bypasses the middleware stack.

- [ ] step one
- [x] step two

### T-4 Fix money rounding

- priority: P1
- feature: Reliability & operations
- merged: T-8, T-9
- created: 2026-10-27

Body with code:

```go
### T-99 not a card
x := "a,b"
```

## Backlog

### T-5 Notification centre

- priority: P2
- feature: Notifications
- created: 2026-10-28

Quoted "text", commas, and
two lines.
