---
issue: 529
title: "Workflow engine B: headless engine core (validator, interpreter, checkpoint log, trust sandbox)"
type: enhancement
complexity: L
priority: P1
stage: RESEARCH
---

# Workflow engine B: headless engine core (validator, interpreter, checkpoint log, trust sandbox)

- **Issue:** #529
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Parent:** [527](527-build-the-workflow-engine-bundled-plugin-running-typ.md)
- **Depends on:** [528](528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md)
- **Design:** [workflow-engine.md](../design-docs/workflow-engine.md)

## Problem

The design (#495) is settled but nothing runs. Users can't execute a typed workflow over `acp` sessions, and a crash or restart would lose it.

## Desired behavior

A bundled headless plugin builds a trusted workflow file into an IR, validates it, and runs it over `acp` sessions, checks and human nodes. The run survives a plugin crash, a `hived` restart and a reboot. It is driven and tested without a GUI.

## Success criteria

Tests use the `internal/acp/acptest` fake adapter, never a real agent.

1. **Open questions closed.** The Node floor, network fencing under `--permission` and the `node:sqlite` tier come from A's research (#528, criterion 5); this spec records the tsgo checker size per platform. Version numbers are verified, not copied.
2. **IR validator.** The prototype's builder, IR and validator move out of `scripts/workflow-proto/`, keeping the doc-drift CI check. `validate` rejects:
   - every case the design lists, including references to non-upstream nodes;
   - an agent with no ACP route, or a mode its catalog entry doesn't list;
   - and runs again on the stored IR before every run.
3. **Interpreter.** Ready, run-or-skip, subgraph, loop and failure rules behave as the design's Run semantics state. Loops respect `max_iters`; `map` respects its concurrency.
4. **Node kinds.** `agent`, `check`, `human`, `group`, `loop` and `map` run end to end, including `reviewLoop`, `fanOutThenVerify` and `planImplementVerify`.
5. **Durability, two tests.**
   - *Plugin crash:* an agent node that finished while the plugin was down is recorded from the ACP transcript and its attempt count is unchanged. A check node is marked `interrupted` and retried.
   - *`hived` restart or reboot:* every running node becomes `interrupted` and is retried up to `max_attempts` (default 2). Retry uses `session/load` when the agent reports `loadSession`; otherwise a fresh session in the same worktree with an "interrupted" note in the prompt.
   - Completed nodes never re-run.
6. **Agent nodes.**
   - One worktree per node, branching from upstream; `worktree: { of }` shares one (through A's worktree frame).
   - After a `map`, a merge worktree; a conflict fails the node and the failure states the recovery path.
   - A turn that ends (`end_turn`) without `submit_result` is re-prompted once; a second miss fails the node.
   - `session/request_permission` becomes `node.waiting` with reason `permission`.
   - Mode is capped at the user's per-agent setting.
7. **Caps and cancel.** Runaway caps (32 map copies, 4 live agent nodes, 50 turns per run) fail the run naming the cap. Cancel waits 10s for `stopReason: cancelled`, then stops. Check nodes get SIGTERM then SIGKILL, with the Windows behaviour specified and `windowsHide` on check spawns.
8. **Branch names.** The engine uses A's ref-name encoding for keys like `fix[2]/work` and `work/each[3]/do`.
9. **Workflow trust.** Each behaviour in the design's Security section has its own test:
   - opening a project builds nothing;
   - a symlink anywhere in the folder fails the snapshot;
   - the build runs from a private 0700 snapshot under `plugin-data/<id>/` after a hash match, with a length-prefixed hash over paths and bytes;
   - the trust prompt returns when the hash changes;
   - the build is refused when `--permission` is unavailable, with no fallback;
   - `node:` and out-of-folder imports are refused;
   - the build environment is `PATH`, `LANG`, `TZ` plus the snapshot variable (no `HOME`, no `HIVE_SOCKET`);
   - `runs.db` and its WAL files are created 0600;
   - prompt interpolation is fenced by a per-run nonce, and values never reach argv, paths, modes or agent ids;
   - secrets are redacted before events are stored, and the event records that.
   - The codex-acp treatment of a never-trusted path is checked (#505 follow-through).
10. **Events.** Run events are keyed by IR node id with a gapless `seq`, `agent.message` coalesced, as the design specifies.
11. **Type check.** A type error blocks the run. With no usable Node, the run is refused with the required version.
12. **Rerun semantics.** A run interprets only its stored IR; a changed source is detectable by its hash.
13. **Risks handled.** Worktrees created per loop iteration and map copy have a cleanup policy that respects the `ErrWorktreeDirty` refusal. A failed plugin (A, criterion 9) leaves runs in a defined, resumable state. Plugin rate limiting under 16-way fan-out is measured, not assumed.
14. **Docs.** `DESIGN.md` for the new plugin and its data, and `docs/plugins.md`.

## Non-goals

- The GUI (C).
- Daemon, wire and SDK changes (A).
- An `llm` node, a durability service, resuming an agent mid-turn, installable workflow packages, a workflow editor.
- Error edges, unless a worked example needs them.
- Hivesmith as baseline, host or evidence.

## Notes

- Preconditions: gate B (#503) passed; #505 fixed in #510.
- Re-check the framework threshold only if LangGraph.js gains static data-only conditional edges and a non-native SQLite saver.
