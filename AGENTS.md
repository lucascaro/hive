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
