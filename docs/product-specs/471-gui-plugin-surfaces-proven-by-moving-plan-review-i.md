---
issue: 471
title: GUI plugin surfaces, proven by moving plan review into a bundled plugin
type: enhancement
complexity: L
priority: P2
pr: 473
stage: GATE
---

# GUI plugin surfaces, proven by moving plan review into a bundled plugin

- **Issue:** #471
- **Exec plan:** [docs/exec-plans/active/471-gui-plugin-surfaces-proven-by-moving-plan-review-i.md](../exec-plans/active/471-gui-plugin-surfaces-proven-by-moving-plan-review-i.md) (or completed/)

## Problem

Headless plugins (#460) can react to sessions and drive them, but they can't show anything in the Hive app. A feature that needs UI, like plan review (#457), can only live in core, so core keeps growing and every UI feature lands in the one maintainer's review queue. The `ui` manifest field is reserved but not built, so plugin authors, first-party ones included, have no supported way to add a view, command, indicator or setting to the app.

## Desired behavior

- A plugin's manifest can declare GUI contributions. It can add a view tied to a session, command palette commands (with key hints), indicators or badges on sidebar session rows, and its own section in Settings.
- Plugin UI is fully trusted, like headless plugins, and goes through the same install warning and enable/disable flow.
- Plan review moves out of core into a bundled first-party plugin that is off by default. The plugin owns the review UI: the rendered plan, comments, approve/deny, and the reviewer-choice setting. Core keeps the agent-side pieces: the blocking `ExitPlanMode` hook, the Pi submit-plan tool, external-reviewer detection and the no-GUI fallback. Core exposes these as generic plugin-facing events and actions, not as plan-review-specific UI.
- The core plan-review UI code is deleted. There is one implementation.
- The bundled plugin uses only the public, documented plugin API. A third-party plugin can use every surface it uses.
- A plugin UI that throws, hangs or renders badly is contained: the rest of the app keeps working and the plugin can be disabled.

## Success criteria

1. With the bundled plan-review plugin enabled and review turned on, all seven #457 success criteria pass against the plugin version. The existing plan-review e2e-real and DOM tests run against it (ported where needed).
2. The core GUI no longer contains a plan-review UI implementation. Plan review appears in the app only through the plugin surfaces.
3. On a fresh install, the plan-review plugin is listed in Settings → Plugins as installed and disabled, and it ships in the app bundle with no git or folder install step.
4. A second small example plugin in the repo, separate from plan review, uses each of the four surfaces (session view, palette command, sidebar badge, settings section), is written only against the published docs, and works end to end.
5. A test GUI plugin that throws on render, one that never finishes rendering or loading, and one that floods updates each leave the rest of the GUI usable, including switching sessions, typing into a terminal and opening Settings. The plugin can then be disabled from Settings.
6. With zero GUI plugins enabled, GUI idle CPU/memory and startup time stay within noise of the pre-change baseline, measured and recorded in the PR.
7. The plugin-author docs (`docs/plugins.md`) cover the `ui` manifest, each surface, UI lifecycle, the trust model and the 0.x stability promise. The plugin API version is bumped from 0.1.
8. Palette commands contributed by plugins show their key hints and appear in the `⌘/` shortcuts overlay, following the Keybindings Policy.

## Non-goals

- Sandboxing or isolating plugin UI. It is fully trusted, like #460.
- Carrying over the old "in-Hive plan review" setting. Users who had it on will find the plugin disabled after upgrading and have to enable it themselves. This is an accepted regression.
- New plan-review features: Ask AI, plan history or revision diffs, HTML artifact review, share or export. #457 deferred these, and they stay deferred.
- Letting plugins inject agent hooks or Pi extensions. The agent-side plan-review wiring stays in core.
- Cosmetic plugins (themes, keybinding packs). That is spec 3.
- A marketplace, registry, auto-update or curated index.
- Moving any core feature other than plan review into a plugin.
- GUI surfaces beyond the four listed, such as a status bar, terminal decorations or new top-level windows.

## Notes

- Spec 2 of 3 in the plugin decomposition started by #460 (headless → GUI surfaces → cosmetic).
- Phasing: phase 1 is the surfaces plus the example plugin (criteria 4–8), and phase 2 is the plan-review migration (criteria 1–3).
- Open: how and where a plugin's UI code runs inside the Wails webview. This is a solution question, left to the plan.
- Open: `docs/design-docs/control-plane.md` currently says a plugin is never counted as a client that can answer a plan review. Phase 2 has to revise that rule.
- Depends on: #460 (shipped), #457 (shipped).
