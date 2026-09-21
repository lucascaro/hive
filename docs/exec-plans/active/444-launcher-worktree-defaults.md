# Launcher: ideas default to a worktree, plain new session never remembers it

- **Spec:** [docs/product-specs/444-launcher-worktree-defaults.md](../../product-specs/444-launcher-worktree-defaults.md)
- **Issue:** #444
- **PR:** #445
- **Branch:** feature/444-launcher-worktree-defaults
- **Status:** active

## Summary

Drop the launcher's sticky worktree preference and have the idea inbox's Start session force the worktree toggle on.

## Research

**Relevant code**
- `cmd/hivegui/frontend/src/app/modals/launcher.ts:103-133` — `openLauncher` computes `useWorktree` as `forced ?? localStorage['hive.worktree'] === '1'`; duplicate / resume modes force `false`.
- `cmd/hivegui/frontend/src/components/modals/Launcher.tsx:582-588` — the checkbox's onChange is the only writer of `hive.worktree`.
- `cmd/hivegui/frontend/src/app/modals/idea-inbox.ts:151-158` — `startSessionFromIdea` calls `openLauncher(project_id, { initialPrompt, ideaId, lockProject })`, no `forceWorktree`.
- Plain openers (all bare `openLauncher()` / `openLauncher(pid)`): `app/keyboard.ts:505,910`, `main.tsx:150`, `components/Sidebar.tsx:518`, `components/EmptyState.tsx:89`, `components/modals/Launcher.tsx:440`. ⇧⌘T openers pass `forceWorktree: true` (`keyboard.ts:504,912`, `main.tsx:154`, `Launcher.tsx:439`).
- Tests: `test/dom/launcher.test.tsx` seeds `localStorage['hive.worktree']` in ~20 cases to turn the toggle on; `open(opts)` forwards opts to `openLauncher`, so `open({ forceWorktree: true })` replaces the seed. `test/dom/idea-inbox.test.tsx:240` asserts the exact options object Start session passes.

**Constraints:** the launcher still hides/disables the toggle for non-git projects (`worktreeOff`), so forcing it on for an idea in a non-git project stays harmless — existing behaviour of ⇧⌘T.

**Prior lessons:** none matched.

**Conventions card**
- Tests: `scripts/test.sh` (layers go · unit · dom · e2e); this change is `unit dom e2e`. Lint: `biome ci .` + `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree).
- TDD: every behaviour change ships with its test.
- User-visible change: `.changesets/<slug>.md` (`type: changed`, `bump: patch`); never edit `CHANGELOG.md`.
- README Keybinds row `⌘T / ⇧⌘T` already reads correctly.

## Approach

Delete the sticky preference instead of special-casing ⌘T: the default becomes `forced ?? false`, and the checkbox stops writing `localStorage`. Every plain opener then opens with worktree off, and callers that want it on say so. `startSessionFromIdea` passes `forceWorktree: true`. Smaller than keeping the pref for some openers, and removes the "one tick changes every future launch" surprise everywhere (operator chose this in the clarifying round).

### Files to change

1. `src/app/modals/launcher.ts` — `useWorktree: … : (forced ?? false)` → simplify to `opts?.forceWorktree === true`; rewrite the sticky-pref comment.
2. `src/components/modals/Launcher.tsx` — remove the `localStorage.setItem('hive.worktree', …)` in the checkbox onChange.
3. `src/app/modals/idea-inbox.ts` — add `forceWorktree: true` to `startSessionFromIdea`, and a line in its comment.
4. `test/dom/launcher.test.tsx` — replace every `localStorage.setItem('hive.worktree', '1')` seed with `open({ forceWorktree: true, … })` (and drop `'0'` seeds); update `survives a query that matches no agent` to assert the checkbox is checked rather than the localStorage write.
5. `test/dom/idea-inbox.test.tsx` — add `forceWorktree: true` to the expected options.
6. `.changesets/launcher-worktree-defaults.md` — `type: changed`, `bump: patch`, issue 444.

### Tests

- `launcher.test.tsx` › new `describe('launcher worktree default')`:
  - `opens with the toggle off by default, whatever the last opening chose` — open, tick the box, close, open bare → unchecked.
  - `never persists the toggle` — tick the box → `localStorage.getItem('hive.worktree')` is null.
  - `ignores a stale remembered preference` — seed `hive.worktree=1`, open bare → unchecked.
  - `opens with the toggle on when forced` — `open({ forceWorktree: true })` → checked.
- `idea-inbox.test.tsx` › `Start session opens the launcher with the prompt and the project pinned` — expects `forceWorktree: true`.

### Verification

- `cd cmd/hivegui/frontend && npx vitest run test/dom/launcher.test.tsx test/dom/idea-inbox.test.tsx` — the new default tests fail on today's code (stale-pref and persistence cases).
- `scripts/test.sh unit dom e2e`, `npx biome ci .`, `npm run typecheck`.
- `grep -rn "hive.worktree" cmd/hivegui/frontend/src` returns nothing.

## Decision log

- **2026-09-20** — Drop the sticky worktree pref entirely rather than special-casing ⌘T. Why: operator's choice in clarifying round A; consistent across all plain openers and deletes code.
- **2026-09-20** — Assumed Start session's worktree default is a one-shot `forceWorktree` (editable, not persisted), and duplicate/resume modes unchanged. Why: matches ⇧⌘T's existing semantics.
- **2026-09-20** — Also gave `.launcher-branch` `box-sizing: border-box`. Why: with the toggle now on for ideas, the e2e layout test (`idea-inbox.spec.ts` › the opening prompt stays inside the launcher popup) caught the branch field overflowing the popup sideways by 2px — a latent bug ⇧⌘T already had.
- **2026-09-20** — Leave the stale `hive.worktree` key in users' localStorage alone. Why: nothing reads it after this change; a cleanup shim is inert code.

## Progress

- **2026-09-20** — Issue #444 filed; triaged S/P2; research done.
- **2026-09-20** — Plan approved. Implemented: new default tests red first, then green; `scripts/test.sh unit dom e2e`, `biome ci`, typecheck, `ui-lint` all pass.

## Open questions

## Second opinion

- **Verdict:** approve · **Confidence:** 8 · **Round:** 1
- **Rationale:** All `openLauncher` call sites traced; the file list covers every reader/writer of `hive.worktree`; duplicate/resume force `useWorktree: false` independently of `forced`, so non-goals hold structurally; the new tests fail on today's code.
- **Nice to have (applied during implementation):** the `openLauncher` comment naming `view.ts` as a bare caller is stale — fix it while in the file.

## PR convergence ledger

- **2026-09-20 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 31e6453.
