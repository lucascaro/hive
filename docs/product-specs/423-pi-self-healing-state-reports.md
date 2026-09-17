---
issue: 423
title: Pi reports self-healing session state (no stale-turn guessing)
type: bug
complexity: M
priority: P2
pr: 424
stage: GATE
---

# Pi reports self-healing session state (no stale-turn guessing)

## Problem

A Pi session sometimes shows no needs-attention after the model asks a question through a custom question tool. The Pi extension reports state as one-shot edges ("a prompt opened", "the turn ended"), one short socket connection each. If one report is lost (a dropped connection, the sender queue overflowing behind a busy daemon) or lands wrong, the daemon holds the wrong state and nothing corrects it. Pi says nothing while a question sits on screen. After `HookStaleAfter` (30 s) the daemon falls back to guessing from the terminal, and it guesses `idle`, so the session reads finished while it is waiting on the user.

## Desired behavior

The Pi extension's reports heal themselves. Each state-bearing report carries an instance id and a sequence number. While Pi runs, the extension re-sends its latest state-bearing report every 5 s, so a lost report of the latest state is restored within one interval, and a report the daemon already applied is a no-op. Because the repeats are no-ops, the user clearing attention sticks. The repeats also keep the extension tier trusted, so a live Pi session never falls back to terminal guessing. That fallback remains only for a Pi process that stopped reporting.

## Success criteria

- A lost Pi state report (for example the question's `waiting_input`) is restored by the next heartbeat: the session reaches `waiting_input` and needs-attention within one heartbeat interval.
- A heartbeat re-sending an already-applied report changes no state, so a wait the user cleared stays cleared.
- A report with a lower sequence number than one already applied, from the same extension instance, is dropped.
- A new extension instance (a new Pi process, `/new`, `/resume`, fork or `/reload`) is accepted from sequence 1, and a straggler from the previous instance is corrected by the next heartbeat.
- A delivered `plan` or `ping` never hides a lost state report, and a replay after the heuristic tier took over restores the reported state on the extension tier.
- A live, idle Pi session stays on the extension tier indefinitely: typing into it no longer flips it through working → idle by terminal heuristics.
- A Pi process that stops reporting still falls back to the heuristic tier after `HookStaleAfter`.
- An extension without sequence numbers (older build) and Claude's hooks behave exactly as today.

## Non-goals

- Changing Claude hook reporting or any hook-tier behavior.
- Fixing the case where Pi itself never emits `ui_prompt_start`, or emits prompt events out of order. The extension can only replay what Pi told it. The debug log decides that case ([handoff](../design-docs/pi-question-attention-debugging.md)).
- A focus-dwell that clears attention (dropped from this spec).
- Changing `HookStaleAfter` or `QuietAfter`.

## Notes

- Debugging handoff: [docs/design-docs/pi-question-attention-debugging.md](../design-docs/pi-question-attention-debugging.md)
- State model: [336-session-state-model.md](336-session-state-model.md)
- Replaces the first approach on this spec (a stale Pi turn ticking to `waiting_input`, plus a 3 s focus dwell), dropped at operator review: it guessed at a lost report instead of recovering it.
