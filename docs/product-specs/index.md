# Product specs

The *what* and *why* of work this project plans to do. Each spec describes user value, success criteria, and explicit non-goals. The *how* lives in `docs/exec-plans/`.

## Active

| Priority | Issue | Title | Stage | Spec |
|----------|-------|-------|-------|------|
| <P1> | #<n> | <title> | TRIAGE \| RESEARCH \| PLAN \| IMPLEMENT | [<slug>](<slug>.md) |
| P1 | #172 | Pin/capture agent session id for Gemini and Copilot | IMPLEMENT | [172-agent-session-id-gemini-copilot](172-agent-session-id-gemini-copilot.md) |
| — | #142 | vt snapshot: CJK / wide-char column misalignment | TRIAGE | [142-vt-snapshot-cjk-wide-char-column-misalignment](142-vt-snapshot-cjk-wide-char-column-misalignment.md) |

## Completed

| Issue | Title | Shipped | Spec |
|-------|-------|---------|------|
| #<n> | <title> | <date> | [<slug>](<slug>.md) |
| #165 | Restarting a session can reload the wrong session when multiple share a worktree/directory | 2026-05-08 | [165-restart-session-wrong-session](165-restart-session-wrong-session.md) |
| #163 | GUI: resize loses scroll position when viewport is 1-2 lines short of bottom | 2026-05-08 | [163-resize-stick-mostly-bottom](163-resize-stick-mostly-bottom.md) |
| #159 | Returning to grid leaves session visually selected but keyboard input doesn't reach it | 2026-05-08 | [159-grid-return-session-input-focus](159-grid-return-session-input-focus.md) |
| #142 | vt snapshot: CJK / wide-char column misalignment | 2026-05-08 | [142-vt-snapshot-cjk-wide-char-alignment](142-vt-snapshot-cjk-wide-char-alignment.md) |
| #155 | Save session name on Enter key when editing | 2026-05-07 | [155-save-session-name-on-enter-key](155-save-session-name-on-enter-key.md) |
| #144 | vt snapshot: RGB / 24-bit colors fall back to default | 2026-05-08 | [144-vt-snapshot-rgb-24-bit-colors](144-vt-snapshot-rgb-24-bit-colors.md) |
| #143 | vt snapshot: scrollback above visible viewport not preserved | 2026-05-08 | [143-vt-snapshot-scrollback-above-visible-viewport](143-vt-snapshot-scrollback-above-visible-viewport.md) |

## Rejected

| Issue | Title | Reason |
|-------|-------|--------|
| #<n> | <title> | <one line> |

## Conventions

- Stage is owned by the exec plan, not the spec — when stage changes, update this index from the plan.
- A spec is created in TRIAGE and lives forever (it is the historical record of *why we built it*). The exec plan moves to `exec-plans/completed/` on merge; the spec stays put.
- `feature-next` reads from this file's Active table, ordered by priority.
