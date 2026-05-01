# Phase 5 — System bell notifications

**Status:** In progress
**Branch:** `silent-light`

## Scope (deliberately narrow)

Per user request: surface notifications when a session emits a BEL
(`\x07`) — the conventional "I need input" signal that agent CLIs
(Claude, Codex, etc.) use when they're waiting on the user.

Everything else from the original Phase 5 sketch (workflows, agent
teams, hooks, status detection) is deferred. This phase is one
discrete UX win.

## Behavior

When a session receives BEL:

- **If the session is the active one and the GUI window is focused**:
  ignore. The user is already there.
- **Otherwise**:
  - Mark the session as "needs attention" — the sidebar row pulses
    in the session's color, and a small dot appears next to its
    name.
  - Fire an OS notification: title `Hive — <session name>`, body
    `<project name> needs attention`. Tag the notification with the
    session id so repeated bells dedupe.
- Switching to a session (single mode) or focusing its tile (grid
  mode) clears the attention state.

## Implementation

Frontend-only:

- xterm.js v5 exposes `term.onBell((listener))`. Wire it in
  `SessionTerm`.
- HTML5 `Notification` API is available in Wails' WKWebView; permission
  is requested once at startup.
- Visual indicator: CSS class on the sidebar item with a pulsing
  outline + colored dot.

No daemon changes — bell bytes flow through the existing DATA frame
path; the GUI just notices them instead of sending raw `\x07` to the
terminal renderer (xterm.js still renders the BEL visually if
`bellStyle: 'sound'`).

## Acceptance

1. Open two sessions, run `claude` in one. Switch focus to the other.
2. In the claude session, ask a question that prompts confirmation
   (or simply `printf '\a'` via a regular shell session for a
   plain-shell test).
3. **Pass criteria:**
   - The non-focused session's sidebar row pulses
   - A macOS notification appears with the session name
   - Clicking the sidebar row or the notification (best-effort)
     clears the attention state
