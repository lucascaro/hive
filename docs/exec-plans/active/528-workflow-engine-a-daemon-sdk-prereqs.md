# Workflow engine A: daemon and SDK prerequisites

- **Spec:** [docs/product-specs/528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md](../../product-specs/528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md)
- **Design:** [workflow-engine.md](../../design-docs/workflow-engine.md) ("What the engine spec must add"), [acp-workflows.md](../../design-docs/acp-workflows.md)
- **Issue:** #528
- **Branch:** `feature/528-daemon-sdk-prereqs` (research/plan); one branch per PR below
- **Status:** active

## Summary

This plan gives a plugin what the workflow engine (B, #529) and its GUI
(C, #530) need from `hived`, the wire and the plugin SDK:
- correlated request/reply;
- a run-scoped provenance label;
- a per-session `submit_result` schema validated by `hived`;
- ACP cancel with a visible stop reason;
- a two-way plugin ⇄ GUI message channel;
- worktrees from an arbitrary base, plus a merge worktree;
- a Node the plugin can find, with a version floor;
- a recoverable crash-loop state;
- the `plugin-data/<id>/` carve-out in `DESIGN.md`.

It ships as **three PRs**. Each bumps `DaemonContract` when a GUI built against it could not drive an older daemon (PR1 and PR2 do; PR3 decides by the same rule).

## Research

Verified on `origin/main` at `0bf4e725` (2026-10-25); line numbers are
against that commit.

### Relevant code (the review findings, each checked)

| Claim | Verified at | Note |
|---|---|---|
| `principalOf` returns `plugin:<id>` | `internal/daemon/daemon.go:2307` | Holds. |
| `SpawnedBy` stamps | `daemon.go:806` (ModeCreate HELLO), `daemon.go:1556` (CREATE_SESSION) | Holds. `Orchestrator` is forced false for plugins (`:808`, `:1560`), and UPDATE_SESSION refuses it (`:1895`). |
| "Nothing a client sends can set principal" | `daemon.go:1431`; `wire.CreateSpec.SpawnedBy` is `json:"-"` (`internal/wire/control.go:141`) | Holds. |
| PROMPT_ACP origin = `ops.principal` | `daemon.go:1706` | **The spec missed it.** B's re-prompts would read as `plugin:<id>`. |
| Initial-prompt origin = `spec.SpawnedBy` | `internal/registry/acp.go:266` | Holds. |
| `originLabel` handles `plugin:` | `cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx:209-212` | A composed principal would render as "From plugin engine/workflow:r1". |
| SDK `send()` has no correlation | `plugins/sdk/hive-plugin.mjs:365` | Holds. |
| `req_id` pattern | `internal/wire/transcript.go:77,102,121,164` | Echoed only on its own reply frames (`TRANSCRIPT_MATCHES`, `TRANSCRIPT_LINES`). **`wire.Error` (`control.go:1379`) has no `req_id`.** |
| CREATE_SESSION has no reply | `control.go` `IdeaID` comment: "nothing correlates the SESSION_EVENT(added) … with the request" | **Spec gap.** A plugin can't learn the id of the session it created. |
| `Registry.Create` blocks | `internal/registry/create.go:108` returns `e, r.finishCreate(…)`; covers worktree setup, a possible user prompt, adapter start and opening prompt | The ACK must not wait for it. |
| `resolveCommand` uses the daemon PATH | `internal/plugin/manager.go:687-701` | Holds. `pluginEnv` (`:704`) passes the daemon env through. |
| Login-shell PATH helper | `internal/proc/loginpath_unix.go:69` `LoginPATH`; `proc.LookPathIn`; used by `internal/agent/acp.go:120,157-171` | Reuse it. |
| `maxCrashes = 5` in 60s → failed | `manager.go:28-31`, `:616-619` | Holds. `failed` and its detail already show in Settings → Plugins (`PluginsPanel.tsx:101-105,292`). |
| Recovery from `failed` | `SetEnabled(true)` starts a fresh runner when `e.run == nil` (`manager.go:396-428`) | **No UI path.** The checkbox is already checked. |
| PLUGIN_EVENT is lifecycle only | `control.go:1807-1814` | Holds. **No GUI→plugin path exists**: `CLIENT_COMMAND` is an allowlist relay (`control.go:1484-1525`). |
| Fixed submit schema | `cmd/hived/mcpsubmit.go:44-57,137-145`; `internal/agent/pi/hive.ts:279-288,688-706` | Holds. The authority is `Registry.SubmitResult` (`internal/registry/acp.go:736`). |
| No `session/cancel` | `internal/acp/agent.go`; `Conn` (`conn.go:174`) has `Call`, no notification send | Holds. `acp.Cancelled()` (`types.go:255`) answers a pending permission. |
| `stopReason` not on the wire | `registry/acp.go:697-723`, folded into agentstate text only | **Spec gap.** |
| `CreateWorktreeAt(…, base)` | `internal/worktree/worktree.go:220` | **Falls back to checking out an existing branch** (`:229`, `:256`). No merge function. |
| Path from branch | `WorktreePath` / `sanitizeBranch` (`worktree.go:57,553`) | Maps `/`→`-`, so it is not injective. `ResolveBranchAndPath` (`:518`) suffixes the branch *and* the path. |
| Orphan reclaim | `registry/projects.go:157` | Deletes **pristine, sessionless** worktrees at boot. |
| Cleanup frames exist | `REMOVE_WORKTREE` (0x18), `DELETE_BRANCH` (0x1b); `registry/worktrees.go` honours `ErrWorktreeDirty`, `ErrBranchUnmerged` | B's cleanup uses these. A adds none. |
| `plugin-data` rule | `DESIGN.md:100`: "written only through the plugin Manager" | Holds. |
| Plugin trust | `docs/plugins.md:32-40`: "A plugin runs as you, with everything you can do" | Frames scoped to the user's reach add no privilege. |
| Node 18 in docs | `docs/plugins.md:81,317`, `plugins/sdk/hive-plugin.mjs:6`, `plugins/webhook/hive-plugin.mjs:6`, `plugins/sdk/README.md:4` | Five places, not two. |
| Embed "UI-only" | `plugins/embed.go:16` embeds `plan-review` | The mechanism (`internal/plugin/builtin.go`) is content-agnostic. A test is enough. |
| Frames | last is `FrameWaited = 0x4e` (`internal/wire/frame.go:294`) | New frames start at `0x4f`. |
| DaemonContract | `internal/buildinfo/contract.go:285` = 33 | Each PR takes main's value +1 at rebase. |
| Plugin costs | `internal/plugin/limiter.go` `FrameCost` | New C→S frames need entries. |
| Plugins see every ACP transcript | `registry.SubscribeACP` (`acp.go:861`), unfiltered | B reads deltas directly. |
| SDK drift tests | `internal/plugin/sdk_drift_test.go`: frame table; typedefs (`HiveError` = `wire.Error`); the webhook copy must be byte-equal | Every SDK change is copied to `plugins/webhook/`. |
| Fake ACP adapter | `internal/acp/acptest/fake.go:194`: one goroutine per message | A cancel notification is seen during a `FlagBlock` prompt. |
| IR schema subset | `scripts/workflow-proto/schema.ts` | string(+enum), number, boolean, array(items), object(properties, required = all keys, `additionalProperties:false`). |
| IR id grammar | `scripts/workflow-proto/dsl.ts:282` `ID_SEGMENT`; keys add `[N]` | `_` never appears in a segment. |
| CI Node | `.github/workflows/ci.yml:113` `node-version: '24'` | A Node 26 leg is needed. |

### Node facts (criterion 5; B consumes these)

All accessed **2026-10-25**.

| Fact | Value | Source |
|---|---|---|
| Permission model | Stable since v23.5.0 / v22.13.0; added v20.0.0 | [permissions (v26.12.1)](https://nodejs.org/api/permissions.html) |
| `--permission` fences the network | Only from **v25.0.0** (`--allow-net` "Added in: v25.0.0", Stability 1.1) | [cli (v26.12.1)](https://nodejs.org/api/cli.html#--allow-net) |
| …but not in 24.x or 22.x | v24.22.1 and v22.23.4 docs have no `--allow-net`, and network is not a restricted resource there | [v24 cli](https://nodejs.org/docs/latest-v24.x/api/cli.html), [v24 permissions](https://nodejs.org/docs/latest-v24.x/api/permissions.html), [v22 permissions](https://nodejs.org/docs/latest-v22.x/api/permissions.html) |
| It is a "seat belt", not a boundary | "It does not provide security guarantees in the presence of malicious code." `process._debugProcess(pid)` "is not gated by the `kInspector` scope or any other Permission Model scope". A sandboxed process can force any same-user Node process to open its inspector. | [permissions (v26.12.1)](https://nodejs.org/api/permissions.html) |
| `--disable-sigusr1` | "Added in: v23.7.0, v22.14.0"; no longer experimental in v24.8.0 / v22.20.0. It disables starting a debug session via `SIGUSR1`. | [cli (v26.12.1)](https://nodejs.org/api/cli.html#--disable-sigusr1) |
| `node:sqlite` | Unflagged v22.13.0 / v23.4.0, "still experimental"; **1.2 Release candidate** from v25.7.0 and v24.15.0 | [sqlite (v26.12.1)](https://nodejs.org/api/sqlite.html), [sqlite (v24.22.1)](https://nodejs.org/docs/latest-v24.x/api/sqlite.html) |
| Release lines | 22 Maintenance LTS, EOL 2027-04-30. 24 LTS, EOL 2028-04-30. 25 EOL 2026-06-01. **26 Active LTS from 2026-10-28**, EOL 2029-04-30. From 27, one major per year. | [nodejs/Release](https://github.com/nodejs/Release) |

**Conclusions (user decision, final):**
- **Engine floor: Node ≥ 26.0.0.** `node:sqlite` is RC there, so B uses SQLite; JSONL is only a fallback if a future line regresses.
- **`--permission` is defence-in-depth, not a security boundary.** It wraps a workflow file the user has explicitly trusted and that is hash-pinned (B's trust model). Node's own docs say it doesn't stop malicious code, and `process._debugProcess` is ungated.
- **The engine process runs with `--disable-sigusr1`**, so a sandboxed build child can't force it to open an inspector. The flag belongs in B's manifest `main` command (`["node", "--disable-sigusr1", "./main.mjs"]`). A documents it in `docs/plugins.md`. A build child under `--permission` creates no `SIGUSR1` listener anyway.
- **Other plugins:** "a supported Node.js LTS (22 or newer)". The SDK uses only `node:net` / `node:events`.

### Constraints

- The lock-step wire rule applies; tags are `snake_case`. `PROTOCOL_VERSION` is not bumped. `DaemonContract` is bumped by every PR that meets that rule.
- Subprocesses go through `proc` only. `LoginPATH` is never called under `m.mu`.
- The registry owns worktrees and session state. After the carve-out, a plugin owns only `plugin-data/<id>/`.
- New broadcasts follow `docs/design-docs/slow-client-policy.md`.

### Gaps B will hit (not A's scope)

- `loadSession` isn't on the wire. B retries through RESTART_SESSION, which already does `session/load` (`registry/acp.go:376-379`), and falls back to a fresh session on `restart_failed`.
- An ACP mode above the user's ceiling is **refused**, not clamped. B must request ≤ ceiling or omit the mode.
- Cleanup goes through the existing REMOVE_WORKTREE / DELETE_BRANCH.

### Prior lessons

None matched (`brain-search`, two queries).

### Conventions card

```
build:  ./build.sh
test:   scripts/test.sh     # all layers: go · unit · dom · e2e
e2e-real: (cd cmd/hivegui/frontend && npm run test:e2e:real)
```

- **TDD:** each behaviour change ships with its test. Go tests sit beside their source; frontend tests live under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- **Wire changes** move together: `internal/wire`, `cmd/hivegui/app_*.go`, `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient`, the SDK `Frame` table and typedefs, and `plugins/webhook/hive-plugin.mjs` (a byte copy).
- **CI toolchain:** use `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`, run staticcheck and vet per GOOS, and run `./scripts/ci-bootstrap.sh` before `npm run typecheck`. Playwright runs with `CI=1`.
- **E2E isolation:** temp `HIVE_SOCKET` / `HIVE_STATE_DIR`.
- **Docs:** a changeset per user-visible change. Never edit `CHANGELOG.md` or `docs/product-specs/index.md`. Update `DESIGN.md` for the carve-out and `docs/plugins.md` for every frame and SDK call.

## Approach

### Interface surface (what B and C plan against)

Frame numbers are provisional; each PR takes the next free number on
`main` at rebase.

#### PR1: request/reply, provenance, typed results, ACP cancel

**Request correlation**

- **`req_id`:** any control request may carry an int `req_id` (omitempty).
  - The daemon peeks it once per frame. Every ERROR sent while handling that frame echoes it, including errors raised later from `runOp`.
  - `wire.Error` gains `ReqID int json:"req_id,omitempty"`.
- **`ACK` (0x4f, S→C):** `{req_id, session_id?, was_busy?}`. It is sent only for a non-zero `req_id`, to the requester only. Frames that ACK:
  - **CREATE_SESSION**, as soon as the entry exists.
    - `registry.Create` is split into exported `BeginCreate(ctx, spec) (*Entry, createPlan, error)` (entry exists and is broadcast as `added`) and `FinishCreate(ctx, e, spec, plan) error`. The daemon ACKs between the two.
    - Any later failure is an ERROR with `req_id` and `session_id`, including today's silent `ErrNotFound` "killed mid-create" case.
    - B watches readiness through SESSION_EVENT / phase.
  - **PROMPT_ACP**, once the turn has started.
  - **ANSWER_PERMISSION** and **KILL_SESSION**.
  - **CANCEL_ACP**, with `was_busy` (absent = false on the wire).
  - **RESTART_SESSION** (R3), `{req_id, session_id}` once `Registry.Restart` returns nil. A failure is `restart_failed` carrying the `req_id`.
- **PROMPT_ACP's ACK also carries `prompt_id` and `epoch`** (R2): the transcript item id of the turn just started and the transcript epoch it is in. Item ids and epochs restart with the daemon, so:
  - **`WELCOME.boot_id`** (R2) is 16 random bytes in hex, chosen once per `hived` process and sent on every WELCOME. A plugin pairs `(boot_id, epoch, prompt_id)` to tie a result to its turn and to tell "the daemon restarted" from "the plugin crashed".
- **Request fields:** `CreateSpec`, `PromptAcpReq`, `AnswerPermissionReq`, `KillSessionReq` and `RestartSessionReq` gain `ReqID`.
- **SDK `hive.request(name, payload = {}, { timeoutMs = 30000 } = {})`:**
  - It assigns `req_id` from a per-connection counter.
  - It resolves **only on a reply frame type**: `ACK`, `PREPARED_WORKTREE`, `TRANSCRIPT_MATCHES` or `TRANSCRIPT_LINES`, carrying that `req_id`. It rejects on `ERROR` carrying it (`err.hive = {code, message, …}`).
  - Broadcasts (`SESSION_EVENT`, `ACP_TRANSCRIPT`, `PLUGIN_MESSAGE`, …) never settle a request, whatever their payload holds.
  - Timeout or `close` rejects everything pending. `send()` is unchanged.

**Provenance (an integrity label, not a defence)**

- `CreateSpec`, `PromptAcpReq` and the ModeCreate HELLO's spec gain `workflow_run_id`, matching `^[a-z0-9][a-z0-9-]{0,62}$`.
- On a plugin connection the daemon labels `SpawnedBy` / the prompt origin `plugin:<id>/workflow:<run-id>`.
  - From a non-plugin connection it is refused with `mode_not_allowed`.
  - A bad charset is refused with `invalid_workflow_run_id`.
  - `Orchestrator` stays false.
- The label exists so the transcript and the user see truthfully which plugin and run sent a turn. A plugin can't mislabel itself as the user or as another plugin.
  - The label is not a boundary. A plugin already runs with the user's full access (`docs/plugins.md:32-40`).
- `wire.ParsePrincipal(s) (plugin, run string)` is the one parser, mirrored by `src/lib/principal.ts`.
- The transcript label reads "From workflow `<run>` (plugin `<id>`)".

**Typed results**

- **Schema field:** `CreateSpec.ResultSchema json.RawMessage json:"result_schema,omitempty"`, ACP only. On a PTY session it is refused with `not_acp_session`.
- **Checked before `BeginCreate`**, so a refused create leaves no entry. The schema must be:
  - exactly the IR subset;
  - an object at the top level;
  - `required` = all keys and `additionalProperties:false`;
  - depth ≤ 8 and ≤ 8 KiB.

  Anything else is refused with `invalid_result_schema`.
- **Persistence:** it is stored on the entry and its persisted meta, so revive and restart keep it.
- **What is validated:** the envelope `{status, summary, data}` is kept. With a schema and `status:"ok"`, `data` is required and is validated against it. `status:"error"` is not validated, so F3 is unchanged.
- **Enforcement:** `Registry.SubmitResult` is the single authority. Its path-qualified error (`data.findings[2].severity: "urgent" is not one of high|low; call submit_result again`) reaches the model as a tool error, so the agent retries.
- **How the tools see it:** the schema reaches `mcp-submit`'s `tools/list` and Pi's tool parameters through `HIVE_SUBMIT_SCHEMA`, beside `HIVE_SUBMIT_NONCE` in `acpExtras` (`registry/acp.go:213`). `mcp-submit` pre-checks with the same Go validator.
- **Shared fixtures:** `internal/acp/testdata/resultschema/*.json` (`{schema, value, ok, error?}`). B's TS validator reuses them.

**ACP cancel**

- **`CANCEL_ACP` (0x50, C→S):** `{session_id, req_id?}`.
  - Unrestricted, like PROMPT_ACP: the user or any plugin.
  - Not in `sessionModeFrames`, so a session connection can't send it.
  - **Idempotent.** With a turn running, it answers any pending permission with `acp.Cancelled()`, sends the `session/cancel` **notification** ([ACP prompt turn, Cancellation](https://agentclientprotocol.com/protocol/prompt-turn)), and ACKs `{was_busy: true}`. With none running it ACKs `{was_busy: false}`. It never errors on "not busy".
- **`AcpTranscriptMsg.StopReason`** (`stop_reason`): the latest prompt's ACP stop reason (`end_turn`, `cancelled`, `max_tokens`, `max_turn_requests`, `refusal`), or `error` if the adapter call failed.
  - Carried on every message like `result_status`, and cleared by a new prompt.
- **`AcpTranscriptMsg.Usage`** (`usage`, R5, `json.RawMessage`, omitempty): the latest `usage_update` payload of the current prompt, verbatim from the adapter, cleared by a new prompt like `stop_reason`. `acp.Update` keeps the raw update (`Raw`) and the registry stores it only while a prompt runs.

**ACP catalog for plugins (R6)**

- **`LIST_ACP_AGENTS` (0x51, C→S) → `ACP_AGENTS` (0x52, S→C)** `{req_id, agents: [{id, name, modes: [{id, label}], default_mode, ceiling, no_approval_gate, experimental, available, reason}]}`, built from `agent.Def.ACP()`, `Settings.ACPCeiling` and `ACPAvailable`.
  - Only agents that can run as ACP are listed.
  - It runs off the read loop (`ACPAvailable` probes the login-shell PATH), is read-only, costs `costCheap`, and is not in `sessionModeFrames`.
  - `ACP_AGENTS` joins `request()`'s reply types.

#### Recorded for PR2 (not built in PR1)

Both come from slice B's plan (#529, R1 and R4), approved by the user. PR2 builds them.

- **`COMMIT_WORKTREE` (R1, C→S)** `{req_id, project_id, workflow_run_id, branch, message}`.
  - The daemon finds the worktree that has `branch` checked out and requires the `hive/wf/<workflow_run_id>/` prefix (`invalid_workflow_branch`).
  - It runs, through `safeGit`, `add -A` then `commit --no-verify -m <message>` with the user's identity (`git_identity_missing`, no fallback).
  - It replies `ACK {req_id, committed, head}`: `committed:false` on a clean tree, a no-op, so the frame is idempotent. `AckMsg` gains `committed` and `head`.
  - Cost `costExpense`. Tests: `TestCommitWorktree_CommitsAll`, `TestCommitWorktree_CleanIsNoop`, `TestCommitWorktree_HooksDoNotRun`, `TestCommitWorktree_RefusesNonRunBranch`.
- **`PREPARED_WORKTREE` gains `base_sha` and `head_sha`** (R4). `base_sha` is the SHA `base` resolved to (empty when `reattached`); `head_sha` is the worktree's HEAD after any merges. B pins every root worktree of a run to the first `base_sha`.

#### PR2: plugin messages and the worktree frame (gets its own security review)

**`PLUGIN_MESSAGE` (0x53, both directions)**

- Payload: `{plugin_id, topic, data}`, with **no `req_id`**. C carries its own nonce in `data` where it needs correlation.
- `topic` matches `^[a-z][a-z0-9._-]{0,63}$`. The whole payload is ≤ **64 KiB** (`wire.MaxPluginMessage`).
- **From a plugin connection:** the daemon overwrites `plugin_id` with the sender's tag and relays to every UI-capable user control client (`tag == nil && canAnswer(hello)`, the GUI and `hived-ws-bridge`). It never relays to other plugins, hivebar or session connections.
- **From a UI-capable user control connection:** `plugin_id` names the target, and the message goes to that plugin's live control connection(s). If none is running: ERROR `plugin_not_running`.
- **From a session connection:** refused.
- GUI→plugin messages add no privilege: anything the GUI can ask a plugin to do, the user can already do, and the plugin already runs with the user's full access.
- **Slow clients:** the relay uses the existing bounded per-connection queue. A GUI that overflows is disconnected under the slow-client policy. It reconnects and re-asks the plugin for state (C's replay topic), so a lost message costs a resync, not a stall.
- **GUI:**
  - Wails event `plugin:message`;
  - binding `App.SendPluginMessage(pluginID, topic, dataJSON string) error`;
  - bridge op `SendPluginMessage` (A-2, approved): CamelCase like every other op in `cmd/hived-ws-bridge/main.go`; the mock (`test/e2e/wails-mock.ts`) and real (`test/e2e-real/wails-bridge.ts`) test bridges export it too;
  - `ControlEventName` maps `FramePluginMessage` to `plugin:message` (A-3, approved), in the shared table in `internal/wire/client.go`, so the GUI and the bridge both emit it.
  - GUI→plugin messages carry no `req_id` (A-1, a `req_id` on them, was rejected).
- **SDK:** `hive.postMessage(topic, data)` and `@typedef PluginMessage`.
- **Cost:** `costFanout`.

**`PREPARE_WORKTREE` (0x54, C→S) → `PREPARED_WORKTREE` (0x55, S→C)**

Request: `{req_id, project_id, workflow_run_id, branch, base, merge?: [branch…]}`

- **`branch`:**
  - Composed by the plugin with the SDK's `encodeWorkflowKey`.
  - The daemon requires the prefix `hive/wf/<workflow_run_id>/` and `git check-ref-format --branch`, and refuses a leading `-`. Otherwise: `invalid_workflow_branch`.
  - **Namespace decision:** the namespace is `hive/wf/<run>/`, not `hive/wf/<plugin>/<run>/`. Run-id collisions across plugins are accepted: one plugin can already do anything another can, and run ids are random.
  - The daemon does not encode keys. The SDK is the only encoder:
    - `encodeWorkflowKey(key)` maps `[N]` to `_N` and keeps `/`;
    - it is injective, because `_` is not in `ID_SEGMENT`;
    - `fix[2]/work` → `fix_2/work`, `work/each[3]/do` → `work/each_3/do`.
- **`base`:** resolved with `git rev-parse --verify --quiet --end-of-options <base>^{commit}`. The daemon passes the **resolved SHA**, never the name, to `worktree add` and to every merge. Any commit-ish is allowed. Unresolvable: `invalid_base`.
- **New branch:** created with a **strict** `worktree.AddNewBranch(ctx, repo, branch, path, sha)`. It runs `git worktree add -b <branch> -- <path> <sha>`, with **no** existing-branch fallback.
- **Existing `hive/wf/<run>/…` branch: re-attach.**
  - If a worktree already has it checked out, return that worktree's path.
  - Otherwise `git worktree add -- <path> <branch>` (`worktree.AddExistingBranch`) and return the new path.
  - This is what lets B re-prepare after a boot reclaim removed a pristine worktree, or after a plugin crash.
  - `base` is then ignored (`reattached: true`).
- **Path:** chosen by the daemon with a path-only collision helper. It suffixes the directory (`-2`, …), never the branch.
- **`merge`:** each branch is resolved to a SHA and merged **sequentially** with `merge --no-ff --no-edit <sha>`.
  - On the first conflict: `diff --name-only --diff-filter=U -z`, then `merge --abort`, then stop.
  - The worktree is **kept**, holding base plus the merges that succeeded, so the user can resolve by hand.
  - A merge already contained in the branch is a no-op ("Already up to date"), so a re-attach can safely repeat `merge`.
- **Every git call** in this path goes through one `safeGit` argument set: `-c core.hooksPath=<empty temp dir> -c core.fsmonitor=false -c commit.gpgSign=false`, `LC_ALL=C`/`LANG=C`, `-z` on path output, and the existing 30s timeouts. That covers rev-parse, check-ref-format, worktree add and merge.
  - Merge drivers and filters from the repo's `.git/config` and `.gitattributes` are **trusted like the rest of the repo**: the user opened it, and their agents already run in it.
- **Merge identity:** merge commits use the user's git identity. A repo with none fails with ERROR `git_identity_missing` and no fallback identity.

Reply: `{req_id, project_id, path, branch, reattached, merged: [branch…], conflict?: {branch, files: [path…]}}`
- Every other failure is an ERROR with `req_id`.
- B treats `branch` and `path` as authoritative.
- Refs are hierarchical, so B gives branches only to leaf keys. A D/F ref clash surfaces as git's error.

#### PR3: plugin runtime and docs

- **Manifest field:** `"node": "<MAJOR.MINOR.PATCH>"` (optional), the minimum Node for `main`.
- **Command lookup:** `resolveCommand` tries the daemon PATH, then `proc.LookPathIn(proc.LoginPATH(), name)`. It returns the PATH it used, and the plugin's `PATH` is set to it.
- **Version probe:**
  - With `node` set, `spawn` resolves `node` on that PATH, not `argv[0]`, and runs `<node> --version` through `proc.CommandContext` (2s timeout).
  - Too old: `PluginRefused`, "requires Node.js 26.0.0 or newer (found v24.11.0 at /usr/local/bin/node)".
  - The probe can't run: `PluginRefused`, "could not run node --version: …".
  - It runs on every start, so upgrading Node and pressing Restart recovers.
- **Restart button:** PluginsPanel shows **Restart** on enabled `failed` and `refused` rows. It sends SET_PLUGIN_ENABLED true; `SetEnabled` already resets `restarts` and starts a fresh runner.
- **Toast:** a transition to `failed` raises one toast, "Plugin `<name>` stopped after repeated crashes", from the existing `plugin:event` handler (`events.ts:777`).
- **`DESIGN.md:100` carve-out:**
  - A running plugin may write its **own** files under `plugin-data/<id>/` (`HIVE_PLUGIN_DATA_DIR`), with atomic or transactional writes. Mode 0600 is required for the engine's `runs.db`.
  - It may write nothing else under the state dir. `config.json`, `ui-config.json` and the log stay the Manager's.
  - Worktree creation and merging, sessions and registry files stay behind daemon frames; a plugin never runs `git worktree` on a project.
  - The registry remains the only writer of session state.
- **Docs and CI:**
  - Node wording at all five sites;
  - the manifest `node` field;
  - PATH behaviour;
  - the `--disable-sigusr1` recommendation;
  - a Node 26 CI leg in `.github/workflows/ci.yml`, running `node --test 'plugins/sdk/*.test.mjs'` and the Pi/plugin Go tests that shell out to Node.

### PRs (in order; each that bumps `DaemonContract` takes main's value + 1 at rebase)

1. **PR1:** request/reply, provenance, typed results, ACP cancel and `stop_reason`. `no-changeset` label: nothing changes until a plugin uses these.
2. **PR2:** `PLUGIN_MESSAGE` and `PREPARE_WORKTREE` / `PREPARED_WORKTREE`. Gets its own security review. `no-changeset`.
3. **PR3:** plugin runtime and docs: Node lookup and floor, Restart and toast, `DESIGN.md` carve-out, Node doc fixes, the Node 26 CI leg. Changeset `.changesets/528-plugin-node-path-restart.md` (`fixed`: plugins started from Finder find Node; `added`: Restart a failed plugin).

### Files to change

**PR1**
- `internal/wire/control.go`:
  - `Error.ReqID`, `CreateSpec.ReqID`, `CreateSpec.WorkflowRunID`, `CreateSpec.ResultSchema`, `KillSessionReq.ReqID`, `RestartSessionReq.ReqID`, `Welcome.BootID`.
- `internal/wire/request.go` (new):
  - `AckMsg{ReqID, SessionID, WasBusy, PromptID, Epoch}`;
  - `ParsePrincipal`, `ValidWorkflowRunID`;
  - codes `invalid_workflow_run_id`, `invalid_result_schema`;
  - `ListAcpAgentsReq`, `AcpAgentsMsg`, `AcpAgentInfo`.
- `internal/wire/client.go`: `control:ack` / `acp:agents` event names, so the ws-bridge forwards them.
- `internal/wire/acp.go`:
  - `PromptAcpReq.{ReqID, WorkflowRunID}`, `AnswerPermissionReq.ReqID`;
  - `CancelAcpReq`;
  - `AcpTranscriptMsg.StopReason`, `AcpTranscriptMsg.Usage`.
- `internal/wire/frame.go`: `FrameAck`, `FrameCancelAcp`, `FrameListAcpAgents`, `FrameAcpAgents`, plus `String()`.
- `internal/daemon/daemon.go`:
  - `peekReqID` and a per-frame `ops` copy whose `sendError` stamps `req_id`;
  - `sendACPError` routed through it;
  - `composePrincipal` at `:806`, `:1556`, `:1706`;
  - the create handler uses `BeginCreate` / ACK / `FinishCreate`;
  - ACKs (create, prompt with `prompt_id`/`epoch`, answer, kill, restart), and the CANCEL_ACP and LIST_ACP_AGENTS handlers;
  - `bootID` on every WELCOME.
- `internal/registry/create.go`: split `Create` into exported `BeginCreate(spec)` + `FinishCreate(ctx, e, spec, plan)`, keeping `Create` as their composition for other callers. `BeginCreate` validates the kind and `ResultSchema` first. (`BeginCreate` takes no `ctx`: it does no I/O that needs one.)
- `internal/registry/acp.go`, `persist.go`, `registry.go`:
  - `ResultSchema` on Entry and meta;
  - `SubmitResult` validation;
  - `HIVE_SUBMIT_SCHEMA`;
  - `CancelACP(id) (wasBusy bool, err error)`, `PromptACPTurn` (PromptACP that also returns the turn's `prompt_id` and `epoch`);
  - `acpResult.stopReason` and `.usage`.
- `internal/acp/conn.go`: `Notify`. `internal/acp/agent.go`: `Cancel`. `internal/acp/types.go`: method const.
- `internal/acp/acptest/fake.go`: handles `session/cancel`. A blocked prompt, or one with a pending fake permission, ends with `stopReason:"cancelled"`.
- `cmd/hived/mcpsubmit.go`: the tool built from env, plus a pre-check.
- `internal/agent/pi/hive.ts`: parameters from env.
- `cmd/hivegui/app_calls.go`: `CancelAcp`. (`app_control.go` needs no change: the shared event table maps `ACK` / `ACP_AGENTS`.)
- `cmd/hived-ws-bridge/main.go`: forwards ACK and ACP_AGENTS; ops `CancelAcp` and `ListAcpAgents` (CamelCase like every other op).
- `internal/wire/testclient/client.go`: a `Request` helper.
- `cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx` (`originLabel`), `src/lib/principal.ts`, `src/lib/acp.ts` (`stop_reason` into the transcript state).
- `plugins/sdk/hive-plugin.mjs` and its webhook copy:
  - `request()`, `Frame.ACK`, `Frame.CANCEL_ACP`, `Frame.LIST_ACP_AGENTS`, `Frame.ACP_AGENTS`;
  - typedefs `Ack`, `AcpAgentInfo`, `AcpAgents`, `HiveError.req_id`, `Welcome.boot_id`;
  - `CreateSpec` docs for `workflow_run_id`, `result_schema`.
- `internal/plugin/sdk_drift_test.go`: typedef map (`Ack`).
- `internal/plugin/limiter.go`: CANCEL_ACP stays cheap (default).
- `docs/plugins.md`: request/reply, the new frames and fields. `docs/design-docs/acp-workflows.md`: the composed label.
- `.github/workflows/ci.yml`: `node --test 'plugins/sdk/*.test.mjs'`.
- `internal/buildinfo/contract.go`.

**PR2**
- `internal/wire/control.go`: `PluginMessage`, `MaxPluginMessage`, `PrepareWorktreeReq`, `PreparedWorktreeMsg` (with `base_sha`, `head_sha`: R4), `CommitWorktreeReq` (R1), `AckMsg.Committed` / `.Head`, `MergeConflict`, error codes.
- `internal/wire/frame.go`: `FramePluginMessage`, `FramePrepareWorktree`, `FramePreparedWorktree`, `FrameCommitWorktree` (R1, 0x56; PR2 numbers start at `0x53`, since PR1 took `0x4f`–`0x52`).
- `internal/worktree/worktree.go`: `safeGit`, `ResolveCommit`, `CheckBranchName`, `AddNewBranch`, `AddExistingBranch`, `FreePath`, `MergeBranches(ctx, wt string, shas []namedSHA) (merged []string, conflict *MergeConflict, err error)`.
- `internal/registry/worktrees.go`: `PrepareWorktree` under `gitMu`.
- `internal/daemon/daemon.go` and plugin routing: the PLUGIN_MESSAGE relay and target lookup; the PREPARE handler in `runOp`.
- `internal/plugin/limiter.go`: PLUGIN_MESSAGE `costFanout`, PREPARE `costExpense`.
- `cmd/hivegui/app_control.go`: emit `plugin:message`. `app_calls.go`: `SendPluginMessage`, `PrepareWorktree`.
- `cmd/hivegui/frontend/src/app/events.ts`: passthrough.
- `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient`.
- SDK and webhook copy: `postMessage`, `encodeWorkflowKey`, typedefs.
- `docs/plugins.md`, `internal/buildinfo/contract.go`.

**PR3**
- `internal/plugin/manifest.go`: `Node` field and validation.
- `internal/plugin/manager.go`: login-PATH fallback, the env PATH, the Node probe.
- `cmd/hivegui/frontend/src/components/modals/PluginsPanel.tsx`: Restart.
- `src/app/events.ts` / `plugins.ts`: the toast.
- `DESIGN.md:100`.
- `docs/plugins.md:81,317` and the Node/PATH/manifest/`--disable-sigusr1` sections.
- `plugins/sdk/README.md`, both SDK headers.
- `.github/workflows/ci.yml`: the Node 26 leg.
- `internal/buildinfo/contract.go`.
- `.changesets/528-plugin-node-path-restart.md`.

### New files

- `internal/acp/resultschema.go`, `resultschema_test.go`, `testdata/resultschema/*.json` (PR1).
- `cmd/hivegui/frontend/src/lib/principal.ts`, `test/unit/principal.test.ts` (PR1).
- `plugins/sdk/hive-plugin.test.mjs`: `node --test` against a fake daemon on a temp unix socket (PR1; extended in PR2).
- `internal/daemon/reqid_test.go` (PR1), `internal/daemon/plugin_message_test.go` and `internal/daemon/prepare_worktree_test.go` (PR2).
- `cmd/hivegui/frontend/test/e2e-real/plugin-message.spec.ts` (PR2).
- `.changesets/528-plugin-node-path-restart.md` (PR3).

### Tests

**PR1**
- `internal/daemon/reqid_test.go`:
  - `TestReqID_EchoedOnError`;
  - `TestReqID_AsyncCreateErrorEchoed`;
  - `TestReqID_CreateAckedBeforeAdapterStarts`: FlagBlock adapter; the ACK arrives while the phase is still spawning.
  - `TestReqID_CreateAckMatchesAddedEvent`: **order-independent**. It collects frames until both the ACK and SESSION_EVENT(added) have arrived, in either order, and asserts the same id.
  - `TestReqID_KilledMidCreateErrors`: ERROR with `req_id`, never silence.
  - `TestReqID_AbsentMeansNoAck`.
- `internal/registry/create_test.go`: `TestBeginFinishCreate_EqualsCreate`.
- `internal/daemon/acp_test.go`:
  - provenance: `TestPluginPrincipal_ComposedWithRun`, `TestPluginPrincipal_CannotClaimUser` (a raw `spawned_by` is ignored), `TestPluginPrincipal_CannotClaimOtherPlugin` (`x/../plugin:q` refused);
  - run id: `TestWorkflowRunID_RefusedFromUser`, `TestWorkflowRunID_OrchestratorStaysFalse`;
  - other entry points: `TestPromptAcp_OriginCarriesRun`, `TestCreateHello_WorkflowRunID`;
  - cancel: `TestCancelAcp_PluginAndUser`, `TestCancelAcp_RefusedOnSessionConn`.
- `internal/wire`: `TestParsePrincipal`.
- `test/unit/principal.test.ts`; `test/dom/acp-transcript.test.tsx` `renders workflow origin`.
- `internal/acp/resultschema_test.go`:
  - `TestCheckSchema_Subset`: rejects `$ref`, `oneOf`, a partial `required`, `additionalProperties:true`, depth 9, > 8 KiB.
  - `TestCheckResult_Fixtures`.
  - `TestCheckResult_ErrorPath`.
- `internal/registry/acp_test.go`:
  - submit validation: `TestSubmitResult_ValidatesAgainstSchema`, `TestSubmitResult_ErrorStatusSkipsSchema`, `TestSubmitResult_RetryAfterRejection`;
  - schema lifecycle: `TestResultSchema_SurvivesRestart`, `TestCreate_InvalidResultSchemaRefusedBeforeEntry` (no entry and no `added` event), `TestCreate_ResultSchemaOnPTYRefused`;
  - cancel: `TestCancelACP_StopReasonCancelled`, `TestCancelACP_AnswersPendingPermissionCancelled`, `TestCancelACP_IdleIsNoopWasBusyFalse`, `TestCancelACP_Twice`;
  - stop reason: `TestStopReason_EndTurnAndClearedOnPrompt`.
- `internal/acp/agent_test.go`: `TestCancel_SendsNotification` (calls.log shows `session/cancel` with no id).
- `cmd/hived/mcpsubmit_test.go`: `TestMCPSubmit_ToolsListCarriesSchema`, `TestMCPSubmit_PrecheckRejectsBadData`, `TestMCPSubmit_NoSchemaUnchanged`.
- `internal/agent/pi/hive.test.ts`: `submit_result parameters carry HIVE_SUBMIT_SCHEMA`.
- `plugins/sdk/hive-plugin.test.mjs`:
  - matching: `request resolves on ACK with req_id`, `request rejects on ERROR with req_id`, `interleaved replies resolve the right promises`;
  - broadcasts never settle: `request ignores broadcasts carrying the same req_id` (SESSION_EVENT, PLUGIN_MESSAGE, ACP_TRANSCRIPT);
  - lifecycle: `timeout rejects`, `close rejects every pending request`.
- `cmd/hived/plugin_e2e_test.go`: `TestE2E_SDKRequest_CreateSessionReturnsID`.
- `internal/plugin/sdk_drift_test.go`: existing tests, extended.
- `test/dom/acp-events.test.ts`: `stop_reason passes through state`.
- R2/R3/R5/R6: `TestReqID_PromptAckCarriesPromptID`, `TestWelcome_BootIDStablePerProcess`, `TestReqID_RestartAcked`, `TestUsage_ForwardedForTheTurn` (registry), `TestUpdateRawKeptForUsage` (acp), `TestListAcpAgents`, `TestCreateWithResultSchema`, and the SDK test `request resolves on the frame a request has of its own`.

**PR2**
- `internal/daemon/plugin_message_test.go`:
  - direction and routing: `TestPluginMessage_PluginToGUIStampsID` (a forged `plugin_id` is overwritten), `TestPluginMessage_GUIToPlugin`, `TestPluginMessage_TargetNotRunning`;
  - who never receives: `TestPluginMessage_NotRelayedToOtherPlugins`, `TestPluginMessage_NotRelayedToHivebarOrSession`, `TestPluginMessage_RefusedOnSessionConn`;
  - limits and slow clients:
    - `TestPluginMessage_SizeCap64KiB`;
    - `TestPluginMessage_SlowGUIDoesNotStallPlugin`;
    - `TestPluginMessage_GUIRecoversAfterSlowClientDisconnect`: a GUI overflowing its queue is dropped, reconnects, sends a GUI→plugin message, and receives the plugin's next message.
- `cmd/hivegui/app_calls_test.go`: `TestSendPluginMessage`, `TestPrepareWorktreeBinding`.
- `internal/worktree/worktree_test.go`:
  - `TestCheckBranchName`: requires the `hive/wf/<run>/` prefix and `check-ref-format`, refuses a leading `-`.
  - `TestAddNewBranch_NoExistingFallback`.
  - `TestAddExistingBranch`.
  - `TestFreePath_SuffixesDirOnly`.
  - `TestMergeBranches_Clean`.
  - `TestMergeBranches_ConflictReportsBranchAndFiles`: the tree is clean after the abort, and `-z` handles a file with a space.
  - `TestMergeBranches_AlreadyMergedIsNoop`.
  - `TestSafeGit_HooksAndFsmonitorOff`: a `post-checkout` / `pre-merge-commit` / `post-merge` hook writing a marker never runs.
  - `TestMergeBranches_NoIdentity`: `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`.
- `internal/daemon/prepare_worktree_test.go`:
  - base: `TestPrepareWorktree_FromArbitraryBase`, `TestPrepareWorktree_PassesResolvedSHA` (`base` moved after resolve → the worktree is at the old SHA), `TestPrepareWorktree_RefusesOptionLikeBase`;
  - branch and path: `TestPrepareWorktree_BranchOutsideRunPrefixRefused`, `TestPrepareWorktree_PathCollisionResolved`;
  - re-attach: `TestPrepareWorktree_ReattachesExistingRunBranch`, `TestPrepareWorktree_AfterReclaimSucceeds` (prepare, boot reclaim removes the pristine worktree, prepare again → same branch, new worktree);
  - merge and adoption: `TestPrepareWorktree_MergeConflictTyped`, `TestPrepareWorktree_SessionAdoptsPath`.
- `plugins/sdk/hive-plugin.test.mjs`: `encodeWorkflowKey` (`fix[2]/work`→`fix_2/work`, `work/each[3]/do`→`work/each_3/do`, injective over a generated set, every output passes `git check-ref-format --branch` when git is present, rejects keys outside the grammar) and `postMessage frames`.
- `cmd/hivegui/frontend/test/e2e-real/plugin-message.spec.ts`: a round-trip through `hived-ws-bridge` in an isolated state dir.

**PR3**
- `internal/plugin/manager_test.go`:
  - PATH: `TestResolveCommand_LoginPATHFallback`, `TestPluginEnv_PATHIsResolvedPATH`;
  - Node probe: `TestSpawn_NodeTooOldRefused` (a fake `node` prints `v24.1.0`), `TestSpawn_NodeProbeUsesNodeNotArgv0` (main is `./run.sh`), `TestSpawn_NodeProbeFails`, `TestSpawn_NodeOK`;
  - lifecycle: `TestHeadlessBuiltin_Starts` (an injected `fstest.MapFS`), `TestFailedThenRestart_ResetsRestarts`.
- `internal/plugin/manifest_test.go`: `TestManifest_NodeFieldValidated`.
- `test/dom/settings-plugins.test.tsx`: `failed row shows Restart and sends enable`, `refused row shows Restart`, `running row has no Restart`.
- `test/dom/plugin-events.test.ts`: `failed transition toasts once`.
- `test/e2e/settings-plugins.spec.ts`: `restart a failed plugin`.
- CI: the Node 26 leg runs the SDK tests and the Node-shelling Go tests.

## Verification

```sh
GT=go$(sed -n 's/^go //p' go.mod)
GOTOOLCHAIN=$GT go build ./... && GOTOOLCHAIN=$GT go test ./...
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os GOTOOLCHAIN=$GT go vet ./...; done
./scripts/ci-bootstrap.sh && (cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
CI=1 scripts/test.sh go unit dom e2e
node --test 'plugins/sdk/*.test.mjs'
(cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real)        # PR1, PR2
scripts/check-daemon-contract.sh origin/main HEAD
scripts/check-changeset.sh
scripts/ui-lint.sh                                             # PR1, PR3 (GUI touched)
```

## Decision log

- **2026-10-25:** A hand-written Go validator for the IR subset, with no new dependency. Why: user decision.
- **2026-10-25:** `Registry.SubmitResult` is the one authority; the `{status, summary, data}` envelope is kept, and the schema governs `data` on `ok`. Rejected: replacing the whole object, which breaks F3 and existing `result` consumers.
- **2026-10-25:** Provenance is a daemon-composed integrity label (`plugin:<id>/workflow:<run>`) on create, the create HELLO and PROMPT_ACP. It is not a defence: plugins already have the user's access. Why: user decision.
- **2026-10-25:** Correlation is a generic `req_id`: peeked, echoed on ERROR, plus an ACK. The SDK settles only on reply frame types. Rejected: correlating through SESSION_EVENT(added), which isn't unique under fan-out.
- **2026-10-25:** CREATE_SESSION ACKs between exported `BeginCreate` and `FinishCreate`. Rejected: ACK after `Create` returns, which blocks through the adapter start and any user prompt.
- **2026-10-25:** CANCEL_ACP and the PREPARE_WORKTREE `base` are unrestricted, like existing plugin frames (`docs/plugins.md:32-40`). `base` is resolved to a SHA, and the SHA is what git receives. Why: user decision.
- **2026-10-25:** CANCEL_ACP is idempotent and ACKs `{was_busy}`. Rejected: an `acp_not_busy` error, which races with the turn ending on its own.
- **2026-10-25:** The plugin composes the branch with the SDK encoder; the daemon enforces the `hive/wf/<run>/` prefix and `check-ref-format`. Rejected: a Go encoder plus a shared fixture (two encoders for one consumer).
- **2026-10-25:** The branch namespace is `hive/wf/<run>/`, without a plugin segment, and cross-plugin run-id collisions are accepted. Why: no privilege difference between plugins; run ids are random.
- **2026-10-25:** PREPARE re-attaches an existing `hive/wf/<run>/…` branch; new branches use a strict add with no fallback. Why: boot reclaim and B's re-prepare must work, and a non-run branch must never be checked out.
- **2026-10-25:** One `safeGit` argument set for every PREPARE git call. Repo config and attributes are trusted like the repo. Why: hooks and fsmonitor run code on ordinary operations; drivers and filters are the user's own repo config.
- **2026-10-25:** `PLUGIN_MESSAGE` is two-way, has no `req_id`, and is capped at 64 KiB. Why: user decision; C carries a nonce in `data`.
- **2026-10-25:** Node floor 26.0.0 for the engine; `--permission` is defence-in-depth; the engine runs with `--disable-sigusr1`; other plugins need a supported LTS (22+). Why: user decision, on the verified facts above.
- **2026-10-25:** Three PRs, each bumping `DaemonContract`. Why: user decision; PR2 gets its own security review.

- **2026-10-25:** Slice B's required changes, approved by the user. PR1 adds R2 (`WELCOME.boot_id`; PROMPT_ACP's ACK carries `prompt_id` and `epoch`), R3 (RESTART_SESSION ACK), R5 (`AcpTranscriptMsg.usage`) and R6 (`LIST_ACP_AGENTS` → `ACP_AGENTS`). PR2 gains R1 (`COMMIT_WORKTREE`) and R4 (`base_sha`, `head_sha` on `PREPARED_WORKTREE`). Why: B (#529) cannot tie a result to its turn, retry a dead adapter, bill, or refuse a bad agent/mode before a run without them, and a plugin may not run `git commit` itself (carve-out).
- **2026-10-25 (PR1 implementation):** Small consistent choices, none changing the design. `ACK.was_busy` is `omitempty` (absent = false). `BeginCreate` takes no `ctx`. Late create failures carry `session_id` only when the request had a `req_id`, so the GUI's own creates see errors exactly as before. The bridge ops are CamelCase (`CancelAcp`, `ListAcpAgents`) like the rest. `ACK` and `ACP_AGENTS` are in the shared event table (`control:ack`, `acp:agents`), so the bridge forwards them.

## Progress

- **2026-10-25:** Research complete; plan drafted.
- **2026-10-25:** Revised to the user's decisions and the plan-review fixes. Awaiting approval.

## Open questions

1. **Proposed Non-goals (not yet in the spec):**
   - no GUI Stop button for ACP sessions (`CANCEL_ACP` is bound for C, but no surface uses it in A);
   - no `loadSession` on the wire (B retries through RESTART_SESSION).

   Approve to add both to the spec's Non-goals, or move either into scope.
