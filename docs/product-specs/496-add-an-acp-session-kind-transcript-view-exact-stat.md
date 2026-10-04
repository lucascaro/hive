---
issue: 496
title: "Add an ACP session kind: transcript view, exact state, permission prompts, typed results, PTY takeover"
type: enhancement
complexity: L
priority: P2
pr: 500
stage: IMPLEMENT
---

# Add an ACP session kind: transcript view, exact state, permission prompts, typed results, PTY takeover

- **Issue:** #496
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/496-add-an-acp-session-kind-transcript-view-exact-stat.md](../exec-plans/active/496-add-an-acp-session-kind-transcript-view-exact-stat.md)

## Problem

Hive can only run an agent in a terminal (PTY).
- **State is guessed** for Codex, Gemini and Copilot: Hive infers working or idle from terminal output.
- **No typed result:** no agent can return a result a program can read.
- **Nothing for the engine to run on:** the workflow engine (#495) has no way to prompt an agent and collect its output.

Spec 492 showed that ACP (Agent Client Protocol) fixes all three. Hive has no ACP session yet.

## Desired behavior

- **Creating one.** In the new-session dialog, a user can pick **ACP** instead of **Terminal** for any agent that supports it. Terminal stays the default, and nothing changes for users who never pick ACP. The engine and the wire can request an ACP session explicitly.
- **The transcript view.** An ACP session opens in a transcript view instead of a terminal. It shows:
  - agent messages, plan steps and tool calls;
  - permission requests, which the user answers in place;
  - a text prompt box.
- **State.** The sidebar shows exact state (working, idle, waiting for permission) from a new `acp` state tier.
- **Typed result.** A session can return a typed result that can be read over the wire:
  - through an injected `submit_result` tool;
  - for Pi, through Hive's Pi extension.
- **Takeover and hand-back.** **Take over** reopens the same conversation in a terminal session. **Hand back** returns it to ACP without losing any turns.
- **Daemon restart.** ACP sessions survive a `hived` restart the way terminal sessions already do: they come back in the same conversation.
- **Agents.**
  - Claude and Pi are the primary agents.
  - Codex is supported.
  - Gemini and Copilot are labeled experimental until they are probed.

## Success criteria

1. **Session kind.** A session can be created as kind `acp` through the existing single spawn path (`Registry.Create`). The kind is persisted and visible on the wire, and Terminal remains the default in the new-session dialog.
2. **Transcript view.** For an ACP session, the GUI shows a transcript view with:
   - agent text, plan steps and tool calls;
   - inline permission requests that the user can allow or deny;
   - a text prompt box.

   No terminal is attached to an ACP session.
3. **State tier.** ACP sessions report `state_source: acp` with exact working, idle and waiting-for-permission states. `control-plane.md` lists the `acp` tier as implemented, no longer as proposed.
4. **Typed result.** A typed result submitted by a Claude or Codex session (through `submit_result`) or a Pi session (through the Pi extension) can be read over the wire. A wire operation lets a client prompt an ACP session, and each prompt records where it came from. `submit_result` is the only permission the daemon allows on its own, matched by its exact tool identity and bound to that session's own server name. Every other permission request goes to the user.
5. **Trust.**
   - **Permission mode.** Each session's ACP permission mode is set explicitly and never exceeds the user's per-agent setting. For example, Codex's default mode is not used, which handles F4.
   - **Pi.** Pi runs only where the user's setting allows unattended tool use (F5).
   - **Provenance.** `SpawnedBy` and prompt provenance are recorded as `acp-workflows.md` defines them.
6. **Takeover and hand-back.**
   - Take over opens the same conversation in a PTY session.
   - Hand back returns it to ACP with every turn intact. For Codex, hand-back releases the background daemon's write lock or fails with a clear error (F2).
   - An automated test with a fake ACP agent proves that no turn is lost across takeover and hand-back.
7. **Restart.** After a `hived` restart, a running ACP session comes back in the same conversation (`session/load`), with its transcript intact.
8. **Adapters.**
   - **Pinned versions.** Hive runs the Claude and Codex adapters at pinned versions through the user's Node.
   - **No Node.** Without Node, the ACP option is disabled with a clear reason.
   - **Unprobed agents.** Gemini and Copilot are labeled experimental in the dialog.
9. **Gate B.**
   - **The gate.** `acp-workflows.md`'s gate B is updated to Claude and Pi.
   - **The log.** The doc logs at least 2 real tasks per agent, at least 1 of them taken over in a PTY and handed back, with 0 lost turns: date, agent, task and outcome for each run. The fake-agent test in criterion 6 is the automated proof that no turn is lost; the real runs confirm it holds against the actual adapters.

## Non-goals

- Changing Terminal as the default session kind, or how terminal sessions behave.
- A rich diff viewer, transcript search or export, and a mode or model picker. These go to a follow-up spec.
- Images or attachments in the prompt.
- The workflow engine, graphs, scheduling or durability (#495).
- Hive-maintained ACP adapters, or bundling adapters into the app.
- Hive acting as an ACP agent; remote or cross-project sessions.

## Notes

- Depended on #494 (Claude takeover and resume failed when the folder path contained `_`); fixed in #497.
- #254 (resume conversations after a daemon restart) looks stale: `Revive` already resumes through `ResumeArgs` (`internal/registry/registry.go:1367`). Close it or re-scope it separately.
- This needs a `DaemonContract` bump, since it adds a new session kind and new wire operations.
