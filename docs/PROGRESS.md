# Waypoint progress

Read this first when resuming. Spec: `docs/superpowers/specs/2026-11-03-waypoint-design.md`. Plans: `docs/superpowers/plans/`. Detailed ledger with every ruling: `.superpowers/sdd/2026-11-03-waypoint-web/progress.md` (local, gitignored).

## State

- **Plan 1 (core, CLI, markdown storage):** merged to `main` (PR #1).
- **Plan 2 (web UI):** on branch `feat/web`, paused.
  - Tasks 1-11 complete and reviewed. Midpoint whole-branch review and its fixes are complete.
  - Tasks 12-13 built, group review not finished.
  - Task 14 (docs) committed. Task 15 (end-to-end suite) not started.
- **Plan 3 (SQLite, convert, import/export, MCP, pharmacy migration):** not written.

## Next steps

1. Finish the review of Tasks 12-13: settings saves must not lose or orphan cards; see the ledger.
2. Build Task 15, the Playwright suite (`make e2e`), with the extra scenarios in the ledger's dispatch notes.
3. Final whole-branch review through the real binary, then its fixes and a re-review.
4. Open a PR for `feat/web`.

## Decisions to keep

- A project is a registry entry (name and board file path), not a folder or git repo. Commands use the nearest registered board in the current folder or a parent.
- Dates are stored as UTC timestamps and shown as the local date. Hand-written date-only values stay valid.
- Reviews go by risk: own review for risky tasks, groups for the rest, no re-review of small fixes unless they touch saving, locking or conflicts, and binary-level whole-branch reviews at the midpoint and end.
- Never run `waypoint trust` or touch the trust store in tests. Tests use temp `WAYPOINT_CONFIG` and `HOME`.

## Known limits

- A hand edit during a Waypoint write can rarely be lost (0-2 of about 230 under stress). Agents should use the CLI or API, which take the lock.
- The constrained local CA hasn't been tried in a real browser; check this on the first `waypoint trust`.
