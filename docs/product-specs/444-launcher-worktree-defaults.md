---
issue: 444
pr: 445
shipped: 2026-09-20
title: "Launcher: ideas default to a worktree, plain new session never remembers it"
type: enhancement
complexity: S
priority: P2
stage: DONE
---

# Launcher: ideas default to a worktree, plain new session never remembers it

- **Exec plan:** [docs/exec-plans/completed/444-launcher-worktree-defaults.md](../exec-plans/completed/444-launcher-worktree-defaults.md)

## Problem

Starting a session from an idea (Idea inbox → Start session) opens the agent launcher with the worktree toggle in whatever state it was last left, so an idea — usually a self-contained unit of work — often launches in the main checkout. Meanwhile ⌘T and every other plain New Session opener remember the last-checked worktree state (`localStorage['hive.worktree']`), so ticking the box once silently turns every later ⌘T into a worktree launch.

## Desired behavior

- Idea inbox → Start session opens the launcher with the worktree toggle **on**.
- ⌘T, the menu / palette New Session, the sidebar project `+` and the empty-state New Session button always open with the toggle **off**.
- ⇧⌘T (and its menu / palette equivalents) keep forcing it on.
- The toggle stays editable for the launch at hand; its state is no longer remembered anywhere.

## Success criteria

- Opening the launcher from Start session on an idea shows the worktree checkbox checked (git project).
- Opening the launcher with no options shows the checkbox unchecked even after it was ticked in a previous opening.
- Toggling the checkbox writes nothing to `localStorage`.
- Duplicate and resume-worktree modes still never create a worktree.

## Non-goals

- A user setting for the default worktree state.
- Changing ⇧⌘T, duplicate or resume-worktree behaviour.
