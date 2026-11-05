# Waypoint progress

Read this first when resuming. Spec: `docs/superpowers/specs/2026-11-03-waypoint-design.md`. Plans: `docs/superpowers/plans/`. Detailed ledger with every ruling: `.superpowers/sdd/2026-11-03-waypoint-web/progress.md` (local, gitignored).

## State

- **Plan 1 (core, CLI, markdown storage):** merged to `main` (PR #1).
- **Plan 2 (web UI):** on branch `feat/web`, PR open, waiting for the owner's guided review.
  - All 15 tasks complete. Midpoint and final whole-branch reviews done, with their fixes and re-reviews.
- **Plan 3 (SQLite, convert, import/export, MCP, pharmacy migration):** not written.

## Next steps

1. With the owner: a guided review of the whole branch, in about 8 sections with the riskiest first.
2. Merge the PR when the owner decides.
3. Write Plan 3 in small milestones: SQLite first, then convert, the pharmacy migration, import/export and MCP.

- **Plan 4 (proposed):** choose the SQL server per project: SQLite, or Postgres or MySQL, local or remote. One person's data only, even on a remote server. Plan 3's SQL storage must leave room for this.

## How we work

- Build in the main session. Subagents only where a fresh context helps, such as code and security reviews.
- Keep milestones small enough to review by hand: one behaviour, about 300-500 changed lines with tests. Stop at each one with what changed, where to look and how to try it.

## Decisions to keep

- Plan 3: SQLite is the default storage for new projects. Markdown stays a supported choice, and existing markdown boards keep working.
- A project is a registry entry (name and board file path), not a folder or git repo. Commands use the nearest registered board in the current folder or a parent.
- Dates are stored as UTC timestamps and shown as the local date. Hand-written date-only values stay valid.
- Reviews go by risk: own review for risky tasks, groups for the rest, no re-review of small fixes unless they touch saving, locking or conflicts, and binary-level whole-branch reviews at the midpoint and end.
- Never run `waypoint trust` or touch the trust store in tests. Tests use temp `WAYPOINT_CONFIG` and `HOME`.

## Known limits

- On plain HTTP, more than five visible Waypoint windows at once can stall writes. Hidden tabs release their connection, and HTTPS has no such limit.
- A read-only board with a hand-added card without an id can't be read until its id is assigned. Temp files from a crash stay until deleted by hand.
- A hand edit during a Waypoint write can rarely be lost (0-2 of about 230 under stress). Agents should use the CLI or API, which take the lock.
- The constrained local CA hasn't been tried in a real browser; check this on the first `waypoint trust`.
