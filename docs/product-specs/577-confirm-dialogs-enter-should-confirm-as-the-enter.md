---
issue: 577
title: "Confirm dialogs: Enter should confirm, as the [enter] hint says"
type: bug
complexity: S
priority: P2
pr: 578
shipped: 2026-11-22
stage: DONE
---

# Confirm dialogs: Enter should confirm, as the [enter] hint says

- **Exec plan:** [docs/exec-plans/completed/577-confirm-dialogs-enter-should-confirm-as-the-enter.md](../exec-plans/completed/577-confirm-dialogs-enter-should-confirm-as-the-enter.md)

## Problem

In-app choice dialogs — Unpin all in the inspector's pinned section, Delete idea, worktree and branch deletes, archived-session deletes, Close session anyway — show `[enter] choose · [esc] cancel`, but Cancel holds focus when the dialog opens, so Enter cancels. AGENTS.md separately says overlays confirm with `y/enter` and cancel with `esc/n` (`y`/`n` are bound nowhere), the choice dialog's own comments say the safe option must hold focus so a stray Enter destroys nothing, and the native `Confirm()` defaults to OK. The rules conflict, and the key on screen does not do what it says.

## Desired behavior

A key shown in a dialog does exactly what its hint says. Enter runs the dialog's default action — its affirmative answer, the least destructive one when there are several — and Esc cancels. Each hint names the real action (`[enter] unpin all · [esc] cancel`), and the Enter hint follows keyboard focus when Tab moves it. A dialog that raises itself unprompted, while the user may be typing (the parked-worktree prompt), keeps a default that destroys nothing, and its hint names that answer. AGENTS.md and the UI design docs state one consistent rule.

## Success criteria

- Opening Unpin all and pressing Enter unpins every pin; pressing Esc keeps them.
- Every other user-raised choice dialog names a `defaultValue`, which holds focus on open and which Enter answers; with several affirmatives it is the least destructive.
- The footer's `[enter]` hint shows the label of the choice Enter will answer, and follows Tab; the `[esc]` hint names what Escape answers (`close` when Escape leaves the question unanswered). A dialog may hide its `[enter]` hint (`enterHint: false`) when Enter only cancels; the dirty-close refusal does (operator decision).
- Enter answers the dialog even when focus has left its buttons.
- A mouse click on any choice still answers it (the hint update must not reflow the button out from under the pointer).
- A dialog that arrives unprompted or after an async gap keeps a default that destroys nothing: the parked-worktree prompt defaults to Retry, the dirty-worktree close refusal, the archived dirty-worktree refusal and the delete-project-with-ideas refusal to Cancel.
- An auto-repeated (held) Enter never answers a dialog.
- AGENTS.md (Feedback on Action, Hard-coded exceptions, Design rule) and `docs/design-docs/ui/components.md` state one rule, with no `y/enter` / `esc/n` left.

## Non-goals

- The native OS `Confirm()` dialog (kill session, delete project, plugin trust) — it already defaults to OK and is owned by the OS.
- Making `Enter` / `Esc` in dialogs rebindable.
- Other modals' Enter handling (Settings, Quick idea, Message session, Archived sessions) — they already do what their hints say.

## Notes

The policy (Enter confirms; self-raised dialogs excepted) was chosen by the operator before this spec was written.
