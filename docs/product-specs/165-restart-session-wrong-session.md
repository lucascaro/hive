# Restarting a session can reload the wrong session when multiple share a worktree/directory

- **Issue:** #165
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/165-restart-session-wrong-session.md](../exec-plans/active/165-restart-session-wrong-session.md)

## Problem

When multiple sessions exist in the same worktree or directory, restarting a session may reload a different session than the one the user intended. The session lookup appears to match by directory/worktree path rather than by a unique session identifier, so the first or most-recent matching session is chosen instead of the specific one being restarted.

## Desired behavior

Restarting a session reloads exactly that session, regardless of how many other sessions share its worktree or directory. Identification is by unique session id, not by path.

## Success criteria

- With two or more sessions in the same worktree/directory, restarting any one of them reloads that exact session (same id, same history, same state).
- No regression for the common case of a single session per worktree.

## Non-goals

- Redesigning session creation, naming, or grouping by worktree.

## Notes

Reported via /hs-feature-loop on 2026-05-08.
