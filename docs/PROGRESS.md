# Waypoint progress

Read this first when resuming. Spec: `docs/superpowers/specs/2026-11-03-waypoint-design.md`. Plans: `docs/superpowers/plans/`. Detailed ledger with every ruling: `.superpowers/sdd/2026-11-03-waypoint-web/progress.md` (local, gitignored).

## State

- **Plan 1 (core, CLI, markdown storage):** merged to `main` (PR #1).
- **Plan 2 (web UI):** on branch `feat/web`, paused.
  - Tasks 1-11 complete and reviewed. Midpoint whole-branch review and its fixes are complete.
  - Tasks 12-13 reviewed and their two Important findings fixed.
  - Task 14 (docs) and Task 15 (`make e2e`, 13 browser tests) committed.
- **Plan 3 (SQLite, convert, import/export, MCP, pharmacy migration):** not written.

## Next steps

1. Final whole-branch review through the real binary, then its fixes and a re-review.
2. Open a PR for `feat/web`.
3. Later, with the owner: a guided review of the whole branch, in about 8 sections with the riskiest first.

## How we work

- Build in the main session. Subagents only where a fresh context helps, such as code and security reviews.
- Keep milestones small enough to review by hand: one behaviour, about 300-500 changed lines with tests. Stop at each one with what changed, where to look and how to try it.

## Decisions to keep

- A project is a registry entry (name and board file path), not a folder or git repo. Commands use the nearest registered board in the current folder or a parent.
- Dates are stored as UTC timestamps and shown as the local date. Hand-written date-only values stay valid.
- Reviews go by risk: own review for risky tasks, groups for the rest, no re-review of small fixes unless they touch saving, locking or conflicts, and binary-level whole-branch reviews at the midpoint and end.
- Never run `waypoint trust` or touch the trust store in tests. Tests use temp `WAYPOINT_CONFIG` and `HOME`.

## Known limits

- A hand edit during a Waypoint write can rarely be lost (0-2 of about 230 under stress). Agents should use the CLI or API, which take the lock.
- The constrained local CA hasn't been tried in a real browser; check this on the first `waypoint trust`.
