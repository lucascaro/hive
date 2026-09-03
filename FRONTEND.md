# FRONTEND.md

Conventions for `cmd/hivegui/frontend`, the Wails desktop client. Architecture
for the whole repo lives in [DESIGN.md](DESIGN.md); visual rules (tokens,
themes, icons) live in [docs/design-docs/ui/](docs/design-docs/ui/README.md).
This file covers how the frontend itself is put together.

## Stack

- **React 19** — one root, mounted in `src/main.tsx`. No router, no SSR, no
  Suspense/concurrent features, no React Compiler.
- **zustand v5** (vanilla store + `useStore` hook) — `src/store/store.ts`.
  Vanilla rather than React-only because non-React code (the daemon event
  handlers, the keyboard pipeline, the Playwright harness) reads and writes it.
- **xterm.js** behind an imperative boundary — `src/app/session-term.ts`. React
  never owns a terminal; see *Terminals* below.
- **Vite** for dev/build; **TypeScript** strict; **Biome** for lint + format
  (`npx biome ci .` — `biome lint` alone does not check formatting).
- **Wails v2** bridge — `src/bridge.ts`, which must stay a direct child of
  `src/`: `vite.config.js` substitutes its wailsjs import specifiers for the
  test harnesses.

## Layout

| Path | What lives there |
|------|------------------|
| `src/main.tsx` | Composition root: command table, cross-module wiring, the single React root, control-connection boot. No behaviour. |
| `src/components/` | React components. `App.tsx` is the tree; `modals/` holds the seven dialogs. |
| `src/store/` | `store.ts` (the zustand store + every action) and `terms.ts` (the SessionTerm registry, deliberately outside the store). |
| `src/app/` | Imperative subsystems: daemon events, keyboard, view commands, grid layout, focus pipeline, the terminal class, and the thin modal controllers the components call into. |
| `src/lib/` | Shared logic, no bridge calls. Almost all of it is pure and unit-tested in the node environment. Three modules are the exception — `focus-trap.ts`, `preserve-focus.ts` and `drag-placeholder.ts` call `document` directly and are exercised under `test/dom`. |
| `src/theme/` | Token CSS, presets, and the runtime theme applier. |
| `src/ui/` | The last two imperative DOM primitives (`icon.ts`, `icon-button.ts`), still used by `session-term.ts`. Everything else was ported to `src/components/`. |

## Rendering

**One React root.** `src/main.tsx` mounts `components/App.tsx` on the empty,
hidden `#react-root`. `App` renders every region as a **portal** into the
element `index.html` already owns. That is not incidental:

- `#terms` must not be React's — its children are SessionTerm hosts.
- `#boot-state`'s card is painted from `index.html` before any module script
  runs, so a tree that owned `#app` would blank and rebuild it at mount.
- Every id, grid-row placement and aria attribute in `index.html` survives, and
  that is what keeps the Playwright specs selecting on ids and `hv-*` classes.

**A portal appends.** The three containers `index.html` seeds with pre-paint
markup (`#status`, `#boot-state`, `#sidebar-hints`) are emptied in `main.tsx`
immediately before the root's first commit, which is flushed synchronously so
no frame lands in between. Add pre-paint markup to a fourth container and it
needs the same treatment — `test/dom/app-root.test.tsx` mounts `App` against the
real `index.html` and fails if any id ends up in the document twice, so the
drift surfaces there rather than as an unrelated Playwright strict-mode
failure.

**Container-level classes** (`.hidden`, `.error`, `.mismatch`) sit on the portal
target, outside React's tree, so each component applies its own in a
`useLayoutEffect` — layout, not passive, so the class lands in the same frame as
the content it belongs to.

## Terminals

`src/app/session-term.ts` stays imperative and React must never recreate one. A
`SessionTerm` owns an xterm instance, one of eight process-wide WebGL slots
(`src/lib/webgl-budget.ts`) and a live PTY attachment. The rules:

- Terminal hosts are reparented, never unmounted and remounted.
- The grid template is written **before** attach, or the scrollback restream
  jumps (`src/app/grid-layout.ts`).
- `ensureAttached()` is not effect-idempotent — it re-latches follow-bottom on
  every call, so an effect must not call it more often than today's paths do.
- The GUI never opens a PTY. Every PTY operation goes through the wire protocol.

## State

`src/store/store.ts` holds everything that is on screen. The contract every
action keeps: **an immutable replace of the slices it touches** — zustand
compares by reference, so a `Set`/`Map` mutated in place never notifies. There
is no separate "save" step: persistence lives in the action that owns the field.

- **React reads** through `useAppStore(selector)`. Selector-scoped subscriptions
  are where the render performance comes from; keep selectors narrow and keep
  memoised children's props primitive or referentially stable.
- **Imperative code reads** through `appStore.getState()`. Modules that do this
  a lot declare a local `const appData = () => appStore.getState();` — a
  function, never a destructured snapshot, because these run inside event
  handlers and must not cache a slice across a store write.
- **Everything writes** through the exported actions. Never assign into state.
- **`nav`** is the one deliberate exception: `lib/nav-history.ts` mutates a
  stable object in place, and nothing renders from it.
- **SessionTerm instances are not in the store** — `store/terms.ts` holds them.
- **`window.__hive_state`** (`store.ts`, gated on `VITE_WAILS_MOCK` /
  `VITE_WAILS_REAL`) is a permanent Playwright API with a frozen shape.
  `test/unit/store.test.ts` asserts every field. Do not narrow it.

Commands that mutate the store and then need the DOM they just changed wrap the
writes in `flushSync` (see `withLayout` in `src/app/view.ts`), so the layout
effect has already repainted before the caller measures or focuses.

## Styling

Token design system, no CSS-in-JS and no CSS Modules (a filed debt item). The
`hv-*` BEM classes and the data attributes are the contract between TS, CSS and
the e2e specs — treat them as an API.

- Tokens in `src/theme/tokens.css`, presets in `themes.css`, per-component files
  under `src/theme/components/`.
- `scripts/ui-lint.sh --strict` (CI) bans raw hex colours, px font sizes and
  inline icon Unicode, in `.ts` and `.tsx` alike.
- Variants are data attributes (`[data-kind]`), not a second class.

## Accessibility

- Every icon-only control carries an `aria-label` — `iconButton()` refuses to
  build one without it, and that assertion is the point of its test.
- Dialogs are `role="dialog" aria-modal="true"` with `aria-labelledby`, and trap
  focus via `src/lib/focus-trap.ts`; the choice dialog is `alertdialog`.
- The keyboard handler is a single **capture-phase** window listener
  (`src/app/keyboard.ts`) — it has to beat inline-rename's `stopPropagation`.
  Modal precedence is decided there, from the store, not from DOM classes.
- Live regions are the text slot alone, never the whole bar, or every navigation
  re-announces static hints.
- Key hints appear at the point of use, in `[key]` / `(key)` form. See AGENTS.md
  › UX Best Practices.

## Testing

Four layers, run with `scripts/test.sh [layer …]` from the repo root.

| Layer | Environment | What belongs there |
|---|---|---|
| `unit` | node | The pure part of `src/lib/*`, and the store. No DOM. The three DOM-touching `lib` modules are tested in the `dom` project instead. |
| `dom` | vitest + jsdom | Components via `@testing-library/react`, and the imperative modules that need a document. |
| `e2e` | Playwright vs the Wails **mock** | User-visible behaviour, against ids and `hv-*` classes. |
| `e2e-real` | Playwright vs a real `hived` | Terminal/PTY behaviour. `npm run test:e2e:real`. Isolation via `HIVE_SOCKET` + `HIVE_STATE_DIR` is mandatory. |

Conventions:

- **Assert the class and data-attribute contract**, not the component's shape.
  That is what makes a test survive a refactor and catch a broken selector.
- **No `data-testid`.** The e2e specs select the same ids and classes the CSS
  does, deliberately: a spec that keeps passing through a markup change it
  should have caught is worse than no spec.
- **A Playwright spec edit means the DOM contract broke.** Fix the component.
- Components that portal into a container need that container in the test's
  markup; several dom suites build the region they exercise in `beforeEach`.
- Anything touching a real browser layout (z-index, overflow, hit testing) is
  verified in Playwright, not jsdom — vitest is CSS-blind.

## Verification before a PR

```bash
cd cmd/hivegui/frontend && npm run typecheck && npm run ci && cd ../../..
scripts/ui-lint.sh --strict
scripts/test.sh unit dom e2e go
cd cmd/hivegui/frontend && npm run test:e2e:real
```

A fresh worktree needs `./scripts/ci-bootstrap.sh` (the wailsjs bindings) and an
`npm install` first, or `tsc` reports a few dozen errors in files you never
touched — they are the missing bindings, not your diff.
Build the app with plain `wails build` — `-s` skips the frontend build and the
app dies at launch with "no index.html".
