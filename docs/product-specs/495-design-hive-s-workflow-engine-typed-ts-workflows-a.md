---
issue: 495
title: "Design Hive's workflow engine: typed TS workflows, an inspectable graph, durable runs"
type: enhancement
complexity: L
priority: P2
stage: RESEARCH
---

# Design Hive's workflow engine: typed TS workflows, an inspectable graph, durable runs

- **Issue:** #495
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** —

## Problem

Hive users who run multi-agent work (implement, then review, then fix; fan out across items and verify each; plan, then implement, then verify) do it by hand today:
- they click between sessions and paste outputs;
- they can only use one vendor per step unless they copy text across;
- they can't see where each agent is in the overall flow;
- a crash, restart or full context loses the run, and they start over.

Spec 492 decided the transport (ACP in `hived`) and that the engine is a bundled 460 plugin. It left open:
- how users define workflows;
- what graph they review before a run;
- how runs survive failures;
- whether a custom engine beats adopting a framework, or building no engine at all.

## Desired behavior

A design doc, plus a small runnable prototype, define 492's follow-up 2:
- **Library and project workflows.** Hive ships a library of generic, typed workflow constructs: higher-order functions such as `reviewLoop(worker, reviewer, {maxIters})`, `fanOutThenVerify(items, step)` and `planImplementVerify(spec)` that return subgraphs. Users compose them in TypeScript workflow files in their own projects.
- **Graph before execution.** Building a workflow produces a static JSON graph (the IR) that can be rendered and reviewed before anything runs. Runtime events are keyed by that graph's node ids, so live state overlays the same picture the user reviewed.
- **Durable, node-level runs.** A run survives a plugin crash, a `hived` restart and a reboot, and resumes at its unfinished nodes. Agent sessions are not resumed mid-turn; a node that was interrupted is retried. No always-on service is added.
- **Node kinds.** Agent nodes over ACP, check nodes, and human nodes. There is no direct-model `llm` node; Hive never holds API keys.

## Success criteria

1. **DSL and IR.** A doc in `docs/design-docs/` specifies the TS builder API and the IR's JSON schema. Worked examples cover loops, conditional edges on typed node outputs, dynamic fan-out, and subgraph composition. It states how the graph stays static and inspectable for each.
2. **Prototype.** A prototype under `scripts/`:
   - builds `reviewLoop`, `fanOutThenVerify` and `planImplementVerify`;
   - emits IR JSON plus Mermaid;
   - is covered by `node --test`.

   A CI step fails when the doc's IR or Mermaid examples differ from what the prototype generates. The prototype executes no agents.
3. **Durability.** A comparison table scores Temporal, Restate, Inngest, DBOS and a SQLite event log on:
   - local-only, single-machine operation;
   - whether it needs an always-on service;
   - TS SDK quality;
   - determinism constraints versus hour-long agent calls;
   - waiting on a human;
   - cost;
   - lock-in;
   - fit with a data-defined graph.

   It names one choice that meets the hard requirement (survive a plugin crash, a `hived` restart and a reboot; resume per node; no extra always-on service).
4. **User workflow files.** The doc defines:
   - where project workflow files live and how Hive discovers them;
   - how they are type-checked and run without the user installing a toolchain;
   - what happens to in-flight runs when a workflow file changes.
5. **Runtime events.** The doc defines the event schema keyed by node id and its OpenTelemetry mapping (run → node → agent turn → tool call). It also says how the GUI overlays live and replayed runs on the reviewed graph.
6. **ACP node layer.** The doc specifies:
   - capability detection per agent;
   - how `session/update` maps to node events;
   - one worktree per node, using Hive's worktree model;
   - cancellation, timeouts and runaway-loop caps;
   - how 492's findings are handled: the Codex write lock (F2), Pi ignoring `mcpServers` (F5), and Codex's default permission mode (F4).
7. **Risks.** A billing section records today's Anthropic Agent SDK policy with its source and date (the change is paused; usage draws from subscription limits) and how the design hedges. A security section covers hooks and MCP servers from untrusted worktrees, permission defaults and secrets. Both stay within 492's trust model.
8. **Go/no-go.** The doc compares three options:
   - (1) no engine, only better visibility of hand-run sessions;
   - (2) Mastra or LangGraph.js with ACP nodes inside the 460 plugin;
   - (3) custom.

   It states a concrete threshold for when custom pays off and gives an explicit go/no-go. On no-go, it says how 492's follow-up 2 changes. `acp-workflows.md` links to the new doc.

## Non-goals

- Shipping product code. The `scripts/` prototype doesn't run agents.
- Reopening 492's decisions: where the engine lives (a bundled 460 plugin; `hived` owns ACP, `submit_result`, permissions and provenance) and the shared trust model.
- An `llm` node kind or any direct model API call by Hive.
- A durability layer that needs a separate always-on service.
- Open Agent Spec export, or any other interop format.
- Hivesmith: it is not the baseline, the host, or a source of evidence.
- Workflows distributed as installable packages, and a GUI workflow editor (492 follow-up 4).
- Resuming an agent session mid-turn.
- Cross-project, remote and A2A workflows.

## Notes

- 492's gate B (ACP session kind proven on 5 or more real tasks) still sits before building spec 2. This doc may be written now; implementing the engine waits on follow-up 1.
- Billing sources: https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan , https://zed.dev/blog/anthropic-subscription-changes
- Durability profile is the operator's stated requirement, not measured: no usage data exists, and the users are all Hive users, not one tool's telemetry.
