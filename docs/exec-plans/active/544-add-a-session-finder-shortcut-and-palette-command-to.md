# Add a session finder: shortcut and palette command to jump to any session by name, title, branch or project

- **Spec:** [docs/product-specs/544-add-a-session-finder-shortcut-and-palette-command-to.md](../../product-specs/544-add-a-session-finder-shortcut-and-palette-command-to.md)
- **Issue:** #544
- **Status:** active

## Summary

A dedicated chord (also a palette command) opens a session-only fuzzy finder over title, name, branch and project, recent-first when empty; Enter focuses the session. This plan is the *how*; the *why* is in the spec.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/modals/CommandPalette.tsx` + `app/modals/command-palette.ts` — the pattern to follow: bare `#command-palette` root outside React, `key={entry.seq}` remount resets state, own keydown listener for ↑/↓/Enter, Esc owned by a `KEY_SCOPES` entry (`app/key-scopes.ts:165`), `closeCommandPalette()` blurs then `flushSync(closeModal)` then `focusActiveTerm()` (Esc-restores-focus already solved there).
- `store/store.ts:195,215` — `ModalId` union and `ModalEntry` union; a new modal id is added in both.
- `app/commands.ts:338,350` — command bus entries (`command-palette`, `command-palette.close`) and `:110-` palette rows (`title`). `app/commands.ts:418` menu map only needed if a macOS menu item is added (not planned).
- `lib/bindings.ts` — `DEFAULT_APP_BINDINGS`; `{mac, other}` split pattern for chords whose plain-Ctrl form is a terminal byte (`Mod+J` vs `Ctrl+Shift+J`, line ~90). `RESERVED_CHORDS` at ~180.
- `lib/shortcuts.ts:348` — help overlay rows; also feeds Settings → Shortcuts.
- `app/selectors.ts:30` `orderedSessions()` — painted order across all projects, incl. collapsed.
- `app/actions.ts:339-365` `navGo(id)` — the correct "go to session" path: restores a minimized session/project via `restoreSession`, else `switchTo`, wrapped in `withoutNavHistory`. Reuse semantics (without the nav-history suppression: a finder jump is real navigation and should record a departure).
- `lib/nav-history.ts` — `state.nav.back` is a stack of DEPARTED ids (most recent last), recorded by `setActive`. Source for recency ordering: reverse `back`, dedupe, drop non-existent, append the rest in `orderedSessions()` order, exclude the active session from the top.
- `app/state.ts:28` `SessionInfo`: `name`, `title` (OSC title, snake-only), `worktree_branch`/`worktreeBranch`, `project_id`/`projectId`, `agent`. Project name from `appData().projects`. Wire fields: read `snake_case ?? camelCase`.
- Tests: pure `lib/` modules under `test/unit` (e.g. `nav-history.test.ts`, `shortcuts.test.ts`, `bindings.test.ts`, `shortcut-labels-parity.test.ts`); `test/e2e/every-shortcut.spec.ts` sweeps every binding/menu command automatically (new *scope* needs a `FIXTURES` entry); vitest-dom for component rendering.

### Constraints / dependencies

- Chord choice is the hard part. Free today: plain `Mod+K`, `Mod+O`, `Mod+L`. On non-mac `Mod` = Ctrl, and Ctrl+K/O/L are readline/terminal bytes, so the default needs a `{mac, other}` split like `toggle-activity`. `Mod+Shift+K` is the palette. Chord must not appear in `RESERVED_CHORDS`; `bindings.test.ts` + `every-shortcut` sweep catch collisions.
- No daemon/wire change: all fields are already in GUI state, so no `DaemonContract` bump.
- Minimized sessions must be restored on jump (see `navGo`), or focus lands on `<body>`.
- `recency` is approximate if derived from `nav.back` (back/forward walks mutate it). Acceptable for "chord, Enter = go back to last", but verify with a test.
- Archived sessions are not in `appData().sessions`, so they are excluded for free.

### Prior lessons

No brain entries matched ("session finder palette").

### Conventions card

- Test: `scripts/test.sh` (layers `go unit dom e2e`); frontend: `cd cmd/hivegui/frontend && npm run typecheck` (needs wails bindings; fresh worktree → `./scripts/ci-bootstrap.sh` first), `npx biome ci .` (not `biome lint`), vitest unit/dom, Playwright with `CI=1`.
- AGENTS.md Build/Lint/Test placeholders are unfilled; use the commands above.
- Verify by exit code (`cmd >/dev/null && echo OK`), not by grepping output.
- Keybindings: binding in `lib/bindings.ts`, command in `app/commands.ts` (with `title`), help row in `lib/shortcuts.ts`, README Keybinds table, `.changesets/<slug>.md`; labels derived via `shortcutLabel(id)`, never hard-coded. New modal key scope → `FIXTURES` in `every-shortcut.spec.ts`.
- User-visible feature (`bump: minor`) → also `site/features.json` entry with `since: "Unreleased"`.
- UI: tokens/icons rules in `docs/design-docs/ui/README.md`; `scripts/ui-lint.sh` enforces. Key hints visible at point of use; status dot + agent badge on session rows.
- CSS/layout must be validated in a real browser (Playwright + Wails mock, `elementFromPoint`), not by reasoning.
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)` for any Go check (none expected here).
- No literal `[skip ci]` in commit/PR text.

## Approach

Model the finder on the archived-sessions modal (spec 518: `ModalShell`, filter box, ↑/↓/Enter, `root` element in `index.html`) and the palette's close/focus handling. All matching and ordering logic is a pure `lib/` module (unit-tested); the modal is a thin view over it. Session data is already in GUI state, so there is no daemon, wire or `DaemonContract` change.

Why not extend the command palette with session rows: the user chose a dedicated chord + palette command (brainstorm round 1), and mixing two row kinds would put session-matching rules and Enter semantics inside a component whose contract is "run a command".

**Chords (decision log, operator's final word at the plan stop):** a re-key of four defaults, same on every platform (`Mod` = ⌘ / Ctrl):

| Command | Was | Now |
|---|---|---|
| `session-finder` (new) | — | `Mod+P` |
| `command-palette` | `Mod+Shift+K` | `Mod+Shift+P` |
| `duplicate-session` | `Mod+P` | `Mod+K` |
| `duplicate-session-choose-tool` | `Mod+Shift+P` | `Mod+Shift+K` |

This is a **user-visible change to three existing defaults** (changeset gets a `changed` entry). Users with a `keymap.json` override keep it. Ctrl+K off macOS is readline kill-line, now stolen by Duplicate; Hive already steals Ctrl+P this way and all of these are rebindable. The chord swap must land in one commit so no intermediate state has two commands on one chord.

**Matching:** `fuzzyScore(query, text)` = case-insensitive subsequence match; score favours contiguous runs, word-start hits (after `-_/. ` or camelCase), and prefix. A row's score is the best score over its four fields (name, title, branch, project), whitespace-separated query terms must each match some field (AND). Ties keep the recency/painted order.

**Order:** empty query → recency: reverse of `state.nav.back` (deduped, existing sessions only), then remaining sessions in `orderedSessions()` order, the active session last. So chord → Enter returns to the previously focused session.

**Jump:** export `goToSession(id)` from `app/actions.ts` — the body of `navGo` minus the `withoutNavHistory` wrapper (restore a minimized session/project, else `switchTo`), then `navGo` calls it inside `withoutNavHistory`. A finder jump is real navigation, so it records a departure. Close the modal first (focus-safe, same as the palette's `activate`), then jump on a `setTimeout(0)`.

**Esc:** a `KEY_SCOPES` entry maps Escape → `session-finder.close`, which blurs the input, `flushSync(closeModal)`, and `focusActiveTerm()` — identical to `closeCommandPalette`. Selection is untouched.

### Files to change

- `cmd/hivegui/frontend/src/store/store.ts` — add `'session-finder'` to `ModalId` and `{ id: 'session-finder'; seq: number }` to `ModalEntry`.
- `cmd/hivegui/frontend/src/app/actions.ts` — extract `goToSession(id)`; `navGo` calls it under `withoutNavHistory`.
- `cmd/hivegui/frontend/src/app/commands.ts` — commands `session-finder` (title `Go to Session…`, runs `openSessionFinder`) and `session-finder.close`.
- `cmd/hivegui/frontend/src/app/key-scopes.ts` — `sessionFinder` modal scope (Escape → `session-finder.close`), added to `KEY_SCOPES` at the palette's precedence. The scope also `mirror`s the open chord (as Settings does, `key-scopes.ts:194-230`) so pressing the chord again while the finder is open closes it, never a silent no-op.
- `cmd/hivegui/frontend/src/lib/bindings.ts` — add `Mod+P` → `session-finder`; re-key `command-palette` to `Mod+Shift+P`, `duplicate-session` to `Mod+K`, `duplicate-session-choose-tool` to `Mod+Shift+K` (line ~114, ~126-127).
- `cmd/hivegui/menu_darwin.go:69,70,123` — default accelerators for Duplicate Session (`k`), Duplicate (choose tool) (`shift+k`), Command Palette (`shift+p`).
- `cmd/hivegui/testdata/menu-default-accelerators.json` — same three entries (a Go test and a TS test read it).
- `cmd/hivegui/frontend/test/unit/shortcuts.test.ts:121` (`'⌘P'` → `'⌘K'`), `test/dom/keymap-parity.test.ts:174,182` (hand-written legacy key mapping), and any other test asserting the old chords. `test/unit/fixtures/shortcuts-v0.json` is a frozen v0 baseline: inspect how it is consumed before touching it; do not edit it blindly.
- Any doc/site text naming ⇧⌘K for the palette or ⌘P for Duplicate (`README.md:373` and its Duplicate row, `site/`, `docs/`): grep for `⇧⌘K`, `⌘P`, `Ctrl+Shift+K`, `Ctrl+P` and fix in the same commit.
- `cmd/hivegui/frontend/src/lib/shortcuts.ts` — help row `{ command: 'session-finder', label: 'Go to session' }` beside the palette row.
- `cmd/hivegui/frontend/src/components/App.tsx` + `index.html` — mount `#session-finder` root and `<SessionFinder root=…/>`.
- `cmd/hivegui/frontend/test/e2e/every-shortcut.spec.ts` — `FIXTURES` entry for the new scope.
- `README.md` — Keybinds table row (defaults only).
- `site/features.json` — shipped entry, `since: "Unreleased"`, one-sentence blurb.
- `.changesets/session-finder.md` — `type: added`, `bump: minor`, `issue: 544`; plus `.changesets/rekey-palette-and-duplicate.md` — `type: changed`, `bump: minor`, listing the three moved defaults.

### New files

- `cmd/hivegui/frontend/src/lib/session-finder.ts` — pure: `fuzzyScore`, `finderRows(sessions, projects, nav, activeId, query)` → ordered rows `{ id, name, project, branch, ... }`. No DOM/store imports (same idiom as `lib/nav-history.ts`).
- `cmd/hivegui/frontend/src/app/modals/session-finder.ts` — `openSessionFinder` / `closeSessionFinder` (palette-style close) and a `finderRows()` reader that feeds live store state into the pure module.
- `cmd/hivegui/frontend/src/components/modals/SessionFinder.tsx` — view: input, listbox, row = status dot + agent badge (reusing the Sidebar's existing components, extracting them if private) + name, project, branch; chord hint from `useShortcutLabel('session-finder')`; footer hint `↑↓ move · enter go · esc cancel` (labels derived). Reuses `.palette-item` styles.
- `cmd/hivegui/frontend/test/unit/session-finder.test.ts`, `cmd/hivegui/frontend/test/e2e/session-finder.spec.ts` (and a dom test if the unit layer cannot cover the view).

### Tests

- `test/unit/session-finder.test.ts`: `fuzzyScore` subsequence/non-match/case-insensitivity/word-start beats mid-word/contiguous beats scattered; `finderRows` — one test per field (name, title, branch via `worktree_branch` and `worktreeBranch`, project name) matches alone (criterion 2); AND over terms; empty query → previous session first, active last, dead ids in `nav.back` skipped, no duplicates (criterion 3); 100-session `finderRows` call under 100 ms (criterion 7); a renamed session / changed branch is matched by the new value because rows are derived from live state per call (criterion 5).
- `test/unit/bindings.test.ts` / `shortcut-labels-parity.test.ts` / `shortcuts.test.ts`: pick up the new binding/help row (assert `Mod+K` resolves to `session-finder`, no overlap, not in `RESERVED_CHORDS`) — criterion 6.
- `test/e2e/session-finder.spec.ts` (Wails mock): chord → type fragment → Enter focuses the right session in another, collapsed project, asserting `document.activeElement` is the target terminal's textarea and not only that the sidebar selection moved (1); chord → Enter returns to previous session (3); Esc returns focus to the prior terminal with the same active session (4); chord pressed again while open closes it (mirror); minimized target is restored; **live update (5):** with the app running, rename a session and change another's `worktree_branch` through the mock bridge, open the finder, match on the new values and not the old; ~100 mock sessions open+filter timing with a generous CI-safe bound (7). `every-shortcut.spec.ts` sweep must pass with a `FIXTURES` entry keyed by the exact scope id `session-finder` (8).
- Extra unit cases: A→B→A (`back=[A,B]`, A active) → B first, A last; sessions with undefined title/branch/project name never throw and render no `undefined` text; the finder hint label follows a user keymap override (`labelIn`).

## Verification

Run from `cmd/hivegui/frontend` after `./scripts/ci-bootstrap.sh` in a fresh worktree; verify by exit code:

- `npm run typecheck >/dev/null && echo OK`
- `npx biome ci . >/dev/null && echo OK`
- `npx vitest run test/unit/session-finder.test.ts test/unit/bindings.test.ts test/unit/shortcuts.test.ts test/unit/shortcut-labels-parity.test.ts >/dev/null && echo OK`
- `CI=1 npx playwright test test/e2e/session-finder.spec.ts test/e2e/every-shortcut.spec.ts >/dev/null && echo OK`
- `scripts/test.sh >/dev/null && echo OK` and `scripts/ui-lint.sh >/dev/null && echo OK` from the repo root.
- Real-browser check of layout/focus ring/selected-row styling via `wails dev` + throwaway Playwright (docs/verifying-the-gui-by-hand.md), `elementFromPoint` on the first row and the input.
- Mutation check, one per field: break each of the name, title, `worktree_branch`/`worktreeBranch` (both spellings) and project-name branches of the matcher in turn; each must fail exactly its own test. Revert with the fix staged (`command cp -f`, never `git checkout --`).
- Run the whole frontend unit suite (`npm test`), not just the named files: keymap-edit/plugin-keymap may enumerate free chords. `CI=1 scripts/test.sh` for the full run.

## Second opinion

Independent reviewer, round 1: **revise**, confidence 7. Must-fix applied (4): (1) non-mac chord — superseded: the operator re-keyed all four chords at the plan stop (see Approach), which dissolves the Ctrl+K-for-finder question but moves Ctrl+K to Duplicate; (2) criterion 5 and 7 now have e2e tests that can fail; (3) mutation check is per field; (4) re-pressing the chord while open closes the finder (`mirror`). Nice-to-haves applied: A→B→A recency test, undefined-field safety, activeElement assertion, full `npm test`, keymap-override label test. No second reviewer round was run (cost); the four items are mechanical.

## Decision log

- **2026-11-11** — Chord = `Mod+K` on all platforms; ⌘P/⌘⇧P stay Duplicate, ⌘⇧K stays palette. Why: operator's explicit choice at clarifying round B (asked for ⌘P, then swapped to ⌘K to keep Duplicate). Ctrl+K off macOS is readline kill-line; accepted because Hive already binds Ctrl+P the same way and the chord is rebindable.
- **2026-11-11** — Plan stop: operator re-keyed to finder=`Mod+P`, palette=`Mod+Shift+P`, duplicate=`Mod+K`, duplicate-choose-tool=`Mod+Shift+K`, all platforms. Why: operator's explicit answer ("cmd/ctrl+p should be search, cmd/ctrl+shift+p should be palette, then cmd/ctrl+k and cmd/ctrl+shift+k is the 2 duplicate actions"). Supersedes the Mod+K-for-finder decisions below.
- **2026-11-11** — (superseded) Ctrl+K off macOS is knowingly taken from the terminal's readline kill-line (reviewer flagged it against criterion 6). Why: operator said "user can change if needed"; Ctrl+P (Duplicate) already does the same; Ctrl+Shift+K is the palette so the usual `{mac, other}` split has no free sibling.
- **2026-11-11** — Matching = subsequence + scoring; recency derived from `nav.back`. Why: operator's choices at round B.
- **2026-11-11** — Scope: sessions only, select-to-focus, exclude archived. Why: brainstorm answers; spec non-goals.

## Progress

- **2026-11-11** — Spec created from brainstorm (issue #544), triaged enhancement/M/P2. Research done.
- **2026-11-11** — Plan approved by operator after a chord re-key round. Stage → IMPLEMENT.

## Open questions

- None.
