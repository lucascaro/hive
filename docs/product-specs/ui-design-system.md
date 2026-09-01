---
issue: null
title: "UI design system: tokens, themes, icons, primitives"
type: enhancement
complexity: L
priority: P2
pr: 312
stage: GATE
---

# UI design system: tokens, themes, icons, primitives

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/ui-design-system.md](../exec-plans/active/ui-design-system.md)

## Problem

The GUI is styled piece by piece: `cmd/hivegui/frontend/src/style.css` is 2159
lines with 51 hex colours, 12 font sizes, and 1 custom property, loaded via a
`<link>` rather than imported from TS. There is no token layer, so a colour or
size change means hunting literals, themes are impossible, and Unicode glyphs
used as icons render differently on every platform.

## Desired behavior

One token layer drives the whole GUI. Users can pick a theme preset. Icons are
SVG and render identically everywhere. Shared UI is built from a small primitive
component layer, and CI keeps literals out.

## Success criteria

- `src/theme/{tokens,themes}.css` + `theme.ts`; `style.css` literals replaced by tokens.
- Five presets, light and dark at launch; xterm theme derives from tokens.
- `src/ui/icons.svg` with `icon()` / `stateIcon()` / `iconButton()` / `kbd()`; no Unicode glyphs left.
- Primitive layer in `src/ui/` (`sessionRow`, `projectCard`, `chip`, `banner`, `dialog`, form fields).
- Settings › Appearance offers a preset picker and custom tokens.
- `scripts/ui-lint.sh` runs in CI at error level, including a contrast check.
- Each phase leaves the app shippable and ships as its own PR.

## Non-goals

- Redesigning GUI layout or information architecture; this is a styling and
  component-layer change.

## Notes

Six phases, each with its own detailed plan under `docs/exec-plans/active/ui-design-system-phase<N>.md`.
One PR per phase: phase 1 in #292, 2 in #295, 3 in #299, 4 in #301 and 5 in #310, all
merged; phase 6 in #312, which is the PR this spec is gated against.
Design docs live in `docs/design-docs/ui/`. Spec written retroactively
(2026-08-30) to give the phase plans a spec to hang off.
