---
issue: 544
title: Add a session finder: shortcut and palette command to jump to any session by name, title, branch or project
type: enhancement
complexity: M
priority: P2
stage: IMPLEMENT
---

# Add a session finder: shortcut and palette command to jump to any session by name, title, branch or project

- **Issue:** #544
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2

## Problem

With many sessions across projects, finding one means scrolling the sidebar and expanding collapsed projects. Mod+1–9 covers projects only, and the command palette lists commands but no sessions. Often the person remembers the branch or project, not the session name.

## Desired behavior

A dedicated chord, also listed as a palette command, opens a session-only finder. Typing fuzzy-matches case-insensitively over session title, session name, git branch/worktree and project name, across all projects including collapsed ones. Each row shows name, project, branch, status dot and agent badge, with the chord hint visible. An empty query lists sessions most-recently-focused first. Up/Down moves, Enter switches project if needed and focuses that session's terminal, and Esc returns focus to the previously active terminal. The chord is rebindable and its label is derived from the keymap.

## Success criteria

1. From any state, chord, type a fragment of a branch, name, title or project, Enter focuses the matching session's terminal, including one in another or collapsed project (e2e test).
2. Each of the four fields independently produces a match. A test per field.
3. With an empty query, the previously focused session is first, so chord then Enter returns to it.
4. Esc restores focus to the prior terminal, and the session selection is unchanged.
5. A renamed session or changed branch is matched by its new value without restarting the app.
6. The default chord collides with no existing binding (every-shortcut sweep and `lib/bindings.ts` conflict check pass) and is not a terminal-owned key (`RESERVED_CHORDS`).
7. Open and filter with ~100 sessions take no perceptible lag (<100 ms measured in a test with 100 mock sessions).
8. The palette command, help overlay row, Settings > Shortcuts entry, README keybinds table and changeset are all present.

## Non-goals

- Searching session output, transcripts or history (specs 430/431).
- Acting on results: no kill, rename or move from the finder, select-to-focus only.
- A persistent sidebar filter box.
- Archived or closed sessions (assumed excluded; operator did not flag otherwise).

## Notes

Open question: whether the finder should also list projects or commands is undecided and out of the stated ask.

Adjacent specs: 430 (find text in a session), 431 (search transcript history).
