---
issue: 529
title: "Workflow engine B: headless engine core (validator, interpreter, checkpoint log, trust sandbox)"
type: enhancement
complexity: L
priority: P1
stage: PLAN
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
   - a mode above the user's ceiling, or an agent the user's settings make
     unavailable, through A's `LIST_ACP_AGENTS` (#528), before the run
     starts;
   - and runs again on the stored IR before every run.
3. **Interpreter.** Ready, run-or-skip, subgraph, loop and failure rules behave as the design's Run semantics state. Loops respect `max_iters`; `map` respects its concurrency.
4. **Node kinds.** `agent`, `check`, `human`, `group`, `loop` and `map` run end to end, including `reviewLoop`, `fanOutThenVerify` and `planImplementVerify`.
5. **Durability, two tests.**
   - *Plugin crash:* an agent node that finished while the plugin was down is recorded from the ACP transcript and its attempt count is unchanged. A check node is marked `interrupted` and retried.
   - *`hived` restart or reboot:* every running node becomes `interrupted`. It is retried with a fixed limit of 2 attempts, an engine constant; the IR has no `max_attempts` field.
     - `loadSession` is not on the wire, so the engine doesn't read it. The daemon's boot revive (or RESTART_SESSION) does the `session/load`, and the engine acts on the outcome: a revived session is re-prompted. When the reload fails (the agent has no `loadSession`), the engine starts a fresh session in the same worktree.
     - Either way, the prompt carries an "interrupted" note.
   - A plugin crash and a `hived` restart are told apart by A's `boot_id`.
   - A result submitted while the plugin was down, then lost to a `hived` restart before the engine read it, is retried. It was never recorded `done`, so this is not a re-run.
   - Completed nodes never re-run.
6. **Agent nodes.**
   - One worktree per node, branching from upstream; `worktree: { of }` shares one (through A's worktree frame).
   - Each agent node's work is committed through A's `COMMIT_WORKTREE` before downstream nodes branch from it.
   - A node with several upstream heads, after a `map` or a join, gets a merge worktree.
   - A loop's iteration *k > 1* branches from the same node's iteration *k−1*.
   - A merge conflict fails the node. The failure states the recovery path: resolve in the kept worktree, then `run.retry`.
   - A turn that ends (`end_turn`) without `submit_result` is re-prompted once; a second miss fails the node.
   - `session/request_permission` becomes `node.waiting` with reason `permission`.
   - A mode is never above the user's per-agent ceiling.
     - The daemon **refuses** a mode above it; it never clamps.
     - The engine checks the ceiling before the run, and always sends an explicit mode: the node's, or the least permissive one when the node names none.
   - When a run ends, its node sessions are archived.
7. **Caps and cancel.**
   - 32 map copies and 50 agent turns per run fail the run, naming the cap.
   - 4 live agent nodes per run is a **scheduler throttle**, not a failure, because a `map` allows `concurrency` up to 16. Cancel waits 10s for `stopReason: cancelled`, then stops. Check nodes get SIGTERM then SIGKILL, with the Windows behaviour specified and `windowsHide` on check spawns.
8. **Branch names.** The engine uses A's ref-name encoding for keys like `fix[2]/work` and `work/each[3]/do`.
9. **Workflow trust.** Each behaviour in the design's Security section has its own test:
   - opening a project builds nothing;
   - a symlink anywhere in the folder fails the snapshot;
   - the build runs from a private 0700 snapshot under `plugin-data/<id>/` after a hash match, with a length-prefixed hash over paths and bytes;
   - the trust prompt returns when the hash changes;
   - the build is refused when `--permission` is unavailable, with no fallback. On the Node 26 floor this cannot happen in production, so it is tested through a seam: a fake build `node` whose runner reports no permission model;
   - `node:` and out-of-folder imports are refused;
   - the build environment is `PATH`, `LANG`, `TZ` plus the snapshot variable (no `HOME`, no `HIVE_SOCKET`);
   - `runs.db` and its WAL files are created 0600;
   - prompt interpolation is fenced by a per-run nonce, and values never reach argv, paths, modes or agent ids;
   - secrets are redacted before events are stored, and the event records that.
   - **#505 follow-through (codex-acp and never-trusted paths).** #510 found that codex-acp marks every cwd trusted (`internal/registry/acp.go:1043-1045`), which a fake adapter cannot exercise.
     - The test is that every engine session runs only in a worktree of the run's own project, which Hive treats as trusted, so the engine never hands Codex a never-trusted path.
     - Re-probing the pinned adapter needs a real run, which spends the user's subscription, and is only done with their approval.
   - The sandbox's remaining gap is accepted and documented in `docs/plugins.md`: a build child can still signal, or open the inspector of, other same-user Node processes.
   - tsgo is fetched once, as a tarball, and verified against an integrity hash pinned in Hive's source.
10. **Events.** Run events are keyed by IR node id with a gapless `seq`, `agent.message` coalesced, as the design specifies. `agent.usage` comes from the usage A adds to the ACP transcript.
11. **Type check.** A type error blocks the run.
    - "No usable Node is refused, naming the version" is A's criterion 5: the Manager refuses the plugin before the engine starts.
    - B's share is the manifest's `node: "26.0.0"`, and a test that pins it.
12. **Rerun semantics.** A run interprets only its stored IR; a changed source is detectable by its hash.
13. **Risks handled.** Worktrees created per loop iteration and map copy have a cleanup policy that respects the `ErrWorktreeDirty` refusal. A failed plugin (A, criterion 9) leaves runs in a defined, resumable state, and `run.retry` resumes a failed or cancelled run.
    - 16-way fan-out is a benchmark with a stated threshold (throttle under 10 s, no failed node), not a pass/fail claim.
    - It covers the plugin token bucket and the transcript listener's overflow-and-resync.
14. **Docs.** `DESIGN.md` for the new plugin and its data, and `docs/plugins.md`.

## Non-goals

- The GUI (C).
- Daemon, wire and SDK changes (A). B's needs from A (`COMMIT_WORKTREE`, `boot_id` and `prompt_id` on the ACK, the RESTART_SESSION ACK, `base_sha`, transcript usage, `LIST_ACP_AGENTS`) are recorded in B's exec plan and approved as A changes.
- An IR `max_attempts` field.
- An `llm` node, a durability service, resuming an agent mid-turn, installable workflow packages, a workflow editor.
- Error edges, unless a worked example needs them.
- Hivesmith as baseline, host or evidence.

## Notes

- Preconditions: gate B (#503) passed; #505 fixed in #510.
- Re-check the framework threshold only if LangGraph.js gains static data-only conditional edges and a non-native SQLite saver.
