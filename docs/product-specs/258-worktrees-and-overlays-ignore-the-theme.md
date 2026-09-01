---
issue: null
title: "Worktrees, overlays and the launcher ignore the chosen theme"
type: bug
complexity: M
priority: P2
stage: IMPLEMENT
---

# Worktrees, overlays and the launcher ignore the chosen theme

- **Issue:** —
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/258-worktrees-and-overlays-ignore-the-theme.md](../exec-plans/active/258-worktrees-and-overlays-ignore-the-theme.md)

## Problem

The UI design system (spec `ui-design-system`, PR #312) shipped six presets, a
token layer and a CI contrast gate — but left 31 hard-coded colours behind a
`/* ui-lint: allow */` escape hatch as an explicit backlog. A literal cannot
respond to `[data-theme]`, so every one of those 31 is a surface that silently
ignores the user's preset.

Sixteen of them are in `src/theme/components/worktrees.css`. Under `hive-light`
the worktree browser paints `.worktree-row { background: #141414 }` — a
near-black card — on a white dialog panel, with `#777` status text on it. The
same applies to the dead-session overlay (`#0d0d0d` card, `#fff` title), the
command palette and launcher inputs, and the help overlay.

Two smaller gaps come from the same phase. `scripts/ui-lint.sh` has hex,
px-size and glyph rules but no radius rule, so the twelve literal
`border-radius` values that made the `terminal` preset's `--radius: 0` a no-op
were found by hand, not by CI. And the per-preset screenshot loop in
`test/e2e/theme.spec.ts` covers only the sidebar and the Settings dialog — the
worktrees panel, the single most broken surface here, has no baseline at all.

## Desired behavior

Every colour in `src/theme/components/` comes from a token, so switching preset
repaints the worktree browser, the launcher, the command palette, the help
overlay and the dead-session overlay along with everything else. The worktree
kind ramp (main / active / holding / idle), the merged badge and the
destructive action keep their distinct meanings in all six presets rather than
in `hive-dark` only, and their colours are legible on each preset's own ground.

`ui-lint` fails a new literal `border-radius: <n>px` the same way it fails a new
literal hex, and the per-preset screenshot loop covers the worktrees panel and
the launcher.

## Success criteria

- `git grep -c 'ui-lint: allow' cmd/hivegui/frontend/src/theme/` reports at most
  the four commented hairline radii; no colour literal remains suppressed.
- `scripts/ui-lint.sh --strict` reports 0 violations with a **radius** rule
  active, and that rule flags `border-radius: 8px` in
  `scripts/testdata/ui-lint/bad.css` while `good.css` stays clean.
- `scripts/ui-lint.sh --contrast` passes with `--state-running`,
  `--state-attention`, `--state-error` and the new `--state-info` checked at
  >= 4.5:1 on `--surface` in all six presets.
- Every new or re-valued token has a row in `docs/design-docs/ui/tokens.md` and
  a resolved value under all six presets, asserted from the live cascade by a
  Playwright test rather than by reading the CSS.
- `test/e2e/theme.spec.ts`'s per-preset loop produces a worktrees-panel and a
  launcher baseline per preset, generated with `HIVE_SNAPSHOT=1` on darwin.
- All gates green: `npx biome ci .`, `npm run typecheck`, `npx vitest run`,
  `npx playwright test` (frontend), `scripts/ui-lint.sh --strict`,
  `scripts/ui-lint.sh --contrast`, `go build ./...`,
  `go test ./cmd/hivegui/...` (root).

## Non-goals

- No layout, markup or IA change. This is a colour-and-token pass; the only
  markup that moves is whatever a new screenshot baseline needs to render.
- No new preset, and no change to the preset picker.
- No change to the ANSI 16 or to any xterm colour.
- The four hairline `border-radius` values (1–3px on drag indicators, the
  project swatch and the worktree badge) stay literal, with an `allow` comment
  — they are sub-scale roundings, not scale steps.

## Notes

The predecessor spec's decision log
(`docs/exec-plans/completed/ui-design-system.md`) records why the literals were
deferred and why the hairline radii are deliberate. The contrast gate's rules
live in `docs/design-docs/ui/themes.md`.
