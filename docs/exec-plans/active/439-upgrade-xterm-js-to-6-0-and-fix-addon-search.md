# Upgrade xterm.js to 6.0 and fix the addon-search version mismatch

- **Spec:** [docs/product-specs/439-upgrade-xterm-js-to-6-0-and-fix-addon-search.md](../../product-specs/439-upgrade-xterm-js-to-6-0-and-fix-addon-search.md)
- **Issue:** #439
- **PR:** #441
- **Branch:** `feature/439-upgrade-xterm-6`
- **Status:** active

## Summary

Move the GUI frontend from `@xterm/xterm` 5.5.0 to 6.0.0, bringing all four
addons onto their matching 6.0-era versions and closing the current cross-major
pairing (a v6-era `addon-search` 0.16.0 running against a v5 core). The type
surface is compatible; the work is re-validating the scroll/follow-bottom state
machine against v6's reworked viewport, re-hiding a scrollbar v6 now draws
itself, and confirming one private-internals access survives.

## Research

### Measured facts (not taken from release notes)

**The bundle gets bigger, not smaller.** The release notes for 6.0.0 claim
379kb → 265kb (a 30% reduction). That does not reproduce. Bundling `Terminal`
plus all four addons with esbuild (`--bundle --minify --format=esm`):

| set | raw | gzip |
|---|---|---|
| 5.5.0 + addons fit 0.10.0 / search 0.16.0 / web-links 0.11.0 / webgl 0.18.0 | 433,341 | 109,574 |
| 6.0.0 + addons fit 0.11.0 / search 0.16.0 / web-links 0.12.0 / webgl 0.19.0 | 511,772 | 135,478 |

That is +18% raw, +24% gzipped. The published artifacts agree: `lib/xterm.js`
goes 289,441 → 488,663 bytes. v6 additionally ships an ESM build
(`lib/xterm.mjs`, 344,970 raw / 88,009 gzip) via a `module` field that 5.5.0
lacks, so Vite will resolve the `.mjs`; even that is larger than v5's UMD. The
size argument for this upgrade does not hold and the spec's Problem section
needs correcting.

**The type surface is clean.** Diffing `typings/xterm.d.ts` between the two
versions, v6 removes exactly three members: `fastScrollModifier`,
`overviewRulerWidth`, `windowsMode`. None are used anywhere in the tree. Added:
`overviewRuler`, `overviewRulerBorder`, `showTopBorder`, `showBottomBorder`,
`width`, `reflowCursorLine`, `scrollOnEraseInDisplay`, `synchronizedOutputMode`,
and three `scrollbarSlider*` theme keys. Every API this app calls —
`scrollToBottom`, `scrollLines`, `scrollToLine`, `refresh(start, end)`,
`buffer.active.{baseY,viewportY,cursorX,cursorY,type,getLine}`, `onScroll`,
`onWriteParsed`, `modes.mouseTrackingMode`, `registerLinkProvider` — is
unchanged in signature. `tsc --noEmit` is therefore expected to pass, which
means **the typecheck will not catch this upgrade's real risks**. Note also
`tsconfig.json` sets `skipLibCheck: true`, lowering type-level detection further.

**v6 replaces the scrollbar implementation.** The CSS class `.xterm-scroll-area`
(v5) becomes `.xterm-scrollable-element` (v6), and v6 ships VS Code's custom
scrollable element: real `.scrollbar` / `.scra` / `.shadow` child divs, arrows,
and inset shadows reading `--vscode-scrollbar-shadow`.
`src/theme/components/xterm-overrides.css:1-10` hides only the **native** webkit
scrollbar (`.xterm-viewport::-webkit-scrollbar { display: none }` plus
`scrollbar-width: none`). Under v6 that rule becomes a no-op and a visible
VS Code-style scrollbar appears in every terminal tile. This is mandatory work,
not optional polish. Nothing in the tree references `.xterm-scroll-area`, so the
rename itself costs nothing.

### Relevant code

**The follow-bottom state machine** — the bulk of the risk, all in
`cmd/hivegui/frontend/`:

- `src/app/session-term.ts:152` — `STICKY_BOTTOM_LINES = 2`, a tolerance band on
  `baseY - viewportY` as "distance from bottom".
- `src/app/session-term.ts:156` — `USER_SCROLL_GRACE_MS = 250`, assumes `onScroll`
  fires promptly enough after a gesture to separate user scrolls from xterm's own
  viewport moves.
- `src/app/session-term.ts:235,254` — `_followBottom` / `_lastViewportY`. The
  design deliberately does **not** trust `baseY - viewportY` as ground truth,
  because v5's cap-trim decouples the two under load.
- `src/app/session-term.ts:745-861` — the `onScroll` handler. Its comments
  (`748-754`, `839-841`) name the exact v5 behavior it works around: at the
  scrollback cap under heavy output, xterm pins `baseY` at the cap while
  `viewportY` drifts, dropping the viewport off-bottom with no user gesture.
  `855-860` calls `scrollToBottom()` re-entrantly from inside the `onScroll`
  callback, guarded by `_repinning` — assuming v5's synchronous re-entrant
  dispatch rather than a rAF/microtask deferral.
- `src/app/session-term.ts:1101-1259` — `_onBodyResize`. `1119-1125` again rejects
  `baseY - viewportY` for the cap-trim reason. `1147-1257` is the column-drift
  replay path, resting on "xterm.js does not reflow history on resize"
  (`1151`) — if v6 changed that, this machinery is dead weight or, worse,
  half-right.
- `src/lib/scrollback.ts:238-390` — replay begin/done. `247-258` captures
  distance-from-bottom before `term.reset()`, sequenced by a parse-ordered
  `term.write('', cb)`, assuming v5's queued, callback-ordered `write` relative to
  `reset`. `294` increments `_replaysInFlight` specifically because cap-trim
  "strands a follower in history" mid-restream.
- `src/lib/view-scroll.ts:60-89` — `snapVisibleTermsToBottom` calls
  `scrollToBottom()` twice per term (once sync, once in a parse-ordered write
  callback) for the same cap-trim reason.
- `src/app/view.ts:434-463`, `src/app/find-box.ts:159-183` — the find box's
  "viewport claim" (`beginSearch`/`endSearch`, `session-term.ts:1381-1407`) gates
  the four bottom-follow yank sites. Both defer ~250ms around
  `scrollToBottom()` because it refreshes the renderer and can fire `focusout` on
  the helper textarea.

**Private-internals access** — the highest-uncertainty item:

- `src/app/session-term.ts:172,415` reads
  `(term as Terminal & LinkifierPeek)._core?.linkifier?.currentLink?.link`,
  because the public API exposes no "is there a link here" query. This has no
  version contract at all. The strings `linkifier` and `currentLink` are both
  still present in v6's minified bundle (10 and 39 occurrences vs 8 and 39 in
  v5), so it is probably intact — but string presence in a minified bundle does
  not prove the path shape. **Unverified; see Open questions.**

**Other DOM coupling:**

- `src/app/session-term.ts:410` — `querySelector('.xterm-screen')` for
  capture-phase link/click hit-testing.
- `src/app/session-term.ts:463-501` — click-to-position derives cell geometry from
  `rect.width / term.cols`, assuming no extra chrome inside `.xterm-screen`'s box.
  v6's new scrollbar element makes this worth re-checking.
- `src/theme/layout.css:190-194` — targets `.xterm` and `.xterm-screen` for
  fill-height in grid and single view. Both classes survive in v6.
- `src/app/session-term.ts:700-743` — the wheel handler takes over scrolling
  entirely (`stopPropagation` + own `scrollLines()` math) to cap macOS trackpad
  momentum.

**Addons and proposed API:**

- `allowProposedApi: true` (`session-term.ts:322-326`) exists solely for
  `addon-search`'s match decorations; without it `findNext` throws and find
  reports 0/0.
- `_recreateSearchAddon` (`session-term.ts:1468-1487`) rebuilds the addon on every
  buffer transition, working around an addon defect where searching the alt
  buffer permanently poisons the normal buffer. Worth re-testing whether v6's
  pairing still needs this.
- WebGL (`session-term.ts:905-946`) is budgeted to 8 contexts
  (`src/lib/webgl-budget.ts`), with context-loss handling and a loss-storm guard
  that permanently falls back to the DOM renderer.

### Test coverage — where a regression would and would not be caught

The good news: `test/e2e` and `test/e2e-real` both run **real, unmocked xterm in
real Chromium**. `VITE_WAILS_MOCK` substitutes only the Wails bridge modules.

Would catch a regression:
- `test/e2e/grid-scroll-regressions.spec.ts:136` (R-follow: resize must not replay
  a bottom-follower), `:169` (R-reader: a reader in history must be replayed),
  `:229` (R-drag).
- `test/e2e/scrollback-invariants.spec.ts:51-186` — I1–I5, no spurious replay.
- `test/e2e/find-in-session.spec.ts:58-370+` — real `addon-search` counts,
  alt-screen transcript search via real DECSET 1049, live updates, scroll
  preserved while paging matches.
- `test/e2e-real/scroll-codex.spec.ts:257-470+` — marker survival and reader
  stranding/yanking against a live PTY flood.
- `test/e2e-real/wheel-scroll.spec.ts:264-330+` — real wheel in pixel/line/legacy
  delta modes; alt-screen must not swallow the wheel.
- `test/e2e-real/glyph-utf8.spec.ts:32` — would catch a Unicode/wcwidth regression.
- `test/e2e/xterm-reflow.spec.ts:30-90` and `test/dom/xterm-reflow.test.ts` —
  construct a real `Terminal` and probe resize/reflow.

Would **not** catch a regression:
- All of `test/unit/**` runs in `environment: node` with hand-rolled mocks
  (`MockXterm` in `test/unit/scrollback.test.ts:12-27`, `MockTerm` in
  `test/unit/view-scroll.test.ts:5-27`). These encode the app's *belief* about
  xterm's contract and will keep passing if real xterm diverges. This is the
  single most misleading signal in the tree for this upgrade.
- `test/dom/xterm-theme.test.ts:108-125` stubs the term object rather than
  constructing one.
- `@xterm/addon-webgl` has no correctness test anywhere; `webgl-budget.test.ts`
  only tests our own slot accounting.
- `@xterm/addon-web-links` has no test at all, in any layer.
- Windows CI runs `e2e_mock` only — never `e2e_real`.
- `test/e2e/xterm-reflow.spec.ts` *documents* rather than asserts the live-cursor
  rewrap limitation, so a v6 change there would read as "still documented".

### Constraints / dependencies

- `.github/dependabot.yml` configures **only** `github-actions`, no npm ecosystem
  — this bump will never arrive automatically and must be done by hand.
- `scripts/check-changeset.sh` does not exempt `cmd/hivegui/frontend/package.json`,
  so this needs a `.changesets/` entry or the `no-changeset` label.
- Frontend-only: no `internal/{wire,daemon,session,registry}` or `cmd/hived`
  changes, so `scripts/check-daemon-contract.sh` will not demand a
  `buildinfo.DaemonContract` bump.
- CI runs `npm run typecheck` only on the Linux biome leg, and only after
  `scripts/ci-bootstrap.sh` generates the wailsjs bindings.
- No bundle-size budget exists in CI — nothing will flag the +24% gzip.

### Prior lessons (hive brain)

- `xterm-focus-vs-renderer-race` — synchronous DOM-mutating helpers
  (`scrollToBottom` / `refresh` / `fit`) called after a rAF-scheduled focus
  restoration race the focus-retry loop; Linux fails when they land before the
  focus rAF, macOS when they land between retries. Defer past the full retry
  budget (8 frames × 16ms ≈ 130ms; `setTimeout` 250ms is the safe default) rather
  than chasing rAF nesting. Cross-platform e2e flake on focus-adjacent xterm code
  is almost always this race, not real platform divergence. The existing 250ms
  defers in `view.ts:451` and `session-term.ts:1402-1405` are that lesson already
  applied — do not shorten them during this upgrade.
- `paired-replay-intent-flags-cleared-together` — paired scroll/replay intent
  flags (wants-bottom + captured-restore-distance) must be cleared together at
  every site that cancels the intent. A flag latched at event time but acted on at
  parse time (xterm's async write queue) leaves a window where a deliberate
  mode-snap flips the intent after latching, and the parse-time callback then
  restores a stale position over the user's action. Only shows up on slow CI
  runners.

### Conventions card

From `AGENTS.md` and `package.json` (the hivesmith block's Build/Lint/Test
entries are still unfilled `<command>` placeholders — these are the real ones):

- **Build:** `./build.sh` (macOS .app). Frontend alone: `npm run build` in
  `cmd/hivegui/frontend`. Build hivegui with plain `wails build`, never `-s`.
- **Lint:** `npm run ci` (`biome ci .`) — `biome lint .` does not check
  formatting. Plus `scripts/ui-lint.sh` for token/icon rules.
- **Typecheck:** `npm run typecheck` (`tsc --noEmit`), after
  `./scripts/ci-bootstrap.sh` in a fresh worktree or ~20 unrelated errors appear.
- **Tests:** `scripts/test.sh [go|unit|dom|e2e]`; `npm run test:e2e:real`
  separately. Run local Playwright with `CI=1` or it reuses a stale vite dev
  server.
- **TDD is mandatory** — no behaviour change ships without the test that would
  have caught the regression. "Boil the lake": fix and cover in the same PR.
- **Changeset, never `CHANGELOG.md`** — add `.changesets/<slug>.md` with `type`
  and `bump`; `block-generated-edits` fails any PR touching `CHANGELOG.md` or
  `docs/product-specs/index.md` directly.
- **UI rules** live in `docs/design-docs/ui/`; `scripts/ui-lint.sh` enforces them.

## Approach

Bump all five packages to one coherent 6.0 generation, then handle the two things
the spike proved are real: v6 draws its own scrollbar, and one e2e test measures
scroll through a DOM property v6 no longer populates.

Everything else stays untouched. The `_followBottom` latch, the cap-trim
workarounds in `session-term.ts` and `scrollback.ts`, the `_core.linkifier`
access, and the `_recreateSearchAddon` dance all survive v6 unchanged — the full
`test/e2e` and `test/e2e-real` suites passed against 6.0.0 with none of it
modified (see Progress). No speculative refactoring on the theory that v6 fixed
the underlying drift: that theory is untested, and removing the machinery is
separate work behind its own spec.

For the scrollbar, the alternative was to copy the existing skin's colours into
`xtermTheme()`. Rejected: `src/theme/base.css:67-82` already hard-codes the app's
scrollbar as literal `rgba()`, so copying them would leave two independent
definitions of one visual fact, drifting the moment either is touched. Instead
promote those literals into tokens and consume them from both the global skin and
`xtermTheme()`, so the app scrollbar and the terminal scrollbar are identical by
construction. Reuses the existing "omit an absent token rather than send `\'\'`"
rule that the ANSI loop in `xtermTheme()` (`src/theme/theme.ts`) already follows.

### Files to change

- `cmd/hivegui/frontend/package.json` + `package-lock.json` — `@xterm/xterm`
  5.5.0 -> 6.0.0, `@xterm/addon-fit` 0.10.0 -> 0.11.0, `@xterm/addon-web-links`
  0.11.0 -> 0.12.0, `@xterm/addon-webgl` 0.18.0 -> 0.19.0. `@xterm/addon-search`
  stays at 0.16.0 — it is already the 6.0-era build, and this bump is what makes
  that correct instead of accidental.
- `cmd/hivegui/frontend/src/theme/tokens.css` — add `--scrollbar-thumb`,
  `--scrollbar-thumb-hover`, `--scrollbar-thumb-active`, seeded with today's
  values (`rgba(255,255,255,.08)`, `.18`, and a new `.26` for the active state
  xterm exposes but the global skin never had). Defined per theme preset so the
  light presets get a dark thumb rather than inheriting white-on-white.
- `cmd/hivegui/frontend/src/theme/base.css:67-82` — replace the three literal
  `rgba()` values with the new tokens. No visual change; this is the
  de-duplication that makes the two scrollbars share one source.
- `cmd/hivegui/frontend/src/theme/components/xterm-overrides.css` — delete the
  two scrollbar-hiding rules (`::-webkit-scrollbar` and `scrollbar-width`), which
  are dead under v6 regardless. Keep the file and add
  `.xterm-scrollable-element > .shadow { display: none; }`: v6 draws an inset
  shadow driven by `--vscode-scrollbar-shadow`, a variable hive never defines.
- `cmd/hivegui/frontend/src/theme/theme.ts` — `xtermTheme()` additionally returns
  `scrollbarSliderBackground`, `scrollbarSliderHoverBackground` and
  `scrollbarSliderActiveBackground` read from the new tokens, each omitted when
  the token resolves empty.
- `cmd/hivegui/frontend/test/e2e/find-in-session.spec.ts:168-174` — `fromBottom()`
  must read `buffer.active.baseY - viewportY` instead of
  `.xterm-viewport` `scrollHeight/clientHeight/scrollTop`. Under v6 the viewport
  element no longer carries the full scroll height (measured: 1258 -> 408), so the
  DOM form returns 0 and the assertion fails even though the terminal scrolled
  correctly. This is a latent defect in the test independent of the upgrade: it
  asserts on an xterm implementation detail rather than on terminal state.
- `.changesets/xterm-6-upgrade.md` — `type: changed`, `bump: minor`. Body covers
  the upgrade and the now-visible terminal scrollbar.
- `docs/product-specs/439-upgrade-xterm-js-to-6-0-and-fix-addon-search.md` —
  update the Problem section's description of the mismatch once the pairing is
  coherent.

### New files

None.

### Tests

- `cmd/hivegui/frontend/test/dom/xterm-theme.test.ts`, extending the existing
  `describe('xtermTheme')`:
  - `returns scrollbar slider colours from tokens` — sets the three custom
    properties on `document.documentElement` and asserts each appears on the
    returned theme object.
  - `omits scrollbar slider keys when the tokens are absent` — removes the
    properties and asserts the keys are absent rather than present-and-empty,
    mirroring the existing ANSI `'black' in t === false` assertion.
- `cmd/hivegui/frontend/test/e2e/theme.spec.ts`:
  - `the terminal scrollbar slider matches the app scrollbar` — asserts the
    computed background of `.xterm-scrollable-element > .scrollbar` equals the
    `--scrollbar-thumb` token value, run against one dark and one light preset.
    This is the only genuinely new behaviour here and the only one a silent
    regression could ruin.
- `cmd/hivegui/frontend/test/e2e/find-in-session.spec.ts`:
  - No new test. The existing `a normal-buffer session finds and steps through
    matches` passes once `fromBottom()` measures terminal state; that fix *is*
    the coverage, and it was verified green against 6.0.0 during the spike.
- No `test/unit` additions. Everything in this change is declarative CSS or
  browser-measured; a mock-based unit test here would encode a belief about xterm
  rather than check a fact — the exact failure mode the Research section
  documents for that layer.

## Verification

```
cd cmd/hivegui/frontend
npm ci
npm run typecheck                                    # verified green on 6.0.0
npm run ci                                           # biome ci — format + lint
CI=1 npx vitest run --config vitest.config.js test/dom
CI=1 npx playwright test
CI=1 npm run test:e2e:real
cd ../../..
scripts/ui-lint.sh
scripts/check-changeset.sh main HEAD
```

`test:e2e:real` is expected at **25 of 26**: `test/e2e-real/glyph-utf8.spec.ts:32`
fails on 5.5.0 as well and is tracked separately as #440. Every other spec,
including all four scroll specs, passed against 6.0.0 during the spike.

A fresh worktree needs `./scripts/ci-bootstrap.sh` before `npm run typecheck`, or
tsc reports ~20 errors in files the change never touched.

One manual row: launch the app and confirm the terminal scrollbar appears, matches
the sidebar scrollbar, and fades when idle. `docs/verifying-the-gui-by-hand.md`
covers driving this through `wails dev` plus a throwaway Playwright script — it
does not need a human.

## Decision log

- **2026-09-20** — Researched without installing v6 into the tree. Why: the
  decisive runtime probes (does `_core.linkifier` survive, does cap-trim still
  drift) need a real browser, which is IMPLEMENT-stage work; package-level
  measurement and typings diffing answered the cheap questions first.
- **2026-09-20** — Ran a full upgrade spike before planning rather than planning
  against the research's open questions. Why: two of the three unknowns were
  empirical, not matters of judgement, and a plan built on guesses about them
  would have mis-sized the work. Result: typecheck green, `test/dom` 900/900,
  `test/e2e` 416 pass after one test fix, `test/e2e-real` 25/26 with the single
  failure reproducing on 5.5.0.
- **2026-09-20** — Keep the `_followBottom` / cap-trim machinery untouched.
  Rejected: deleting it on the theory v6 fixed the underlying drift. The suites
  passing *with* the machinery in place does not demonstrate it is now
  unnecessary, and the synthetic probe reproduced no drift on either version, so
  there is no evidence either way. Removal needs its own spec.
- **2026-09-20** — Adopt v6's scrollbar and theme it, rather than hiding it.
  Operator decision. Rejected: extending the hide rules to
  `.xterm-scrollable-element > .scrollbar`, which preserves today's look at zero
  risk but keeps the terminal the one surface with no scroll-position indicator.
- **2026-09-20** — Promote the global skin's literal `rgba()` values into tokens
  instead of duplicating them into `xtermTheme()`. Why: one visual fact, one
  definition; the alternative drifts silently.
- **2026-09-20** — The pre-existing `glyph-utf8` failure goes to its own issue
  (#440) rather than being diagnosed here or absorbed into this scope.
- **2026-09-20** — Reused `test/e2e-real/term-harness.ts`'s cast shape for the
  new `fromBottom()` buffer access in `find-in-session.spec.ts` rather than
  writing a second typing for the same boundary. The store's term tile is
  untyped there, so an inline `as` is where the type has to come from; having
  two spellings of it would invite them to diverge.
- **2026-09-20** — Review iteration 1 raised three IMPORTANT findings; verified
  each in a real browser before acting, and one was misdiagnosed. The inert
  `.shadow` override is indeed dead code, but not because it loses a specificity
  war: xterm 6 constructs its scrollable element with `useShadows:false`, so the
  node is never created at all (confirmed against a filled, scrolled terminal and
  in the shipped bundle). Deleted the rule rather than adding the suggested
  `--vscode-scrollbar-shadow: transparent`, which would also have been dead.
- **2026-09-20** — Restored the `.xterm-viewport::-webkit-scrollbar` suppression
  that the first pass deleted. v6 does keep `overflow-y: scroll` on that element,
  so base.css's global 8px skin would reserve a gutter under macOS "Always show
  scrollbars" — invisible under the overlay scrollbars headless Chromium uses,
  which is why the first round missed it. Cheap and cannot regress anything, so
  restored rather than proven.
- **2026-09-20** — Re-valued the three scrollbar tokens in all 19 non-empty preset
  blocks rather than only the five light ones. `themes.css`'s header rule is
  strictly honoured today — six sampled tokens each appear in all 19 — so a
  partial addition broke a live invariant even though coverage was complete for
  current presets. Did **not** add a token-completeness test: no such guard
  exists for any other token, and inventing one for these three alone is scope
  that belongs to a separate change.
- **2026-09-20** — Regenerated the wailsjs bindings after branching. Why: the
  bindings had been generated against the older worktree HEAD, so `tsc` failed
  on `UpdateBuildLog` / `hasBuildLog` from #433 — errors in files this change
  never touched. `./scripts/ci-bootstrap.sh` is the fix, not a code change.

## Progress

- **2026-09-20** — Research complete. Bundle-size premise measured and found
  false; type surface confirmed clean; scrollbar regression identified; test
  coverage mapped.
- **2026-09-20** — Upgrade spike run against 6.0.0 in this worktree, then
  reverted. Results, all reproducible: `tsc --noEmit` passes. `test/dom` 900/900.
  `test/e2e` 416 pass / 1 fail, the failure being
  `find-in-session.spec.ts:151` measuring scroll via `.xterm-viewport` DOM
  geometry that v6 no longer populates — re-measured via `buffer.active`, all 19
  pass. `test/e2e-real` 25 pass / 1 fail (`glyph-utf8.spec.ts:32`), which
  reproduces identically on 5.5.0 and is therefore pre-existing (#440). All four
  scroll specs (`scroll-codex`, `scroll-restream-strand`, `grid-focus-scroll`,
  `wheel-scroll`) passed on v6 with the follow-bottom machinery unmodified.
- **2026-09-20** — Open question 1 resolved: `_core.linkifier.currentLink`
  survives. v6 renames the backing field to `_linkifier` but keeps a `linkifier`
  getter, so `session-term.ts:415`'s access path still resolves. Probed in real
  Chromium.
- **2026-09-20** — Open question 3 resolved by operator decision: proceed with the
  full upgrade despite the measured size increase.
- **2026-09-20** — Plan approved. Stage advanced to IMPLEMENT.
- **2026-09-20** — Implemented on `feature/439-upgrade-xterm-6`, branched from
  `origin/main` at `2cc319d4` (the worktree was 2 commits stale). All checks
  green: `tsc --noEmit`, `biome ci .`, `vitest run` (1501 tests, 112 files),
  `playwright test` (389 passed / 31 skipped), `ui-lint.sh --strict`.
  `test:e2e:real` 25/26, the one failure being the pre-existing #440.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.

## Open questions

None blocking. Two carried forward as notes rather than blockers:

1. **Does v6 still drift `viewportY` while `baseY` is pinned at the cap?**
   Unresolved, and deliberately so. A synthetic probe reproduced no drift on
   either 5.5.0 or 6.0.0, so it proves nothing; the real e2e-real scroll specs
   pass on v6 with the workarounds in place. Whether the workarounds are now
   *unnecessary* is a separate question behind its own spec, and nothing in this
   plan depends on the answer.
2. **Light-preset scrollbar contrast.** The global skin hard-codes a white thumb,
   so if `hive-light` currently renders a near-invisible scrollbar, this change
   makes that visible in the terminal too. The new `theme.spec.ts` test asserts
   against both a dark and a light preset specifically to surface it.

## Risks

- `scripts/ui-lint.sh` may reject raw `rgba()` in `tokens.css`. If it does, the
  three tokens move into the per-preset colour files where the other colour tokens
  already live — a relocation, not a redesign.
- `test/e2e-real` is not fully green before *or* after this change (#440), which
  weakens it as a merge gate for this PR. Compare against a 5.5.0 baseline run
  rather than expecting 26/26.
- The bundle grows ~26kb gzipped with no CI budget to catch further growth. Out of
  scope here, but worth a separate spec if size ever starts to matter.
