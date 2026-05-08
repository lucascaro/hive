# Product specs

The *what* and *why* of work this project plans to do. Each spec describes user value, success criteria, and explicit non-goals. The *how* lives in `docs/exec-plans/`.

## Active

| Priority | Issue | Title | Stage | Spec |
|----------|-------|-------|-------|------|
| <P1> | #<n> | <title> | TRIAGE \| RESEARCH \| PLAN \| IMPLEMENT | [<slug>](<slug>.md) |
| P2 | #159 | Returning to grid leaves session visually selected but keyboard input doesn't reach it | IMPLEMENT | [159-grid-return-session-input-focus](159-grid-return-session-input-focus.md) |

## Completed

| Issue | Title | Shipped | Spec |
|-------|-------|---------|------|
| #<n> | <title> | <date> | [<slug>](<slug>.md) |
| #155 | Save session name on Enter key when editing | 2026-05-07 | [155-save-session-name-on-enter-key](155-save-session-name-on-enter-key.md) |

## Rejected

| Issue | Title | Reason |
|-------|-------|--------|
| #<n> | <title> | <one line> |

## Conventions

- Stage is owned by the exec plan, not the spec — when stage changes, update this index from the plan.
- A spec is created in TRIAGE and lives forever (it is the historical record of *why we built it*). The exec plan moves to `exec-plans/completed/` on merge; the spec stays put.
- `feature-next` reads from this file's Active table, ordered by priority.
