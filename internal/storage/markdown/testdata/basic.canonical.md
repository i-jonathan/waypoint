---
waypoint: 1
name: Pharmacy POS
prefix: T
next_id: 113
columns:
  - {name: Todo, moves_to: [In progress, Done]}
  - {name: In progress, wip: 2, moves_to: [Todo, Done]}
  - {name: Done, moves_to: [In progress], done: true}
groups:
  - {name: Security hardening, color: blue}
  - {name: Purchasing & receiving, color: amber}
archive: {mode: auto, after_days: 14}
appearance: {accent: '#7c3aed'}
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
- created: 2026-10-27
- estimate: small
- reviewer: dana

Sketch of the query:

```sql
### not a card
SELECT * FROM stock;
```

#### Notes

Keep the supplier list small.

### T-112 Call the plumber

## In progress

### T-017 Rate limiting

- priority: P1
- created: 2026-10-20

## Done

### T-010 Ship the login screen

- priority: P2
- created: 2026-10-01
- done: 2026-10-15
