# Rank sessions needing attention higher in the ⌘P session finder

- **Spec:** [docs/product-specs/562-rank-sessions-needing-attention-higher-in-the-p-se.md](../../product-specs/562-rank-sessions-needing-attention-higher-in-the-p-se.md)
- **Issue:** #562
- **Status:** completed
- **PR:** #564
- **Branch:** feature/562-finder-attention-first

## Summary

The ⌘P session finder (spec 544) orders an empty query purely by recency, so a session waiting on the user can sit far down the list. This plan adds a "flagged" group to the finder's ordering — right after the previous session on an empty query, and as a tie-break on a typed query — without touching ⌘B.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/lib/session-finder.ts` — pure ranking module. `recencyOrder()` (previous session first, active last, rest in input order) and `finderRows()` (score, stable sort; ties keep recency). All ordering lives here.
- `cmd/hivegui/frontend/src/lib/session-state.ts:159` — `sessionState(s: StateCarrier)`, the single SessionInfo → state resolution used by the sidebar, chip and grid tile. Pure and importable from the node unit suite, so `session-finder.ts` can call it directly. The flagged states named by the spec are `attention`, `waiting-permission`, `failed`, `error`.
- `cmd/hivegui/frontend/src/components/modals/CommandPalette.tsx:113-150` — renders `finderRows()` inside a `useMemo` keyed on the store's `sessions`, so a state change re-ranks the open list. Selection is tracked **by index** (`selected`, line 109; `activeIndex`, line 186).
- `cmd/hivegui/frontend/src/app/selectors.ts:56` — `nextAttentionId()` (⌘B) uses `readNeedsAttention`, not `sessionState`. Out of scope (spec non-goal).
- Tests: `cmd/hivegui/frontend/test/unit/session-finder.test.ts` (recency + scale cases), `test/e2e/session-finder.spec.ts`.
- User-facing docs: `README.md:379` (⌘P row), `site/features.json:244` ("Go to session" blurb).

### Constraints / dependencies

- `FinderSession` is a structural slice; it must gain the `StateCarrier` fields (`alive`, `phase`, `state`, `needs_attention`, `last_error`/`lastError`) for `sessionState()` to resolve. The store's `SessionInfo` already carries them.
- "Previous session" means the most recent `back` entry that still exists and is not active. With no history there is no previous session to pin, and the flagged group leads.
- Live reorder hazard: with flags in the ordering, a state flip while the finder is open moves rows under an index-based selection, so Enter could hit a different session than the highlighted one at the time the user looked.

### Prior lessons

- No prior lessons matched.

### Conventions card

- Tests: `scripts/test.sh [go|unit|dom|e2e]`; frontend unit tests in `cmd/hivegui/frontend/test/unit`. Local Playwright with `CI=1`.
- Typecheck needs the generated wailsjs bindings: `./scripts/ci-bootstrap.sh` in a fresh worktree.
- Lint: `biome ci .` (checks formatting too), `scripts/ui-lint.sh`.
- TDD: every behaviour change ships with the test that would catch its regression.
- A user-visible change needs `.changesets/<slug>.md`; README and `site/features.json` must stay accurate.

## Approach

All ordering stays in the pure `lib/session-finder.ts`. It gains a `flagged(s)` predicate: `sessionState(s) ∈ {attention, waiting-permission, failed, error}` (reusing `lib/session-state.ts`, the single state resolver the sidebar/chip/tile use, so the finder can never disagree with the status icon it renders). The active session is never treated as flagged (it is pinned last; ⌘B skips it for the same reason).

`recencyOrder()` keeps its existing previous-session logic unchanged (the first item it takes from `back` via `take()`, with `seen` seeded by `activeId`) and only partitions what comes after it:
1. previous = that first item taken from `back`, if `back` yielded one (no history ⇒ nothing pinned),
2. flagged sessions in recency order,
3. unflagged sessions in recency order,
4. active.
The previous session appears exactly once even if it is itself flagged (`seen` dedupes).

Flags are computed **once per row** before sorting and carried in the `scored` entry (not inside the comparator). `finderRows()` typed sort comparator: `b.score - a.score`, then flagged-before-unflagged, then recency position (stable sort). Score always wins; attention only breaks ties.

`CommandPalette.tsx`: selection becomes `{ key, index, query }` (what was picked, where, under which query). `activeIndex` = the picked key's current index **when the query is unchanged and the key is still listed** (follows the session through a live reorder); otherwise the old index-clamp rule exactly as today (`index < filtered.length ? index : 0`). So typing behaves byte-for-byte as now (spec 544 e2e top-match tests stay the guard), and only a store-driven reorder under an unchanged query is identity-tracked. Arrow/Tab/hover record `{key, index, query}` of the target row.

Alternative ruled out: freezing order at open — operator chose live order + id selection.

### Files to change

1. `cmd/hivegui/frontend/src/lib/session-finder.ts` — `FinderSession extends StateCarrier`; `isFlagged()`; partitioned `recencyOrder()`; tie-break in `finderRows()` sort; update header comment.
2. `cmd/hivegui/frontend/src/components/modals/CommandPalette.tsx` — `{key,index,query}` selection (state, activeIndex derivation, keydown handler, onMouseEnter).
3. `README.md:379` — ⌘P row: "…empty search lists the one you were just in first, then sessions waiting on you".
4. `site/features.json` — new entry `since: "Unreleased"`, title "Waiting sessions first in Go to session", one-sentence blurb. (Existing 2.10.0 "Go to session" entry left as shipped history.)
5. `.changesets/562-finder-attention-first.md` — `type: changed`, `bump: minor`, `issue: 562`.

### New files

- `.changesets/562-finder-attention-first.md` (above).

### Tests

`cmd/hivegui/frontend/test/unit/session-finder.test.ts`, new `describe('finderRows — attention ranking (spec 562)')`:
- `keeps the previous session first even when others are flagged` (crit 1)
- `ranks each flagged state ahead of unflagged sessions` — `it.each` over `{needs_attention:true}` (attention), `{state:'waiting_permission'}`, `{state:'error'}` alive (failed), `{alive:false,last_error:'x'}` (error), all with `phase` ready (crit 2)
- `keeps recency order within the flagged and unflagged groups` (crit 3)
- `typed query: a better-scoring unflagged match beats a worse flagged one` and `typed query: equal score puts the flagged session first` (crit 4)
- `keeps the active session last even when flagged` (crit 5)
- `a flag that clears is ranked unflagged on the next call` (crit 6)
- `does not flag a starting or exited session` (sessionState semantics), including a dead session with `needs_attention: true`
- `lists a flagged previous session once, first` (dedupe)
- existing `filters 100 sessions well inside the budget` extended with flagged sessions mixed in (crit 7)
- existing recency suites must stay green unchanged (crit 1 / spec 544 crit 3)

`cmd/hivegui/frontend/test/dom/command-palette.test.tsx`:
- `session finder: highlight follows the selected session when a state change reorders rows` — open finder, ArrowDown to row X, update store so another session becomes flagged and shifts X, assert `[data-selected]` still has `data-sid=X` and Enter activates X.

- `typing after ArrowDown keeps today's index behaviour` — regression guard (passes on current code by design; not a red test).
- `a removed selected session falls back to the clamped index; Enter never activates a gone id`.

`cmd/hivegui/frontend/test/e2e/session-finder.spec.ts`:
- `a waiting session is listed right after the previous one` — boot with 3 sessions, visit to build history, `updateSession(id, {state:'waiting_input'})` (the spec file's local helper; mock seeds `alive: true`), open finder, assert row order **and** that the moved row's state icon reads "Waiting for you", so the test cannot pass on an unflagged row.

### Verification

- TDD red step, enforced: write the new unit + dom tests first and run `scripts/test.sh unit dom` against the **unmodified** source; record in Progress that each new test fails (a dom reorder test that passes on current code is vacuous and gets rewritten). Then implement and re-run green.
- `(cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/session-finder.spec.ts test/e2e/every-shortcut.spec.ts)`
- `(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)`; `scripts/ui-lint.sh`
- `scripts/test.sh` full before push.

### Risks

- Typed tie-break rarely fires on long queries (integer scores) — accepted in spec Notes.
- Live reorder while open is visible (rows move); mitigated by key-based selection. Visual treatment of flagged rows stays an open spec question, not done here.
- Selection tracking is shared with command mode; since typing keeps today's index rule and command rows never reorder under a fixed query, command mode is unchanged.
- Changeset `bump: minor` + new features.json entry: operator asked to update both docs; AGENTS ties features.json entries to minor. PR notes the status icon is the only flagged-row cue (visual-treatment question stays open).

## Second opinion

- Round 1: **revise** (confidence 7), 4 must-fix, all applied: selection is `{key,index,query}` so typing keeps today's index rule; the existing previous-session logic is kept, with a dedupe test; the e2e asserts the "Waiting for you" icon; the TDD red step is enforced.
- Round 2: **approve** (confidence 8), no must-fix. Nits applied: the dom regression-guard test was moved to the dom file, and a stale-id case was added.

## Decision log

- **2026-11-17** — Empty query keeps the previous session first, then flagged sessions, then the rest; current last. Why: preserves spec 544 criterion 3 (⌘P then Enter goes back).
- **2026-11-17** — Typed query: attention is a tie-break only. Why: never bury the session the user typed.
- **2026-11-17** — Live reorder: selection follows the session by id (operator choice over freezing order). Docs: README + new features.json entry.
- **2026-11-17** — Session finder flags are computed once per row inside `recencyOrder()` and carried through the sort. Why: keeps `sessionState()` out of the comparator (O(n) calls, not O(n log n)).
- **2026-11-17** — Flagged = `attention`, `waiting-permission`, `failed`, `error`; recency order within the group. Why: operator choice at brainstorm.

## Progress

- **2026-11-17** — Spec triaged (S/P2); research done.
- **2026-11-17** — Plan approved (chat).
- **2026-11-17** — TDD red confirmed on unmodified source: 9 new unit tests failed; the dom reorder test failed (on ordering first, then — after the finder change — on the selection, proving it is not vacuous). The e2e test first passed on old code (recency already put the target second: boot activates each added session, so it is in history); retargeted to the session third by recency, then confirmed red on old code and green on new.
- **2026-11-17** — Implemented; full `scripts/test.sh` green (go, 878 unit, 1350 dom, 579 e2e), typecheck, `biome ci`, ui-lint OK.

## PR convergence ledger

- **2026-11-17 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 1081e9c.
- **2026-11-17 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 9a04941.

## Gate verdict

- **2026-11-17** — verdict: PASS; phase: —; checks: 17 passed / 0 failed / 0 followups; followups: none; one-line: all 7 success criteria, 5 non-goals and doc accuracy verified on PR #564.
  - 2026-11-17 dimensions:
    - acceptance — PASS — 38 unit + 20 dom + 11 e2e finder tests green; one case per flagged state; 100-session budget <20 ms (spec <100 ms)
    - non-goals — PASS — ⌘B code, filters, flag clearing, severity ordering and command-mode ranking untouched
    - doc accuracy — PASS — README ⌘P row, changeset (changed/minor), features.json Unreleased entry, whats-new test green; no generated files edited

## Open questions

- Flagged-row visual treatment (spec Notes).
