---
issue: 528
title: "Workflow engine A: daemon and SDK prerequisites (provenance, typed results, ACP cancel, plugin request/reply)"
type: enhancement
complexity: M
priority: P1
stage: PLAN
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

Every wire change moves `internal/wire`, `cmd/hivegui/app_*.go`, `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient` in lock-step, with `snake_case` tags. The work ships as three PRs (see the exec plan). Each bumps `buildinfo.DaemonContract` only if a GUI built against it could not correctly drive an older daemon ([daemon-contract.md](../design-docs/daemon-contract.md)); PR1 and PR2 do, and PR3 decides by the same rule.

1. **Provenance.** The daemon labels a plugin's sessions and prompts with a composed principal, `plugin:<id>/workflow:<run-id>`. It builds it from its own principal (`principalOf`, `internal/daemon/daemon.go:2307`) plus a run id the plugin sends. The composition covers CREATE_SESSION, the create HELLO and PROMPT_ACP.

   This is an integrity label, not a defence: plugins already run with the user's access. Tests:
   - a plugin cannot claim a user principal or another plugin's;
   - `Orchestrator` stays false (#524);
   - the transcript labels the origin (`AcpTranscript.tsx`), and prompt origin (`internal/registry/acp.go`, `daemon.go:1706`) carries the run.
2. **Typed results.** `CreateSpec` accepts a per-node result schema.
   - `hived` validates `submit_result` against it in `Registry.SubmitResult`, with a hand-written validator for the IR's schema subset (object, string and enum, number, boolean, array) and no new dependency.
   - `cmd/hived/mcpsubmit.go` and `internal/agent/pi/hive.ts` show the schema to the agent, and the agent can retry on a validation error.
   - The `{status, summary, data}` envelope is kept, and the schema applies to `data`.
3. **ACP cancel.** `session/cancel` in `internal/acp`, and an idempotent frame a plugin can send. A cancelled turn reports `stop_reason: cancelled` on `ACP_TRANSCRIPT`, which carries the latest prompt's stop reason.
4. **Request/reply.** Control requests carry an optional `req_id`, echoed on ERROR and on an ACK reply. CREATE_SESSION's ACK carries the new session id. `plugins/sdk/hive-plugin.mjs` correlates on reply frames only, never on broadcasts. Covered by an SDK test and documented in `docs/plugins.md`.
5. **Node for headless plugins.**
   - Research pinned the Node facts with sources:
     - the engine floor is Node 26.0.0;
     - `--permission` fences the network only from 25.0.0 and is defence-in-depth, not a boundary;
     - `--disable-sigusr1` is available;
     - `node:sqlite` is a release candidate (JSONL is the fallback).

     B consumes these.
   - A headless bundled plugin starts on a Node it can find, through the login PATH (`internal/plugin/manager.go:687`).
   - A Node older than a plugin's declared floor yields a visible "requires Node X" state rather than a silent failure.
   - The Node 18 wording is corrected at all five sites (`docs/plugins.md:81,317`, both SDK headers, `plugins/sdk/README.md`) to "a supported LTS, 22 or newer".
   - CI gains a Node 26 leg.
6. **Plugin ⇄ GUI messages.** A two-way frame carries plugin-defined messages between a plugin and GUI clients, capped at 64 KiB, with no `req_id`. `PLUGIN_EVENT` today carries lifecycle events only (`internal/wire/control.go:1809`), and nothing reaches a plugin from the GUI.
7. **Plugin-owned data.** `DESIGN.md` allows a plugin to write its own files under `plugin-data/<id>/` (today "written only through the plugin Manager", `DESIGN.md:100`). The carve-out states what the plugin may not do: worktree creation and merging stay behind a daemon frame, since the registry and worktree packages own them.
8. **Worktree frame.**
   - A plugin can ask the daemon to create a worktree on a `hive/wf/<run>/…` branch from an arbitrary base. The base is resolved to a commit, and git receives the SHA.
   - It can re-attach an existing run branch.
   - It can merge several node branches into a merge worktree, with a conflict reported as a typed failure.
   - Every git call runs with hooks and fsmonitor off.
   - The SDK encodes loop and map keys to valid refs (`[` is forbidden by `git check-ref-format`); a test covers `fix[2]/work` and `work/each[3]/do`. The daemon checks the run prefix and `check-ref-format`.
9. **Crash-loop recovery.** After more than 5 crashes in 60s a plugin is marked failed and stops restarting (`internal/plugin/manager.go:28`). The state is already visible in Settings → Plugins. The work is a recovery action that works on an already-enabled plugin (Restart), plus a toast when a plugin fails.

## Non-goals

- The engine itself, its trust sandbox and any GUI beyond the failed-plugin state (B and C).
- Bumping `wire.PROTOCOL_VERSION` for a new frame; see `DESIGN.md`.
- Changing 492's trust model or reopening #505 (fixed in #510).

## Notes

- #525 `WAIT_SESSION` and `LIST_SESSIONS` are session-mode only and don't serve plugins.
- Design citations were rechecked on HEAD `f51c0f01`: line numbers moved, claims hold. Research should re-verify.
- `docs/plugins.md` also needs the new SDK call documented.
