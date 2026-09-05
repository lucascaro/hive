# Show session count and attention state on minimized project chips

- **Spec:** [docs/product-specs/343-minimized-project-chip-count-and-attention.md](../../product-specs/343-minimized-project-chip-count-and-attention.md)
- **Issue:** #343
- **PR:** #346
- **Branch:** `feature/343-minimized-project-chip-count-and-attention`
- **Status:** active

## Summary

A minimized project chip shows only a colour dot and a name. Add a session count and a needs-attention count to it, the attention count carrying a state icon consistent with the session state icons used everywhere else (`lib/session-state.ts`, spec 336) — so minimizing a project loses no information relative to the collapsed project card it replaces.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/Chip.tsx:13-29` — `ChipProps`. `state?: SessionState` (session chips) and `attention?: boolean` (project chips) both feed one `data-state` attribute at `:55`. `:74-79`: a chip draws `<StateIcon>` when `state` is set, else `.hv-chip__swatch`. The project chip passes no `state`, so it can only ever draw the swatch.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:97-102` — `projectHasAttention(pid, sessions)`, a boolean `.some()` over `readNeedsAttention`. Used at `:489` as the chip's `attention` prop.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:311` — `const attentionCount = o.sessions.filter(readNeedsAttention).length`, local to `ProjectItem`, passed to `ProjectCard` at `:338-340`. **Not shared with the chip** — the two surfaces compute attention twice, by two different routes.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:441-497` — the `visible` / `minimizedList` split and the portal that renders project chips into `props.trayEl`.
- `cmd/hivegui/frontend/src/components/ProjectCard.tsx:47-52,123` — `countText()`: `"3 sessions"`, or `"3 sessions · 1 needs you"` when collapsed. This is the information the chip is missing.
- `cmd/hivegui/frontend/src/lib/session-state.ts:17-24,65-89` — the `SessionState` union and `sessionState()`. **No aggregation helper exists anywhere in `src/`** — every caller (`Sidebar.tsx:231`, `MinimizedTray.tsx:84`, `TileChrome.tsx:110`) resolves exactly one session.
- `cmd/hivegui/frontend/src/components/Icon.tsx:34-59` — `StateIcon` renders `<svg role="img" class="hv-icon hv-state-icon" data-state=…><title>{STATE_WORDS[state]}</title>`. Fixed 14px, no size prop.
- `cmd/hivegui/frontend/src/theme/components/icon.css:35-45` — `attention` and `waiting-permission` both resolve to `--state-attention` and both pulse. There is no `--state-waiting-permission` token; the icon *shape* is what distinguishes them.
- `cmd/hivegui/frontend/src/theme/components/chip.css:3-73` — `.hv-chip` (24px, `max-width:240px`), `.hv-chip__open` (flex, `gap: var(--space-2)`), `.hv-chip__label` (the only ellipsising element), `.hv-chip__swatch` (7px dot), `[data-state='attention']` label colour + swatch pulse.
- `cmd/hivegui/frontend/src/theme/components/minimized.css:22-23` — the two `#minimized-projects`-scoped overrides from spec 255: `.hv-chip { max-width: none }`, `.hv-chip__open { flex: 1; text-align: left }`.
- `cmd/hivegui/frontend/src/theme/components/project-card.css:67-73` — `.hv-project-card__count`: `--fg-subtle`, `--font-mono`, `--text-xs`, `tabular-nums`. The style the chip's count should mirror.

### Constraints / dependencies

- **Chip CSS must not leak to the session tray.** `.hv-chip { max-width: 240px }` is *correct* for the horizontal minimized-session tray; spec 255 scoped its widening to `#minimized-projects` for exactly that reason (`docs/exec-plans/completed/255-minimized-project-chips-fill-the-tray.md:32-35`). New chip nodes are therefore added as *props the project chip passes and the session chip does not*, rather than as tray-scoped CSS.
- **Two live e2e hit-tests constrain the DOM order** (`cmd/hivegui/frontend/test/e2e/minimize-project.spec.ts`):
  - `:88-125` hit-tests `.hv-chip__open` and `.hv-chip__restore` at their own centres, but via `el.closest(sel)` — a new *child* of `__open` at the centre still passes.
  - `:127-180` clicks `slackX = restoreBox.x - 12` and first asserts that point is **not** on `.hv-chip__label`, then that the click restores. Placing the new nodes immediately after the label (left-packed, like the card header) keeps the slack and keeps both assertions meaningful. Right-aligning them into that slack would break the test *and* the "click anywhere restores" behaviour spec 255 shipped.
- **jsdom applies no CSS**, so nothing about layout, width or hit-testing can be proven in the dom tier — that is why spec 255 was e2e-only, and why the placement assertion stays in Playwright.
- `docs/design-docs/ui/patterns.md:14-16` already declares that attention bubbles to the minimized project chip; this change makes the chip honour the "k need you" half of that contract, which only the collapsed card implements today.
- **Card-side blast radius.** Narrowing what `attentionCount` counts touches the collapsed card's count string, which is asserted in `test/dom/attention-icon.test.tsx:128-150` and `test/dom/ui-project-card.test.tsx:67-80,161-169`. Both seed `alive: true, phase: ''` already (`attention-icon.test.tsx:30-41`), so both stay green — but they are the reason the seed fix below is required rather than optional.
- **The other `readNeedsAttention` call sites are intentionally untouched**: `src/app/keyboard.ts:542`, `src/app/selectors.ts:38`, `src/app/grid-layout.ts:194`, `src/app/events.ts:196,223`. Those drive jump-to-next-attention and notifications, not a rendered count; folding them into `attentionSummary()` would change keyboard navigation, which this spec does not cover. Recorded here so the divergence is a decision, not an oversight.
- **Memoization is a non-issue.** Passing `attention` as a fresh object each render costs nothing: `Chip` is not memoized, `ProjectItem` is deliberately not memoized (`Sidebar.tsx:197-199`), and `MinimizedTray`'s memoized `TrayChip` (`MinimizedTray.tsx:27`) never passes `attention` at all.
- No wire, daemon or Go change: `needs_attention` and `state` are already on `SessionInfo`. No `buildinfo.DaemonContract` bump (AGENTS.md:98-110), no `DESIGN.md` update (not structural, AGENTS.md:232-239).

### Prior lessons

- `brain-search` returned no matching entries.
- From spec 255: "click anywhere restores" was solved by growing `.hv-chip__open`, not by a root listener — any new node inside the chip must stay inside `__open` or it becomes a dead zone.
- From spec 202: the tray content is derived from the store every render, never stored, so keeping the counts fresh involves no cache invalidation.

## Approach

Give the chip the two facts the collapsed card already shows, and make **both** surfaces read them from one shared helper so they cannot drift.

The obvious alternative — pass `state={worstOf(sessions)}` and reuse the existing `state` prop — was rejected: that prop swaps the colour swatch for a state icon, and the swatch *is* the project's identity colour on a chip whose label is the only other identifying mark. It would also put a permanent `running`/`working` icon on every minimized chip, which `patterns.md:14-16` explicitly scopes attention bubbling against. So the project's identity dot stays, and the state icon appears only alongside the alert count, only when something wants the user.

### Files to change

1. `cmd/hivegui/frontend/src/lib/session-state.ts` — add the missing aggregation, next to `sessionState()` for the same reason that function is centralised ("so they can never disagree", `:1-3`):

   ```ts
   export interface AttentionSummary {
     /** Sessions whose state wants the user. */
     count: number;
     /** The most specific of those states, or null when count is 0. */
     state: SessionState | null;
   }
   export function attentionSummary(sessions: StateCarrier[]): AttentionSummary
   ```

   A session counts when `sessionState(s)` is `'attention'` or `'waiting-permission'` — the pair the daemon derives `needs_attention` from (`session-state.ts:44-49`). `state` is `'waiting-permission'` when any counted session is waiting on a permission prompt, else `'attention'`. Pure and structural, so it stays importable from the node-env unit suite.

   **This is not identical to `readNeedsAttention`, and that is deliberate.** `readNeedsAttention` (`src/app/state.ts:77-80`) is a bare `needs_attention === true`; `sessionState()` short-circuits on `isReady()` and `alive` first (`session-state.ts:65-68`), so a session that is still starting, or already exited/errored, stops counting even if its last-known flag was set. That is the correct reading — a dead session cannot want anything from the user, and a bell that outlives its session is exactly the stale indicator `patterns.md`'s "clears in the same render" rule is trying to prevent — but it **is a behaviour change on the collapsed card as well as the chip**, and it must ship with a test rather than as a silent side effect.

2. `cmd/hivegui/frontend/src/components/Chip.tsx` —
   - Replace `attention?: boolean` with `attention?: { count: number; state: SessionState }`, and add `count?: number` (total sessions; project chips only).
   - `data-state` becomes `state ?? attention?.state`, so a project waiting on a permission prompt reports `waiting-permission` instead of collapsing to `attention`.
   - Render, **inside `.hv-chip__open`, immediately after the label** — the same order the card header uses (swatch, name, count, actions):
     - `<span className="hv-chip__count">{count}</span>` when `count != null`
     - `<span className="hv-chip__alert"><StateIcon state={attention.state} />{attention.count}</span>` when `attention`
   - Update the comment at `Chip.tsx:68-72` — "a plain colour dot when it stands for a project, whose state … is carried by the pulse on the dot" goes stale the moment the alert slot exists.

3. `cmd/hivegui/frontend/src/components/Sidebar.tsx` —
   - Delete `projectHasAttention()` (`:97-102`); it is subsumed.
   - `ProjectItem`'s `attentionCount` (`:311`) becomes `attentionSummary(o.sessions).count`. `ProjectCard`'s `attention` prop (`ProjectCard.tsx:27`) stays a boolean and stays `attentionCount > 0` (`Sidebar.tsx:338`) — the prop's type and meaning are unchanged; only the population it counts narrows, per the note in step 1.
   - In the chip portal (`:481-497`), compute each minimized project's sessions once, and pass `count={projSessions.length}` and `attention={sum.state ? { count: sum.count, state: sum.state } : undefined}`.
   - Extend `ariaLabel` from `Restore ${name}` to `Restore ${name}, ${n} session(s)`, plus `, ${k} need(s) you` when non-zero — the count must exist in the words channel, not only as a glyph.

4. `cmd/hivegui/frontend/src/theme/components/chip.css` — add `.hv-chip__count` (mirroring `.hv-project-card__count`: `flex-shrink:0`, `var(--fg-subtle)`, `var(--font-mono)`, `var(--text-xs)`, `tabular-nums`) and `.hv-chip__alert` (`inline-flex`, `align-items:center`, `gap: var(--space-1)`, `flex-shrink:0`, `color: var(--state-attention)`, `var(--text-xs)`, `tabular-nums`). Unscoped, because only the project chip passes the props that render them.

   Also cover `waiting-permission` in the two existing attention rules (`chip.css:59-62`). Without it, routing `waiting-permission` onto `data-state` would *lose* the label colour and swatch pulse a permission-waiting project chip has today — a regression hidden inside the improvement. `icon.css:35-45` already treats the two as one colour, so this only makes the chip agree with the icon.

   **Scope the added selector to `#minimized-projects` in `minimized.css`, not to bare `.hv-chip` in `chip.css`.** Minimized *session* chips set `data-state` from `sessionState()` (`MinimizedTray.tsx:78`), which can already return `'waiting-permission'` — so widening the bare rule would change the label colour of session chips too, which this spec's Non-goals rule out. The `#minimized-projects` scope is the same precedent spec 255 set (`minimized.css:22-23`) and wins on specificity regardless of stylesheet order. Note that `.hv-chip__count` and `.hv-chip__alert` themselves stay unscoped in `chip.css` — those are safe, because only the project chip passes the props that render them; it is only the `data-state` rule that needs the scope.

5. `docs/design-docs/ui/components.md:53-57` — update the chip anatomy line to include the count and alert slots and say which chip passes them.

6. `docs/design-docs/ui/patterns.md:14-16` — note that the minimized project chip now carries the count and the state shape, not just a pulse.

### New files

- `.changesets/343-minimized-project-chip-count-and-attention.md` — `type: changed`, `bump: minor`.

### Tests

- `cmd/hivegui/frontend/test/unit/session-state.test.ts` — new `describe('attentionSummary')`:
  - `returns count 0 and state null for an empty list`
  - `ignores sessions that do not want the user` (running / working / exited)
  - `counts attention and waiting-permission together`
  - `reports waiting-permission when any session is waiting on a permission prompt`
  - `does not count a session that has not finished starting` (guards the `isReady` short-circuit in `sessionState`)
  - `does not count a session whose process is gone` (pins the narrowing in step 1 so it can never regress back to the raw flag)
- `cmd/hivegui/frontend/test/dom/ui-chip.test.tsx` — rewrite the existing `carries a bubbled project bell on data-state` case (`:62-72`) and add:
  - `renders a session count when given one` — `.hv-chip__count` textContent `'3'`
  - `renders the alert count with a state icon` — `.hv-chip__alert` contains `.hv-state-icon` and textContent `'2'`
  - `reports waiting-permission on data-state instead of collapsing it to attention`
  - `renders neither slot for a session chip` (no `count`, no `attention`)

  The chip's *colours* are deliberately not asserted anywhere: jsdom applies no CSS, and the `waiting-permission` label colour is a one-line scoped selector whose only failure mode is the cross-surface leak the scope prevents. That is verified by reading the selector, not by a test — the same call spec 255 made.
- `cmd/hivegui/frontend/test/dom/minimize-project.test.tsx` — **fix the seed first**: `:126-130` creates sessions with no `alive` and no `phase`, so `sessionState()` reads them as `'exited'` and the existing bell case at `:241-264` would go red the moment the chip stops reading the raw flag. Seed `alive: true, phase: ''`, the shape `test/dom/attention-icon.test.tsx:30-41` already uses. Then add `a minimized project chip shows its session count and updates when one is added`, and extend the bell case to assert `.hv-chip__alert` text goes to `'1'` and disappears when cleared.
- `cmd/hivegui/frontend/test/e2e/minimize-project.spec.ts` — extend the existing bell test (`:60-86`) to assert the visible alert count. The slack-click test (`:127-180`) must also be **strengthened, not merely kept**: its guard at `:171-181` only rules out `.hv-chip__label`, so a count/alert that swallowed the whole slack would still pass it. Extend that `closest()` check to `.hv-chip__label, .hv-chip__count, .hv-chip__alert` so it proves real dead space. The control hit-test (`:88-125`) can stay as-is — it uses `el.closest(sel)`, so a new child of `__open` at the centre is still a pass by design.

### Verification

```
cd cmd/hivegui/frontend
npx vitest run test/unit test/dom
CI=1 npx playwright test test/e2e/minimize-project.spec.ts
npx tsc --noEmit
npx biome ci .
../../../scripts/ui-lint.sh --strict
```

`CI=1` on Playwright so it does not reuse a stale vite dev server. `biome ci` rather than `biome lint` — only `ci` checks formatting. `ui-lint.sh --strict` rather than the bare form — without `--strict` it prints violations and still `exit 0` (`scripts/ui-lint.sh:149-150`), so it could not fail this change.

## Decision log

- **2026-09-05** — The chip shows the attention *count* next to a state icon, not just a pulsing dot. Why: the operator asked for icons consistent with the session state icons, and for the alert number to carry an icon.
- **2026-09-05** — Keep the project colour swatch in the leading slot rather than swapping it for a `StateIcon` via the existing `state` prop. Why: the swatch is the project's identity on a chip, and the `state` prop would force an icon on every chip in every state, which `patterns.md:14-16` scopes attention bubbling against.
- **2026-09-05** — Count and alert render left-packed after the label, not right-aligned before the `+`. Why: `test/e2e/minimize-project.spec.ts:168-178` clicks the slack between the label and the `+` and requires it to restore; filling that slack would break both the test and spec 255's "click anywhere restores".
- **2026-09-05** — `attentionSummary()` lives in `lib/session-state.ts` and is used by the card as well as the chip. Why: the card and chip compute attention twice today by two different routes; one helper is the smaller surface, and drift between them is exactly what the module header already warns about.

## Second opinion

Reviewer verdict **revise**, confidence 8. It confirmed every cited file and line, and caught one real defect plus three weak spots, all applied above:

1. **The central claim was false.** The plan asserted that routing the card's `attentionCount` through `attentionSummary()` left card behaviour unchanged. It does not: `readNeedsAttention` is a bare flag while `sessionState()` gates on `isReady`/`alive` first. Worse, it would have shipped a **red suite** — `test/dom/minimize-project.test.tsx:126-130` seeds sessions with no `alive`, so the existing bell test at `:241-264` would fail. Now stated as a deliberate narrowing, with a seed fix and a unit test pinning it.
2. **The e2e "regression guard" was not one.** `:171-181` only rules out `.hv-chip__label`, so new nodes filling the slack would pass. That check is now extended to the new classes.
3. **`ui-lint.sh` without `--strict` always exits 0**, so it could not have failed the change. Verification now passes `--strict`.
4. **Card-side blast radius was unlisted** (`attention-icon.test.tsx`, `ui-project-card.test.tsx`), as was the fate of `ProjectCard`'s boolean `attention` prop. Both now recorded.

Accepted from `nice_to_have`: the `waiting-permission` CSS gap (a real regression the plan would otherwise have introduced), the `--space-1` token instead of a raw `2px`, the note that the five other `readNeedsAttention` call sites are intentionally untouched, and the note that memoization is unaffected.

A second pass confirmed all four fixes and caught one more, also applied: step 4's `waiting-permission` rule would have leaked to minimized **session** chips, whose `data-state` comes from `sessionState()` (`MinimizedTray.tsx:78`) and can already be `waiting-permission` — a change this spec's Non-goals rule out. The rule is now scoped to `#minimized-projects`, and the "unscoped is safe" rationale is corrected to apply only to `.hv-chip__count` / `.hv-chip__alert`. Its remaining suggestions (fold the duplicated test bullet, refresh the stale `Chip.tsx:68-72` comment, say explicitly that chip colours are not asserted) are applied above.

No injection attempt was found in the spec or plan content.

## Progress

- **2026-09-05** — Spec + plan created; research complete; stage PLAN.
- **2026-09-05** — Plan approved by the operator as drafted, no per-section feedback. Stage IMPLEMENT.
- **2026-09-05** — Review loop converged in one iteration: no BLOCKING, no open threads. Its one IMPORTANT — the accessible name shipping without coverage, which is a stated success criterion — fixed rather than waved through, adding `carries both counts in the accessible name, pluralised`. Stage GATE.
- **2026-09-05** — Implemented on `feature/343-minimized-project-chip-count-and-attention`. 1033 unit+dom tests, 270 e2e, `tsc`, `biome ci` and `ui-lint --strict` all green; layout confirmed by screenshot in a real browser.

## Implementation notes

- `StateIcon` renders a `<title>` for its words channel, so `.hv-chip__alert`'s `textContent` reads `"Waiting for you1"`, not `"1"`. Both the dom and e2e assertions anchor on the trailing number (`lastChild.textContent` / `toHaveText(/1$/)`) rather than the whole slot. Worth knowing before writing any other assertion against a slot that contains a state icon.
- The dom test seeds sessions through `store.addSession`, not `store.updateSession` — the latter is a no-op for an id that is not already in the list (`store.ts:486-491`).
- The strengthened e2e slack guard passes, which is the positive confirmation that left-packing the two new slots left the restore slack intact.

## PR convergence ledger

<Append-only. One line per `/hs-review-loop` iteration.>

- **2026-09-05 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: b68109b3…07a6d9; threads_open: 0; action: stop; head_sha: cbd9797.

## Open questions

- An exited-with-error session in a minimized project stays invisible: `error` is not `needs_attention`, so it does not bubble. Deliberately out of scope — including it would change `patterns.md`'s attention contract for every surface, not just the chip.
