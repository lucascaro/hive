---
issue: null
title: "Add 12 IDE-inspired theme presets"
type: enhancement
complexity: M
priority: P2
stage: IMPLEMENT
---

# Add 12 IDE-inspired theme presets

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/305-add-ide-inspired-theme-presets.md](../exec-plans/active/305-add-ide-inspired-theme-presets.md)

## Problem

Hive ships six presets, all of them designed in-house (`hive-*`, `native-*`, `terminal`,
`classic`). People arriving from VS Code, Neovim or JetBrains have a palette they already
read fluently — Dracula, Nord, Gruvbox, Tokyo Night, Catppuccin, One Dark, Solarized,
GitHub — and
none of them are here. Spec 258 retired the last colour literals, so a preset is now purely
a block of token values with no component CSS knowing a theme name; the cost of adding one
has dropped to a `themes.css` block plus a line in `PRESETS`, which is what makes this worth
doing now rather than earlier.

## Desired behavior

Settings › Appearance › Theme offers 19 presets grouped into "Hive", "Native" and
"Community". Picking any of them repaints the whole app *and* the live terminals — chrome,
worktree browser, overlays and the ANSI 16 — with no restart, exactly as the existing
presets do.

The presets a user can land on without choosing one — `system` and the five it sits beside —
stay fully WCAG AA. The community ports do not: they keep their upstream values, which is
what makes them worth having, and several put text below AA. That is the trade this spec
makes, and it is stated in the docs rather than buried.

## Success criteria

- Twelve new presets are selectable and each paints its own distinct tokens: `dracula`,
  `nord`, `gruvbox-dark`, `tokyo-night`, `catppuccin-mocha`, `one-dark`, `neon`,
  `solarized-dark`, `solarized-light`, `catppuccin-latte`, `github-dark`, `github-light`.
- Presets that ship in both moods sit adjacent in the picker, not sorted dark-then-light.
- `scripts/ui-lint.sh --contrast` reports 0 failures. The twelve new presets opt out via
  `--contrast-exempt: 1` and are reported as skipped with a count, never silently; no pair
  is removed from or relaxed in `scripts/ui-contrast.mjs`, and the six default presets
  remain strictly gated.
- A default preset (`hive-*`, `native-*`, `terminal`, `classic`) that declares
  `--contrast-exempt` is a gate **failure**, not an exemption.
- Every new preset declares all sixteen `--ansi-*`, and those values reach xterm's cached
  palette (not just the CSS cascade) — the existing `test/e2e/theme.spec.ts` check covers it.
- The three light presets (`solarized-light`, `catppuccin-latte`, `github-light`) declare
  all sixteen `--ansi-*` at their upstream values. They are **not** held to ≥4.5:1 on
  their own `--term-bg` — that rule stays first-party-only.
- The theme picker renders the presets in `<optgroup>` buckets; other `selectInput` callers
  are unaffected.
- `index.html`'s pre-paint `KNOWN` list covers every new id, so a stored preset survives a
  cold start with no flash of the wrong theme.
- `docs/design-docs/ui/themes.md` lists every new preset, states the accessibility trade-off
  in plain terms, and gives MIT attribution for each source palette.

## Non-goals

- Correcting the palettes to pass the WCAG gate. Reversed after the measurements below were
  reviewed: the ports ship at their upstream values and opt out of the gate instead. The
  gate's own rules and thresholds are unchanged, and the default presets stay under them.
- Shipping the Monokai palette under the name "Monokai" (active trademark of Monokai Pro).
  It ships as `neon`.
- Per-preset type, spacing or motion scales. Those are one scale for the whole app; presets
  re-value colour, font-family, radius and shadow only.
- Regenerating the `HIVE_SNAPSHOT` screenshot baselines. They are darwin-local and CI skips
  them by default.

## Notes

Measured before planning — every canonical palette fails the existing gate as shipped
upstream. Worst pairs: Nord `--state-error` on `--sel` 1.80:1, Catppuccin Mocha
`--fg-subtle` 1.87:1, Tokyo Night `--fg-subtle` 1.99:1, One Dark `--fg-subtle` 2.16:1,
Gruvbox `--fg-subtle` 2.40:1, Dracula `--fg-subtle` 2.51:1. Solarized Light fails 13 pairs.

Two ways out were considered. Correcting each palette until it passes is what `native-light`
does to VS Code Light+ (seven of Light+'s sixteen fail on white), and it was the original
plan. It was reversed on the owner's call: a corrected Dracula is not Dracula, these presets
are opt-in, and fidelity to the palette the user came for is the point of shipping it.

The cost is real and is not hidden: some text in these presets is below WCAG AA. The
containment is that the exemption is per-preset, declared in the CSS, printed on every gate
run, and refused outright on the six presets a user can reach without opting in.

`docs/design-docs/ui/themes.md` § "Adding a preset" is the checklist this work follows.
