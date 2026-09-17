# Handoff: Pi question without "needs attention"

Status: **unreproduced**. Written so the investigation can continue on the
machine where the bug shows up. The fix shipped alongside this note (a
stale agent turn that goes quiet raises attention) masks the symptom
after ~32 s; it does not explain why the question's own `waiting_input`
was lost. Find that here.

## Symptom

In a Hive-spawned Pi session, the model asks a question through the
custom `ask_question` tool (pi-devkit, `extensions/ask-question/index.ts`).
Sometimes the session does not show needs-attention. It later reads idle.

## What is already verified

- `ask_question` calls `ctx.ui.custom(...)`. Pi 0.85.1 wraps `custom`
  (`dist/core/extensions/runner.js`, `wrapUIPromptContext`/`withUIPrompt`)
  and emits `ui_prompt_start{kind:"custom"}` for the outermost prompt, so
  the Hive extension (`internal/agent/pi/hive.ts`) posts `waiting_input`.
  The happy path works.
- `ask_question` declares `executionMode: "sequential"`, so a batch that
  contains it never runs tools in parallel. A parallel `tool_end`
  clobbering the wait is ruled out.
- Pi awaits `tool_execution_start` listeners before `execute`, so a
  `tool_start` is posted before the question's `ui_prompt_start`.
- `agent_settled` fires from `_runAgentPrompt`'s `finally`, the only run
  path in 0.85.1, so a plain-text question ending the turn posts `turn_end`.
- `announceStateLocked` (`internal/registry/registry.go`) derives
  attention from state alone, not from the tier.
- Before the fix, `Machine.Tick` turned a stale-tier `working` into
  `idle`/heuristic after `HookStaleAfter` (30 s) of no events plus
  `QuietAfter` (2 s) of static screen. That is the "working → idle, no
  attention" the user sees.

## Hypotheses, most likely first

1. **Wait overwritten by a working event.** `Apply` accepts events with
   equal `at` stamps in arrival order, and `permission_resolved`,
   `tool_start` and `tool_end` all force `working`. Two questions back to
   back (a batch of two `ask_question` calls, or answer then an immediate
   re-ask) emit `ui_prompt_end` and the next `ui_prompt_start` a microtask
   apart, so usually in the same millisecond. The current extension
   serializes connections (`createSender`), so arrival follows send order,
   but send order is Pi's handler order across all loaded extensions,
   which is not guaranteed. The pre-#421 extension dialed each event in
   parallel, so it raced freely.
2. **No `ui_prompt_start` at all.** `withUIPrompt` only emits for the
   outermost prompt (`uiPromptDepth`). If another extension holds a
   long-lived `ui.custom`/`select`, the question is nested and silent. A
   grep of pi-devkit and `~/.pi/agent/extensions` found none, but the
   other machine may load more.
3. **GUI cleared it.** `SetAttention(false)` → `ClearWaiting` fires on a
   keystroke or paste into the tile (`noteUserInput`), a mousedown on the
   already-active tile, a session switch, a notification click, or ⌘B
   with nothing flagged.

## How to collect evidence

1. Confirm the reporter version:
   `grep -c createSender ~/Library/Application\ Support/Hive/pi/hive.ts`
   (non-zero means the serialized sender).
2. Enable the transition log for the GUI-launched daemon:
   `launchctl setenv HIVE_DEBUG_STATE 1`, then Hive → Restart Daemon. If
   no `state:` lines appear, quit Hive fully and reopen it.
3. Reproduce. Note the wall time. Leave the unanswered question up for at
   least 40 s.
4. `grep -E ' state: ' ~/Library/Application\ Support/Hive/hived.log | tail -80`
5. Afterwards: `launchctl unsetenv HIVE_DEBUG_STATE`.

Each line reads `state: <id> <from> -> <to> src=<tier> reason=<cause>`.

| Log pattern for the session | Means |
|---|---|
| `-> waiting_input reason=waiting_input`, then `-> working reason=permission_resolved\|tool_start\|tool_end` | Hypothesis 1: the wait was overwritten |
| no `waiting_input` line around the question | Hypothesis 2: Pi never reported it |
| `waiting_input -> idle reason=clear` | Hypothesis 3: the GUI marked it seen |
| `working -> waiting_input src=heuristic reason=tick` about 32 s after the question | the new fallback caught it; look at the lines before it for the cause |

## Once reproduced

Write the failing test first. For hypothesis 1 that is a
`internal/agentstate` table test: `waiting_input` then
`permission_resolved` with the same `At` must not leave a displayed
question reading `working`. The candidate fix is to make the extension
emit prompt end/start as one ordered report, or to make the machine
refuse a `permission_resolved` that does not follow a wait. Pick one with
the log in hand, not before.
