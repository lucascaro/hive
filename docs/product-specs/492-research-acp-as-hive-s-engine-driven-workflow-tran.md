---
issue: 492
title: "Research ACP as Hive's engine-driven workflow transport"
type: enhancement
complexity: M
priority: P2
pr: 493
shipped: 2026-10-03
stage: DONE
---

# Research ACP as Hive's engine-driven workflow transport

- **Issue:** #492
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/492-research-acp-as-hive-s-engine-driven-workflow-tran.md](../exec-plans/completed/492-research-acp-as-hive-s-engine-driven-workflow-tran.md)

## Problem

The user hand-runs multi-agent workflows (plan→implement→review loops, parallel fan-out, cross-vendor review) by clicking between sessions and pasting outputs. Hive sees each agent only through PTY heuristics or per-vendor hooks, so it can't run these workflows, compare results in one shape, or show where each agent is. ACP (Agent Client Protocol) might fix all three, but nobody has verified what the adapters actually support. The current design (`docs/design-docs/agent-orchestration.md`) assumes agents orchestrate agents.

## Desired behavior

A design doc exists that:

1. records verified ACP capabilities for each built-in agent (Claude, Codex, Gemini, Copilot, Pi);
2. decides where the workflow engine lives (daemon core or a 460 headless plugin);
3. defines engine-driven workflows (ACP as the primary node transport, an injected MCP `submit_result` tool for typed outputs that look the same across agents, deterministic check nodes for pass/fail claims, a per-vendor headless adapter as the escape hatch, and PTY for human takeover) as a **parallel track** next to orchestration phases 1–3, with both sharing one trust model;
4. lists the follow-up specs in order: ACP session kind → code-defined workflow engine → live workflow monitoring → workflow authoring.

## Success criteria

1. A doc in `docs/design-docs/` has a capability table. For each of Claude, Codex, Gemini, Copilot and Pi it records: whether ACP works (native or adapter, with the adapter's name and version), MCP server injection via `session/new`, permission prompts, plan/tool-call streaming, `loadSession`/resume, and whether an ACP session can be reopened interactively in a Hive PTY session. Each cell is marked verified-by-running or doc-only.
2. The doc names the workflow engine's home, with the reasons for and against both daemon core and a 460 plugin.
3. The doc states how ACP workflow sessions and phase 1–3 orchestration share one trust model: no permission escalation beyond the user's per-node settings, and provenance on every spawn and every prompt.
4. `agent-orchestration.md` and `control-plane.md` link to the new doc and are updated where it changes them (the Phase 4 row, and a possible `acp` state tier).
5. The follow-up specs are listed in order, each with a one-line problem statement. No product code ships.
6. The doc gives an explicit go/no-go. If PTY takeover or MCP injection fails for both Claude and Codex, the doc says so and recommends stopping.

## Non-goals

- Shipping product code (this is research and design only).
- Changing PTY as the default session kind; interactive sessions behave exactly as today.
- Dropping agents without ACP (shell, Aider, custom); they must remain usable as workflow nodes via the headless or PTY escape hatch.
- Hive acting as an ACP agent or server (for example, for Zed or JetBrains).
- A2A, remote, or networked agents.
- Cross-project workflows.
- Retiring 338 or 389–391; the operator chose a parallel track.

## Notes

Open questions carried from the brainstorm:

- The operator chose **parallel track** over replacing phases 1–3, accepting the risk that two orchestration models and two trust models both spawn sessions. The design doc must show how that drift is contained.
- Per-adapter resume into a PTY ("ACP agents use their normal session stores, so you can usually reopen interactively") is the operator's own unverified claim. It is the key thing to verify.
- Writing or maintaining Hive's own ACP adapters was **not** excluded; the doc should say whether it is needed (for example for Pi, Copilot or Aider).
- Must preserve: ACP sessions live in `hived` and survive GUI reloads, like PTY sessions.
- The visualization is phased: code-defined workflows first, then live monitoring, then authoring.
- The 389–391 evidence gates measure agent-initiated `hived msg`/`wait` and cannot measure engine-driven workflows. The doc should say what gates the ACP track instead.

Sources:

- https://agentclientprotocol.com/protocol/session-setup
- https://agentclientprotocol.com/overview/agents
- https://blog.marcnuri.com/agent-client-protocol-acp-introduction
