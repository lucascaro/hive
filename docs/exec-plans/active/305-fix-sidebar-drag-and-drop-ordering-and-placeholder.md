# Fix sidebar drag-and-drop ordering and drop placeholder

- **Spec:** [docs/product-specs/305-fix-sidebar-drag-and-drop-ordering-and-placeholder.md](../../product-specs/305-fix-sidebar-drag-and-drop-ordering-and-placeholder.md)
- **Issue:** —
- **Status:** active

## Summary

Two independent sidebar drag defects. The session reorder math resolves the drop
slot against the wrong list and lands the row one position too low; the drop
affordance is a zero-height line that reserves no space, so the list reflows all
at once on drop. Fix the math as a pure, unit-tested function and replace the
line with a real placeholder that occupies the dragged element's box.

## Research

Authored via plan-first mode. Code touched:

- `cmd/hivegui/frontend/src/app/sidebar.ts:569-613` — `reorderDroppedSession`.
  `targetIdx`/`projIdx` are computed against `projSessions` (includes the
  dragged row) and then used to index `pretend` (dragged row filtered out).
  When the dragged row sits before the target, every `pretend` index is shifted
  left by one, so the resolved neighbour is one row too far down.
  Trace `[A,B,C,D]`, drag A above C: `pretend[2]` = D (should be C), global
  index 3 → compensated to 2 → daemon splices `[B,C,A,D]`. A lands below C.
- `cmd/hivegui/frontend/src/app/sidebar.ts:416-432` — `reorderDroppedProject`
  indexes one consistent list and is **not** affected.
- `internal/registry/registry.go:1040-1060` — `moveInOrder` is
  delete-then-insert at a global index into `r.order`; `reindexLocked` keeps
  `Order` equal to that index. Both frontend reorder paths depend on it.
- `cmd/hivegui/frontend/src/lib/reorder.ts` — `reorderTarget` (keyboard
  ⇧⌘↑/↓ path) already documents the same invariant and cross-references the
  drag path. Natural home for the extracted drag math.
- `cmd/hivegui/frontend/src/theme/components/session-row.css:155-169` and
  `project-card.css:118-134` — `.dragging { opacity: 0.45 }` plus an
  absolutely-positioned 2px `::after`; neither reserves layout space.
- `cmd/hivegui/frontend/src/app/sidebar.ts:96-125` — `sidebarShape()` /
  `domShape()` select `.hv-project-card, .hv-session-row`; a placeholder must
  carry neither class or the in-place-update path mistakes it for a row.
- `renderSidebar()` (`sidebar.ts:76`) clears `projectsUL.innerHTML`, so a poll
  landing mid-drag wipes drag chrome. The placeholder module re-asserts state
  on every `dragover` to stay self-healing.

## Approach

Split the two defects.

**Ordering** — extract the index computation into a pure
`dropTargetIndex(sessions, draggedID, targetID, above)` in `lib/reorder.ts`,
with the slot derived from `pretend` instead of `projSessions`. Chosen over an
in-place one-line patch in `sidebar.ts` because the function is untestable where
it currently lives (module-private, calls `UpdateSession` directly), and this
class of off-by-one is exactly what a table test catches. It also puts both
order-index conversions in one file under the shared `.order === r.order index`
invariant comment.

**Placeholder** — one shared `lib/drag-placeholder.ts` used by both
`wireSessionDrag` and `wireProjectDrag`: measure the dragged element, take it
out of flow, insert a spacer of the same margin box at the drop slot. Shared
rather than duplicated because the two wiring functions already mirror each
other and the measure/hide/insert/cleanup mechanics are identical.

Two mechanics the implementation must get right:

1. Setting `display: none` on the drag source *inside* `dragstart` aborts the
   drag (the drag image snapshot is taken after the handler returns). Defer the
   hidden state by one tick with `setTimeout(…, 0)`.
2. The spacer must reproduce the full margin box. Project cards carry
   `margin: var(--space-1) var(--space-2) var(--space-2)`; adjacent-sibling
   margin collapsing in the `<ul>` means a zero-margin spacer would not occupy
   the same space. Copy the computed vertical margins onto the spacer alongside
   the measured content height.

### Files to change

- `cmd/hivegui/frontend/src/lib/reorder.ts` — add `dropTargetIndex(...)`
  returning the global index for `UpdateSession`, or `null` for a no-op.
  Extend the header comment to cover the third caller.
- `cmd/hivegui/frontend/src/app/sidebar.ts` — `reorderDroppedSession` reduces
  to `dropTargetIndex(...)` + `UpdateSession`. `wireSessionDrag` /
  `wireProjectDrag`: `dragstart` → `beginDrag`, `dragover` → `moveTo`,
  `dragend`/`drop` → `endDrag`. The `.drop-above`/`.drop-below` class toggles
  and both `querySelectorAll` sweeps collapse into `endDrag()`. Project drag
  keeps its header-rect hit test and `dragleave` `contains()` guard.
- `cmd/hivegui/frontend/src/theme/components/session-row.css` — drop the
  `.drop-above/.drop-below::after` rules; `.dragging` → `display: none`.
- `cmd/hivegui/frontend/src/theme/components/project-card.css` — same.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css` — add
  `.hv-drop-placeholder` (dashed token-coloured outline, `--radius-md`,
  `list-style: none`, `box-sizing: border-box`, `pointer-events: none`).
  Tokens only; `scripts/ui-lint.sh` rejects colour literals.

### New files

- `cmd/hivegui/frontend/src/lib/drag-placeholder.ts` — `beginDrag(el)` records
  the element, its `getBoundingClientRect().height` and computed vertical
  margins, and defers the hidden state one tick; `moveTo(target, above)`
  lazily creates/sizes the spacer, re-asserts the hidden state and inserts it
  before `target` or `target.nextSibling`; `endDrag()` removes the spacer,
  unhides and clears state (idempotent).

### Tests

- `cmd/hivegui/frontend/test/unit/reorder.test.ts` (extend) — `dropTargetIndex`
  table test: drag-down-above-target, drag-down-below-target, drag-up-above,
  drag-up-below, drop onto self (`null`), drop at head, drop past the last row,
  and the interleaved multi-project case. Each case asserts the
  post-`moveInOrder` array, not the raw index — a bare index assertion just
  re-encodes the bug.
- `cmd/hivegui/frontend/test/dom/drag-placeholder.test.ts` (new) — `moveTo`
  inserts `.hv-drop-placeholder` at the right sibling position carrying neither
  row class; `endDrag` removes it and restores the element; a second `moveTo`
  moves the single spacer rather than creating a second one.
- `cmd/hivegui/frontend/test/e2e/ordering.spec.ts` (extend) — drag a session
  onto the top half of another against the Wails mock and assert the row order
  lands exactly at the drop point; must fail against pre-fix `main`. Also
  capture `boundingBox().y` of the row below the drop target before and during
  the drag and assert it is unchanged (vitest is CSS-blind; layout stability is
  only meaningful in the browser).

## Decision log

- **2026-09-01** — Extract the drag index math into `lib/reorder.ts` rather than
  patching in place. Why: the private function is untestable where it lives, and
  the invariant it depends on is already documented next to `reorderTarget`.
- **2026-09-01** — One placeholder module for both session rows and project
  cards. Why: identical mechanics, and the user asked for the affordance in both
  places.
- **2026-09-01** — Placeholder replaces the 2px line rather than joining it, and
  the dragged element leaves the flow. Why: only a net-zero height change keeps
  surrounding content from shifting during the drag.

## Progress

- **2026-09-01** — Plan-first scaffold; stage = IMPLEMENT (set in spec frontmatter).

## Open questions

- Cross-project drags stay unsupported; the placeholder will render over a
  foreign project's rows and snap back on drop. Acceptable, worth a follow-up.
- If the deferred `display: none` still cancels the drag in WKWebView, fall back
  to `visibility: hidden; height: 0` on the source. The e2e test catches it.
- The spacer is `pointer-events: none`, which should prevent a spurious
  `dragleave` on the project card when the cursor crosses it — verify in the
  browser, not by reasoning.
