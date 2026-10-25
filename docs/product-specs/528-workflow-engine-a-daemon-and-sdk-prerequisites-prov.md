---
issue: 528
title: "Workflow engine A: daemon and SDK prerequisites (provenance, typed results, ACP cancel, plugin request/reply)"
type: enhancement
complexity: M
priority: P1
stage: RESEARCH
---

# Workflow engine A: daemon and SDK prerequisites (provenance, typed results, ACP cancel, plugin request/reply)

- **Issue:** #528
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Parent:** [527](527-build-the-workflow-engine-bundled-plugin-running-typ.md)
- **Design:** [workflow-engine.md](../design-docs/workflow-engine.md), "What the engine spec must add"

## Problem

The workflow engine (a bundled plugin) needs daemon, wire and plugin-SDK capabilities `main` doesn't have. Typed results and ACP cancel are also missing for ordinary `acp` sessions, so they are worth shipping independently of workflows.

## Desired behavior

A plugin can start `acp` sessions that carry a run's provenance and a per-node result schema, cancel them, correlate replies to its requests, run on a Node it can find, push events to GUI clients, and own its data under `plugin-data/<id>/`.

## Success criteria

Every wire change moves `internal/wire`, `cmd/hivegui/app_*.go`, `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient` in lock-step, with `snake_case` tags. Each daemon-side PR bumps `buildinfo.DaemonContract`.

1. **Provenance.** A plugin-created session is stamped with a composed principal, for example `plugin:<id>/workflow:<run-id>`. The daemon builds it from its own principal plus a run id the plugin sends (`principalOf`, `internal/daemon/daemon.go:2305`). Tests:
   - a plugin cannot claim a user principal or another plugin's;
   - `Orchestrator` stays false (#524);
   - the transcript labels the origin (`AcpTranscript.tsx` recognises it) and prompt origin (`internal/registry/acp.go`) handles it.
2. **Typed results.** `CreateSpec` accepts a per-node result schema. `hived` validates `submit_result` against it, in `cmd/hived/mcpsubmit.go` and `internal/agent/pi/hive.ts`, and the agent can retry on a validation error. The spec's plan decides between a hand-written validator for the IR's schema subset (object, string and enum, number, boolean, array) and a new dependency; `go.mod` has no JSON Schema library today.
3. **ACP cancel.** `session/cancel` in `internal/acp`, and a frame a plugin can send. A cancelled turn reports `stopReason: cancelled`.
4. **Plugin SDK request/reply.** `plugins/sdk/hive-plugin.mjs` correlates replies to requests with a client counter echoed on the reply, as `req_id` does in `internal/wire/transcript.go`. Covered by an SDK test, and documented in `docs/plugins.md`.
5. **Node for headless plugins.** Research pins the Node floor and records, with verified versions, whether it fences the network under `--permission` and the `node:sqlite` stability tier (JSONL is the fallback); B consumes these. A headless bundled plugin starts on a Node it can find, with the login PATH or its own lookup (`internal/plugin/manager.go:685`). A Node older than the engine's floor yields a visible "requires Node X" state rather than a silent failure. `docs/plugins.md` (Node 18+ at two places) is corrected.
6. **Plugin to GUI events.** A frame carries plugin-defined run events to GUI clients. `PLUGIN_EVENT` today carries lifecycle events only (`internal/wire/control.go:1800`).
7. **Plugin-owned data.** `DESIGN.md` allows a plugin to write its own files under `plugin-data/<id>/` (today "written only through the plugin Manager", `DESIGN.md:100`). The carve-out states what the plugin may not do: worktree creation and merging stay behind a daemon frame, since the registry and worktree packages own them.
8. **Worktree frame.** A plugin can ask the daemon to create a worktree branched from an arbitrary upstream branch (`worktree.CreateWorktreeAt` already takes a base) and to merge several node branches into a merge worktree, with a conflict reported as a typed failure. No merge function exists in `internal/worktree` today. Ref names for loop and map keys are encoded to valid refs (`[` is forbidden by `git check-ref-format`); a test covers `fix[2]/work` and `work/each[3]/do`. Without this frame, B would need a second daemon PR and another contract bump.
9. **Crash-loop visibility.** After more than 5 crashes in 60s a plugin is marked failed and stops restarting (`internal/plugin/manager.go:28`). The state is visible to the user, with a recovery action.

## Non-goals

- The engine itself, its trust sandbox and any GUI beyond the failed-plugin state (B and C).
- Bumping `wire.PROTOCOL_VERSION` for a new frame; see `DESIGN.md`.
- Changing 492's trust model or reopening #505 (fixed in #510).

## Notes

- #525 `WAIT_SESSION` and `LIST_SESSIONS` are session-mode only and don't serve plugins.
- Design citations were rechecked on HEAD `f51c0f01`: line numbers moved, claims hold. Research should re-verify.
- `docs/plugins.md` also needs the new SDK call documented.
