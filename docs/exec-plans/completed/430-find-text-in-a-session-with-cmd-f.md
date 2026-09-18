# Find text in a session with ⌘F

- **Spec:** [docs/product-specs/430-find-text-in-a-session-with-cmd-f.md](../../product-specs/430-find-text-in-a-session-with-cmd-f.md)
- **Issue:** #430
- **Status:** superseded — **HELD at PLAN, not approved for implementation.** See `## Superseded`.

## Hold

**This plan was not approved and is not implemented from directly.** It is superseded by [431](431-search-an-agent-session-s-transcript-history.md), which merges this feature behind one ⌘F binding. Its reviewed findings are folded into 431's plan; kept here as the record of two review rounds. The operator held it at PLAN on 2026-09-17, at the plan stop, after research established that in-terminal search cannot reach agent-session history.

Why: agent sessions run on the alternate screen buffer, so a browser-side find box can only ever search one screenful — measured, not assumed (see `### PoC findings`). The operator's judgement is that this makes the feature unusable as scoped.

What changed the picture: the history **does** exist, in the daemon's 8 MiB per-session raw-byte ring, alt-screen output included (`internal/session/vt.go:166-171`, asserted by `TestInitialReplayBytesAltScreen`). So "search deeper than the buffer" needs a query path, not a new data source. The plan's earlier framing of that as an untouchable non-goal understated what is feasible.

Decision: design the history-search feature first, then revisit whether ⌘F is the right entry point for it and for this. Source (daemon ring / agent transcripts / VT alt-screen scrollback) is deliberately left to that brainstorm.

The routes and their costs are written up in [docs/design-docs/session-history-search.md](../../design-docs/session-history-search.md) so the follow-up does not repeat the investigation.

**Everything below is preserved as-is.** It is a reviewed, twice-corrected plan for the in-terminal half, and it stays accurate for that half: the binding collision, the four viewport yank sites, the addon-poisoning defect and the 1000-match cap are all properties of the terminal layer and survive whatever the history feature decides. Reuse it rather than re-deriving it.

## Summary

Add an in-terminal find box to the focused session, opened with ⌘F (Ctrl+F off macOS). Incremental search over the live xterm buffer with highlighted matches, a live `n/total` count, and next/prev by key and mouse. The interesting part is not the search — `@xterm/addon-search` does that — it is holding the viewport steady against Hive's bottom-follow machinery while a search is active, and behaving sanely on alt-screen agent sessions that have no scrollback.

## Research

### Relevant code

**Terminal layer**

- `cmd/hivegui/frontend/src/app/session-term.ts` — one `SessionTerm` class per session (`:148-1453`). xterm `Terminal` constructed at `:268-288` (`scrollback: 5000` at `:278`). Addons loaded at `:289-291` (`FitAddon`, stored on `this.fit`), `:324-332` (`WebLinksAddon`, fire-and-forget), and lazily in `_attachWebgl()` (`:835-876`, stored on `this.webgl`, budget-gated). A `SearchAddon` belongs next to `fit` in the constructor with an explicit dispose in `destroy()` (`:1294-1327`, mirroring the webgl teardown at `:1316-1323`, before `this.term.dispose()` at `:1324`).
- Registry is a plain `Map<string, TermTile>` in `src/store/terms.ts:19-24` — deliberately outside the reactive store. Factory/upsert is `ensureTerm(info)` (`session-term.ts:1512-1531`).
- DOM: `this.host` (`.term-host`, `:237-240`) contains `.tile-header` (28px, fixed), `.term-body` (xterm mounts here), and `.tile-overlays` (`display: contents`, `:258`). All created imperatively; React reaches them by portal.

**Where the overlay mounts**

- `src/components/TileChrome.tsx:69-83` portals into `term.header` and `term.overlays`. The search box mounts as a third portal into **`term.overlays`**, alongside `TileOverlays` and `ActivityTileMount` — *not* into `term.header`, which is pinned to 28px for `fit()` row-measurement correctness (see `docs/product-specs/329-react-ify-sessionterms-tile-chrome.md` Constraints).
- `src/components/TileOverlays.tsx` is the precedent: `position: absolute` overlay cards (`DeadOverlay`, `PhaseOverlay`) driven by per-session `TileChromeState` in `src/store/store.ts:657-670` (`initialTileChrome` at `:672`), patched via `patchTileChrome(id, {...})`, read with `useAppStore((s) => s.tileChrome.get(id))` (`TileChrome.tsx:63`). Search state (`open`/`query`/`matchIndex`/`matchCount`) belongs here — per-session by construction, which is what criterion 1 requires.

**Bottom-follow machinery (the hard part — criterion 7)**

- Follow intent is instance state on `SessionTerm`: `_followBottom` (default `true`, `:207`), `_lastUserScrollTs`, `_lastReplayTs`, `_lastViewportY`, `_repinning`, `_pointerDown` (`:206-213`).
- `term.onScroll()` handler (`:745-832`) decides follow **from user-gesture recency, never buffer geometry alone** (rationale comment at `:676-685`). Wheel (`:653`), pointer drag (`:708-724`) and Shift+PageUp/Down/Home/End (`:729-743`) each stamp `_lastUserScrollTs`. A scroll only updates `_followBottom` when `now - _lastUserScrollTs <= USER_SCROLL_GRACE_MS` (250ms, `:759-761`), as `buf.baseY - to <= STICKY_BOTTOM_LINES` (2, `:125`). Non-gesture upward drift (cap-trim under heavy output) is deliberately ignored so it cannot clear follow intent.
- Auto re-pin: when `_followBottom` is set and the viewport has drifted, `onScroll` calls `term.scrollToBottom()` itself, guarded by `_repinning` (`:779-791`). **This is one of three sites that would yank the viewport off an active match**; see Approach item 2 for the full list and why clearing `_followBottom` handles all three.
- `_onBodyResize` (`:1031-1184`) reads `wasAtBottom` and re-snaps at `:1072`; mode switches call `snapVisibleTermsToBottom()` (`lib/view-scroll.ts:66-89`), which force-resets `_followBottom = true`. `lib/scrollback.ts` exports `resetFollowIntent()`, already used by `ensureAttached()`.

**Alt-screen (criterion 8)**

- Already load-bearing, read on demand as `term.buffer.active.type === 'alternate'`: `lib/scrollback.ts:118` (`decideResizeReplay` skips replay entirely on alt-screen; rationale at `:84-107`), `lib/wheel-scroll.ts:37,45` (`shouldScrollViewport` returns false so xterm handles the wheel natively), `session-term.ts:641-648`.
- No `onBufferChange` listener exists anywhere in the frontend — buffer type is sampled at the moment of use, never subscribed to.
- Those same comments name "Claude, Codex, pi" as alt-screen TUIs (`scrollback.ts:84-107`, `wheel-scroll.ts:29-30`, `session-term.ts:637`, `:1132-1136`). This confirms the spec's premise: agent sessions really are alt-screen today, so search there sees one screenful and no scrollback.

**Lifecycle (criterion 6)**

- Terminals are **never destroyed on session switch or grid transitions** — only reparented (`lib/grid-layout.ts:217`; "reparent-never-recreate", `:6`). `show()`/`hide()` (`session-term.ts:996-1025`) only toggle classes.
- The only real teardown is `SessionTerm.destroy()` (`:1294-1327`), fired from `app/events.ts:664` on session removal.
- **Therefore criterion 6 has no lifecycle hook to hang off.** Dismissal must be driven from the `setActive` choke point (`app/focus.ts:46-68`), which every switch path funnels through — tile click, arrow nav, project switch, nav-history replay. The existing `switched` boolean at `focus.ts:55` is precisely that edge.

**Keybinding surface (the 5 files, per `AGENTS.md:191-218` and `lib/shortcuts.ts:1-16`)**

1. `src/app/keyboard.ts` — one window `keydown` listener from `:120`, an ordered cascade of early-return gates: inline rename (`:155-162`), choice dialog (`:164-176`), each `isModalOpen(...)` (`:177-317`), dead-session overlay (`:319-347`), non-uniform chords incl. `activityKey` (`:355-388`), then the generic `cmdOrCtrl(e)` gate (`:394`). ⌘F is uniform (⌘ on mac / Ctrl elsewhere), so the open-toggle sits after the `cmdOrCtrl` gate; but Escape-while-open needs its own early gate, parallel to the dead-overlay one.
2. `src/lib/shortcuts.ts` — **both** `shortcutGroups()` (the "Inside a terminal" group starting at `:208` is the right home) and `paletteShortcuts()` (`:262`). Label via `mod(isMac, 'F')`.
3. `src/main.tsx:222-226` — the `paletteCommands` array (starts `:138`).
4. `cmd/hivegui/menu_darwin.go` — `keys.CmdOrCtrl("f")` + `emit("menu:find-in-session")`; the emitted action needs a handler in `keyboard.ts`'s menu-action map (`:893-903`). The file's own comment (`:17-21`) requires every shortcut to be menu-reachable.
5. `README.md:264-287` keybinds table.

⌘F is confirmed unbound in `keyboard.ts`, `keymap.ts` and `menu_darwin.go`.

**Keeping typed keys out of xterm**

- Inline rename uses a capture-phase listener on its own input (`app/inline-rename.ts:139-147`, rationale `:133-138`) *and* a `keyboard.ts` gate (`:155-162`, why at `inline-rename.ts:49-55`). For the find box only the second half is the mechanism: `keyboard.ts`'s window listener is itself capture-phase (`:121`, `true` at `:534`), so an input-level `stopPropagation()` cannot stop it, and xterm's textarea never sees the keys once focus is in the input. See Approach, "Keeping typed keys out of the terminal".
- Focus return on close: `refocusActiveTerm()` (`app/focus.ts:332-338`), used exactly this way at `TileChrome.tsx:177`.

### Constraints / dependencies

- **`@xterm/addon-search` is not a dependency yet.** Not in `package-lock.json`. Stable line is 0.14.0 / 0.15.0 / 0.16.0; `@xterm/addon-search@0.16.0` declares `peerDependencies: { "@xterm/xterm": "^5.0.0" }`, matching the pinned `@xterm/xterm@5.5.0` and the same generation as the already-pinned `addon-fit@0.10.0`. **Pin 0.16.0**; avoid the `0.17.0-beta.*` stream, which targets the unreleased xterm 6.
- **`node_modules` had to be bootstrapped** — `./scripts/ci-bootstrap.sh` was run during research (it was absent in this worktree); it must run before `typecheck` or any vitest/playwright layer, or roughly 20 unrelated `tsc` errors appear from the missing wailsjs bindings. The PoC then side-installed `@xterm/addon-search` with `--no-save`, so it is in `node_modules` but in neither `package.json` nor the lock — see Files to change, step 1.
- **CSS Modules are NOT available.** `docs/product-specs/frontend-css-modules.md` is still `stage: TRIAGE`, `priority: P3`, and blocked on an e2e-selector-strategy prerequisite. The live convention is global CSS, one file per component under `src/theme/components/*.css` (32 files), every selector prefixed by a parent context (`tile-header.css` prefixes every rule with `.term-host`). Tokens only — `scripts/ui-lint.sh --strict` rejects raw hex and raw px font sizes.
- Closest visual precedent for a small floating input: `.tile-name-input` (`theme/components/tile-header.css:63-74`) — `background: var(--surface-raised); border: 1px solid var(--accent); border-radius: var(--radius-sm)`. Contrast floor is `--fg-muted`, not `--fg-subtle` (`tile-header.css:49-53`) — matters for the match-count text.
- The GUI never opens a PTY (`AGENTS.md:66-68`). Search operates on the client-side xterm buffer only; no `internal/session` import.
- `site/features.json` needs an entry (`AGENTS.md:251-272`): `status: shipped`, `since: "Unreleased"`, one-sentence blurb.
- A changeset is mandatory (`AGENTS.md:224-249`): `type: added`, `bump: minor`. Never edit `CHANGELOG.md` or `docs/product-specs/index.md` — `block-generated-edits` fails the PR.

### Prior lessons

- **Audit xterm's own keyboard handling before adding a terminal keybinding.** Read `evaluateKeyboardEvent` in the installed `@xterm/xterm` (`node -e` over `node_modules/@xterm/xterm/lib/xterm.js`, find `t.evaluateKeyboardEvent=`). xterm handles some chords internally; a binding can need far less app code than assumed, or collide silently. Actionable here: confirm ⌘F/Ctrl+F is not already consumed by xterm before wiring the app-level shortcut. Cannot be done until `ci-bootstrap.sh` installs `node_modules`.
- **xterm focus-vs-renderer race.** Synchronous DOM-mutating xterm helpers (`scrollToBottom`/`refresh`/`fit`) called right after a rAF-scheduled focus restoration race the focus-retry loop cross-platform — Linux fails when the helper lands before the focus rAF, macOS when it lands mid-retry. Defer past the retry budget (~130ms ceiling; 250ms `setTimeout` is the safe default). Directly relevant: close-search restores terminal focus *and* scrolls to bottom, which is exactly this pairing.
- **Keybinding changes drift beyond the documented checklist.** Prose comments elsewhere asserting the old routing (e.g. "this key is inert") survive silently. Grep the whole tree for both glyph and word forms (`⌘F`, `Cmd+F`, "find") in live source, not just the five enumerated files.
- Nothing in the brain covers terminal search or scrollback-follow directly; the above are the closest analogues.

### Conventions card

Commands, verbatim:

```
./build.sh                  # macOS .app (GUI + daemon)
./scripts/ci-bootstrap.sh   # REQUIRED FIRST in a fresh worktree: installs node_modules + generates wailsjs bindings
scripts/test.sh             # all layers: go unit dom e2e
scripts/test.sh go          # go test ./... -count=1 -timeout 120s
scripts/test.sh unit        # vitest run test/unit
scripts/test.sh dom         # vitest run test/dom
scripts/test.sh e2e         # playwright install chromium && playwright test
scripts/ui-lint.sh --strict # token/icon rules; required for src/theme, src/components, markup
```

In `cmd/hivegui/frontend/`: `npm run typecheck` (`tsc --noEmit`), `npm run ci` (`biome ci .` — `ci` is the only one that checks formatting; `lint` does not).

Conventions this feature touches:

- **Keybindings Policy (`AGENTS.md:191-218`) applies in full**: `keymap.ts`/`keyboard.ts` (never hard-code the modifier — use `lib/platform.ts` helpers), the ⌘/ help overlay **and** the command palette, the README keybinds table, plus a changeset.
- Global CSS under `src/theme/components/`, parent-prefixed selectors, design tokens only. Read `docs/design-docs/ui/README.md` before touching `src/theme/`, `src/components/`, or markup.
- Test layers: `test/dom/` (Vitest jsdom; `xterm-reflow.test.ts` constructs a real `Terminal`, writes text, inspects `buffer.active` — the shape for asserting search/match state), `test/e2e/` (Playwright against the Wails mock in `test/e2e/wails-mock.ts`; `launcher-search.spec.ts` is the near-exact precedent — a keybinding opens a filter box, asserts auto-focus, types, asserts filtering, Enter, asserts state).
- Run local Playwright with `CI=1`, or it reuses a stale vite dev server and a green run means nothing.
- Commit style: `type(scope): summary (#PR)`, e.g. `feat(gui): …`.
- `site/features.json` entry required for a `bump: minor` user-facing feature.

## Approach

`@xterm/addon-search@0.16.0` does the searching. This plan is about the five things the addon does not handle. All of it is grounded in a PoC against the real addon (see `### PoC findings`) plus two review rounds that each corrected a design error (see `### Review history`).

**1. A per-session search box, state in `TileChromeState`.** The box is a React component portalled into `term.overlays` — a third portal alongside `TileOverlays` and `ActivityTileMount` in `TileChrome.tsx:69-83`. State lives in `TileChromeState` (`store/store.ts:657-670`, `initialTileChrome` at `:672`) as a single nullable `search` field, patched via the existing `patchTileChrome`. This gets criterion 1 for free: the state is keyed by session id, so the box cannot render on another session. Rejected alternative: a global entry in the `modals` array — `anyModalOpen()` means "owns the keyboard app-wide" (`store.ts:1059-1077`), and a per-tile find box must not block the rest of the app.

Not `term.header`: it is pinned to 28px and that pin is load-bearing for `fit()` row measurement (spec 329 Constraints).

**2. A `_searchActive` flag, guarding four yank sites.** `findNext` moves the viewport (PoC finding 6). Four separate sites would drag it back, and they do not all read the same way:

| Site | What it does | If `_followBottom` were merely cleared |
|---|---|---|
| `session-term.ts:779-791` | `onScroll` auto re-pin | disabled |
| `session-term.ts:1057,1072` | `_onBodyResize`: `wasAtBottom = _followBottom`, then `scrollToBottom()` | disabled |
| `session-term.ts:1129` | `_replayWantsBottom = wasAtBottom` | disabled |
| `scrollback.ts:303` / `:344` | replay-done `finish()`: snaps to bottom **only** when `wantsBottom && _followBottom`; otherwise falls through to `scrollToLine(target)` and overwrites `_followBottom` from geometry (`:347`) | **ENABLED — actively restores the reader position** |

So the field cannot carry this alone. A dedicated `_searchActive` flag on `SessionTerm` guards all four explicitly:

- The `onScroll` re-pin, `_onBodyResize`'s re-snap, `_replayWantsBottom`, and `scrollback.ts`'s `finish()` restore branch each early-out while `_searchActive` is set.
- `resetFollowIntent()` (`scrollback.ts:200-206`) early-returns while `_searchActive`. Without this, any mid-search `ensureAttached()` — reachable from `grid-layout.ts:70,125,196`, `session-term.ts:1041,1379` and others — force-sets `_followBottom = true` (`:203`) and silently revokes search's claim. `_wire` does the same at `session-term.ts:686`.
- On open: `_followBeforeSearch = _followBottom`, then clear it. On close: restore `_followBeforeSearch`, clear `_searchActive`, and `scrollToBottom()` if it was set — deferred 250ms per the brain's focus-vs-renderer-race lesson, because close also restores terminal focus and that pairing is what flakes cross-platform.

An earlier draft tried to avoid the flag by clearing `_followBottom` alone, claiming that fixed every site "with no new conditionals". That was wrong twice: it misses `scrollback.ts`'s restore branch (which a cleared field *enables*), and it does not survive `resetFollowIntent()`. Both were caught in review round 2 and verified directly against the files.

Search jumps do not stamp `_lastUserScrollTs` (only wheel, pointer and Shift+Page keys do — `:653`, `:708-724`, `:729-743`), so a manual scroll back to the bottom mid-search still re-latches follow, which is the correct reading of that gesture.

**3. The addon is recreated on buffer-type change.** The PoC found a hard defect: **any** search performed while the alt buffer is active permanently poisons the addon for the normal buffer after the app exits — whether that in-alt search succeeded or failed. `clearDecorations()` does not clear it; an empty-query search does not clear it; only a fresh `SearchAddon` instance recovers. Untreated: "search a Claude session, Claude exits, search again, silently find nothing, forever."

Fix: subscribe to `term.buffer.onBufferChange`, dispose and reload the `SearchAddon` on every transition. This is the one piece with no precedent here — everything else samples `term.buffer.active.type` on demand (`scrollback.ts:118`, `wheel-scroll.ts:37`, `session-term.ts:641`) and never subscribes.

**4. On macOS the entry point is the native menu, not the keydown.** `keyboard.ts:899-902` documents this for ⌘/: "the native ⌘/ accelerator intercepts the key before the webview on macOS, so the keydown close path … never sees ⌘/". The same applies to `keys.CmdOrCtrl("f")` in `menu_darwin.go`. Therefore:

- On macOS, ⌘F arrives as `menu:find-in-session`. That handler is a **toggle/refocus**, exactly as `menu:keyboard-shortcuts` is — pressing ⌘F with the box already open refocuses the input rather than stacking a second open.
- `findKey()` in `keymap.ts` fires **only off-mac** (Ctrl+Shift+F).
- **The Playwright e2e cannot exercise the real mac path** — it drives the webview directly, so `Meta+f` there tests a path production never takes. The e2e asserts the off-mac chord and the menu-event handler separately; the accelerator itself is a manual check in the built app. A stated coverage gap, not an oversight.

**Binding: ⌘F on macOS, Ctrl+Shift+F elsewhere.** Not plain Ctrl+F. xterm converts plain Ctrl+letter to a C0 control char (`xterm.js`: `keyCode>=65&&keyCode<=90 ? String.fromCharCode(keyCode-64)`), so Ctrl+F is `0x06` — readline's `forward-char`, live in bash, zsh and agent input. Same reasoning already written into `keymap.ts:186-189` for ⌘J/Ctrl+Shift+J. `findKey()` follows `activityKey()`'s shape, dispatched before the `cmdOrCtrl` gate (`:394`).

**5. Single-session view only, and the box closes on view change.** Per operator decision, the chord in grid mode does nothing, gated on `view === VIEW_SINGLE` (`lib/view.ts:5`). Closing on view change is not cosmetic: the mode snap force-sets `_followBottom = true` (`view-scroll.ts:85`), so a ⌘G mid-search would revoke search's claim and leave the restore path never firing.

**Count renders `n/1000+` past the cap.** The addon hard-caps `resultCount` at 1000 (PoC: 3000 occurrences reported as 1000). A bare `1000` would be a confidently wrong number.

**Live count, coalesced to one search per frame, on both buffers.** Criterion 3's second half ("updates when new output adds or trims matches") needs a write hook: `SessionTerm.writeData` (`:1274-1292`) schedules a re-search on rAF, coalescing bursts so a streaming session runs at most one search per frame. The PoC measured per-*keystroke* cost (4.9ms first, then ≤1.7ms), not per-*write* cost with decorations on a flooding session — so the coalescer is load-bearing for criterion 9, not an optimization.

The pending frame is cancelled on `searchClose()` **and** in `destroy()`, following the in-repo precedent at `session-term.ts:1297` (`if (this._revealRaf) cancelAnimationFrame(this._revealRaf)`). Without that, a frame landing after close re-searches a closed box, and one landing after `this.term.dispose()` (`:1324`) calls into a disposed addon.

**This re-searches on alt-screen writes too**, which meets criterion 3 literally. Flicker is handled as a rendering concern, not by holding back the count (operator rule at the plan stop, verbatim: "avoid flicker if possible (flicker is a visual artifact that is not the same as number changing). count can change if new matches enter or existing matches exit the buffer."):

- **The count updates whenever the match set changes, on both buffers, with no delay.** The store is patched only when `{count, index}` actually differs — `patchTileChrome` already shallow-compares and stays silent on an identical patch (`store.ts:686-700`) — so an unchanged set costs no render while a changed one is never delayed.
- **The count field never reflows.** Fixed min-width plus `font-variant-numeric: tabular-nums`, so `9/17` → `10/17` does not resize the box or shift the buttons. Digit-width jitter is the most likely source of perceived flicker and it is pure CSS.
- **Decorations are not rebuilt when the match set is unchanged.** Re-registering identical decorations is a visible flash on a streaming session.
- **Addon recreation re-applies the active query in the same frame.** The `onBufferChange` fix necessarily drops decorations; re-applying synchronously after the reload avoids a visible gap on every alt-screen transition.

An earlier draft proposed a 150ms settle on the alternate buffer to stop the count moving during repaints. Dropped: it suppresses genuine count changes to hide a rendering artifact, which is the wrong layer. A match that genuinely appears or disappears is reflected immediately; a repaint that leaves the match set unchanged produces no store write and no decoration rebuild, so nothing visible happens.

**Keeping typed keys out of the terminal.** The mechanism is the `findBoxActive()` gate in `keyboard.ts`, not the input's own listener. `keyboard.ts`'s window listener is capture-phase (`:121`, `true` at `:534`), so a capture `stopPropagation()` on the input cannot stop it; and xterm's textarea never sees the keys anyway once focus moves to the input. The gate sits beside `inlineRenameActive()` (`:155-162`) and handles Escape (close), Enter / Shift+Enter (next / prev), and lets ⌘A / ⌘V / ⌘C through as normal text editing.

### PoC findings

Probes run against real `@xterm/xterm@5.5.0` + `@xterm/addon-search@0.16.0` in the `dom` layer, then deleted. Each drove a decision above.

1. Normal buffer searches the full scrollback — needle on line 5 found with the viewport at line 291.
2. Alt buffer corpus is exactly `rows` lines (length 10 at 10 rows); normal-buffer content is unreachable while alt is active. Confirms the spec's ceiling empirically.
3. `onDidChangeResults` fires **only** when the `decorations` option is passed — the hit count and the highlighting are one feature, not two.
4. `resultCount` is hard-capped at 1000.
5. Cost per keystroke on a full 5000-line buffer: 4.9ms first search, then 1.7 / 0.4 / 0.3 / 0.3 / 0.3ms; pathological single-char query 2.1ms. Does **not** cover per-write cost on a streaming session — hence the rAF coalescer.
6. `findNext` moves the viewport (291 → 0).
7. Searching while the alt buffer is active poisons the addon for the normal buffer permanently; only a fresh instance recovers. Locked down by a regression test, because it is invisible by inspection.

### Files to change

1. `cmd/hivegui/frontend/package.json` + `package-lock.json` — via `npm install --save-exact @xterm/addon-search@0.16.0`, run from `cmd/hivegui/frontend`. **Not a hand-edit of `dependencies`**: the lockfile must move together or CI's `npm ci` fails. The addon is currently in `node_modules` from the PoC's `--no-save` install but in neither manifest.
2. `cmd/hivegui/frontend/src/app/session-term.ts` — load `SearchAddon` beside `FitAddon` (`:289`), store on `this.search`. Add `_searchActive` and `_followBeforeSearch` next to `_followBottom` (`:207`), and guard the `onScroll` re-pin (`:779-791`), `_onBodyResize`'s re-snap (`:1057,1072`) and `_replayWantsBottom` (`:1129`) on `!this._searchActive`. Add the `buffer.onBufferChange` subscription that disposes and reloads the addon. Add `searchOpen()` / `searchClose()` / `searchFind(query, dir)` driving the addon and the store, plus the rAF-coalesced re-search hook in `writeData` (`:1274-1292`). In `destroy()` (`:1294-1327`): cancel the pending re-search frame (precedent `:1297`), dispose the addon and the `onBufferChange` listener, all before `this.term.dispose()` (`:1324`).
3. `cmd/hivegui/frontend/src/lib/scrollback.ts` — guard the replay-done `finish()` restore branch (`:303`, `:344-347`) on `!st._searchActive`, and early-return from `resetFollowIntent()` (`:200-206`) while it is set. This is the fourth yank site and the reattach hole; see Approach item 2.
4. `cmd/hivegui/frontend/src/store/store.ts` — add `search: TileSearchState | null` to `TileChromeState` (`:657-670`) and `null` to `initialTileChrome` (`:672`). No new write path; `patchTileChrome` already handles it.
5. `cmd/hivegui/frontend/src/components/TileChrome.tsx` — third portal into `term.overlays` rendering `<FindBox>` when `chrome.search` is non-null.
6. `cmd/hivegui/frontend/src/lib/keymap.ts` — add `findKey(e, isMac)` after `activityKey` (`:186-205`; the why-not-plain-Ctrl comment is at `:186-189`), noting that the mac branch is unreachable behind the native accelerator.
7. `cmd/hivegui/frontend/src/app/keyboard.ts` — dispatch `findKey` before the `cmdOrCtrl` gate (`:394`); add the `findBoxActive()` gate beside `inlineRenameActive()` (`:155-162`); add `'menu:find-in-session'` to the menu-action map (`:893-903`) as a **toggle**, following `menu:keyboard-shortcuts`.
8. `cmd/hivegui/frontend/src/app/view.ts` — dismiss the box **synchronously in `setView`'s body** (`:422`), the only view-change choke point (every ⌘G/⇧⌘G caller routes through it: `keyboard.ts:474,476,570,574,614,619`, `view.ts:247,356`). Not at `:166` — that line is inside the session-focus path that calls `deps.setActive(id)` (`:128`), already covered by file 9. Not at `:458` either: that snap is inside a `setTimeout(…, 250)`, so a dismissal there would run after the yank. **Ordering note:** that 250ms mode snap force-sets `_followBottom = true` (`view-scroll.ts:85`) and races the plan's own 250ms close-restore; dismissing synchronously in `setView` means the restore completes before the snap timer is armed.
9. `cmd/hivegui/frontend/src/app/focus.ts` — dismiss the box for the previously-active session on the `switched` edge (`:46-55`). Criterion 6.
10. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — add to the `'Inside a terminal'` group (starts `:208`) **and** `paletteShortcuts()` (`:262`). Platform-split label helper beside `activityToggle()` (`:97-99`).
11. `cmd/hivegui/frontend/src/main.tsx` — `{ id: 'find-in-session', name: 'Find in Session', run: toggleFind }` in `paletteCommands` (`:222-226`).
12. `cmd/hivegui/menu_darwin.go` — `view.AddText("Find…", keys.CmdOrCtrl("f"), emit("menu:find-in-session"))` under the **View** submenu (`:102`), beside Command Palette. macOS convention would put Find under Edit, but `:100` appends Wails' stock `menu.EditMenu()` wholesale (Cut/Copy/Paste/Select All) — inserting into it means replacing that stock menu, a bigger change than this feature warrants.
13. `README.md` — a row in the keybinds table (`:264-287`): ⌘F / Ctrl+Shift+F, noting that on a full-screen agent it searches the visible screen only, and why Ctrl+F itself is not used.
14. `cmd/hivegui/frontend/src/theme/components/index.css` — import the new stylesheet.
15. `site/features.json` — `status: shipped`, `since: "Unreleased"`, one-sentence blurb (`AGENTS.md:251-272`).
16. `docs/product-specs/430-find-text-in-a-session-with-cmd-f.md` — amend criterion 1 (Ctrl+Shift+F, not Ctrl+F) and the Desired-behavior sentence claiming grid parity. Without this, `/hs-merge-gate` reads the implementation as failing its own spec.

**Existing tests that must be extended — they fail otherwise:**

17. `cmd/hivegui/frontend/test/unit/shortcuts.test.ts` — asserts no duplicate key combos per group and the platform label shape.
18. `cmd/hivegui/frontend/test/dom/help-overlay.test.tsx` — the ⌘/ overlay renders from `shortcutGroups()`.
19. `cmd/hivegui/frontend/test/dom/keyboard-precedence.test.tsx` — the ordered cascade that `findBoxActive()` joins.
20. `cmd/hivegui/frontend/test/unit/whats-new.test.ts` — asserts every `status: shipped` entry in `site/features.json` has a `since`.

### New files

- `cmd/hivegui/frontend/src/components/FindBox.tsx` — input, `n/total` (`1000+` past the cap), prev/next buttons, close button. Enter / Shift+Enter step matches; Escape closes. Auto-focuses the input on open.
- `cmd/hivegui/frontend/src/theme/components/find-box.css` — global CSS, every selector prefixed `.term-host`, tokens only (`--surface-raised`, `--border`, `--accent`, `--radius-sm`, `--space-2`, `--text-sm`). Match-count text uses `--fg-muted`, not `--fg-subtle` — the WCAG AA floor per `tile-header.css:49-53`.
- `cmd/hivegui/frontend/src/lib/find-state.ts` — pure: `TileSearchState`, `formatMatchCount(index, count, capped)` → `'3/17'` / `'0/0'` / `'3/1000+'`.
- `.changesets/430-find-in-session.md` — `type: added`, `bump: minor`, `pr: <n>`. Issue-prefixed, matching `428-activity-grid-tasks-first.md`.

### Tests

**`test/unit/find-state.test.ts`**
- `formatMatchCount` renders `0/0`, `1/17`, and `3/1000+` at the cap.

**`test/dom/xterm-find.test.ts`** (follows `xterm-reflow.test.ts` — real `Terminal`, write, assert)
- `findsNeedleAboveViewport` — 200 lines, needle on line 5, viewport at the bottom.
- `reportsCountOnlyWithDecorations` — locks PoC finding 3, so dropping the `decorations` option cannot silently kill the counter.
- `capsCountAtOneThousand` — 3000 occurrences report 1000; the UI string is `1000+`.
- `altBufferCorpusIsVisibleScreen` — enters alt via `\x1b[?1049h`; the normal-buffer needle is not findable, the alt needle is.
- **`recreatesAddonAfterAltBufferSearch`** — the regression test for PoC finding 7: search while in alt, exit alt, assert the normal needle is findable. **Fails today without the `onBufferChange` recreation.**

**`test/dom/find-viewport.test.ts`** — one test per yank site, because a single write-path test passes on a broken implementation:
- `writeDoesNotYankOffMatch` (`session-term.ts:779-791`)
- `resizeDoesNotYankOffMatch` (`:1072`)
- `replayDoesNotYankOffMatch` (`:1129`)
- `replayRestoreDoesNotYankOffMatch` — drive `scrollback.ts`'s replay-done `finish()` with the viewport parked on a match above the bottom; assert `scrollToLine` was not called (`scrollback.ts:303,344`). **This is the site the previous draft got wrong.**
- `reattachDoesNotRevokeSearchClaim` — call `ensureAttached()` mid-search; assert `_followBottom` is still cleared and `_searchActive` still set (`scrollback.ts:203`).
- `closeRestoresFollowAndBottom` — close; follow restored and the viewport returns to the bottom.
- `closeRestoresNothingWhenNotFollowing` — opened while scrolled up; close leaves the viewport where it was.

**`test/dom/find-coalesce.test.ts`**
- `oneSearchPerFrameUnderFlood` — spy `findNext`; write 200 chunks in one frame; assert a single search ran. Criterion 9's check; without it the criterion is unfalsifiable.
- `pendingFrameCancelledOnClose` — schedule a re-search, close, flush frames; assert no search ran against the closed box.
- `pendingFrameCancelledOnDestroy` — same with `destroy()`; assert no call into the disposed addon.
- `identicalCountDoesNotPatchStore` — spy `patchTileChrome`; re-search twice with an unchanged match set; assert one patch, not two.
- `identicalMatchSetDoesNotRebuildDecorations` — spy decoration registration; re-search an unchanged set; assert no rebuild. The visible-flash check.
- `countUpdatesImmediatelyWhenMatchesEnterOrExit` — on both buffers, add then remove a match; assert the count moves on the next frame with no delay.
- `addonRecreationReappliesQuerySameFrame` — drive a buffer change mid-search; assert the active query is re-applied without an intervening frame showing no highlights.

**`test/e2e/find-in-session.spec.ts`** (Playwright vs the Wails mock; follows `launcher-search.spec.ts`)
- The off-mac chord opens `.term-find` with the input auto-focused, no click.
- Typing updates the count with no submit.
- Enter / Shift+Enter **and** the prev/next buttons each advance the active match, update `n`, and scroll the match into view (criterion 4).
- Escape closes and the terminal regains focus; **the close button does the same** (criterion 5).
- Switching sessions dismisses the box (criterion 6).
- Switching to grid dismisses the box (⌘G through `setView`).
- The chord in grid mode does nothing — the negative case for single-view-only.
- Typed characters do not reach the shell.
- The `menu:find-in-session` event opens the box, and firing it again refocuses rather than re-opening — the only automatable proxy for the mac accelerator path.

### Verification

```bash
./scripts/ci-bootstrap.sh
cd cmd/hivegui/frontend && npm ci                   # proves package.json + lock agree
cd cmd/hivegui/frontend && npm run typecheck
cd cmd/hivegui/frontend && npm run ci               # biome ci . — only `ci` checks formatting
./scripts/ui-lint.sh --strict
scripts/test.sh unit
scripts/test.sh dom
CI=1 scripts/test.sh e2e                            # CI=1 or it reuses a stale vite dev server
scripts/test.sh go
```

Why each fails on a wrong implementation: `npm ci` fails if the lock was not updated; `recreatesAddonAfterAltBufferSearch` fails today without the fix; the five `find-viewport` yank/claim tests each fail if their specific site is unhandled; `oneSearchPerFrameUnderFlood` and the two cancel tests fail without the coalescer and its teardown; `capsCountAtOneThousand` fails on a raw count; the e2e grid case fails if the binding is not view-gated; `ui-lint.sh --strict` fails on a raw hex color.

Per the brain's stale-routing lesson, before opening the PR:

```bash
grep -rn "Cmd+F|Ctrl+F|⌘F" -E --include='*.ts' --include='*.tsx' --include='*.go' --include='*.md' . | grep -v node_modules
```

Manual check (not automatable): in the built app on macOS, ⌘F opens the box and ⌘F again refocuses it — the native accelerator path Playwright cannot reach.

### Review history

Two second-opinion rounds, both `revise` at confidence 8. Round 2's items were applied **without a third review** (the pipeline allows one re-review), so they carry less independent scrutiny than round 1's. Both rounds' design findings were verified against the real files before being applied, not taken on the reviewer's word.

**Round 1 — 8 must-fix items, all applied:** incomplete dependency step (now `npm install --save-exact` + `npm ci` in Verification); macOS accelerator precedence (Approach 4); viewport under-gating (see round 2 — this fix was itself wrong); no close-on-view-change hook; criterion 3's second half had no mechanism; criterion 9 had no verification; two unrecorded spec deviations; four existing test files missed. Also took its citation corrections and its correction of the key-shielding mechanism.

**Round 2 — 5 must-fix items, all applied. Round 1's viewport fix was only half right:**

1. **Fourth yank site, and round 1's fix turned it on.** `scrollback.ts:303` snaps to bottom only when `wantsBottom && _followBottom`; with the field cleared it falls through to `scrollToLine(target)` (`:344`) and overwrites `_followBottom` from geometry (`:347`). Round 1's "one field, no new conditionals" claim was false. Fixed by reinstating a dedicated `_searchActive` flag.
2. **Nothing protected search's claim against a reattach.** `resetFollowIntent()` force-sets `_followBottom = true` (`scrollback.ts:203`) from `ensureAttached()` (`session-term.ts:1208`), reachable mid-search from many call sites.
3. The coalescing rAF was never cancelled — now cancelled on close and in `destroy()`.
4. Criterion 3's alt-screen skip silently failed the criterion — now re-searches on both buffers, with the churn trade-off raised as an open decision rather than chosen silently.
5. Dismissal placement was wrong: `view.ts:166` is a session-focus path and `:458` is inside a 250ms timer; moved into `setView`'s body.

Its correction that `menu.EditMenu()` does exist (`menu_darwin.go:100`) was verified and the justification rewritten.

### Open questions / risks

- **Alt-screen count churn — resolved at the plan stop.** Criterion 3 met literally; the count is never delayed. Flicker is addressed as a rendering concern (tabular-nums fixed-width count, no decoration rebuild on an unchanged match set, same-frame re-apply after addon recreation). No spec deviation. Residual risk: "no visible flicker" is a judgement the tests can only approximate — the decoration-rebuild and store-write suppression are assertable, but whether it *looks* steady on a busy agent session is a manual check in the built app.
- **The mac accelerator path has no automated coverage.** Verified by hand in the built app; the `menu:find-in-session` e2e case is a proxy.
- **`onBufferChange` is unprecedented here.** Fallback if it proves noisy: sample `buffer.active.type` at each `searchFind` and recreate lazily on a change. The regression test pins the behavior, not the mechanism.
- **Recreating the addon drops decorations mid-search**; they rebuild on the next keystroke. Acceptable versus a permanently broken search.
- **The alt-screen ceiling is the real product risk**, carried from brainstorm and now measured: one screenful on exactly the agent sessions most worth searching. A deeper fix needs a daemon-side scrollback query, which the spec makes a non-goal.
- `_searchActive` adds a fourth actor to viewport ownership (user gesture, replay, resize, now search). The dom tests cover each known yank site, but not every mode-switch permutation.
- The e2e screenshot snapshots (`test/e2e/chrome.spec.ts-snapshots/`, `theme.spec.ts-snapshots/`) should be unaffected since the box only renders when open — to be confirmed on the first CI run rather than assumed.

## Decision log

- **2026-09-17** — Scope limited to the focused session only. Why: operator chose it at the brainstorm gate; cross-session search is a different (daemon-indexed) feature.
- **2026-09-17** — Corpus is whatever xterm holds (5000-line cap, one screenful on alt-screen). Why: no daemon-side scrollback query, keeps the change frontend-local. Measured in the PoC.
- **2026-09-17** — Plain substring, case-insensitive only; case-sensitive / regex / whole-word deferred. Why: operator asked the UI to leave room for the toggles without specifying them yet.
- **2026-09-17** — Search detaches bottom-follow while open and restores it on close. Why: operator chose this; new output must not yank the viewport off the active match.
- **2026-09-17** — **Spec deviation:** binding is Ctrl+Shift+F off macOS, not Ctrl+F as criterion 1 states. Why: xterm converts plain Ctrl+letter to a C0 control char, so Ctrl+F is `0x06` (readline `forward-char`), live in bash, zsh and agent input. Same reasoning as ⌘J/Ctrl+Shift+J (`keymap.ts:186-189`). Spec criterion 1 to be amended.
- **2026-09-17** — **Spec deviation:** search opens in single-session view only; the spec's Desired behavior claims parity with grid tiles. Why: operator chose single-view-only when told a grid tile may be too narrow. Spec sentence to be amended.
- **2026-09-17** — macOS entry point is the native menu event, not the keydown. Why: `keyboard.ts:899-902` documents that the native accelerator intercepts before the webview. The handler toggles, as `menu:keyboard-shortcuts` does.
- **2026-09-17** — Use a dedicated `_searchActive` flag rather than clearing `_followBottom` alone. Why: `scrollback.ts:303/344` *enables* a viewport restore when the field is false, and `resetFollowIntent()` force-sets it back to true on any reattach. Found in review round 2 and verified.
- **2026-09-17** — Re-search on alt-screen writes too, with no-op suppression; flicker addressed in CSS and decoration handling rather than by delaying the count. Why: skipping the re-search silently fails criterion 3 on the sessions criterion 8 exists for; the flicker it would otherwise cause is a display concern, so it is fixed at the display layer. Operator rule at the plan stop: flicker is a visual artifact and is not the same as the number changing; the number may change whenever matches enter or exit. An earlier 150ms alt-screen settle was dropped as a result — it suppressed real changes to hide a rendering artifact. 150ms is a starting constant to tune against a real agent session.
- **2026-09-17** — Must work on alt-screen TUI agents (Claude Code and friends), matching the visible screen. Why: operator clarified "fullscreen agents" means alt-screen, not single-session view. Ceiling recorded in the spec's Notes and measured in the PoC.

## Progress

- **2026-09-17** — Spec created (#430), triaged enhancement / M / P2.
- **2026-09-17** — Research complete (3 Explore agents + brain lookup); stage RESEARCH → PLAN.
- **2026-09-17** — PoC run against the real addon; 7 findings, 2 of which changed the design. Probe files deleted.
- **2026-09-17** — Plan drafted; two second-opinion rounds (both `revise`, confidence 8), 13 must-fix items applied.
- **2026-09-17** — Plan presented at the plan stop over four HTML review rounds. Rounds 1-3 resolved the count-flicker question (and corrected a wrong answer of mine: a 150ms settle that suppressed real count changes to hide a rendering artifact).
- **2026-09-17** — Round 4: operator raised that the feature is unusable if it cannot search alt-screen sessions. Research established the daemon ring retains alt-screen bytes; three routes documented in `docs/design-docs/session-history-search.md`.
- **2026-09-17** — **HELD at PLAN by operator decision.** Not implemented. Next step is a brainstorm for session-history search; source to be decided there.
