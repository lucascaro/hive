---
issue: 423
title: Raise needs-attention when an agent turn goes quiet without reporting its end
type: bug
complexity: S
priority: P2
pr: 424
stage: REVIEW
---

# Raise needs-attention when an agent turn goes quiet without reporting its end

## Problem

A Pi session sometimes shows no needs-attention after the model asks a question through a custom question tool. When an agent session's reporter goes silent mid-turn, `agentstate.Machine.Tick` demotes the stale `working` state to plain `idle` after `HookStaleAfter` (30 s) plus `QuietAfter` (2 s) of a static screen. `idle` does not raise attention, so a session that stopped and is waiting on the user reads as finished-and-fine. Why the question's own `waiting_input` goes missing is still unreproduced ([debugging handoff](../design-docs/pi-question-attention-debugging.md)).

## Desired behavior

A Pi session whose Pi-started turn goes quiet with no end report lands in `waiting_input` instead of `idle`, so it raises needs-attention. The state is still attributed to the heuristic tier ("guessed from terminal output").

## Success criteria

- A Pi turn opened by an extension event (`permission_resolved`, `tool_start`, `tool_end`) that goes stale and quiet ticks to `waiting_input` with source `heuristic`, and `needs_attention` is true.
- A Claude (hook-tier) turn and a plain shell session still tick `working` → `idle`.
- An agent session that is idle and then repaints because the user types, with no agent event in between, still ticks back to `idle`, not `waiting_input`. Typing into a finished session must not light it up.
- A declined question, once cleared, closes the turn: typing afterwards does not raise attention again.
- Agent-reported endings (`turn_end`, `idle`, `error`, `session_end`) close the turn: after them, a later quiet tick raises nothing new.
- Once the tick has raised attention and the user clears it, further repaint-then-quiet cycles do not raise it again until a new agent event opens a turn.

## Success criteria (added at plan review)

- When the active Pi session needs attention and the window stays focused for 3 s, attention clears (status only; the terminal is untouched). Blur, a switch or an answer before then cancels it.

## Non-goals

- Any change to Claude or other hook-tier agents, or to shell sessions.

- Fixing why a question's `waiting_input` is lost (unreproduced; see the handoff).
- Raising attention for agent-reported `idle` (Esc abort, `/new`, answering a dialog outside a turn), or for heuristic-only sessions.
- Changing `HookStaleAfter` or `QuietAfter`.

## Notes

- Debugging handoff: [docs/design-docs/pi-question-attention-debugging.md](../design-docs/pi-question-attention-debugging.md)
- State model: [336-session-state-model.md](336-session-state-model.md)
