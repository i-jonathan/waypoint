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
- depends: T-017, T-018
- created: 2026-10-26

`/ws` bypasses the middleware stack… (free markdown, checklists allowed)

- [ ] step one
- [x] step two

### T-020 Reorder suggestions
- group: Purchasing & receiving
- priority: P1
- estimate: small
- reviewer: dana
- created: 2026-10-27

Sketch of the query:

```sql
### not a card
SELECT * FROM stock;
```

#### Notes

Keep the supplier list small.

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
