# ACP workflows

Can Hive drive agents through the Agent Client Protocol (ACP) as the nodes of a
workflow — prompt them, read back typed results, check those results with real
commands, and still let a human take any node over in its own terminal? This doc
answers that with evidence from running the agents (spec
[492](../product-specs/492-research-acp-as-hive-s-engine-driven-workflow-tran.md)),
decides where a workflow engine would live, and orders the specs that would
build it. It ships no product code.

It is a **parallel track** to [agent-orchestration.md](agent-orchestration.md).
That plan makes an *agent* the orchestrator (a granted session messages, watches
and spawns siblings). This one makes a *deterministic engine* the orchestrator.
Both are allowed to spawn sessions, so both run under the one trust model in
[One trust model](#one-trust-model).

## Verdict

**Verdict: go**

The rule (spec 492, success criterion 6), applied to Claude and Codex: an agent
is *ok* when MCP injection and the Hive-PTY takeover both pass. **go** if at
least one is ok; **no-go** if MCP fails for both, or takeover fails for both;
otherwise **undecided** — never reported as go. A **no-go** means this doc recommends
stopping the ACP track: no follow-up spec below is started, and the probe is
re-run only when an adapter's version changes. `scripts/acp-probe/check-doc.mjs`
recomputes this verdict from the results files in CI, so it cannot drift from
the evidence.

Both Claude and Codex are ok. Pi works over ACP but ignores injected MCP
servers, so a Pi node returns its result through the escape hatch below.

## How the evidence was gathered

`scripts/acp-probe/` (see its [README](../../scripts/acp-probe/README.md)) runs one
scripted session per agent and checks each capability **out-of-band** where it
can:

- **MCP injection.** `session/new` injects a stdio MCP server exposing
  `submit_result`. The check passes only when *that server* received this run's
  nonce. What the agent says it did is not evidence.
- **Permissions.** The probe answers `session/request_permission`, allowing only
  the exact injected tool. It matches on the structured tool identity, never on
  the title, because a `Bash` call can be *titled* `submit_result`.
- **Load.** A **new** adapter process calls `session/load`. The check passes
  only if the replay contains an *agent-authored* update with the nonce.
  Replaying our own prompt proves nothing.
- **Takeover (b), Hive PTY.** The probe runs the same argv Hive's `ResumeArgs`
  builds (`internal/agent/`), in a real PTY. It passes when the agent's own
  reply, as ACP delivered it, renders on screen.
- **Takeover (a), headless.** The CLI's headless resume (`claude -p --resume`,
  `codex exec resume`, `pi --session-id … -p`) is asked for the nonce.

Every agent ran on 2026-10-02 local time (2026-10-03 UTC, the date in the
results file names), in a fresh temp cwd, with the user's normal
agent configuration. Versions: claude 2.1.288, codex-cli 0.157.1, pi 1.0.0.
Gemini CLI and Copilot CLI are not installed on the probe machine, so their
row is from documentation only (spec 492 decision log). Each results file in
`scripts/acp-probe/results/` keeps only whitelisted fields: verdicts,
capability flags, version strings and update kinds.

## Capability table

Cells are tagged **[run]** (verified by the probe; the first word must equal
`results/<agent>-*.json`) or **[doc]** (from the cited source, not run).

<!-- capability-table:start -->
| Agent | ACP route | MCP via `session/new` | Permission prompts | Plan / tool-call streaming | Load / resume | Reopen headless (a) | Reopen in Hive PTY (b) |
|---|---|---|---|---|---|---|---|
| Claude | adapter `@agentclientprotocol/claude-agent-acp@0.85.1` (wraps the Agent SDK) [run] | pass [run] — stdio; http + sse advertised | yes [run] — tool identity in ACP's `name` field | yes [run] — `plan` comes from the TodoWrite tool | pass [run] — also list / resume / close / delete / fork | pass [run] | pass [run] — but only with `--resume`; Hive picked `--session-id` here at probe time ([F1](#f1), fixed in #494) |
| Codex | adapter `@agentclientprotocol/codex-acp@2.1.1` (drives `codex app-server`) [run] | pass [run] — 1 of 5 runs made no call ([F3](#f3)); http advertised, no sse | no [run] — none asked in the default `agent` mode ([F4](#f4)) | partial [run] — `tool_call` yes, `plan` not observed | pass [run] — also list / resume / close / delete / fork | fail [run] — thread writer lock ([F2](#f2)) | pass [run] — after answering the update nag and folder-trust dialogs |
| Gemini | native `gemini --acp` (0.62.0) [doc] | yes [doc] — stdio / http / sse | yes [doc] — default / autoEdit / yolo / plan | partial [doc] — no `plan` updates | load [doc] — no list / resume / close | likely [doc] — `loadSession` uses the same selector as `--resume` | unknown [doc] |
| Copilot | native `copilot --acp --stdio` (1.0.91, public preview) [doc] | unknown [doc] — the changelog claims it; [issue #1040](https://github.com/github/copilot-cli/issues/1040) says it is ignored | yes [doc] | yes [doc] | load [doc] — plus close; list / resume unknown | unknown [doc] — closed binary | unknown [doc] |
| Pi | adapter `pi-acp@0.0.34` (community, runs `pi --mode rpc`) [run] | fail [run] — `mcpServers` accepted but ignored ([F5](#f5)) | no [run] — pi has no approval gate ([F5](#f5)) | partial [run] — `tool_call` yes, no `plan` | pass [run] — also list / delete | pass [run] | pass [run] — ACP id equals the pi session id |
<!-- capability-table:end -->

Doc sources: [Gemini ACP mode](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/acp-mode.md),
[Copilot ACP server](https://docs.github.com/en/copilot/reference/copilot-cli-reference/acp-server),
[Copilot changelog](https://github.com/github/copilot-cli/blob/main/changelog.md),
[ACP session setup](https://agentclientprotocol.com/protocol/session-setup),
[ACP agents list](https://agentclientprotocol.com/get-started/agents).

## Findings

<a id="f1"></a>**F1 — Hive's Claude resume missed transcripts. This bug was
independent of ACP. Fixed in #494:** `encodeClaudeProjectDir` now ports
claude's own encoder, and the probe's mirror follows it.
- **Mismatch.** Claude names its transcript directory by folding *every*
  non-alphanumeric character of the cwd to `-`. Hive's `encodeClaudeProjectDir`
  (`internal/agent/claude.go`) folded only `/`, `.` and `:`.
- **What breaks.** For a cwd containing `_` (every macOS `$TMPDIR` does, and
  so do many repo names), `claudeSessionExists` returned false. `claudeResumeArgs`
  then fell back to `claude --session-id <id>`, and claude exits with
  `Error: Session ID <id> is already in use.` (observed). So Hive's Restart and
  Revive of such a Claude session failed, in plain PTY sessions.
- **Effect on the verdict.** The probe tested the ACP question with the observed
  encoding and recorded Hive's branch separately (`hive_branch` in the results;
  the 2026-10-03 Claude run shows `session-id`). Since #494 the two encoders
  agree, so `hive_branch` always equals the branch the probe takes.

<a id="f2"></a>**F2 — A Codex thread has one writer, and the interactive CLI leaves a
daemon holding it.**
- **What happens.** `codex resume <id>` in a PTY starts a managed
  `codex app-server` daemon. That daemon has parent pid 1 and its own process
  group, and it outlives the TUI (`detached_daemons_after_pty: 2`). Until it
  releases the thread, any other writer fails with `thread … already has an
  active writer`. That covers the headless resume above, and equally an ACP
  adapter taking the session back.
- **Design consequence.** Handing a node from PTY back to ACP must be an
  explicit step: close the PTY, then make sure the thread is released, then call
  `session/load`. An ACP node and a PTY view cannot write to the same Codex
  session at once.

<a id="f3"></a>**F3 — Codex sometimes doesn't call the injected tool.**
- **Observation.** One of five Codex runs (one of the three run in parallel)
  produced no tool call at all; the agent answered directly.
- **Likely cause, not proven.** The injected MCP server may not have been ready
  on the first turn.
- **Design consequence.** The engine treats a missing `submit_result` as a node
  failure with one bounded re-prompt. It must never be read as success.

<a id="f4"></a>**F4 — In its default mode, Codex calls the injected tool without
asking.** codex-acp starts in mode `agent` and called `submit_result` with no
`session/request_permission`. Whether `read-only` or `workspace-write` would
prompt for an MCP tool is untested. Either way, the engine must set every
node's mode explicitly from the user's per-agent setting and never rely on the
adapter's default (see the trust model below).

<a id="f5"></a>**F5 — Pi accepts `mcpServers` but never starts them, and pi has no
approval gate.**
- **Result transport.** A Pi node cannot return `submit_result` over MCP. Hive
  already loads its own Pi extension at spawn (`internal/agent/pi.go`); that
  extension can register the same tool. That is the per-vendor escape hatch,
  and it needs no adapter of our own.
- **Permissions.** Pi never asks before using a tool, over ACP or in a PTY. A
  Pi node can therefore run only where the user's setting for Pi already
  allows unattended tool use, exactly as in a Pi PTY session today.

**F6 — ACP has no PTY.** The spec defines no PTY and no interactive stdin. Its
`terminal/*` methods run commands *for* the agent; they do not let a human
type to it. So "human takeover" is necessarily:

1. stop the ACP process;
2. run the CLI's own resume in a Hive PTY session;
3. reload over ACP afterwards.

Claude, Codex and Pi all keep ACP sessions in their normal session stores, so
this works for them (column b). That is the user's original claim, now verified
for three agents.

**F7 — Permission requests can be matched on a stable field, for Claude.**
`claude-agent-acp` puts the tool name in ACP's `name` field (`mcp__hive__submit_result`)
and in `_meta.claudeCode.toolName`. Codex and Pi raised no requests, so their
identity fields are unobserved. The engine's allow rule must match on such a
structured identity and fail closed when none is present.

## Architecture

### Session kinds

`acp` becomes a second session kind next to `pty`. PTY stays the default, and
interactive sessions behave exactly as today.

- **Ownership.** `hived` owns the ACP child process and its stdio, just as it
  owns a PTY. That is what lets an ACP session survive GUI reloads, show up in
  the registry, and carry provenance.
- **Code path.** Today `session.Start` always allocates a PTY
  (`internal/session/session.go:144`), and `registry.Entry` has no kind field.
  The first follow-up spec adds one behind the existing `Registry.Create`
  spawn seam.
- **State tier.** ACP's `session/update` stream gives exact turn boundaries and
  permission requests. That makes it a fifth state tier, `acp`, ranked with
  `hook` (see [control-plane.md](control-plane.md#tiers-of-knowledge)).

### Workflow nodes

| Node | Transport | Result | Used for |
|---|---|---|---|
| agent | ACP session (`acp` kind) | `submit_result` through the injected MCP server (Pi: through Hive's Pi extension), with the schema set by the node definition | Claude, Codex; Pi where the user allows unattended tool use; Gemini and Copilot once probed |
| agent (escape hatch) | per-vendor headless (`claude -p`, `codex exec`) or a Hive extension | the same `submit_result` schema | agents or features ACP lacks (F5, Pi) |
| check | a plain command in the node's cwd | exit code plus captured output | every pass/fail claim ("tests pass", "it builds") — never a model's word |
| human | a Hive PTY session through `ResumeArgs` | the node resumes when the user hands it back | takeover of any agent node (F1, F2, F6) |

Agents without ACP (shell, Aider, custom) remain usable: as check nodes, through
the headless escape hatch, or as human/PTY nodes.

`hived` serves `submit_result`. Each agent node gets its own stdio MCP server
instance (a `hived` subcommand) that records the call against that node in the
registry. So results are typed, and identical across agents, by construction.
The submit tool is the **only** tool the daemon auto-allows. The match is on
exact structured identity (F7), bound to that node's own server name: Hive
names the server per node, so a user's MCP server that happens to be called
"hive" never matches.

### Where the engine lives

| | Daemon core | Bundled 460 headless plugin |
|---|---|---|
| For | Owns the ACP stdio directly. One process enforces trust. Nothing new on the wire to drive nodes. | Crash-isolated: a bug restarts the plugin, not `hived`, so running sessions survive. Ships without `DaemonContract` bumps. Workflows-as-code fits the existing JS plugin SDK (`plugins/sdk`). Supervised by `hived` with backoff, so it outlives GUI reloads. |
| Against | Every engine bug can crash the daemon, and a daemon restart kills every PTY session. Every engine change is a contract bump. The daemon grows a scheduler, retry policy and a DSL. | It has no stdio channel to agents (`pluginModeAllowed`, `internal/daemon/plugins.go:175`), so it needs new control frames to prompt nodes and collect results. Plugins are fully trusted and unsandboxed (spec 460), so trust can only be enforced on what passes through `hived`. |

**Decision: split along the trust boundary.**
- **`hived` core:** the `acp` session kind, the transport, the per-node
  `submit_result` server, permission relay and caps, and provenance.
- **Bundled first-party 460 plugin:** the engine — graph scheduling, check
  nodes, bounded re-prompts and the workflow definitions.

The plugin drives nodes only through new control frames, so every spawn and
prompt passes the daemon's checks.

Rejected:
- **The whole engine in core.** Crash blast radius and contract churn, for no
  trust gain.
- **The plugin owning ACP processes itself.** Those sessions would be invisible
  to the registry and the GUI, and they would escape provenance. It would also
  give up the "seeing every agent in the hive" goal.

## One trust model

ACP workflows and phases 1–3 of [agent-orchestration.md](agent-orchestration.md)
share one set of rules. A *principal* is either a granted session (phases 1–3)
or the engine plugin, which `hived` identifies by its plugin socket.

1. **Same project only.** Neither principal reaches outside its project (the
   cross-project non-goal of both tracks).
2. **The daemon stamps provenance.**
   - **Spawns.** `SpawnedBy` becomes a principal string: `session:<id>`,
     `workflow:<run-id>`, or `plugin:<id>` for a create sent on a plugin's
     own socket (spec 496, phase 1); empty means the user. It is shown on
     every row the principal creates.
   - **Prompts.** Every engine prompt records its origin (run and node) in the
     registry's activity for that session.
   - The caller never supplies either value.
3. **Nothing is inherited, and nothing escalates.**
   - A spawned node never holds the orchestrator grant.
   - Its ACP mode is set explicitly and capped at the user's per-agent setting.
     For example, Codex runs at most `workspace-write` unless the user allows
     more; Claude never runs `bypassPermissions` unless the user allows it. This
     closes F4.
   - Pi runs only where the user's setting allows unattended tool use (F5).
4. **Permission requests go to the user.** The engine never answers
   `request_permission` except for its own `submit_result`, matched by exact
   identity and bound to the node's own server name (F7). Everything else surfaces as `waiting_permission`, just like a
   hook-tier session.
5. **Caps.** The engine is limited to a fixed number of live nodes per run, in
   the same way spec 391's planned `maxChildrenPerOrchestrator` will limit a granted session (not built yet).
   Its frames are charged against the plugin token bucket (460).

**Containing the drift.** Two tracks that both spawn sessions drift when each
grows its own checks. Both therefore go through one code path:
- spawns use `Registry.Create`;
- authorization is one daemon function, `authorize(principal, action, target)`,
  that phase 3 and the engine frames both call;
- provenance uses the one `SpawnedBy` field.

A spec that adds a spawn path outside these is out of policy.

## Gates for the ACP track

The 389–391 gates count agent-initiated `hived msg` and `wait`. An
engine-driven workflow produces neither, so this track has its own gates:

| Gate | Passed when | Status |
|---|---|---|
| A — protocol | the probe's go/no-go is **go** | passed (this doc) |
| B — ACP session kind | ≥2 real tasks run as `acp` sessions for each of Claude and Pi, at least 1 per agent taken over in a PTY and handed back, with no lost turns ([run log](#gate-b-run-log)). The fake-agent test `TestTakeoverHandBackLosesNoTurn` is the automated no-lost-turn proof; these runs confirm it against the real adapters | open — spec 1 shipped (#499–#502); the run log below is not yet filled (#503) |
| C — engine | ≥5 runs of the plan → implement → review loop in two weeks that the user would repeat, at least one with a cross-vendor reviewer | after spec 2 |
| D — monitoring | ≥3 code-defined workflows each reused ≥3 times | before spec 4 |

Re-run the probe whenever a pinned adapter version changes. `check-doc.mjs`
then fails CI until this table matches the new results.

<a id="gate-b-run-log"></a>**Gate B run log.** One row per real task, run by
the user on their own subscription (CI cannot). *Replayed after restart* records
whether the transcript came back with its tool and plan rows after a `hived`
restart, or with message text only (risk Q3 in the 496 exec plan).

| Date | Agent | Task | Taken over + handed back | Lost turns | Replayed after restart | Outcome |
|---|---|---|---|---|---|---|

## Follow-up specs, in order

0. **Fix Claude transcript lookup (bug, independent) — done in #494.** Hive's
   `encodeClaudeProjectDir` didn't fold `_` and the other non-alphanumeric
   characters that Claude does. So Restart and Revive of a Claude session in
   such a cwd ran `--session-id` and failed with "Session ID … is already in
   use" (F1).
1. **ACP session kind — shipped in spec 496 (four phases).** Hive could only
   run agents in a PTY. An `acp` session kind, owned by `hived`, gives exact
   state, typed output and permission relay for agents that speak ACP. It
   includes the `acp` state tier and PTY takeover and hand-back (F2, F6): Take
   Over in Terminal and Hand Back to ACP move a session between the two kinds
   in the same conversation. Gate B waits on its run log.
2. **Code-defined workflow engine.** Multi-agent loops are run by hand today.
   A bundled plugin runs workflows written in code over ACP, check and human
   nodes, with `submit_result` and the shared trust model. Its design is
   [spec 495](../product-specs/495-design-hive-s-workflow-engine-typed-ts-workflows-a.md);
   building it waits on gate B.
3. **Live workflow monitoring.** The user can't see where each agent is in a
   running workflow. The GUI shows the run graph, each node's state, and its
   provenance.
4. **Workflow authoring.** Workflows can only be written in code. A GUI editor
   for workflow graphs is built on the gate-D evidence.

**Hive-maintained adapters are not needed.**
- Claude and Codex work through the maintained `@agentclientprotocol` adapters.
- Pi works through `pi-acp`; its only gap (MCP, F5) is covered by Hive's
  existing Pi extension.
- Gemini and Copilot are native, so they need a probe run, not an adapter.
- Aider has no ACP; it stays on the headless or PTY escape hatch.

Writing an adapter becomes worth it only if `pi-acp` (a 0.0.x community MVP)
is abandoned.

## What is deliberately not here

- No product code. PTY stays the default session kind.
- Hive acting as an ACP agent (for Zed or JetBrains); A2A or remote agents;
  cross-project workflows.
- Retiring 338 or 389–391. They stay on their own evidence gates.
