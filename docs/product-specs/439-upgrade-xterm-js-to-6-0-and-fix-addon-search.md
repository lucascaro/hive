---
issue: 439
pr: 441
title: "Upgrade xterm.js to 6.0 and fix the addon-search version mismatch"
type: enhancement
complexity: M
priority: P2
stage: GATE
---

# Upgrade xterm.js to 6.0 and fix the addon-search version mismatch

- **Issue:** #439
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/439-upgrade-xterm-js-to-6-0-and-fix-addon-search.md](../exec-plans/active/439-upgrade-xterm-js-to-6-0-and-fix-addon-search.md)

## Problem

The GUI frontend pinned `@xterm/xterm` 5.5.0 (April 2024) but `@xterm/addon-search`
0.16.0, which shipped alongside xterm 6.0.0 in December 2025. That addon dropped
its `peerDependencies` declaration entirely (0.15.0 still declared
`@xterm/xterm: ^5.0.0`), so npm never flagged the mismatch — a v6-era addon ran
against a v5 core, unchecked. It arrived with #432 and worked by accident, not by
contract.

xterm 6.0.0 adds synchronized output (DEC 2026), which matters for the
full-screen agent TUIs hive hosts, plus OSC 52 clipboard, a progress addon
(OSC 9;4), an ESM build, and a macOS CapsLock double-input fix. None of the v6
removals bite us — we use no canvas renderer, no `overviewRulerWidth`, no
`windowsMode`, no `fastScrollModifier`.

The release notes also claim a 30% bundle reduction (379kb to 265kb). That does
not reproduce for our configuration: measured against `Terminal` plus our four
addons, v6 is **+18% raw and +24% gzipped**. See the exec plan's Research
section for the numbers. This spec does not claim a size benefit.

The risk is concentrated in one place: v6 reworked the viewport and scroll bar
substantially for the VS Code integration. Hive's `_followBottom` latching,
user-vs-parse `onScroll` attribution, and the cap-trim `baseY`/`viewportY` drift
handling in `session-term.ts`, plus `find-box.ts`'s viewport claim and `view.ts`'s
`scrollToBottom` sites, were all tuned against v5 behavior and have already been
paid for twice. Unit and DOM tests are jsdom and cannot see any of it; only
`e2e-real` can.

## Desired behavior

The frontend runs xterm 6.x with all four addons on their matching 6.x-era
versions, no silent cross-major pairing. Scroll, follow-bottom, find, and
alt-screen behavior are indistinguishable from today's, proven by tests that
exercise the real renderer rather than jsdom. Users see less redraw flicker in
agent TUIs; the measured bundle growth (+18% raw, +24% gzipped) is accepted, not
a benefit claimed.

## Success criteria

- `@xterm/xterm` at 6.x with `addon-fit` 0.11.0, `addon-search` 0.16.0,
  `addon-web-links` 0.12.0, `addon-webgl` 0.19.0 — one coherent generation.
- An `e2e-real` test floods a session past the 5000-line scrollback cap, resizes,
  and asserts the viewport does not jump — the cap-trim regression class, which a
  sub-cap buffer false-passes.
- An `e2e-real` test covers follow-bottom: scrolled to bottom stays pinned under
  output; scrolled up stays put under output.
- Find box behavior holds across a normal/alt buffer transition (the #430/#431
  poisoning defect does not return under the new addon pairing).
- `scripts/test.sh` green on all layers, plus `npm run test:e2e:real`.
- A changeset records the upgrade.

## Non-goals

- Removing the `_followBottom` / cap-trim machinery in `session-term.ts` and
  `scrollback.ts`. It survives v6 unchanged and all scroll tests pass with it in
  place; whether it is still *needed* is a separate question behind its own spec.
- Replacing xterm.js. The in-house VT emulator spec covers the **Go** snapshot
  path (`hinshun/vt10x`); it is unrelated to the frontend renderer.
- Adopting synchronized output (DEC 2026), OSC 52 clipboard, or the progress
  addon (OSC 9;4) as features. This spec only makes them reachable.
- Moving to the 6.1 beta line.
- Diagnosing or fixing #440 (`glyph-utf8.spec.ts` fails on 5.5.0 too).
- Adding a CI bundle-size budget, despite the measured growth.
- Any daemon-side change — frontend-only, so no `DaemonContract` bump.

## Notes

- xterm 6.0.0 released 2025-12-22. 6.1 is still beta (304 betas as of
  2026-09-19), so 6.0.0 is the stable target.
- v6 breaking changes: canvas renderer addon removed; `overviewRulerWidth` moved
  under `overviewRuler`; `windowsMode` and `fastScrollModifier` removed; the
  alt→ctrl+arrow hack removed (embedder must bind it); viewport/scroll bar
  reworked.
- Relevant prior lessons in the hive brain:
  `paired-replay-intent-flags-cleared-together`, `xterm-focus-vs-renderer-race`.
- Affected code: `cmd/hivegui/frontend/src/app/session-term.ts` (~lines 312-336,
  749-820), `src/app/find-box.ts`, `src/app/view.ts`,
  `cmd/hivegui/frontend/package.json`.
