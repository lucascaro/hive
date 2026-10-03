# Research ACP as Hive's engine-driven workflow transport

- **Spec:** [docs/product-specs/492-research-acp-as-hive-s-engine-driven-workflow-tran.md](../../product-specs/492-research-acp-as-hive-s-engine-driven-workflow-tran.md)
- **Issue:** #492
- **Status:** active
- **PR:** #493
- **Branch:** feature/492-research-acp-workflows

## Summary

Research and design only. Produce a design doc under `docs/design-docs/` that records verified ACP capabilities per built-in agent, picks the workflow engine's home, defines engine-driven workflows as a parallel track to orchestration phases 1–3 under one trust model, and orders the follow-up specs. No product code.

## Research

### Relevant code

- **No session kind exists.** `session.Start` always allocates a PTY (`internal/session/session.go:144-152`). `Options` carries only Shell, Cmd, Cwd, Cols, Rows and Env (`:100`). `registry.Entry` has `Agent string` plus `sess *session.Session` (`internal/registry/registry.go:91-110`). Every spawn goes `Registry.Create` → `resolveAgentCmd` → the `spawn` seam → `session.Start` (`internal/registry/create.go:96,148,691`; `registry.go:41,50`). An ACP kind needs either a new `Entry` field or a non-PTY implementation behind `session.Session`.
- **Agent catalog** (`internal/agent/agent.go:155-251`; `Def` fields at `:29-113`). Every built-in agent has `ResumeArgs` except aider: claude `--resume <id>`, codex `resume <id>`, gemini `--resume <id>`, copilot `--resume=<id>`, pi `--session-id <id>`. Claude (`claude.go:288-318`) and Pi (`pi.go:38`, `-e hive.ts`) inject hooks; codex and gemini do not. No agent has a headless field.
- **Revive** already resumes after a daemon restart: `MarkPendingRevive`, then `Revive` with `ResumeArgs(AgentSessionID)` (`registry.go:828,1315,1367`; `create.go:1376`). This is the same mechanism a PTY takeover of an ACP session would reuse. The 254 plan is stale because steps 1–4 already exist.
- **GUI reload survival:** the daemon owns the process, and clients reattach with `SubscribeWithAtomicReplay` (`session.go:476`).
- **Headless plugins (460):** a plugin is a supervised child process with its own socket (`HIVE_SOCKET`) and identity bound to that socket. It may use control, attach and create, but not event, session or plan_review (`pluginModeAllowed`, `internal/daemon/plugins.go:175`). It is token-bucket limited, with CREATE/KILL costing 100, and is fully trusted and unsandboxed (spec 460 `:40`). Env is set at `internal/plugin/manager.go:662-665`. **Consequence:** an engine plugin can create and kill Hive sessions, but has no stdio channel to an agent that `hived` spawned.
- **State tiers:** `docs/design-docs/control-plane.md:30-46` lists hook, extension and heuristic. The code also has a fourth tier, `laya` (`internal/wire/control.go:324-347`), which the doc omits. Agent events are accepted only from hook or extension sources (`internal/daemon/daemon.go:962`). The state machine is at `internal/agentstate/machine.go:250-345`. An `acp` tier is a new `StateSource*` constant plus an allowlist entry.
- **Trust model** (`docs/design-docs/agent-orchestration.md`):
  - The grant is a per-session `Orchestrator bool` that is off by default and persisted (`:53-58`).
  - Reach is limited to the same project (`:59`).
  - The daemon stamps the sender (`:62`).
  - Spawned sessions inherit nothing: the daemon rewrites ProjectID, Cmd and Orchestrator (`:66`; 391 plan `:47-50`).
  - Depth is 1 (`:70`), each spawn records `SpawnedBy` (`:72`), and live children are capped at `maxChildrenPerOrchestrator = 4`.
  - Gates (`:15-17`): ≥10 session-originated sends over ≥2 weeks, then `wait` appearing in real transcripts, then one real fan-out.
  - 338/389/390/391 are all "Not started". The Phase 4 row (`:18`) reads `| 4 | — | results collection, headless workers, templates | **not designed** until phase 3 has evidence |`.
- **MCP:** Hive injects no MCP servers anywhere. The only mention is a comment at `internal/wire/control.go:658`.
- **Wire and contract:** modes are at `internal/wire/control.go:15-46`, `CreateSpec` at `:51`, and `PROTOCOL_VERSION = 1` is a hard gate (`frame.go:30`). HELLO dispatch is at `daemon.go:764-875`. `DaemonContract = 21` (`internal/buildinfo/contract.go:201`). This spec ships no code, so no bump is needed.

### ACP capabilities (web research, 2026-10-02; nothing has been run yet)

| Agent | ACP | MCP via session/new | Permissions | Plan stream | load/resume | Shared store with CLI |
|---|---|---|---|---|---|---|
| Claude | adapter `@agentclientprotocol/claude-agent-acp` 0.85.1 (SDK-based) | stdio+http+sse [code] | yes; modes default/acceptEdits/plan/auto/bypassPermissions | yes | load + list/resume/close/fork [code] | likely `~/.claude/projects` [inferred] |
| Codex | adapter `@agentclientprotocol/codex-acp` 2.1.1 (drives `codex app-server`) | stdio+http, no sse | yes; read-only/workspace-write/agent/agent-full-access | yes | load + list/resume/close/fork [code] | threadId = rollout, so likely [inferred] |
| Gemini | native `gemini --acp` (0.62.0; docs disagree on the experimental flag) | stdio+http+sse | yes; default/autoEdit/yolo/plan | **no** | load only | same `SessionSelector` as `--resume` [code] |
| Copilot | native `copilot --acp --stdio` (1.0.91, public preview) | claimed stdio/http/sse; issue #1040 says MCP is ignored (may be stale) | yes; allow_all, agent/plan | yes | load + close; list/resume unknown | unknown (closed binary) |
| Pi | adapter `pi-acp` 0.0.34 (community MVP) | **accepted but ignored** | none (pi has no approval gate) | no | load + list/delete | doc-claimed shared `~/.pi/agent/sessions` |

- **Spec:** v1 is stable and v2 is a draft. `session/resume`, list, close, delete and config options are now stable. Fork, subagents and mcp-over-acp are unstable. The terminal capability runs commands for the agent. **There is no PTY and no interactive stdin anywhere in the spec**, so human takeover must be a CLI resume into a PTY session.
- **Go SDK:** none is official. `github.com/coder/acp-go-sdk` v0.13.5 trails the schema (v1.24.1).
- **Early read on go/no-go:** MCP injection is doc/code-claimed for both Claude and Codex, and takeover is inferred for both. Neither has been verified by running. Pi already fails MCP injection, so a Pi node needs the headless or PTY escape hatch for `submit_result`.
- **Local environment:** `claude`, `codex`, `pi` and `npx` are installed. `gemini` and `copilot` are not.
- The research agent's output was flagged by the harness for the token `bypassPermissions`. That is Claude's mode name, a factual finding and not an instruction.

### Prior lessons

- No prior lessons matched (brain searches: 'acp orchestration', 'headless plugin', 'mcp').

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh` (layers go, unit, dom, e2e) · the AGENTS.md hivesmith block leaves Build/Lint/Tests as `<command>` placeholders.
- The PR touches only `docs/`, `scripts/` and `.github/`, all changeset-exempt (`scripts/check-changeset.sh:28-37`). There is no product code, so no DaemonContract bump.
- Record non-obvious architectural decisions under `docs/design-docs/`. A doc made incorrect by the change is fixed in the same commit (`agent-orchestration.md`, `control-plane.md`).
- Never edit `docs/product-specs/index.md` or `CHANGELOG.md` (both are generated).
- Running an adapter for verification must not read or copy credentials. It uses the CLIs' existing login as-is, in isolated temp cwds.

## Approach

Two parts, in this order. The **probe** produces the evidence and the **design doc** records the decisions.

**1. Evidence: a probe harness under `scripts/acp-probe/` (Node, no dependencies).**
`probe.mjs <agent>` launches the pinned adapter with `npx -y <pkg>@<ver>` (`--acp` for the native agents). It speaks newline-delimited JSON-RPC 2.0 over stdio and runs one scripted session. The cwd is a fresh `mkdtemp` that is **`realpath`'d** (macOS `/var` → `/private/var`), and that same path is used for every later step so the CLIs' cwd-keyed session lookups match.

1. `initialize`, recording `agentCapabilities` (loadSession, sessionCapabilities, mcpCapabilities).
2. `session/new` with `mcpServers: [{name: "hive", command: node, args: [submit-mcp.mjs, <outFile>]}]`, recording the mode and config-option ids it returns.
3. **One** `session/prompt` with this text: "Write a two-step plan. Call the `submit_result` tool with `{status: "ok", nonce: "<random>"}`. Then reply with only the nonce." The nonce therefore appears in plain assistant text as well as in the tool argument. Every `session/update` kind seen is recorded.
4. **Permissions** match the tool's **identity**, never free text. The tool is allowed only when the tool-call identity field (`toolCallId` → the matching `tool_call` update's `rawInput`/tool name, or the structured MCP server+tool fields) equals the exact injected tool. Per adapter, that means `mcp__hive__submit_result` for Claude, and for Codex whatever server/tool pair the probe actually records, pinned in `agents.mjs`. Everything else is rejected, including a Bash call whose title mentions `submit_result`. ACP's `ToolCall` has no standard tool-name field, so the probe records **which field carried the identity**. A permission request with no structured identity is rejected, but with the reason `no_structured_identity`, and it makes MCP `inconclusive`, not `fail`. A probe artifact therefore can't flip the go/no-go.
5. **MCP injection = pass** only when `submit-mcp.mjs` wrote **the exact nonce of this run** to `<outFile>`. This is checked out-of-band; the agent's own claims are not trusted.
6. **Load/resume = pass** when a **new** adapter process, given the same id through `session/load` (or `session/resume` when advertised), replays an **agent-authored** update (`agent_message_chunk` or `tool_call`) that contains the nonce. Replaying the user prompt alone doesn't count, and advertising the capability while replaying nothing is a fail.
7. **Takeover** reproduces Hive's real path with two independent checks:
   - **(a) Headless CLI resume in the same cwd.** One prompt, "Reply with only the nonce from earlier", sent through `claude -p --resume <id>`, `codex exec resume <id>`, and Pi's resume flag. It passes when stdout contains the nonce.
   - **(b) Hive PTY reopen.** It runs the argv Hive's `ResumeArgs` would build. That argv is mirrored in `agents.mjs`, with a comment citing `internal/agent/claude.go:221-226`, `codex.go`, `pi.go` and `agent.go:155-251` so drift can be grepped. For Claude it also mirrors `claudeSessionExists(id, cwd)` and records **which branch fired**: `--resume` when the transcript exists, otherwise `--session-id`, which silently starts fresh and is a fail. The process runs under `script -q <log>` with stdin held open by the probe (no EOF). When the captured screen shows a known folder-trust or onboarding prompt (the patterns are listed in `agents.mjs`), the probe answers with the keystroke the prompt itself offers. It never edits a config file and never touches credentials. Outcome: `pass` (nonce on screen within the phase budget), `fail` (wrong branch, or a fresh session evident), or **`inconclusive`** (nothing recognisable rendered). The doc reports (a) and (b) as separate columns. Only (b) decides takeover; (a) is supporting evidence and never substitutes for it.
   - When the ACP session id is not the CLI's session id (pi-acp keeps a mapping file), the probe records the mismatch and resolves it through the mapping. The doc reports this, because Hive would need the same mapping.
8. **Budgets:** each phase has its own timeout (init/new 60 s, prompt 180 s, load 60 s, (a) 120 s, (b) 45 s). On timeout the whole process group is killed. Spend is bounded at two model prompts per agent, with no retries.
9. **Results are whitelisted.** `results/<agent>-<date>.json` holds only: adapter and CLI versions, capability flags, mode/config-option ids, the set of update kinds seen, per-check verdicts plus a short reason enum, and the takeover branch. No raw payloads, stderr, transcripts, paths or auth-method data. Raw logs go to an untracked `$TMPDIR` dir, printed at exit. The README lists everything the run writes: the session stores (`~/.claude/projects`, `~/.codex/sessions`, Pi's store, pi-acp's mapping), plus any folder-trust entry the CLI itself records when the probe answers its trust prompt (for example in `~/.claude.json`). The probe never edits those files itself. Nothing is deleted automatically.

Runs: Claude (`@agentclientprotocol/claude-agent-acp@0.85.1`), Codex (`@agentclientprotocol/codex-acp@2.1.1`) and Pi (`pi-acp@0.0.34`). Gemini and Copilot are doc-only (Decision log). Confound: sessions load the user's global agent settings, hooks and plugins, so the doc records the CLI versions and states this.

**2. Decisions: `docs/design-docs/acp-workflows.md`.** One section per success criterion:
- **Capability table** (SC1): five agent rows × columns {ACP route + adapter@version, MCP via session/new, permission prompts, plan/tool-call streaming, load/resume, reopen headless (a), reopen in Hive PTY (b)}. Every cell is tagged `run` (linked to the results JSON) or `doc` (with a source URL).
- **Where the engine lives** (SC2): pros and cons of daemon core versus a 460 plugin. The research constrains this. A plugin has no stdio channel to an agent spawned by `hived`, and plugins are fully trusted and unsandboxed (460 `:40`). So the **ACP transport lives in `hived`** (an ACP session kind next to PTY); otherwise sessions would not survive GUI reloads and would escape the registry's provenance. The doc decides where the *engine* lives and records the alternative it rejects.
- **One trust model** (SC3): ACP nodes reuse the 389–391 rules (same project only, the daemon stamps provenance, nothing is inherited, depth and child caps), with the engine as the orchestrator principal. Each node's permission mode is capped at the user's setting for that agent. `request_permission` goes to the user and is never auto-approved by the engine. Permission decisions match tool identity, not free text, which is the same lesson as the probe. Every engine spawn and every prompt records `SpawnedBy` and per-prompt provenance.
- **What the engine does:** ACP as the primary transport; the `submit_result` MCP tool, whose schema is part of the node definition; deterministic check nodes (commands with exit codes, not model claims); a per-vendor headless adapter as the escape hatch (Pi, Aider, shell, custom); and PTY takeover through `ResumeArgs`, including the Claude transcript-exists branch.
- **Gates for the ACP track:** the 389–391 gates count agent-initiated messaging, which this track never produces. The doc defines its own gates (a probe go, then N real plan→implement→review runs).
- **Follow-up specs, in order** (SC5), each with a one-line problem statement: ACP session kind → code-defined workflow engine → live monitoring → authoring. The doc also says whether Hive needs its own adapters (Pi ignores MCP; Aider has no ACP).
- **Go/no-go** (SC6) uses one rule, computed the same way by `check-doc.mjs` and stated in the doc. For each of Claude and Codex, `ok = mcp==pass && takeover_b==pass`.
  - **go:** at least one of them is `ok`.
  - **no-go:** MCP is `fail` for both, or `takeover_b` is `fail` for both. The doc then recommends stopping.
  - **undecided:** anything else, including all-inconclusive. The doc then says "re-run / check by hand before deciding". Undecided is never reported as go.
- **Containing drift:** one principal model and one provenance field shared with 389–391, plus the code seams both tracks must use (the registry `Create` spawn seam and the `SpawnedBy` stamp).

### Files to change

1. `docs/design-docs/agent-orchestration.md`: link the new doc, and rewrite the Phase 4 row (`:18`) and the `:109` paragraph. Results collection and headless workers move to the ACP track, which is gated separately.
2. `docs/design-docs/control-plane.md`: link the new doc. Fix the stale tier count: the code has four tiers including `laya` (`internal/wire/control.go:324-347`), and the doc adds the proposed `acp` tier. Amend `:284-287` ("Run Claude/Pi headless so Hive owns the stream", rejected; "a future orchestration layer") with a cross-link to the ACP track.
3. `docs/design-docs/index.md`: add the `acp-workflows.md` row, and fix the control-plane row's "three knowledge tiers" (`:11`).
4. `.github/workflows/ci.yml`: add `node --test 'scripts/acp-probe/*.test.mjs'` and `node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md`, both gated `if: matrix.biome` next to the `check-spec-discovery.mjs` step (`:342-349`), run from the repo root (they must not inherit that step's `working-directory`). The tests use only the fake agent, with no `script(1)` and no real CLIs, so they are portable.
5. `docs/exec-plans/active/492-…md`: Progress, the Decision log, and the conventions card wording (the PR touches `docs/`, `scripts/` and `.github/`, all changeset-exempt per `scripts/check-changeset.sh:28-37`).

### New files

- `docs/design-docs/acp-workflows.md`: the design doc.
- `scripts/acp-probe/probe.mjs`: the JSON-RPC client, the scripted session, and the verdict logic (exported for tests).
- `scripts/acp-probe/submit-mcp.mjs`: a minimal stdio MCP server exposing `submit_result`, which writes its args to a file.
- `scripts/acp-probe/agents.mjs`: the pinned agent table (package@version, launch argv, tool identity, headless-resume and Hive-`ResumeArgs` mirrors with Go citations, trust-prompt patterns).
- `scripts/acp-probe/testdata/fake-agent.mjs`: a scriptable fake ACP agent with modes `--ignore-mcp`, `--hang`, `--empty-load`, `--bash-perm-named-submit` and `--wrong-nonce`.
- `scripts/acp-probe/probe.test.mjs`: the `node:test` tests.
- `scripts/acp-probe/check-doc.mjs` — asserts the design doc's capability table matches `results/*.json` and has a go/no-go verdict; run in CI alongside the tests (plus a test `check-doc: fails on a run cell that disagrees with results`).
- `scripts/acp-probe/README.md`: how to re-run the probe, the session stores it writes into, and how to read the results.
- `scripts/acp-probe/results/{claude,codex,pi}-2026-10-02.json`: the whitelisted evidence.

### Tests

- `submit-mcp: tools/list exposes submit_result with a JSON schema`
- `submit-mcp: tools/call writes exact arguments to the out file`
- `probe: all pass against a well-behaved fake agent`: mcp, load, kinds ⊇ {plan, tool_call}.
- `probe: mcp=fail when the agent ignores mcpServers`
- `probe: mcp=fail when the out file holds a different nonce`
- `probe: load=fail when loadSession is advertised but replays nothing`
- `probe: rejects permission for a Bash call whose title/input contains "submit_result"`
- `probe: allows permission only for the exact injected tool identity`
- `probe: hung agent times out per phase, the process group is gone (kill(-pgid,0) throws ESRCH), and the probe exits`
- `takeover: claude branch resolves --session-id when no transcript exists and verdict=fail`: a temp HOME fixture, so the user's real `~/.claude` is untouched.
- `probe: permission request with no structured identity, so mcp=inconclusive with reason no_structured_identity`
- `probe: load=fail when only the user prompt is replayed`
- `takeover: pi-acp id differs from the pi session id and is resolved through the mapping fixture`
- `check-doc: computes go / no-go / undecided from results fixtures, and fails when the doc disagrees or a probed row is tagged doc`
- `results: writer emits only whitelisted keys`: a fake payload with auth/path/stderr fields is stripped.

### Verification

- `node --test 'scripts/acp-probe/*.test.mjs'` passes. The negative tests fail on a probe that always reports pass or always allows.
- `node scripts/acp-probe/probe.mjs claude|codex|pi` writes `results/*.json`. The doc's `run` cells must match them, as checked by the next item.
- `node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md` (added to New files; run in CI too) parses the capability table and asserts all of the following:
  - 5 agent rows × 7 data columns, with every cell tagged `run` or `doc`.
  - Claude, Codex and Pi each have a results file, and their MCP, load and takeover cells are `run`.
  - Every `run` cell equals the verdict in the matching results JSON.
  - The doc's `Go/no-go` verdict equals the one **computed** from `results/{claude,codex}-*.json` with the SC6 rule. It fails today because the doc does not exist.
- `grep -l 'acp-workflows.md' docs/design-docs/{agent-orchestration,control-plane,index}.md | wc -l` = 3. Today it is 0.
- `grep -n 'three knowledge tiers' docs/design-docs/index.md` and `grep -n 'Three tiers of knowledge' docs/design-docs/control-plane.md` both return nothing.
- `scripts/check-changeset.sh origin/main HEAD` exits 0, and `git diff --name-only origin/main | grep -E '^(internal|cmd)/'` is empty, so no product code changed.

### Risks

- **Spend:** at most 2 prompts × 3 agents.
- **pi-acp:** a 0.0.x MVP that may not run at all. That is recorded as a finding, not a probe bug.
- **Adapter churn:** expected, which is why the probe is committed and re-runnable.
- **Check (b) and TUI changes:** (b) can still go `inconclusive` if a TUI changes. The doc keeps (a) and (b) as separate columns and never treats inconclusive as pass.
- **Engine home** is decided in the doc. This plan only fixes that the transport lives in `hived`.

## Second opinion

- **Round 1:** revise, confidence 8. 8 must-fix items, all 8 applied: vacuous table grep replaced by a parser; Claude `claudeSessionExists` branch and realpath cwd mirrored; trust dialog, stdin EOF and inconclusive handling; exact-identity permission rule; negative tests; whitelisted results; index.md:11 and control-plane.md:284-287 blast radius; CI glob and gating.
- **Round 2:** revise, confidence 7. 3 must-fix items, all 3 applied without a third review (loop cap): one SC6 rule with an `undecided` outcome that is never go; `check-doc.mjs` computes the verdict and requires run-tagged probed rows; no-structured-identity permission requests map to inconclusive. Nice-to-haves applied too.
- **Disposition:** presented with all fixes applied; operator approved via plan-html on 2026-10-02, round 1, no feedback.

## Decision log

- **2026-10-02** — Parallel track next to 389–391, not a replacement. Why: operator's choice at brainstorm; drift risk accepted and must be contained in the doc.
- **2026-10-02** — Verify Claude, Codex and Pi by running them; Gemini and Copilot cells are doc-only. Why: only those three are installed; the go/no-go depends only on Claude and Codex; operator chose this at clarifying round B.
- **2026-10-02** — Commit the probe harness under `scripts/acp-probe/` (no dependencies). Why: lets the capability table be re-run when adapters change; it is a dev tool, not product code (operator choice).
- **2026-10-02** — Added `scripts/acp-probe/pty-run.py` (python3 stdlib PTY host). Why: macOS `script(1)` refuses a piped stdin ("tcgetattr: Operation not supported on socket"), and Node has no stdlib PTY.
- **2026-10-02** — The PTY marker is the agent's own phase-1 reply, verbatim, not a derived string. Why: Codex "uppercased" the nonce with a Cyrillic Е (U+0415), so a derived marker missed a transcript that rendered fine. The marker is never taken from text in our prompt.
- **2026-10-02** — Dialog rules match only output that arrived after the last keystroke, and Codex's update nag (`blocksEnter`) pre-empts every Enter. Why: the nag's default option runs `curl … | sh`, and stale screen text blocked the folder-trust answer.
- **2026-10-02** — Children run without parent-session markers (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `HIVE_SOCKET`, …) but keep user preferences. Why: the markers turned off Claude's transcript saving and would send hook events to the user's real daemon. Stripping `CLAUDE_CODE_ENABLE_TODO_TOOLS` hid Claude's `plan` updates.
- **2026-10-02** — The Claude takeover mirror records Hive's branch (`hive_branch`) separately and tests ACP with Claude's observed encoding. Why: Hive's `encodeClaudeProjectDir` misses `_`, so `ResumeArgs` falls back to `--session-id` and claude exits "Session ID … is already in use". That is a Hive bug independent of ACP (F1); it is follow-up spec 0, not part of this PR.
- **2026-10-02** — Results files are named for the UTC date (`-2026-10-03`). Why: `probe.mjs` stamps UTC; the runs happened on the evening of 2026-10-02 local time.

## Progress

- **2026-10-02** — Plan created; research started.
- **2026-10-02** — Research done (code + web); stage PLAN.
- **2026-10-02** — Plan approved (html, round 1); stage IMPLEMENT.
- **2026-10-02** — Probe implemented (30 tests, each mutation-checked against an injected bug). Final runs: Claude all pass; Codex MCP and PTY pass, headless fails on the writer lock; Pi MCP fails (ignored), the rest pass. Verdict **go**. Design doc and linked docs updated; check-doc is green.
- **2026-10-02** — PR #493 opened; stage REVIEW.

## Open questions
