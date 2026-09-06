---
issue: 362
title: "Natural text editing keybindings in the terminal"
type: enhancement
complexity: S
priority: P2
stage: IMPLEMENT
---

# Natural text editing keybindings in the terminal

- **Issue:** #362
- **Type:** enhancement
- **Exec plan:** [docs/exec-plans/active/362-natural-text-editing-keybindings.md](../exec-plans/active/362-natural-text-editing-keybindings.md)
- **Complexity:** S
- **Priority:** P2

## Problem

Hive forwards some macOS text-editing chords to the PTY (⌘⌫ → `\x15`, ⌘←/⌘→ →
`\x01`/`\x05`, ⇧⏎ → `\x0a`), and xterm.js covers several more on its own. But the
set is incomplete: some chords still send sequences no shell or agent CLI binds,
and none of the terminal-level editing keys appear in the README keybind table or
the ⌘/ overlay.

## Desired behaviour

The chords a macOS user expects for text editing work inside a hive session the
way they do in iTerm2's "Natural Text Editing" preset, and every binding hive
owns is documented on the surfaces AGENTS.md › Keybindings Policy requires.

## Success criteria

- On macOS, ⌥⌦ (Option + forward delete) writes `\x1bd` to the PTY, deleting the
  word after the cursor in bash, zsh and the agent CLIs.
- On macOS, ⌘⌦ (Cmd + forward delete) writes `\x0b` to the PTY, deleting from the
  cursor to the end of the line — the mirror of the existing ⌘⌫ → `\x15`.
- Neither binding fires off macOS, and neither fires for a bare ⌦ or for
  ⌫/Backspace.
- Every terminal-level editing key hive relies on — ⌥⌫, ⌥←/→, ⌦, ⌥⌦, ⌘⌦ — is
  listed in the ⌘/ help overlay's "Inside a terminal" group and in the README
  Keybinds table.
- Unit tests in `test/unit/keymap.test.ts` cover the new predicate, in the same
  shape as the existing `macLineEditSeq` block.

## Non-goals

- **Option-as-Meta** (`macOptionIsMeta`). Enabling it would make ⌥. , ⌥d, ⌥u/l/c
  work, but it breaks typing `[ ] { } \ @ #` on German, French and Spanish Mac
  layouts. A settings toggle is more surface than these two chords warrant.
- Changing forward delete to iTerm's `0x04`. That is Ctrl+D — delete-char in
  readline, but EOF on an empty line, so it can close the user's shell. xterm's
  `\x1b[3~` is bound by readline, zsh, vim and the agent CLIs.
- Windows/Linux equivalents. Ctrl+⌦ already emits `\x1b[3;5~` there, which
  readline binds as kill-word; adding bindings risks colliding with tmux.
- Rebindable keybindings, or any keymap UI.
- Re-implementing anything xterm.js already gets right (⌥⌫, ⌥←/→, ⌦, ⌘A).

## Notes

Prior art: iTerm2 Natural Text Editing preset. Behaviour of the current stack
verified against `@xterm/xterm@5.5.0`'s `evaluateKeyboardEvent`; see the exec
plan's Research section for the chord-by-chord table.
