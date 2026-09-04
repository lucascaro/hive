---
issue: null
pr: 335
title: "Reorganize Settings into tabbed sections"
type: enhancement
complexity: S
priority: P2
stage: GATE
---

# Reorganize Settings into tabbed sections

- **Issue:** —
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/335-reorganize-settings-into-tabbed-sections.md](../exec-plans/active/335-reorganize-settings-into-tabbed-sections.md)

## Problem

The Settings modal is one long scroll holding several unrelated concerns:
**Custom agents** (a list that grows without bound), **Appearance** (theme
picker plus a four-row token textarea), **Menu bar** (macOS-only, whether
launchd starts the menu bar at login) and **Updates** (channel, source repo,
action button).
The flat layout only works because of a structural workaround —
`#settings-scroll` scrolls while `#settings-updates` is pinned below it at its
natural height — added precisely so a dozen custom agents cannot push the
channel picker off screen. Anyone with more than a couple of agents scrolls past
sections they did not come for.

## Desired behavior

Settings opens on a tab strip. Each section is its own tab, on screen without
scrolling past the others. It opens on **Agents** — the section people open
Settings to edit. Switching tabs never discards an in-progress edit, and an
error raised from any section is visible whichever tab is active.

## Success criteria

- The Settings body renders a tab strip — Agents, Appearance, Updates, plus
  Menu bar between Appearance and Updates on macOS — and opens with Agents
  selected.
- The Menu bar tab is absent, not disabled, wherever its section was absent
  before: off macOS, and on a Mac whose build cannot register a login item.
- The strip follows the ARIA tabs pattern: `role="tablist"` / `role="tab"` /
  `role="tabpanel"`, `aria-selected` on the active tab, roving `tabindex`, and
  Left/Right/Home/End moving focus and selection together.
- Only the active panel is visible; the inactive ones are hidden with
  `display: none` and are therefore out of the Tab order, so focus stays inside
  the dialog and never lands on an invisible control.
- Editing state survives tab switches: an unsaved agent row, the theme selection
  and the token textarea are all intact after leaving a tab and returning.
- `#settings-error` is visible from every tab — including errors raised by the
  Updates section (`SaveUpdateSettings`, `PickDirectory`).
- The Updates channel picker is reachable and hittable with twelve custom agents
  present, and the dialog panel does not grow past the window.
- The `#settings-scroll` / `#settings-updates` pinning rules are gone from
  `settings.css`; the tab replaces the invariant they existed to protect.
- `scripts/ui-lint.sh --strict` and `--contrast` pass on the new CSS.

## Non-goals

- Persisting the selected tab across openings. Settings always opens on Agents.
- Any new Hive keybinding. Left/Right inside the strip is the ARIA pattern, not
  a rebindable binding, so `keymap.ts`, the help overlay and the README keybinds
  table are untouched.
- A command-palette deep link into a specific tab ("Settings → Updates").
- Widening the dialog from `md` (560px) to `lg`.
- Any change to what the sections do — this is layout only. `main`'s Menu bar
  section moves into a tab unaltered.

## Notes

Planned via `/hs-feature-loop plan` (plan-first mode); TRIAGE / RESEARCH / PLAN
gates satisfied by the plan-mode approval. Local spec — no GitHub issue.

Design-system rules that shaped the approach live in
[docs/design-docs/ui/README.md](../design-docs/ui/README.md) § "How to change
the UI from now on".
