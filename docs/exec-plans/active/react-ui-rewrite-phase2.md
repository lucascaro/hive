# React UI rewrite — Phase 2: Chrome island: status bar, banners, boot/empty state, tray, footer

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New store state + actions: `status {text, hint, modeHint, flash}`, `banners[]`, `bootState`; actions `setStatus/flashStatus/setModeHint/reportFailure/setBootState/showBanner/dismissBanner`. Reuse, don't re-derive: flash timing stays in `src/lib/status.ts`'s `createStatus` engine (store holds only its rendered output — do not reimplement FLASH_MIN_MS semantics in actions); empty-state content comes from `src/lib/empty-state.ts`'s pure `emptyStateModel()` called in a selector, not stored; the minimized tray is derived in a selector via `src/lib/minimized.ts` (`filterHidden`) from sets already in the store — no new stored state for either.

New files: `src/components/StatusBar.tsx`, `Banner.tsx`, `Banners.tsx`, `BootState.tsx`, `EmptyState.tsx`, `MinimizedTray.tsx`, `VersionFooter.tsx`.

Files to change / delete:
- `src/app/dom.ts` — shrinks to `termsHost` + status functions whose bodies forward to store actions (signatures kept — every module calls `setStatus`). Import-time DOM mutation moves to `main.ts`.
- `src/app/banners.ts` — DOM building deleted; show/dismiss policy + `src/lib/update-state.ts` integration become actions.
- `src/app/version-footer.ts`, empty-state DOM parts of `src/lib/empty-state.ts`, boot-state writes in `main.ts` — replaced by components. Static boot overlay markup stays in `index.html` for pre-JS paint; `BootState` takes over the same ids on mount. `reportFailure` + bounded 5-attempt `retryBoot` keep exact semantics.
- `src/ui/banner.ts`, `button.ts`, `icon.ts`, `icon-button.ts`, `kbd.ts` — deleted.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Known spec-edit exception (carried from Phase 0 review)

`test/e2e/nav-history.spec.ts:100` does
`window.__hive_state?.minimized.add(id)` — an **in-place** mutation of a store
Set, the one pattern the store's reference equality cannot see.

It is correct today and stays correct through Phase 1: the facade getter returns
the live Set, and with no component subscribed to `minimized` the following
render picks the change up. **It stops working in the first phase that
subscribes to `minimized`** — Phase 2's `MinimizedTray` selector, and again in
Phase 5's `GridView`.

Deliberately NOT fixed in Phase 0: the migration's safety proof is that the e2e
specs never change, and editing one to chase a latent issue would have spent
that proof on a non-issue. When the subscriber lands, this is the **one
sanctioned spec edit** — `window.__hive.store.minimizeSession(id)` (or the
equivalent action exposed on the test global) instead of the raw `.add`. It is
NOT a DOM-contract break, so the "a spec edit means the contract broke" rule
does not apply to this line. Note it in that phase's PR description as the
signed-off exception the master plan's Tests section requires.
