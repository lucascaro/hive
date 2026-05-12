# Regression: focus inconsistent — switching from single-session to grid mode disables typing

- **Issue:** #186
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/completed/186-grid-mode-focus-regression-cant-type.md](../exec-plans/completed/186-grid-mode-focus-regression-cant-type.md)

## Problem

Regression of #159 (and possibly tangled with the grid-mode work in #177). When transitioning from single-session view into grid mode, keyboard input no longer reaches the focused session — typing has no effect. This was previously fixed under #159 and the user reports a recent attempted fix did not stick. Visual focus state and the element actually receiving keystrokes are once again out of sync.

## Desired behavior

Switching between single-session view and grid mode (in either direction) consistently restores keyboard focus to the visually selected session. Typing immediately reaches that session's input without an extra click.

## Success criteria

- After single → grid mode transition, the visually focused session receives keystrokes immediately.
- After grid → single transition, the session input receives keystrokes immediately (no #159 regression).
- A regression guard (test or runtime assertion) catches future divergence between visual focus and the active input element.

## Non-goals

- Broader focus-management refactor outside the single ↔ grid transition.

## Notes

Related: #159 (original fix), #176 (visibility-gated xterm canvas resize), #177 (grid mode revert + restart bugs).
