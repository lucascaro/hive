# Session notes

The example UI plugin. Pin a short note to a session and Hive shows it
where you look:

- a **Note** badge on the session's sidebar row,
- a bar above the terminal with the note's first line and an **Edit**
  button,
- a **Session notes** panel beside the terminal listing every note —
  click one to go to that session.

It is written only against [docs/plugins.md](../../docs/plugins.md), so
it doubles as a worked example of every app surface a plugin can add.
It has no process of its own and needs no build step: Hive loads
`ui.mjs` into the app.

## Install

In Hive, open **Settings → Plugins**, install from this directory (or a
git URL of a copy of it), and allow it when asked.

## Use

| Action | How |
|---|---|
| Toggle the notes panel | `⇧⌘O` (Ctrl+Shift+O), or the command palette |
| Edit the note for the focused session | Command palette → *Edit note for this session*, or **Edit** on the bar |
| Clear a note | **Clear** in the editor |

## Settings

Under the plugin's row in **Settings → Plugins**:

- **Show a badge on sessions with a note** — on by default.
- **New note template** — what a new note starts with. Press Enter to
  save it.

Notes and settings are kept in the plugin's settings file,
`plugin-data/session-notes/ui-config.json` under Hive's state directory,
and survive removing and reinstalling the plugin.
