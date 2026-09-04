# GUI: don't switch sessions while the close confirmation is up

- **Spec:** [docs/product-specs/close-confirm-no-switch.md](../../product-specs/close-confirm-no-switch.md)
- **Issue:** #330
- **PR:** #331
- **Branch:** `feature/330-close-confirm-no-switch`
- **Status:** completed

## Summary

Focus jumps to a neighbouring session during the pre-flight worktree check, so the
worktree-dirty close dialog is shown over the wrong session. Narrow the focus-switch
branch in `events.ts` to the real `closing` phase.

## Research

Authored via plan-first mode.

- `cmd/hivegui/frontend/src/app/events.ts` — the `session:event` handler; the
  `ev.kind === 'updated' && isClosing(phaseOf(ev.session))` branch hands focus to
  `neighbourOf(...)` when the active session is going away. Same file's
  `control:error` handler raises the three-way `worktree_dirty` choice dialog.
- `cmd/hivegui/frontend/src/lib/phase-steps.ts` — `isClosing()` returns true for
  both `checking` and `closing`; `PHASE` carries the phase constants.
- `internal/registry/registry.go` — `Registry.kill` sets phase `checking`, runs
  `worktree.HasUncommitted`, and on a dirty worktree sets the phase back to `ready`
  and returns `ErrWorktreeDirty` without touching anything else. Phase `closing` is
  only set past that pre-flight, i.e. once the close is really happening.
- Other `isClosing()` callers — `src/app/session-term.ts` (tile `.closing`
  dimming), `src/components/SessionRow.tsx` ("Closing…" label),
  `src/app/events.ts` `pty:disconnect` guard — none switch focus, so the shared
  predicate stays as-is.

## Approach

Narrow the one focus-switch site instead of redefining `isClosing()`: compare
`phaseOf(ev.session)` against `PHASE.closing` directly. The alternative — dropping
`checking` from `isClosing()` — would also stop the tile dimming and the "Closing…"
row label during the pre-flight `git status`, which can take seconds on a large
worktree and is exactly when the user wants to see that something is happening.

### Files to change

- `cmd/hivegui/frontend/src/app/events.ts` — switch focus only on `PHASE.closing`;
  comment says why `checking` is excluded.

### New files

- `cmd/hivegui/frontend/test/dom/close-focus.test.ts` — regression test.
- `.changesets/330-close-confirm-no-switch.md` — user-visible fix.

### Tests

- `close-focus.test.ts` › "keeps focus on the session while its worktree is only
  being checked" — active session, `updated` with phase `checking`, `switchTo` spy
  not called.
- `close-focus.test.ts` › "switches to the neighbour once the session is really
  closing" — same session, phase `closing`, `switchTo` called with the neighbour id.

## Decision log

- **2026-09-03** — Narrowed the call site, not `isClosing()`. Why: the other three
  callers want `checking` included.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec frontmatter).
- **2026-09-03** — Implemented; PR #331 open; stage = REVIEW.
- **2026-09-03** — Gate PASS; stage = DONE.
- **2026-09-03** — Review loop converged (APPROVE, 1 iteration, 0 open threads); applied the one MINOR test-hygiene nit; stage = GATE.

## PR convergence ledger

- **2026-09-03 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 53d3532.

## Gate verdict

- **2026-09-03** — verdict: PASS; checks: 3 passed / 0 failed / 0 followups; followups: none; one-line: all three success criteria demonstrated, no non-goal bleed, changeset and docs accurate.
  - 2026-09-03 dimensions:
    - acceptance — PASS — `checking` no longer switches focus, `closing` still does, cancel leaves the user in place (daemon resets to ready on refusal).
    - non-goals — PASS — `isClosing()` untouched and its three other callers still cover both phases; no Go/daemon change.
    - doc accuracy — PASS — changeset present and schema-correct; spec/plan match the diff; no README/AGENTS.md claim invalidated.

## Open questions

None.
