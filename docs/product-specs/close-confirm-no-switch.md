---
issue: 330
title: "GUI: don't switch sessions while the close confirmation is up"
type: bug
complexity: S
priority: P2
stage: IMPLEMENT
---

# GUI: don't switch sessions while the close confirmation is up

- **Issue:** #330
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/close-confirm-no-switch.md](../exec-plans/active/close-confirm-no-switch.md)

## Problem

Closing a session whose worktree has uncommitted changes makes the GUI jump to a
neighbouring session before the "Close this session anyway?" dialog is shown. The
daemon publishes a `checking` phase while it runs the pre-flight `git status`, and
the GUI treats that as "already closing" and hands focus to the neighbour. The
dialog therefore appears over a different session than the one being closed, which
implies the wrong session is at risk — and cancelling leaves the user parked on the
neighbour with no way back except manual navigation.

## Desired behavior

The active session stays focused for the whole confirmation. Focus moves to the
neighbour only once the close is actually happening — after the user has confirmed,
or immediately when there was nothing to confirm.

## Success criteria

- A `session:event` `updated` carrying phase `checking` for the active session does
  not change the focused session.
- A `session:event` `updated` carrying phase `closing` for the active session still
  hands focus to the neighbour, as before.
- Cancelling the worktree-dirty dialog leaves the user on the session they tried to
  close.

## Non-goals

- Changing what `isClosing()` means for the tile dimming, the sidebar "Closing…"
  label, or the `pty:disconnect` guard — those legitimately cover both phases.
- Changing the daemon's phase sequence or the dirty pre-flight itself.

## Notes

Root cause: `cmd/hivegui/frontend/src/app/events.ts` switches on
`isClosing(phaseOf(ev.session))`, and `isClosing()`
(`cmd/hivegui/frontend/src/lib/phase-steps.ts`) covers both `checking` and `closing`.
The daemon sets `checking` in `Registry.kill` (`internal/registry/registry.go`)
before it may refuse with `ErrWorktreeDirty`.
