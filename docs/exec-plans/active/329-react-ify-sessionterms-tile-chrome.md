# React-ify SessionTerm's tile chrome — master plan

- **Spec:** [docs/product-specs/329-react-ify-sessionterms-tile-chrome.md](../../product-specs/329-react-ify-sessionterms-tile-chrome.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Summary

Port the terminal tile's chrome — header, dead-session overlay, phase loading
panel — from `document.createElement` to React, without letting React near the
xterm instance, the WebGL slot or the attach/resize/replay machinery. React
reaches the tile through `createPortal`; `SessionTerm` keeps owning every
element whose lifetime or geometry is load-bearing. Two phases, each its own PR
and its own detailed plan, mirroring the `react-ui-rewrite` master/phase layout.

## Research

### Relevant code

- `src/app/session-term.ts` — 1710 lines, one `SessionTerm` class (143–1640).
  The chrome construction is four blocks: the tile header (243–317), the dead
  overlay (650–687), the phase overlay (689–705), and `_showPhaseOverlay()`'s
  `<li>` builder (1555–1580). Roughly 330 lines total. Everything else is
  attach/resize/replay/scroll state and must not move.
- `src/store/terms.ts` — the `session id -> SessionTerm` registry, deliberately
  outside the reactive store, with the reason written into its header comment.
  Nothing subscribes to it today.
- `src/components/GridView.tsx` — the existing model for this boundary: React
  owns *when*, `app/grid-layout.ts` owns *what*, the component renders `null`.
  Its comment documents why `sessions` and `attention` are excluded from the
  grid subscription; that reasoning constrains how `TileChrome` subscribes.
- `src/app/grid-layout.ts` — reparent-never-recreate, grid template before
  attach, deferred attach stagger.
- `src/theme/components/tile-header.css:6-20` — `.tile-header` is
  `height: 28px; flex-shrink: 0; box-sizing: border-box`. Fixed box.
- `src/theme/components/empty-state.css:68-127` — `.phase-overlay`,
  `.phase-overlay[hidden]`, `.phase-overlay.fading`, `.phase-steps`,
  `.phase-step[data-state]`. All class-based, no direct-child combinators.
- `src/lib/phase-steps.ts` — `phasePanel()` already returns a pure
  `{ status, steps[] }` model. The React port renders it directly.
- `src/app/inline-rename.ts` — already the shared rename control flow; the tile
  rename reuses it rather than growing a second copy.
- `src/ui/icon.ts` — `ensureSprite`, `ICON_NAMES`, `IconName` are still imported
  by `src/components/Icon.tsx`. Only `icon()`, `stateIcon()` and
  `updateStateIcon()` are dead once the tile is ported.
- `src/app/banners.ts:423-436` — `wireCheckUpdatesButton()`, the second
  imperative `iconButton()` caller.

### DOM contract (the acceptance gate)

The existing suites already select the tile by class, so the port is verified by
tests it must not touch:

| Selector | Pinned by |
|---|---|
| `.tile-header` height 28, children visible | `test/e2e/chrome.spec.ts:101-108`, `test/e2e/theme.spec.ts:105-108` |
| `.tile-minimize` | `test/e2e/minimize.spec.ts`, `test/e2e/grid-scroll-regressions.spec.ts:100,115` |
| `.tile-worktree` | `test/e2e/worktrees.spec.ts:292` |
| `.tile-name`, `input.tile-name-input` | `test/e2e/silent-failures.spec.ts:101-126`, `test/unit/focus.test.ts:39` |
| `.dead-overlay[role="alertdialog"]` | `test/e2e/ux-polish.spec.ts:375` |
| `.phase-overlay` | `test/e2e/session-lifecycle.spec.ts:37` |

### Constraints / dependencies

Every constraint in the spec's `## Constraints` section. The two that shape the
design rather than merely forbidding things:

1. **`.tile-header`'s fixed 28px box** is what makes a one-frame-late portal
   safe. It is the reason the header element is created imperatively and only
   its *children* are portalled.
2. **`phasePanel()` is already pure**, so the phase overlay is the cheapest of
   the three regions to port and carries no logic migration.

## Approach

### The portal boundary

`SessionTerm` keeps owning `host`, `body`, the xterm instance, the WebGL slot
and every attach/resize/replay/scroll invariant — untouched. React renders the
chrome into it via `createPortal`, through two mount points that are
deliberately different:

| Region | Element created by | Why |
|---|---|---|
| `.tile-header` | `SessionTerm` (empty div) | `tile-header.css` pins `height: 28px; flex-shrink: 0`, so an empty header keeps `.term-body`'s box byte-identical on the frame before React fills it. Creating it in React would shrink the body for one frame, mis-fit rows and fire a spurious PTY resize during first attach. |
| `.dead-overlay`, `.phase-overlay` | React (fully) | Both are `position: absolute` and contribute no layout. `SessionTerm` appends one style-less `div.tile-overlays` **after** `body` with `display: contents`; React renders the real overlay elements inside it, owning `class`, `hidden`, `role` and content. DOM order stays header → body → overlays, so paint order is unchanged. |

**Rejected:** letting React own the whole tile. It re-fights
reparent-never-recreate, the 8-slot WebGL budget, the grid-template-before-attach
ordering and `ensureAttached()`'s non-idempotence — the exact reasons the
rewrite deferred this file.

### Observable membership without reactive values

`store/terms.ts` stays a plain `Map` outside the store; a `SessionTerm` must
never be a value React can recreate. It gains a monotonic version counter
bumped in `setTerm`/`deleteTerm`, exposed as `useTermIds()` via
`useSyncExternalStore`. `<TileChromeHost>` subscribes to *membership only* and
emits one `<TileChrome id>` per live term.

### Per-tile subscription

Each `<TileChrome>` subscribes to `s.tileChrome[id]` plus `attention.has(id)`.
`GridView.tsx`'s comment warns that subscribing to `attention` runs a full
layout pass per bell — that hazard does not apply here: `TileChrome` renders no
layout and calls no `ensureAttached()`, so a bell repaints one header.

### State

A new `tileChrome: Record<sid, TileChromeState>` slice —
`{ termTitle, phaseVisible, phaseFading, dead, deadReason, renaming }`. Only the
tile-local facts; name, worktree branch, project and session state are already
derivable from `sessions` / `projects` / `attention`. `setPhase`, `setDead`,
`_renderTermTitle`, `refreshStateIcon` and `_beginRename` keep their names, call
sites and timing — their bodies become store writes. The control flow does not
move; only the rendering does.

## Phases

- **Phase 1 — portal infrastructure + the tile header.** ([plan](../completed/329-react-ify-sessionterms-tile-chrome-phase1.md), PR #329, gate PASS)
  `store/terms.ts` version counter, the `tileChrome` slice, `TileChromeHost`,
  `TileHeader`, the inline rename. `session-term.ts` loses the header block.
  Ends with `src/ui/icon.ts`'s `stateIcon`/`icon` still alive (the phase overlay
  uses them).
- **Phase 2 — the overlays and the last of `src/ui/`.**
  `DeadOverlay`, `PhaseOverlay`, `src/ui/icon.ts` → `src/lib/icon-sprite.ts`,
  `wireCheckUpdatesButton()` → `Sidebar.tsx`, `src/ui/` deleted, the
  host-identity e2e spec, docs.

Phase PRs are behaviour-preserving and carry the `no-changeset` label; Phase 2
adds the one changeset for the whole port.

## Invariants (every phase — violating any reintroduces a shipped bug)

- No `SessionTerm` instance is stored in React state, keyed by React, or
  reachable from a component's props by value. Components take a session **id**.
- No component creates, destroys or reparents `.term-host` or `.term-body`.
- No effect calls `ensureAttached()`. It re-latches follow-bottom; only today's
  imperative paths may call it.
- `.tile-header` exists, with its 28px box, from the moment `SessionTerm`'s
  constructor returns — before any React pass.
- The DOM contract table above holds exactly. `e2e` and `e2e-real` specs are
  never edited to accommodate the port.
- `applyXtermTheme()` / `applyFontSize()` keep iterating `allTerms()` and
  calling `_onBodyResize()`; theming does not become a React concern.

## Verification

Per phase: `npm run typecheck`, `biome ci .`, `scripts/test.sh unit dom e2e`,
then `npm run test:e2e:real`. Playwright runs locally with `CI=1` so a stale
vite dev server cannot green a run. `e2e-real` failures are compared against
`.plans/react-rewrite-flake-baseline.md`.

## Decision log

- **2026-09-03** — Header element created imperatively, header *children*
  portalled; overlays created entirely in React behind a `display: contents`
  mount. Why: `.tile-header`'s fixed 28px box makes a one-frame-late portal
  geometrically free, while the overlays are absolutely positioned and so cost
  nothing either way — this keeps `fit()` measuring the right rows on first
  attach without a `flushSync` inside a layout effect.
- **2026-09-03** — Terms registry made *observable* (version counter) rather
  than *reactive* (values in the store). Why: `store/terms.ts`'s own header
  comment states the reason — a `SessionTerm` in reactive state invites React to
  recreate it, which is the bug the whole migration exists to avoid.
- **2026-09-03** — Spec renumbered to 329 rather than the local `max+1` of 324.
  Why: every other spec number in this repo is a GitHub number and #324 is the
  Phase-6 PR; a local 324 would resolve to unrelated work.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec
  frontmatter). Spec renumbered from `sessionterm-react.md` and corrected on two
  stale points (`ensureSprite()` must move rather than be deleted;
  `app/banners.ts` is a second imperative `iconButton()` caller).
- **2026-09-03** — Phase 1 implemented and green on every layer; see its
  plan for the verification block. `src/ui/icon.ts` and
  `src/ui/icon-button.ts` still stand, as designed — the phase overlay and
  `app/banners.ts` are their last callers, and both are Phase 2's.

## Open questions

- `_revealAfterPhase()`'s `.fading` class becomes React-owned. Today the class
  and `hidden` are set in the same tick and the transition relies on browser
  batching; React batches identically, but this is the one place the port could
  change observable behaviour. Covered by `session-lifecycle.spec.ts` — confirm
  in Phase 2 rather than assuming.
- `display: contents` on `.tile-overlays` with absolutely-positioned children:
  correct per spec and supported in the Chromium/WebKit Wails ships, but it is
  the one layout trick here. Fallback if it misbehaves:
  `position: absolute; inset: 0; pointer-events: none`.
