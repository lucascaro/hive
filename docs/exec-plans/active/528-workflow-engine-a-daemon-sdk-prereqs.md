# Workflow engine A: daemon and SDK prerequisites

- **Spec:** [docs/product-specs/528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md](../../product-specs/528-workflow-engine-a-daemon-and-sdk-prerequisites-prov.md)
- **Design:** [workflow-engine.md](../../design-docs/workflow-engine.md) ("What the engine spec must add"), [acp-workflows.md](../../design-docs/acp-workflows.md)
- **Issue:** #528
- **Branch:** `feature/528-daemon-sdk-prereqs` (research/plan); one branch per phase below
- **Status:** active

## Summary

Give a plugin what the workflow engine (B, #529) and its GUI (C, #530)
need from `hived`, the wire, and the plugin SDK:
- correlated request/reply;
- a run-scoped provenance principal;
- a per-session `submit_result` schema validated by `hived`;
- ACP cancel with a visible stop reason;
- worktrees from an arbitrary base, plus a merge worktree;
- a plugin ⇄ GUI message channel;
- a Node the plugin can find, with a version floor;
- a recoverable crash-loop state;
- the `plugin-data/<id>/` carve-out in `DESIGN.md`.

The work ships as six PRs. Each one is useful to plain `acp` sessions or
plugins on its own.

## Research

Verified on `origin/main` at `0bf4e725` (2026-10-25). Line numbers are
against that commit.

### Relevant code (the review findings, each checked)

| Claim | Verified at | Note |
|---|---|---|
| `principalOf` returns `plugin:<id>` | `internal/daemon/daemon.go:2307` | Holds |
| `SpawnedBy` stamps | `daemon.go:806` (ModeCreate HELLO), `daemon.go:1556` (CREATE_SESSION) | Holds. `Orchestrator` is forced false for plugins at `:808` / `:1560`, and UPDATE_SESSION refuses it at `:1895` |
| "nothing a client sends can set principal" | `daemon.go:1431`; `wire.CreateSpec.SpawnedBy` is `json:"-"` (`internal/wire/control.go:141`) | Holds |
| PROMPT_ACP origin = `ops.principal` | `daemon.go:1706` | **Missed by the spec.** B's re-prompts would read as `plugin:<id>`, not the run |
| Initial-prompt origin = `spec.SpawnedBy` | `internal/registry/acp.go:266` | Holds |
| `originLabel` renders `plugin:` | `cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx:209-212` | A composed principal would render "From plugin engine/workflow:r1" |
| SDK `send()` has no correlation | `plugins/sdk/hive-plugin.mjs:365` | Holds |
| `req_id` pattern | `internal/wire/transcript.go:77,102,121,164` | Echoed on its own reply frames only. **`wire.Error` (`control.go:1379`) has no `req_id`** |
| CREATE_SESSION has no reply | `control.go` (`IdeaID` comment): "nothing correlates the SESSION_EVENT(added) … with the request" | **Spec gap.** A plugin cannot learn the id of the session it created |
| `resolveCommand` uses the daemon's PATH | `internal/plugin/manager.go:687-701` | Holds. `pluginEnv` (`:704`) passes the daemon env through unchanged |
| Login-shell PATH helper exists | `internal/proc/loginpath_unix.go:69` `LoginPATH`; `proc.LookPathIn`; used by `internal/agent/acp.go:120,157-171` | Reuse it |
| `maxCrashes = 5` in 60s → failed | `manager.go:28-31`, `:616-619` | Holds. Status `failed` plus its detail already render in Settings → Plugins (`PluginsPanel.tsx:101-105,292`) |
| Recovery from `failed` | `SetEnabled(true)` restarts it when `e.run == nil` (`manager.go:396-428`) | **No UI path.** The checkbox is already checked, so the user can't send `enabled=true` |
| PLUGIN_EVENT is lifecycle only | `control.go:1807-1814` | Holds. **No GUI→plugin path exists either**: `CLIENT_COMMAND` is an allowlist relay (`control.go:1484-1525`), and `SET_PLUGIN_CONFIG` is UI-plugin only |
| Fixed submit schema | `cmd/hived/mcpsubmit.go:44-57,137-145`; `internal/agent/pi/hive.ts:279-288,688-706` | Holds. The authority is `Registry.SubmitResult` (`internal/registry/acp.go:736`), which only checks "is a JSON object ≤ 64 KiB" |
| No `session/cancel` | `internal/acp/agent.go` has `Prompt`/`SetMode`/`LoadSession` only. `Conn` (`conn.go:174`) has `Call` but no notification send | Holds. `acp.Cancelled()` exists (`types.go:255`) for answering a pending permission |
| `stopReason` not on the wire | `registry/acp.go:697-723` folds it into agentstate text only | **Spec gap.** B needs `end_turn` vs `cancelled` |
| `CreateWorktreeAt(…, base)` | `internal/worktree/worktree.go:220` | Holds. It **checks out an existing branch** when the name exists (`:229`). No merge function exists |
| Path from branch | `worktree.WorktreePath` / `sanitizeBranch` (`worktree.go:57,553`) maps `/`→`-` | Not injective (`a-b/c` and `a/b-c` collide). `ResolveBranchAndPath` (`:518`) suffixes on collision |
| Orphan reclaim | `registry/projects.go:157` `ReclaimOrphanWorktrees` | Deletes **pristine, sessionless** worktrees at boot |
| DESIGN.md plugin-data rule | `DESIGN.md:100` "written only through the plugin Manager" | Holds |
| Node 18 in docs | `docs/plugins.md:81,317`, **also** `plugins/sdk/hive-plugin.mjs:6`, `plugins/webhook/hive-plugin.mjs:6`, `plugins/sdk/README.md:4` | Five places, not two |
| Embed is UI-only | `plugins/embed.go:16` embeds `plan-review` only | The mechanism (`internal/plugin/builtin.go`) is content-agnostic. A headless builtin needs no embed change, only a test |
| Frame numbers | last is `FrameWaited = 0x4e` (`internal/wire/frame.go:294`) | New frames start at `0x4f` |
| DaemonContract | `internal/buildinfo/contract.go:285` = 33 | Each phase takes main's value +1 at rebase |
| Plugin rate costs | `internal/plugin/limiter.go` `FrameCost` | New frames need entries |
| Plugins see every ACP transcript | `registry.SubscribeACP` (`acp.go:861`) is unfiltered per control connection | B reads deltas without subscribing |
| SDK drift tests | `internal/plugin/sdk_drift_test.go`: frame table, typedefs (`HiveError` = `wire.Error`, …), webhook copy byte-equal | Every SDK change copies to `plugins/webhook/` |
| Fake ACP adapter | `internal/acp/acptest/fake.go`; each message is handled on its own goroutine (`:194`), so a notification arrives during a `FlagBlock` prompt | A cancel flag is straightforward |
| IR schema subset | `scripts/workflow-proto/schema.ts`: string(+enum), number, boolean, array(items), object(properties, required = all keys, `additionalProperties: false`) | The validator accepts exactly this shape |
| IR id grammar | `scripts/workflow-proto/dsl.ts:282` `ID_SEGMENT = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/`; keys add `[N]` | `_` and `.` never appear in a segment |

### Node facts (criterion 5; B consumes these)

All accessed **2026-10-25**.

| Fact | Value | Source |
|---|---|---|
| Permission model stable | v23.5.0 / v22.13.0 ("no longer experimental"); added v20.0.0 | [permissions (v26.12.1 docs)](https://nodejs.org/api/permissions.html) |
| `--permission` fences the **network** | Only from **v25.0.0** (`--allow-net`, "Added in: v25.0.0", Stability 1.1 Active development). Under `--permission` the network is denied unless `--allow-net` is passed | [cli (v26.12.1)](https://nodejs.org/api/cli.html#--allow-net) |
| …not in 24.x or 22.x | v24.22.1 and v22.23.4 docs list no `--allow-net`, and their permission pages don't list network among restricted resources | [v24 cli](https://nodejs.org/docs/latest-v24.x/api/cli.html), [v24 permissions](https://nodejs.org/docs/latest-v24.x/api/permissions.html), [v22 permissions](https://nodejs.org/docs/latest-v22.x/api/permissions.html) |
| `node:sqlite` tier | Unflagged v22.13.0 / v23.4.0, "still experimental". **1.2 Release candidate** from v25.7.0 and v24.15.0 | [sqlite (v26.12.1)](https://nodejs.org/api/sqlite.html), [sqlite (v24.22.1)](https://nodejs.org/docs/latest-v24.x/api/sqlite.html) |
| Release lines | 22 Maintenance LTS, EOL 2027-04-30. 24 Active LTS until 2026-10-20, EOL 2028-04-30. **25 EOL 2026-06-01.** **26 Current, Active LTS 2026-10-28**, EOL 2029-04-30. From 27 on, one major per year | [nodejs/Release](https://github.com/nodejs/Release) |

Conclusions:
- **Engine floor: Node ≥ 26.0.0.**
  - It is the only supported line whose `--permission` fences the network. That is what the design's build sandbox needs ("a Node floor that fences the network").
  - `node:sqlite` is at RC (1.2) there, so B uses SQLite. JSONL stays the documented fallback only if a future line regresses.
  - `--allow-net` itself is 1.1. B pins the behaviour with a test (a build that dials the network gets `ERR_ACCESS_DENIED`).
- **General plugin floor: a supported Node LTS (≥ 22).** Node 18 has been EOL since 2025. The SDK uses only `node:net` / `node:events`, so the docs say "a supported Node.js LTS (22 or newer)", and the engine states its own floor.

### Constraints

- Lock-step wire rule, `snake_case` tags, no `PROTOCOL_VERSION` bump, a `DaemonContract` bump per daemon-side PR (`scripts/check-daemon-contract.sh`).
- `proc` only for subprocesses (`git`, `node --version`).
- `LoginPATH` must never be called under `m.mu` (a first call can take 20s).
- The registry owns worktrees and session state. The plugin may own only `plugin-data/<id>/` files after the carve-out.
- `docs/design-docs/slow-client-policy.md` governs any new broadcast.

### Gaps B will hit that this spec does not list

Recorded so B plans for them; neither is in A's scope unless the user says so:
- `loadSession` capability is not on the wire. B's "retry with `session/load`" can use RESTART_SESSION on the ACP entry, which already does `session/load` (`registry/acp.go:376-379`), and fall back to a fresh session on its `restart_failed` error. No capability bit is needed.
- CI pins `node-version: '24'` (`.github/workflows/ci.yml:113`). B's network-fence test and its Node 26 floor need a Node 26 CI leg.
- An ACP mode above the user's ceiling is **refused** (`ErrCodeACPModeAboveCeiling`), not clamped. B must request ≤ ceiling, or omit the mode.

### Prior lessons

None matched (`brain-search`, two queries).

### Conventions card

```
build:  ./build.sh
test:   scripts/test.sh     # all layers: go · unit · dom · e2e
e2e-real: (cd cmd/hivegui/frontend && npm run test:e2e:real)
```

- **TDD.** Every behaviour change lands with the test that proves it. Go tests sit beside the source; frontend tests go under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- **Wire changes** move `internal/wire`, `cmd/hivegui/app_*.go`, `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient`, the SDK `Frame` table and typedefs, and `plugins/webhook/hive-plugin.mjs` (byte copy) together.
- **Verify under the CI toolchain:** `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`. Run per-GOOS staticcheck + vet. Run `./scripts/ci-bootstrap.sh` before `npm run typecheck`. Run Playwright with `CI=1`.
- **E2E isolation:** temp `HIVE_SOCKET` / `HIVE_STATE_DIR` (`testclient.RequireIsolation`).
- **Docs:** a changeset for each user-visible change (`.changesets/<slug>.md`). Never edit `CHANGELOG.md` or `docs/product-specs/index.md`. `DESIGN.md` changes for the new hard rule and carve-out. `docs/plugins.md` gets every new frame and SDK call.

## Approach

### Interface surface (what B and C plan against)

Frame numbers are provisional. Each PR takes the next free number on
`main` at rebase. Names are final unless review changes them.

**Request correlation (phase 1), all control connections**

- **Any control request may carry `req_id` (int, omitempty).**
  - The daemon peeks it once per frame (`{"req_id":N}`).
  - It echoes it on every ERROR it sends while handling that frame, including errors sent later from `runOp`.
  - `wire.Error` gains `ReqID int json:"req_id,omitempty"`.
- **`ACK` (0x4f, S→C)** `{req_id, session_id?}`.
  - Sent only when the request carried a non-zero `req_id` and had no other reply.
  - Covers CREATE_SESSION, PROMPT_ACP (turn started), ANSWER_PERMISSION, KILL_SESSION and CANCEL_ACP.
  - **CREATE_SESSION ACKs as soon as the entry exists.** `session_id` is the new entry's id, delivered through a new `registry.Create` option callback, `OnEntry(id)`.
    - It does not wait for `reg.Create` to return. Create blocks through worktree setup, a possible user worktree prompt, the adapter start (a first `npx -y` can take longer than 30s) and the opening prompt.
    - Any later failure is an ERROR carrying `req_id` and `session_id`. That includes today's silent `ErrNotFound` "killed mid-create" case, which must not leave a request hanging.
    - B watches readiness through SESSION_EVENT / phase, as the GUI does.
  - The GUI and today's plugins send no `req_id` and so see nothing new.
- `CreateSpec` gains `ReqID int json:"req_id,omitempty"`. Requests that gain `req_id` follow the same pattern.
- **SDK** (`plugins/sdk/hive-plugin.mjs`):
  - `hive.request(name, payload = {}, { timeoutMs = 30000 } = {}) → Promise<object>`.
  - It assigns `req_id` from a per-connection counter.
  - It resolves on the first non-ERROR frame carrying that `req_id`, and rejects on an ERROR carrying it. The rejection is an `Error` with `.hive = {code, message, …}`.
  - It rejects every pending request on timeout or `close`.
  - `send()` is unchanged.

**Provenance (phase 2)**

- `CreateSpec` and `PromptAcpReq` gain `WorkflowRunID string json:"workflow_run_id,omitempty"`.
- **Charset:** `^[a-z0-9][a-z0-9-]{0,62}$`. It is ref-safe and contains neither `/` nor `:`.
- **On a plugin connection** the daemon stamps `SpawnedBy` / origin as `plugin:<id>/workflow:<run-id>`.
- **From a non-plugin connection:** refused with `mode_not_allowed` ("workflow_run_id is set by plugins only"). It is never silently dropped, because a user claiming a run would be a forged label.
- **Invalid charset:** `invalid_workflow_run_id`.
- **ModeCreate HELLO:** gets the same treatment.
- **PROMPT_ACP without `workflow_run_id`:** origin stays `ops.principal`.
- `wire.ParsePrincipal(s) (plugin, run string)` is the single parser, mirrored by `parsePrincipal()` in the frontend (`src/lib/principal.ts`).
- **Transcript label:** "From workflow `<run>` (plugin `<id>`)".

**Typed results (phase 2)**

- `CreateSpec.ResultSchema json.RawMessage json:"result_schema,omitempty"`. ACP sessions only; on a PTY session it is refused with `not_acp_session`.
- It is validated at create:
  - exactly the IR subset;
  - top level `type:"object"`;
  - `required` = all property keys;
  - `additionalProperties:false`;
  - depth ≤ 8;
  - ≤ 8 KiB.
  
  Failure is refused with `invalid_result_schema`.
- It is persisted on the entry (`registry/persist.go` meta), so revive and restart keep it.
- **Envelope kept:** the result is still `{status, summary?, data?}`. With a schema and `status:"ok"`, `data` is **required** and is validated against the schema. `status:"error"` is not validated, so F3 and `result_status` are unchanged.
- On a validation failure, `Registry.SubmitResult` returns `ErrSubmitRejected` with a path-qualified message, e.g. `data.findings[2].severity: "urgent" is not one of high|low; call submit_result again`. Both tools relay that to the model as a tool error, so the agent retries.
- The schema reaches the tool definitions through `HIVE_SUBMIT_SCHEMA` (compact JSON), added next to `HIVE_SUBMIT_NONCE` in `acpExtras` (`registry/acp.go:213`):
  - `mcp-submit` puts it under `data` in `tools/list` and marks `data` required.
  - Pi's `SUBMIT_PARAMETERS` does the same.
  - `mcp-submit` also pre-checks with the same Go validator (`internal/acp`), which saves a socket round-trip.
- **Shared fixtures:** `internal/acp/testdata/resultschema/*.json` holds `{schema, value, ok, error?}` cases. B's TS IR validator reuses them, which stops drift.

**ACP cancel (phase 3)**

- **`CANCEL_ACP` (0x50, C→S)** `{session_id, req_id?}`.
  - Same scope as PROMPT_ACP: user or plugin. Refused on session connections, since it's not in `sessionModeFrames`.
  - It ACKs once the notification is sent.
  - On a session with no running turn: `acp_not_busy`.
- **Daemon side:**
  - It answers any pending permission request with `acp.Cancelled()`, as the ACP spec requires.
  - It sends the `session/cancel` **notification** ([ACP prompt-turn, Cancellation](https://agentclientprotocol.com/protocol/prompt-turn)).
  - The turn then ends through the existing path.
- **`AcpTranscriptMsg.StopReason string json:"stop_reason,omitempty"`**:
  - the latest prompt's ACP stop reason (`end_turn`, `cancelled`, `max_tokens`, `max_turn_requests`, `refusal`);
  - carried on every message like `result_status`, and cleared by a new prompt;
  - set to `error` when the adapter call fails.

**Plugin messages (phase 4)**

- **`PLUGIN_MESSAGE` (0x51, both directions)** `{plugin_id, topic, data, req_id?}`.
  - `topic`: `^[a-z][a-z0-9._-]{0,63}$`.
  - `data`: any JSON value, ≤ 256 KiB (`wire.MaxPluginMessage`).
- **From a plugin connection:**
  - The daemon **overwrites** `plugin_id` with the sender's tag.
  - It relays to every *UI-capable non-plugin* control client: the GUI and `hived-ws-bridge`, selected the way `reviewAnnouncer` selects (`tag == nil && canAnswer(hello)`).
  - It never relays to other plugins, hivebar or session connections.
- **From a UI-capable user control connection:**
  - `plugin_id` names the target.
  - It is delivered to that plugin's live control connection(s) unchanged.
  - Unknown or not-running target → ERROR `plugin_not_running` (with `req_id`).
- **From a session connection:** refused.
- `req_id` passes through untouched. The daemon does not correlate; the plugin and its GUI agree on reply topics. Two windows can share a counter, so C carries a string nonce in `data` when it needs per-window correlation, as INSTALL_PLUGIN does.
- **Slow clients:** the relay uses the existing per-connection bounded queue (slow-client policy). A dropped message to a GUI is that GUI's desync, and its recovery is C's replay request.
- **GUI bindings:**
  - Wails event `plugin:message` (JSON);
  - Go binding `App.SendPluginMessage(pluginID, topic string, dataJSON string, reqID int) error`. It is **host-only**: a UI plugin module must not reach it, or it could answer a human node or start a run for the user. A containment test pins this (open question 7);
  - bridge op `send_plugin_message`.
- **SDK:**
  - `hive.on('PLUGIN_MESSAGE', …)` (exists generically);
  - `hive.postMessage(topic, data)` (a broadcast send);
  - `@typedef PluginMessage`.

**Worktrees (phase 5)**

- **`PREPARE_WORKTREE` (0x52, C→S)** `{req_id, project_id, workflow_run_id, key, base, merge?: [branch…]}`.
- **The daemon composes the branch** `hive/wf/<run-id>/<encoded key>`. The plugin never names a raw branch, so it cannot check out `main` or anyone's existing branch.
- If the composed branch already exists, the request is refused with `worktree_branch_exists`.
  - This is deliberate. The engine reuses an existing node worktree through CreateSpec `worktree_path` (`{of}`, retries), not through this frame.
- **`base`** is any existing commit-ish. It is validated by `git rev-parse --verify --quiet --end-of-options <base>^{commit}`, and a leading `-` is refused outright. Typically it is the upstream node's branch.
- **The path is chosen by the daemon** and returned.
  - It uses a new path-only collision helper that suffixes the *directory* (`-2`, `-3`, …) and never the branch. `ResolveBranchAndPath` suffixes both, which would break the composed name.
  - The branch is always exactly `hive/wf/<run>/<encoded key>`.
  - B treats `PREPARED_WORKTREE.branch` and `.path` as authoritative.
- **`merge`:**
  - Each named branch (each `rev-parse`-verified) is merged **sequentially** with `git -c core.hooksPath=/dev/null merge --no-ff --no-edit -- <b>`. On Windows the hooks path is an empty temp dir.
  - On the first conflict, the daemon collects `git diff --name-only --diff-filter=U`, runs `git merge --abort`, and stops.
  - The worktree is **kept**, holding base plus the merges that succeeded, so the user can resolve by hand.
- **`PREPARED_WORKTREE` (0x53, S→C)** `{req_id, project_id, path, branch, merged: [branch…], conflict?: {branch, files: [path…]}}`. Every other failure is an ERROR with `req_id`.
- **Key encoding:**
  - **Go:** `worktree.EncodeWorkflowKey(key) (string, error)`.
  - **SDK:** `encodeWorkflowKey(key)`, so B can name branches without a round-trip.
  - Grammar: `seg(\[N\])?(/seg(\[N\])?)*` with `seg` = `ID_SEGMENT`. Each `[N]` becomes `_N`, and `/` is kept.
  - Injective, because `_` never appears in a segment.
  - Examples: `fix[2]/work` → `fix_2/work`, and `work/each[3]/do` → `work/each_3/do`. Both pass `git check-ref-format --branch`.
  - A key outside the grammar → `invalid_workflow_key`.
- **Refs are hierarchical,** so a branch for `a` and one for `a/b` cannot coexist (git D/F conflict). Only leaf nodes get branches; B names its merge worktrees as leaf keys. A D/F failure surfaces as `worktree_branch_exists`.
- **Reclaim:** a prepared worktree no session has adopted is "sessionless". Once it holds merges it is not pristine, so boot reclaim keeps it. A pristine one may be reclaimed at boot. That is safe, because B re-prepares an `interrupted` node; **B must not assume a prepared-but-unused worktree survives a `hived` restart.**

**Plugin runtime (phase 6)**

- **Manifest:** `"node": "<semver>"` (optional, `internal/plugin/manifest.go`), the minimum Node for `main`. It is validated as `MAJOR.MINOR.PATCH` at load.
- **`resolveCommand`:** `exec.LookPath` on the daemon PATH, then `proc.LookPathIn(proc.LoginPATH(), name)`. The error names both PATHs.
- **Plugin env PATH:** `PATH` is set to the PATH the command was found on, so the plugin's own children (`node`, `git`, the TS checker) resolve the same way.
- **Version check:** with `node` set, `spawn` resolves `node` on the chosen PATH (not `argv[0]`, which may be `./run.sh`) and runs `<node> --version` via `proc.CommandContext` (2s timeout) before starting.
  - Too old: `PluginRefused` with "requires Node.js 26.0.0 or newer (found v24.11.0 at /usr/local/bin/node)".
  - Not runnable: `PluginRefused`, detail "could not run node --version: …".
  - The same check runs on every restart, so upgrading Node and pressing Restart recovers.
- **Restart action:** PluginsPanel shows a **Restart** button on `failed` and `refused` rows when enabled. It calls SET_PLUGIN_ENABLED true; `SetEnabled` already resets `restarts` and starts a fresh runner.
  - No manager change is needed: `SetEnabled` starts a runner only when `e.run == nil`, and that is always the case for `failed` and `refused`. `TestFailedThenRestart_ResetsRestarts` pins this.
- **Toast on failure:** a PLUGIN_EVENT moving a plugin to `failed` raises one toast, "Plugin `<name>` stopped after repeated crashes", with a Settings link. It is GUI-only, using the existing `plugin:event` handler (`events.ts:777`).
- **`DESIGN.md:100` carve-out:**
  - A running plugin may create and write its **own** files under `plugin-data/<id>/` (it receives the path as `HIVE_PLUGIN_DATA_DIR`), with atomic (temp + rename) or transactional writes. Mode `0600` is recommended and is required for the engine's `runs.db`.
  - It may write nothing else under the state dir.
  - `config.json`, `ui-config.json` and the log stay the Manager's.
  - Worktree creation and merging, sessions and every registry-owned file stay behind daemon frames. A plugin never runs `git worktree` against a project.
  - The registry remains the only writer of session state.

### Phases (one PR each, in order)

Each daemon-side PR bumps `DaemonContract` to *main's value + 1 at
rebase*, with a contract-history line. Phases 1 and 2 can be combined if
the user prefers fewer bumps; within one release extra bumps cost
nothing more.

1. **P1: request correlation.** `wire.Error.req_id`, `ACK`, `CreateSpec.req_id`, the daemon peek/echo, SDK `request()`, `docs/plugins.md` (request/reply section; Node floor wording fixed here for SDK headers). Bump.
2. **P2: provenance + typed results.** `workflow_run_id` on CreateSpec/PromptAcpReq/HELLO create; `ParsePrincipal`; `originLabel`; `result_schema`, `internal/acp/resultschema.go`, `SubmitResult` validation, persistence, `HIVE_SUBMIT_SCHEMA` in mcp-submit and Pi. Bump. Changeset: none (no user-visible change without a plugin), apply `no-changeset`.
3. **P3: ACP cancel + stop_reason.** `Conn.Notify`, `Agent.Cancel`, `Registry.CancelACP`, `CANCEL_ACP`, `AcpTranscriptMsg.stop_reason`, fake `session/cancel`. Bump. No GUI button (spec non-goal); the frame is bound in `app_calls.go` and the bridge for lock-step and C.
4. **P4: plugin messages.** `PLUGIN_MESSAGE` both directions, GUI event + binding, bridge op, SDK `postMessage`, `FrameCost`. Bump.
5. **P5: worktrees.** `PREPARE_WORKTREE` / `PREPARED_WORKTREE`, `worktree.EncodeWorkflowKey`, `worktree.MergeBranches`, `Registry.PrepareWorktree`, SDK `encodeWorkflowKey`. Bump.
6. **P6: plugin runtime + docs.** Login-PATH lookup, manifest `node`, the version probe, the Restart button + toast, the headless-builtin test, `DESIGN.md` carve-out, `docs/plugins.md` Node corrections. Bump (the `internal/plugin` behaviour is daemon-side even if the gate script doesn't require it). Changeset (`fixed`: plugins launched from Finder now find Node; `added`: Restart for a failed plugin).

### Files to change

P1
- `internal/wire/control.go`: `Error.ReqID`, `CreateSpec.ReqID`, `AckMsg{ReqID, SessionID}`.
- `internal/wire/frame.go`: `FrameAck`, plus `String()`.
- `internal/wire/acp.go`: `ReqID` on `PromptAcpReq`, `AnswerPermissionReq`.
- `internal/wire/control.go`: `KillSessionReq.ReqID`.
- `internal/daemon/daemon.go`: `peekReqID`, a per-frame `ops` copy whose `sendError` stamps `req_id`, `sendACPError` through `ops.sendError`, `ack()` on CREATE_SESSION / PROMPT_ACP / ANSWER_PERMISSION / KILL_SESSION.
- `internal/plugin/limiter.go`: no change (ACK is S→C).
- `internal/wire/testclient/client.go`: a `Request` helper.
- `cmd/hivegui/app_control.go`: ignore `FrameAck` (logged at debug), so it isn't the "unexpected frame" path.
- `cmd/hived-ws-bridge/main.go`: forward `ACK`.
- `plugins/sdk/hive-plugin.mjs` and the `plugins/webhook/hive-plugin.mjs` copy: `request()`, `Frame.ACK`, `HiveError.req_id`, `@typedef Ack`; header "Node >= 22".
- `internal/plugin/sdk_drift_test.go`: add `Ack` to the typedef map.
- `docs/plugins.md`: request/reply section; frame table rows.
- `plugins/sdk/README.md`: Node wording.
- `internal/buildinfo/contract.go`: bump.

P2
- `internal/wire/control.go`: `CreateSpec.WorkflowRunID`, `CreateSpec.ResultSchema`, error codes `invalid_workflow_run_id`, `invalid_result_schema`; `ParsePrincipal`.
- `internal/wire/acp.go`: `PromptAcpReq.WorkflowRunID`.
- `internal/daemon/daemon.go`: `composePrincipal(ops.principal, runID) (string, error)` used at `:806`, `:1556`, `:1706`.
- `internal/acp/resultschema.go`: `CheckSchema(raw) error`, `CheckResult(schema, result json.RawMessage) error`.
- `internal/registry/create.go` (P1: `OnEntry` create option) / `acp.go` / `persist.go` / `registry.go`: store `ResultSchema` on Entry + meta; validate at create; enforce in `SubmitResult`; add `HIVE_SUBMIT_SCHEMA` in `acpExtras`.
- `cmd/hived/mcpsubmit.go`: build the tool from env; pre-check.
- `internal/agent/pi/hive.ts`: parameters from env.
- `cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx`: `originLabel` via the new `src/lib/principal.ts`.
- `cmd/hivegui/frontend/src/app/state.ts`: comment.
- SDK + webhook copy: `CreateSpec` doc mentions `workflow_run_id`, `result_schema`.
- `docs/plugins.md`, `docs/design-docs/acp-workflows.md` (trust model: composed principal).
- `internal/buildinfo/contract.go`.

P3
- `internal/acp/conn.go`: `Notify(method, params)`.
- `internal/acp/agent.go`: `Cancel(id)`, `MethodSessionCancel`.
- `internal/acp/types.go`: method const.
- `internal/acp/acptest/fake.go`: track cancelled sessions; a `FlagBlock` prompt that sees cancel returns `stopReason:"cancelled"`; a pending fake permission request answered `cancelled` ends the turn the same way.
- `internal/registry/acp.go`: `CancelACP(id)`, `acpResult.stopReason`, `acpMsgLocked` sets it.
- `internal/wire/acp.go`: `CancelAcpReq`, `AcpTranscriptMsg.StopReason`, `ErrCodeACPNotBusy`.
- `internal/wire/frame.go`: `FrameCancelAcp`.
- `internal/daemon/daemon.go`: handler.
- `internal/plugin/limiter.go`: cheap (default).
- `cmd/hivegui/app_calls.go`: `CancelAcp(sessionID)`.
- `cmd/hived-ws-bridge/main.go`: `cancel_acp`.
- `internal/wire/testclient`.
- SDK `Frame`, webhook copy, `docs/plugins.md`.
- `cmd/hivegui/frontend/src/app/state.ts`: type gets `stop_reason`.
- contract.

P4
- `internal/wire/control.go`: `PluginMessage`, `MaxPluginMessage`, `ErrCodePluginNotRunning`.
- `internal/wire/frame.go`: `FramePluginMessage`.
- `internal/daemon/daemon.go` + `internal/daemon/plugin*.go`: route by tag. A registry of plugin connections keyed by id; the UI-client set already exists for `reviewAnnouncer`.
- `internal/plugin/limiter.go`: `costFanout`.
- `cmd/hivegui/app_control.go`: emit `plugin:message`.
- `cmd/hivegui/app_calls.go`: `SendPluginMessage`.
- `cmd/hivegui/frontend/src/app/events.ts`: typed passthrough (C consumes it).
- `cmd/hived-ws-bridge/main.go`.
- `internal/wire/testclient`.
- SDK `postMessage`, typedef, webhook copy, `docs/plugins.md`.
- contract.

P5
- `internal/worktree/worktree.go`: `EncodeWorkflowKey`, `MergeBranches(ctx, wtPath string, branches []string) (merged []string, conflict *MergeConflict, err error)`, `verifyCommitish`.
- `internal/registry/worktrees.go`: `PrepareWorktree(ctx, req) (wire.PreparedWorktreeMsg, error)` under `gitMu`.
- `internal/wire/control.go`: `PrepareWorktreeReq`, `PreparedWorktreeMsg`, `MergeConflict`, codes.
- `internal/wire/frame.go`.
- `internal/daemon/daemon.go`: handler (`runOp`), `FrameCost` = `costExpense`.
- `cmd/hivegui/app_calls.go`: binding (C may show conflicts).
- bridge, testclient.
- SDK `encodeWorkflowKey`, webhook copy, `docs/plugins.md`.
- contract.

P6
- `internal/plugin/manifest.go`: `Node string json:"node,omitempty"` + validation.
- `internal/plugin/manager.go`: `resolveCommand` login-PATH fallback returning `(bin, pathUsed)`, `pluginEnv` PATH override, the `checkNode` probe in `spawn`.
- `internal/plugin/doc_test.go`, if it pins the manifest docs.
- `cmd/hivegui/frontend/src/components/modals/PluginsPanel.tsx`: Restart button (`Button` primitive, no hard-coded key).
- `cmd/hivegui/frontend/src/app/events.ts` / `plugins.ts`: failed toast.
- `DESIGN.md:100`: carve-out text.
- `docs/plugins.md:81,317`: Node wording + manifest `node` field + PATH behaviour.
- `internal/buildinfo/contract.go`.
- `.changesets/528-plugin-node-path-restart.md`.

### New files

- `internal/acp/resultschema.go`, `internal/acp/resultschema_test.go`, `internal/acp/testdata/resultschema/*.json` (P2).
- `cmd/hivegui/frontend/src/lib/principal.ts` + `test/unit/principal.test.ts` (P2).
- `plugins/sdk/hive-plugin.test.mjs`: `node --test`, a fake daemon over a temp unix socket (P1, extended in P4/P5).
- `internal/daemon/reqid_test.go` (P1), `internal/daemon/plugin_message_test.go` (P4), `internal/daemon/prepare_worktree_test.go` (P5).
- `.changesets/528-plugin-node-path-restart.md` (P6).

### Tests

P1
- `internal/daemon/reqid_test.go`:
  - `TestReqID_EchoedOnError` (PROMPT_ACP to an unknown session with `req_id:7` → ERROR `req_id:7`);
  - `TestReqID_CreateSessionAcked` (ACK carries the id that the following SESSION_EVENT(added) names);
  - `TestReqID_AsyncCreateErrorEchoed` (a create failing inside `runOp` echoes it);
  - `TestReqID_CreateAckedBeforeAdapterStarts` (FlagBlock adapter: ACK arrives while phase is still spawning);
  - `TestReqID_KilledMidCreateErrors` (ERROR with `req_id`, never silence);
  - `TestReqID_AbsentMeansNoAck` (no `req_id` → no ACK; GUI behaviour unchanged).
- `internal/plugin/sdk_drift_test.go`: typedef map + frame table (existing tests, extended).
- `plugins/sdk/hive-plugin.test.mjs`:
  - `request resolves on matching req_id`;
  - `request rejects on ERROR with req_id`;
  - `interleaved replies resolve the right promises`;
  - `timeout rejects`;
  - `close rejects every pending request`.
- `cmd/hived/plugin_e2e_test.go`: `TestE2E_SDKRequest_CreateSessionReturnsID` (real hived, real SDK).
- CI: add `node --test 'plugins/sdk/*.test.mjs'` to the probe step in `.github/workflows/ci.yml`.

P2
- `internal/daemon/acp_test.go`:
  - `TestPluginPrincipal_ComposedWithRun` (SpawnedBy = `plugin:p/workflow:r1`);
  - `TestPluginPrincipal_CannotClaimUser` (a raw `"spawned_by":""` / `"spawned_by":"user"` in JSON is ignored);
  - `TestPluginPrincipal_CannotClaimOtherPlugin` (`workflow_run_id:"x/../plugin:q"` → `invalid_workflow_run_id`);
  - `TestWorkflowRunID_RefusedFromUser`;
  - `TestWorkflowRunID_OrchestratorStaysFalse`;
  - `TestPromptAcp_OriginCarriesRun`;
  - `TestCreateHello_WorkflowRunID`.
- `internal/wire`: `TestParsePrincipal` (user, `plugin:x`, `plugin:x/workflow:r`, malformed).
- `test/unit/principal.test.ts` + `test/dom/acp-transcript.test.tsx`: `renders workflow origin`.
- `internal/acp/resultschema_test.go`:
  - `TestCheckSchema_Subset` (rejects `$ref`, `oneOf`, missing `required` key, `additionalProperties:true`, depth 9, > 8 KiB);
  - `TestCheckResult_Fixtures` (table over `testdata/resultschema`);
  - `TestCheckResult_ErrorPath` (message names `data.findings[2].severity`).
- `internal/registry/acp_test.go`:
  - `TestSubmitResult_ValidatesAgainstSchema`;
  - `TestSubmitResult_ErrorStatusSkipsSchema`;
  - `TestSubmitResult_RetryAfterRejection` (a second valid submit in the same turn is recorded);
  - `TestResultSchema_SurvivesRestart`;
  - `TestCreate_InvalidResultSchemaRefused`;
  - `TestCreate_ResultSchemaOnPTYRefused`.
- `cmd/hived/mcpsubmit_test.go`:
  - `TestMCPSubmit_ToolsListCarriesSchema`;
  - `TestMCPSubmit_PrecheckRejectsBadData`;
  - `TestMCPSubmit_NoSchemaUnchanged`.
- `internal/agent/pi/hive.test.ts`:
  - `submit_result parameters carry HIVE_SUBMIT_SCHEMA`;
  - `daemon rejection is surfaced as a tool error` (the existing pattern).

P3
- `internal/acp/agent_test.go`: `TestCancel_SendsNotification` (calls.log shows `session/cancel <sid>` and no id).
- `internal/registry/acp_test.go`:
  - `TestCancelACP_StopReasonCancelled` (FlagBlock);
  - `TestCancelACP_AnswersPendingPermissionCancelled` (FlagPermission);
  - `TestCancelACP_NotBusy`;
  - `TestStopReason_EndTurnAndClearedOnPrompt`.
- `internal/daemon/acp_test.go`: `TestCancelAcpFrame_PluginAndUser`, `TestCancelAcp_RefusedOnSessionConn`.
- `test/dom/acp-events.test.ts`: `stop_reason passes through state`.

P4
- `internal/daemon/plugin_message_test.go`:
  - `TestPluginMessage_PluginToGUIStampsID` (a forged `plugin_id` is overwritten);
  - `TestPluginMessage_NotRelayedToOtherPlugins`;
  - `TestPluginMessage_NotRelayedToHivebarOrSession`;
  - `TestPluginMessage_GUIToPlugin`;
  - `TestPluginMessage_TargetNotRunning`;
  - `TestPluginMessage_RefusedOnSessionConn`;
  - `TestPluginMessage_SizeCap`;
  - `TestPluginMessage_SlowGUIDoesNotStallPlugin`.
- `cmd/hivegui/app_calls_test.go`: `TestSendPluginMessage`.
- `plugins/sdk/hive-plugin.test.mjs`: `postMessage frames`.
- e2e-real: `plugin-message.spec.ts`, a round-trip through `hived-ws-bridge` with an isolated state dir (C reuses the harness).

P5
- `internal/worktree/worktree_test.go`:
  - `TestEncodeWorkflowKey` (`fix[2]/work`→`fix_2/work`, `work/each[3]/do`→`work/each_3/do`, every output passes `git check-ref-format --branch`, injectivity over a generated set, rejects `Fix`, `a..b`, `-x`, `a[b]`);
  - `TestMergeBranches_Clean`;
  - `TestMergeBranches_ConflictReportsBranchAndFiles` (the worktree is left clean after abort);
  - `TestMergeBranches_HooksNotRun` (a `pre-merge-commit` / `post-merge` hook writing a marker never runs);
  - `TestMergeBranches_NoIdentity` (isolated `GIT_CONFIG_GLOBAL=/dev/null` → typed error).
- `internal/daemon/prepare_worktree_test.go`:
  - `TestPrepareWorktree_FromArbitraryBase`;
  - `TestPrepareWorktree_ComposesBranch`;
  - `TestPrepareWorktree_RefusesExistingBranch`;
  - `TestPrepareWorktree_RefusesOptionLikeBase`;
  - `TestPrepareWorktree_MergeConflictTyped`;
  - `TestPrepareWorktree_PathCollisionResolved` (directory suffixed, branch unchanged);
  - `TestPrepareWorktree_SessionAdoptsPath` (CREATE_SESSION `worktree_path` → entry claims it).
- `plugins/sdk/hive-plugin.test.mjs`: `encodeWorkflowKey matches Go` (shared fixture `internal/worktree/testdata/workflow-keys.json`).

P6
- `internal/plugin/manager_test.go`:
  - `TestResolveCommand_LoginPATHFallback` (seam over `proc.LoginPATH`);
  - `TestPluginEnv_PATHIsResolvedPATH`;
  - `TestSpawn_NodeTooOldRefused` (a fake `node` script printing `v24.1.0`, detail names the floor and the found version);
  - `TestSpawn_NodeProbeFails`;
  - `TestSpawn_NodeOK`;
  - `TestHeadlessBuiltin_Starts` (injected `Config.Builtin` `fstest.MapFS` with `main`);
  - `TestFailedThenRestart_ResetsRestarts`.
- `internal/plugin/manifest_test.go`: `TestManifest_NodeFieldValidated`.
- `test/dom/settings-plugins.test.tsx`:
  - `failed row shows Restart and sends enable`;
  - `refused row shows Restart`;
  - `running row has no Restart`.
- `test/dom/plugin-events.test.ts`: `failed transition toasts once`.
- `test/e2e/settings-plugins.spec.ts`: `restart a failed plugin` (mock bridge).

## Verification

Run for every phase, from the repo root:

```sh
GT=go$(sed -n 's/^go //p' go.mod)
GOTOOLCHAIN=$GT go build ./... && GOTOOLCHAIN=$GT go test ./...
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os GOTOOLCHAIN=$GT go vet ./...; done
./scripts/ci-bootstrap.sh && (cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
CI=1 scripts/test.sh go unit dom e2e
node --test 'plugins/sdk/*.test.mjs'
(cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real)        # P1, P4, P5
scripts/check-daemon-contract.sh origin/main HEAD
scripts/check-changeset.sh
scripts/ui-lint.sh                                             # P2, P6 (GUI touched)
```

## Decision log

- **2026-10-25:** Typed-result validation is a hand-written Go validator for the IR subset, with no new dependency. Why: the user decided it, and `go.mod` has no JSON Schema library.
- **2026-10-25:** The validator's authority is `Registry.SubmitResult`. The tools only display the schema (`mcp-submit` also pre-checks). Why: one enforcement point covers Pi and MCP alike, and the rejection already reaches the model as a tool error that it can retry.
- **2026-10-25:** The envelope `{status, summary, data}` is kept; the node schema governs `data` when `status:"ok"`. Rejected: replacing the whole object, which breaks F3's `status:"error"` path and every existing `result` consumer.
- **2026-10-25:** Provenance is daemon-composed from the tag plus a charset-restricted `workflow_run_id`. It is also applied to PROMPT_ACP and the HELLO create. Rejected: trusting a client-sent principal string. User decision.
- **2026-10-25:** Correlation is a generic `req_id` peek plus ERROR echo, and an `ACK` for reply-less requests. Rejected: one reply frame per verb (more frames, same information); correlating via SESSION_EVENT(added) (not unique under fan-out).
- **2026-10-25:** The daemon composes worktree branch names (`hive/wf/<run>/<encoded key>`) and chooses the path. Rejected: a plugin-named branch, since `CreateWorktreeAt` checks out an existing branch, so a plugin could put an agent on `main`.
- **2026-10-25:** Merges are sequential, run with hooks disabled, and the worktree is kept on conflict. Rejected: an octopus merge (it can't name the conflicting branch).
- **2026-10-25:** `PLUGIN_MESSAGE` is bidirectional. Why: C needs GUI→engine (start run, answer human node, replay), no path exists today, and adding it later means another daemon PR and contract bump, the same reason the spec gives for criterion 8.

## Progress

- **2026-10-25:** Research complete. Plan drafted. Not approved; open questions below.

## Open questions

1. **Node floor (security/product).** The recommendation is **26.0.0** for the engine, since it is the only supported line whose `--permission` fences the network.
   - It excludes users on Node 22/24 LTS, which is most users today. 26 becomes Active LTS on 2026-10-28.
   - The alternative, 24 without network fencing, contradicts the design's sandbox rule and spec 529 #9.
   - Blocks: the manifest `node` value B ships, and the C "requires Node X" copy.
2. **`PLUGIN_MESSAGE` GUI→plugin direction (scope).** It is added to A beyond criterion 6's wording. Approve, or move it to a later daemon PR for C (another contract bump).
3. **Daemon-run `git merge` (security).** Agent-authored branches are merged by `hived`. Hooks are disabled (`core.hooksPath`), but merge drivers and filters from repo `.gitattributes` with matching user config, and `.git/config` in the main repo, still apply. Accept, or also pass `-c merge.*.driver=` overrides? Merge commits use the user's git identity. A repo with none fails with a typed error and no fallback identity. Confirm.
4. **Who may CANCEL_ACP (security).** The plan uses the same scope as PROMPT_ACP: any plugin, any ACP session. The alternative restricts a plugin to sessions it spawned (`SpawnedBy` has prefix `plugin:<id>`). PROMPT_ACP is unrestricted today, so restricting only cancel buys little; restricting both changes shipped behaviour.
5. **Base for `PREPARE_WORKTREE` (security).** Any verified commit-ish, so a plugin can branch from any local ref (read-only exposure of repo content it can already read through sessions). Confirm no allowlist is needed.
7. **GUI→plugin containment (security).** UI plugin modules run inside the app (`plugin-host.ts`, `test/e2e/plugin-containment.spec.ts`). Confirm that `SendPluginMessage` and the `plugin:message` event stay host-only. If they don't, a third-party UI plugin could answer the engine's human nodes or start runs as the user. P4 adds `plugin-containment.spec.ts: a UI plugin cannot call SendPluginMessage`. If containment can't guarantee it, the GUI→plugin path needs a host-side allowlist or a user-confirmation step. Decide before C.
6. **Phase count.** Six PRs, each with a contract bump. Combine P1+P2 (and P3) into one PR if fewer review rounds matter more than PR size.

## Spec corrections proposed (for the user)

- **Criterion 1:**
  - PROMPT_ACP origin (`daemon.go:1706`) must also carry the run; the spec names only `registry/acp.go`.
  - `principalOf` is at `:2307`.
  - The HELLO create path (`:806`) needs the same handling.
- **Criterion 2:** validation lives in `Registry.SubmitResult` (one place). `mcpsubmit.go` and `pi/hive.ts` display the schema; they are not the validators. The envelope is kept and the schema applies to `data`.
- **Criterion 3:** add `stop_reason` to `ACP_TRANSCRIPT`. Without it "reports `stopReason: cancelled`" is not observable by any client, and B can't tell `end_turn` from `cancelled`.
- **Criterion 4:** an SDK counter alone correlates nothing. It needs `req_id` echoed on ERROR and a reply for CREATE_SESSION (today fire-and-forget, so a plugin can't learn its session's id). This is daemon work, not SDK-only.
- **Criterion 5:**
  - Floor findings above.
  - Node 18 appears in **five** places, not two.
  - The design doc's "embed is UI-only" is about content, not mechanism. No embed change is needed; a test proves a headless builtin starts.
- **Criterion 6:** make it bidirectional (open question 2).
- **Criterion 8:**
  - The daemon composes the branch and validates `base`; a raw plugin branch name is a hole.
  - The encoding is `[N]`→`_N`, and the SDK exports it.
  - A pristine prepared worktree can be reclaimed at boot.
- **Criterion 9:** the failed state is already visible (Settings row). What's missing is a recovery action that works, because the checkbox can't re-enable an enabled plugin.
- **Spec 529 #13** cites "A, criterion 8" for the failed plugin; it should be criterion 9.
- **Success-criteria preamble:** "Each daemon-side PR bumps": P6 touches only `internal/plugin` and the GUI. The gate script doesn't require a bump there, but the plan bumps anyway, because a GUI with Restart against an old daemon is harmless while the PATH/Node behaviour is daemon-side. Optional.
