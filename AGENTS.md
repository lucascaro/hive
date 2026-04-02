# Hive – AI Coding Guidelines

## UX Best Practices

Always apply these principles when adding or modifying UI elements in the TUI:

### Key Discoverability
- **Always show the key next to the action it triggers.** If a number, letter, or chord activates something, display it inline — e.g. `[1] ProjectName`, `(n) new`, `[enter] attach`.
- Never rely on the user memorizing bindings from the help screen alone. Hints should be visible at point of use.
- When screen space is tight, abbreviate the hint but keep it present (e.g. `[1]` rather than removing it).

### Navigation Context
- Show positional context so users know where they are: active item should be clearly marked (current `←` indicator for sessions is good; keep it).
- Numbered shortcuts (1–9 for projects) must have their number shown in the sidebar label so the mapping is obvious without opening help.

### Status Visibility
- Status dots/badges must appear on every session row — never omit them to save space.
- Agent type badges should always accompany session labels.

### Feedback on Action
- Destructive actions (kill, quit+kill) must always go through the confirm overlay — never skip it.
- Overlays must display the relevant keybinding to confirm and cancel (`y/enter` to confirm, `esc/n` to cancel).

### Consistency
- Key hints use the format `[key]` for number/symbol keys and `(key)` for letter keys — pick one format and apply it uniformly across the whole UI.
- Help text in the status bar and help overlay must stay in sync with actual bindings.

### Information Density
- Prefer showing information inline over requiring a modal/overlay for basic facts (e.g. project number, agent type, session status).
- Reserve overlays for actions that need confirmation or multi-field input.

## Documentation Maintenance

Keep project documentation accurate and up-to-date as part of every code change. Stale docs are a bug.

### Changelog (`CHANGELOG.md`)

- **Every meaningful commit or push must update the `[Unreleased]` section** of `CHANGELOG.md`.
- Classify entries under the appropriate subsection: `Added`, `Changed`, `Fixed`, `Removed`, or `Security`.
- Use concise, user-facing language — describe what changed and why it matters, not internal implementation details.
- Do **not** create a new versioned section; only append to `[Unreleased]`. Versioning happens at release time.
- Skip purely cosmetic or internal refactors that have no user-visible effect (e.g. renaming a private variable). Use judgment.

### Architecture (`ARCHITECTURE.md`)

Update `ARCHITECTURE.md` whenever a change is **significant** — meaning any of the following:
- A new package is added or an existing one is removed/renamed.
- A major interface, abstraction boundary, or data-flow path changes.
- New components are added to the TUI layer (new files in `internal/tui/components/`).
- The `cmd/` command set changes (new subcommands, removed subcommands).
- The multiplexer, state, config, or hook subsystems change in a way that affects the high-level description.

Minor changes (bug fixes, adding a field to an existing struct, small refactors) do **not** require architecture updates.

### README and other docs

- Update `README.md` when **user-visible features, CLI flags, configuration options, or default behaviour** change.
- Update the relevant file in `docs/` when the subsystem it documents changes:
  - `docs/agent-teams.md` — multi-agent team behaviour
  - `docs/hooks.md` — hook events or environment variables
  - `docs/keybindings.md` — key bindings or navigation
  - `docs/features.md` — high-level feature descriptions
  - `docs/design-decisions.md` — only when a significant architectural decision is made
- If a doc file becomes incorrect after your change, fix it in the same commit.
