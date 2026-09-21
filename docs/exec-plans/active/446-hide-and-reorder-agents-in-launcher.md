# Let users hide agents from the launcher and reorder them

- **Spec:** [docs/product-specs/446-hide-and-reorder-agents-in-launcher.md](../../product-specs/446-hide-and-reorder-agents-in-launcher.md)
- **Issue:** #446
- **Status:** active
- **PR:** #448
- **Branch:** feature/446-hide-and-reorder-agents-in-launcher

## Summary

Add a per-agent enable checkbox and a pin + drag-to-reorder control to Settings → Agents. The launcher shows pinned agents first in the user's order, then the remaining enabled agents by usage, as today. Preferences live in GUI localStorage.

## Research

Paths relative to `cmd/hivegui/frontend/` unless absolute.

**Relevant code**
- `src/components/modals/Launcher.tsx:229-245` — `ListAgents()` then usage sort (`loadAgentUsage`, ties keep catalog order). Only place agents are *picked*; duplicate/restart never open the launcher (`src/app/modals/launcher.ts:1-20`), so filtering here cannot break existing sessions.
- `src/app/modals/launcher.ts:79-96` — `hive.agentUsage` load/bump with the try/catch localStorage pattern. No tests cover it.
- `src/lib/drag-placeholder.ts` — the one DnD core (native HTML5): `beginDrag`/`moveTo`/`endDrag`, a spacer `<li>` that is itself a drop target. Sidebar-coupled in two places: `ROW = '.hv-session-row, .hv-project-card'` (used by `slot()`) and `selectorFor()` keyed on `data-sid`/`data-pid` (rebuild recovery, vestigial under React).
- `src/components/Sidebar.tsx:250-331` (session rows) and `:485-588` (project cards) — near-identical four-handler wiring (dragstart sets a MIME key + `beginDrag(el, commit)`; dragover checks the key, `moveTo(el, clientY < mid)`; drop `endDrag()` + `commit`). Project cards add guards (ignore drags bubbling from session rows / starting in inputs) and measure "above" against the header. `reorderDroppedProject` (:163) holds the flat-list above/below index math.
- Placeholder CSS: `src/theme/components/sidebar.css:68-81`; `.dragging{display:none}` per row class in `session-row.css:375`, `project-card.css:167`.
- `src/components/modals/Settings.tsx` — Agents panel `:663-772`; `draft` custom agents loaded via `ListCustomAgents` (:273-296); `saveSettings()` (:559-600) chains `SaveCustomAgents` → `SaveAgentSettings` → `SaveUpdateSettings`; Cancel discards (asserted by `test/dom/settings.test.tsx:513`, `test/e2e/settings.spec.ts:221`). Settings does not call `ListAgents` today. `AgentInfo` (`cmd/hivegui/app_calls.go:26`): id, name, color, available, installCmd?, takesPrompt.
- Icon sprite (`src/lib/icon-sprite.ts`) has no pin/grip glyph.

**Constraints**
- e2e specs assume a fresh context puts "Shell" first in the launcher (`test/e2e/launcher-search.spec.ts`, mock catalog `test/e2e/wails-mock.ts:660-690`) — defaults must hide/pin nothing.
- `test/e2e/settings.spec.ts:411` "Tab never reaches a control in a hidden panel" must keep passing with the new controls.
- Custom agents added in the dialog reach `ListAgents` only after Save.
- jsdom has no DragEvent/DataTransfer; dom tests fire plain `Event('drop')` with a fake `dataTransfer` (`test/dom/sidebar-reorder.test.tsx:77`); e2e dispatches real DragEvents sharing one `DataTransfer` (`test/e2e/ordering.spec.ts:300-340`) because headless Chromium does not synthesise native drags.

**Prior lessons:** no prior lessons matched.

**Conventions card**
- Test: `scripts/test.sh` (layers go · unit · dom · e2e); frontend: `npm test` (vitest unit+dom), `CI=1 npx playwright test` (mock e2e); `npm run typecheck` (fresh worktree needs `./scripts/ci-bootstrap.sh`); `npx biome ci .`; `scripts/ui-lint.sh`.
- TDD: every behaviour change ships its test; boil the lake on review nits.
- UI tokens/components per `docs/design-docs/ui/`; update `docs/design-docs/ui/components.md` / `patterns.md` when the drag helper is generalised.
- User-visible: `.changesets/<slug>.md` (type `added`, bump `minor`) + `site/features.json` entry with `since: "Unreleased"`; README launcher bullet (`README.md:16`).
- GUI-only change: no wire change, no DaemonContract bump.

## Approach

Two independent pieces joined by one stored value: **`hive.agentPrefs`** in localStorage, `{ hidden: string[], pinned: string[] }`. `pinned` is ordered, so the array *is* the manual order. Nothing is stored for agents that are shown and unpinned, which means a fresh profile (the e2e baseline) behaves exactly like today.

**Launcher.** A pure function `orderAgents(list, usage, prefs)` replaces the inline sort at `Launcher.tsx:233-245`:
1. It drops hidden ids.
2. It emits pinned ids in `pinned` order. Unknown ids (a deleted custom agent) are skipped.
3. It emits the rest by usage, highest first, with ties keeping catalog order. This is today's rule, unchanged.

Search runs over the result, so hidden agents never match. The launcher shows no pin glyph, because the order itself shows the pins; the icon sprite has none to use anyway.

**Settings → Agents.** A new "Launcher" section lists every agent from `ListAgents()`, which Settings now calls on mount next to `ListCustomAgents`:
- Pinned agents come first, in pinned order. The rest follow in catalog order.
- Each row contains a "Show in launcher" checkbox, the agent's swatch and name, and a "Pin" checkbox.
- The list is two `<ul>`s: a pinned list and an unpinned list. Only rows in the pinned list carry `data-drag-row` and drag handlers. That way `slot()` can never resolve a drop into an unpinned row, and a pinned row can never land among the unpinned ones. Dropping reorders `pinned`.
- Alt+↑/↓ inside a pinned row moves it one slot, as the keyboard path for the same reorder. The section hint shows the binding, per the key-discoverability rule. This is safe: the settings capture branch only handles Esc, ⌘ and Tab (`keyboard.ts:227-240`).

Edits go into draft state and are written to `hive.agentPrefs` only when `saveSettings()` succeeds, after the existing Save chain. Cancel discards them, matching the custom-agents contract.

If `ListAgents` failed or has not answered yet, the section shows an error or loading row with disabled controls, and **Save skips the prefs write entirely**. This is the same guard `saveSettings` already applies to agents.json (`Settings.tsx:560-562`). Without it, a draft built from nothing would wipe the stored `hidden` and `pinned` lists.

**Hiding everything.** If every agent is hidden, the launcher shows "All agents are hidden — enable some in Settings → Agents" instead of "No agents found" (`Launcher.tsx:623`). Settings shows an inline hint that the launcher will be empty. Save is not blocked.

**DnD reuse (operator requirement: one implementation).** This generalises `src/lib/drag-placeholder.ts` and extracts the repeated handler wiring:
- `ROW` becomes `'[data-drag-row]'`. `SessionRow`, `ProjectCard` and the new agent row carry `data-drag-row`.
- The placeholder CSS and a shared `[data-drag-row].dragging { display: none }` rule move from `sidebar.css` and the per-row files to a new `src/theme/components/drag.css`.
- New `src/lib/drag-row.ts` exports `dragRowProps({ mime, id, onCommit, cancelFrom?, aboveOf? })`. It returns `{ draggable, onDragStart, onDragOver, onDrop, onDragEnd }`, wired exactly as the sidebar does today. The project card's two dragstart guards stay distinct, because they behave differently:
  - **Ignore (no `preventDefault`).** If the target is an `Element` (a text-selection drag can target a Text node, which has no `closest`; today's `instanceof Element` guard at `Sidebar.tsx:543` stays) and `e.target.closest('[data-drag-row]') !== e.currentTarget`, the event bubbled up from a nested drag row, so `onDragStart` and `onDragEnd` return without doing anything. This generalises today's `.hv-session-row` check (`Sidebar.tsx:544`, `:566`) and never cancels an inner session drag.
  - **Cancel (`preventDefault`).** If `e.target.closest(cancelFrom)` matches, `onDragStart` calls `preventDefault`. Project cards pass `'.hv-project-card__actions, .project-name-input, .group-name-input'` (`:545-553`).
- `onDrop` always checks the MIME type, as project cards already do. Session drops gain the check. That is harmless, because a project drop on a session row still bubbles to its card; it is noted in a code comment.
- Session rows and project cards both switch to it. Project cards pass `aboveOf` so "above" is measured against the header.
- The flat-list index math in `reorderDroppedProject` is extracted as a pure `moveIndex(len, from, to, above)`. The project drop and the pinned-agent drop both use it.

**Is the drag-and-drop React-based? (your question)** Partly. It is a hybrid.
- **React side:** the four handlers (`onDragStart`, `onDragOver`, `onDrop`, `onDragEnd`) are React props on `SessionRow` and `ProjectCard`, and the commit goes through store actions. `dragRowProps` keeps it that way and gives callers a React-only API.
- **Imperative side:** the core, `drag-placeholder.ts`, inserts a spacer `<li>` into the React-owned `<ul>` with `insertBefore`, and toggles `.dragging` with `classList`. This is deliberate:
  - The spacer changes no React state, so a drag re-renders nothing.
  - Keyed reconciliation leaves the foreign node alone.
  - The browser snapshots the drag image before the swap. The file documents this timing at length.
- **Should it be React?** A React core would be a `useDragReorder()` hook that holds `{draggedId, slot}` and has each list render its own placeholder element.
  - The session list is the hard part. It renders through worktree groups and clusters, so every list renderer would need to learn about the placeholder.
  - Every dragover would re-render the sidebar, and a mid-drag re-render is the scenario the current design avoids.
  - The gain is architectural purity. There is no bug it fixes.
  - **Recommendation:** keep the imperative core, wrapped in the React-prop factory. This feature needs no change there.
  - If you want the core converted anyway, it is a separate refactor with its own regression surface. I would file it as its own issue rather than fold it into #446. Say so and I will.

**Why not a local drag for the settings list.** The operator ruled that out. It would also duplicate the spacer and drop-target subtleties that `drag-placeholder.ts` documents at length.

**Why localStorage.** It is the operator's choice. It is also where `hive.agentUsage`, theme and density already live, so no wire change or DaemonContract bump is needed.

### Files to change

Paths are relative to `cmd/hivegui/frontend/`.

1. `src/lib/drag-placeholder.ts`: `ROW` becomes `[data-drag-row]`. Update the header comment. `selectorFor` falls back to `''` for rows with no sid or pid, and is documented as sidebar-only rebuild recovery. The spacer stays an `<li>`, which is why the agent lists are `<ul>`s.
2. `src/components/Sidebar.tsx`: session rows and project cards use `dragRowProps`. `reorderDroppedProject` uses `moveIndex`.
3. `src/components/SessionRow.tsx` and `src/components/ProjectCard.tsx`: add `data-drag-row`. Accept the spread props, or keep today's prop names fed from the helper, whichever is the smaller diff.
4. `src/theme/components/sidebar.css`, `session-row.css`, `project-card.css`: move the placeholder and `.dragging` rules into `drag.css`. `src/theme/components/index.css` imports `drag.css` late, before `xterm-overrides.css`. A later two-class rule setting `display` would otherwise beat `[data-drag-row].dragging` (0,2,0).
5. `src/components/modals/Launcher.tsx`: call `orderAgents(list, loadAgentUsage(), loadAgentPrefs())`. Add the all-hidden empty message.
6. `src/app/modals/launcher.ts`: add `loadAgentPrefs` / `saveAgentPrefs` next to the usage helpers, using the same try/catch pattern. They validate shape and fall back to empty prefs on bad JSON.
7. `src/components/modals/Settings.tsx`:
   - Call `ListAgents()` on mount and hold the prefs draft.
   - Add the Launcher section markup with drag and Alt+Arrow.
   - Write prefs on successful Save.
   - Change the panel hint text at `:666-669`.
8. `src/theme/components/settings.css`: styles for the agent-visibility rows, tokens only. `scripts/ui-lint.sh` enforces this.
9. `README.md:16`: the launcher bullet mentions hiding and pinning agents in Settings.
10. `docs/design-docs/ui/components.md` and `patterns.md`: the drag-reorder notes name the shared `drag-row` helper and `data-drag-row`, and add the agents list as a third drag surface.
11. `site/features.json`: add a new shipped entry with `since: "Unreleased"`. It is a new entry, not an extension of "Every agent" (line 58), so it shows up in the What's New modal's unreleased bucket.
12. `test/e2e/ordering.spec.ts`: switch to the shared drag helper (see New files).
13. `test/e2e/wails-mock.ts`: `addSession` takes an optional `agent`.

### New files

- `src/lib/agent-order.ts`: the `AgentPrefs` type and the pure `orderAgents()` and `moveIndex()` functions.
- `src/lib/drag-row.ts`: `dragRowProps()`, the shared React DnD handler factory.
- `src/theme/components/drag.css`: the shared placeholder and `.dragging` rules.
- `test/e2e/fixtures/drag.ts`: `dragStart(page, selector)` and `dragEvent(page, type, selector, upperHalf)`, moved out of `ordering.spec.ts:300-345` and made selector-based. `ordering.spec.ts` and `settings.spec.ts` both use it.
- `.changesets/hide-and-pin-launcher-agents.md`: `type: added`, `bump: minor`, `issue: 446`.

### Tests

Paths are relative to `cmd/hivegui/frontend/test/`.

- **`unit/agent-order.test.ts`**
  - `orderAgents`:
    - With empty prefs, it equals the current usage sort, ties keeping catalog order. This is the regression guard for today's behaviour.
    - Hidden ids are removed.
    - Pinned ids come first in pinned order, whatever their usage.
    - The unpinned rest is sorted by usage.
    - An id that is both hidden and pinned is hidden.
    - Unknown ids in `hidden` and `pinned` are ignored.
  - `moveIndex`: a table test of above/below and source-before-target compensation. This covers the math lifted from `reorderDroppedProject`.
- **`dom/drag-placeholder.test.ts`**: update fixtures to `data-drag-row`, and add a case where a row that is neither a session nor a project resolves its slot.
- **`dom/sidebar-reorder.test.tsx`**: the existing cases stay unchanged and must pass. They are the regression proof for the refactor. New cases cover the dragstart guards, which no existing suite checks (the e2e drags are synthetic, so a wrong `preventDefault` cancels nothing):
  - "a session dragstart bubbling through its project card is not cancelled": fire a bubbling dragstart on a session row inside a card, and assert `defaultPrevented === false` and that the project MIME was not set.
  - "a dragstart inside project card actions or group-name-input is cancelled": assert `defaultPrevented === true`.
- **`dom/launcher.test.tsx`**, with new cases:
  - "hidden agents are not listed": set `hive.agentPrefs`, open the launcher, and assert the hidden name is absent.
  - "pinned agents lead in pinned order and the rest follow usage".
  - "search does not surface a hidden agent".
- **`dom/settings.test.tsx`**, with new cases:
  - "lists every agent with show and pin checkboxes".
  - "unchecking show and saving writes hive.agentPrefs.hidden".
  - "cancel discards visibility edits".
  - "pinning moves the row into the pinned group".
  - "dropping a pinned row reorders pinned", using the `dropOn`-style fake `dataTransfer`.
  - "Alt+ArrowUp moves a pinned row".
  - "dropping below the last pinned row keeps it pinned and last": the drop resolves within the pinned `<ul>`.
  - "a failed ListAgents leaves Save working and hive.agentPrefs byte-for-byte unchanged": seed prefs, reject `ListAgents`, Save, and compare the stored string.
- **`e2e/settings.spec.ts`**:
  - "a hidden agent disappears from the launcher": uncheck Codex, Save, press ⌘T, and assert Codex is absent and Shell and Claude are present. Then `page.reload()`, reopen Settings, and assert Codex is still unchecked (persistence across a restart).
  - "pinned agents lead the launcher in dragged order": pin Claude and Codex, drag Codex above Claude with `test/e2e/fixtures/drag.ts`, Save, and assert the launcher order is Codex, Claude, Shell.
  - The existing "Tab never reaches a control in a hidden panel" test must pass.
- **`e2e/ordering.spec.ts`**: the existing session drag tests must pass unchanged, now on the shared fixture. They are the real-browser proof of the drag refactor.
- **`e2e/settings.spec.ts`**, in the drag test: during the drag, assert the dragged agent row's computed `display` is `none`. This catches a CSS cascade override that jsdom cannot see.
- **`dom/launcher.test.tsx`**: "all agents hidden shows the Settings hint".
- Hidden agents' sessions are unaffected by construction, because the filter lives only in `orderAgents` and only the launcher calls it. An e2e test proves it: **"a hidden agent's session keeps its badge and duplicates"**.
  - `window.__hive.addSession` (`test/e2e/wails-mock.ts:1435`) gains an optional `agent` parameter.
  - The test seeds a Codex session, hides Codex, and Saves.
  - It asserts that the row still shows the Codex colour and badge.
  - It duplicates the session and asserts the copy's agent is `codex`.

### Verification

Run from `cmd/hivegui/frontend`:
- `npm test`: unit and dom, including the new `agent-order`, launcher and settings cases.
- `CI=1 npx playwright test`: mock e2e, including settings, ordering and launcher-search.
- `npm run typecheck && npx biome ci .`
- From the repo root: `scripts/ui-lint.sh`, `scripts/test.sh`.
- `cd cmd/hivegui/frontend && npx vitest run test/unit/whats-new.test.ts` checks the features.json entry.
- Manual check without a human: `wails dev` and a throwaway Playwright script at `localhost:34115`. The script hides an agent, pins two, drags them, then opens ⌘T and reads `.launcher-item` order.

### Risks

- **Drag refactor.** This is the regression risk: sidebar drag is load-bearing. It is mitigated by keeping the existing dom and e2e drag suites unchanged, and running them as the proof.
- **Unpinned rows are not draggable.** Dragging an unpinned row onto the pinned group does not pin it. You pin with the checkbox first.
- **Custom agents added in the same dialog** only appear in the list after Save. A hint says so; see Research.
- **Hiding every agent** leaves an empty launcher. This is allowed, and Settings warns about it.

## Second opinion

- **Round 1: revise (confidence 7).** 5 must_fix items, all applied:
  - The three-way dragstart guard: ignore a bubbled drag vs. cancel one from an input.
  - New dom tests that check `defaultPrevented`.
  - A separate pinned `<ul>`, so a drop cannot land among the unpinned rows.
  - Save skips the prefs write when `ListAgents` failed, and a test checks the stored value is byte-for-byte unchanged.
  - A shared e2e drag fixture.

  Nice-to-haves applied: `drag.css` import order, the all-hidden launcher message, the Alt+Arrow hint, and a MIME check on drop. The one declined: extending the existing "Every agent" feature entry. A new entry is kept so the feature appears in What's New.
- **Round 2: revise (confidence 8).** 1 must_fix, applied: the badge test was vacuous, because the mock's `addSession` has no agent parameter. It now seeds a real Codex session, hides Codex, and checks the badge and a duplicate. Nice-to-haves applied: the `instanceof Element` guard, the duplicate check, and a stale fixture reference. Not re-run again: the loop allows one revise round.

## Decision log

- **2026-09-20** — Order model: pinned agents first in manual order, unpinned agents below sorted by usage (existing rule). Why: operator choice at clarifying round A; keeps auto ordering for everything not deliberately placed.
- **2026-09-20** — Persist enabled/pinned/order in GUI localStorage, not daemon settings. Why: operator choice; matches `hive.agentUsage`, theme and density; GUI-only concern, so no wire change or DaemonContract bump.
- **2026-09-20** — Uninstalled agents default to enabled (install hint stays visible). Why: operator choice; keeps the discovery path.
- **2026-09-20** — Reorder by drag handles, reusing the existing session drag-and-drop implementation (refactor into a shared piece if needed); no second DnD implementation. Why: operator requirement.
- **2026-09-20** — Assumptions: hiding is launcher-only (existing sessions, restart and duplicate unaffected); new custom agents start enabled and unpinned; unknown ids in stored prefs are ignored; changes apply on Settings Save; launcher search filters enabled agents only.
- **2026-09-20** — The Settings list is its own controlled component, `components/modals/LauncherAgents.tsx`, with Settings owning the draft. Why: Settings.tsx is already over 1000 lines, and a controlled child keeps Save/Cancel semantics in one place.
- **2026-09-20** — `loadAgentPrefs`/`saveAgentPrefs` live in `lib/agent-order.ts`, not `app/modals/launcher.ts` as the plan said. Why: importing the launcher module from Settings pulled its bridge calls into every Settings test mock; the storage half belongs with the parser anyway.
- **2026-09-20** — Pinning and Alt+Arrow moves restore focus to the row's Pin checkbox after the commit. Why: pinning remounts the row in the other list, and React may re-insert a moved row; both blur it.

## Progress

- **2026-09-20** — Spec and plan created; triaged enhancement / M / P2.
- **2026-09-20** — Plan approved (2 HTML rounds; operator asked whether DnD is React-based — answered in Approach, imperative core kept).

## Open questions
- **2026-09-20** — Implemented. Checks: tsc, `biome ci`, vitest 1554/1554, `ui-lint --strict`, mock e2e 400/400. Not verified: the browser's own decision to start a native drag in WKWebView. The synthetic e2e drag cannot cover that (see `fixtures/drag.ts`).

## PR convergence ledger

- **2026-09-20 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: f5ad41b. Two MINORs: fixed the `saveAgentPrefs` comment. Kept the Alt+Arrow binding out of README/⌘/, consistent with other in-dialog keys (launcher 1–9/arrows); the inline hint covers it.
