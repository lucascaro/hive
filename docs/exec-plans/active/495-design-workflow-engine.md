# Design Hive's workflow engine: typed TS workflows, an inspectable graph, durable runs

- **Spec:** [docs/product-specs/495-design-hive-s-workflow-engine-typed-ts-workflows-a.md](../../product-specs/495-design-hive-s-workflow-engine-typed-ts-workflows-a.md)
- **Issue:** #495
- **Status:** active
- **PR:** #506
- **Branch:** feature/495-workflow-engine-design

## Summary

A design doc and a small prototype that settle the questions spec 492 left open about its follow-up 2, the code-defined workflow engine: how workflows are written, what graph is reviewed before a run, how runs survive failures, and whether to build an engine at all. Spec 492 already fixed the transport (ACP in `hived`) and where the engine lives (a bundled 460 plugin).

## Research

Three research passes ran on 2026-10-05: the codebase, durable execution, and frameworks with ACP, billing, OTel and security. External sources were all accessed that day; the design doc carries the full citations.

### Relevant code

- **Plugin host (spec 460).**
  - Manifest `hive-plugin.json`: `main.command` argv, optional `ui`, `api_version` "0.2" exact match (`internal/plugin/manifest.go:33,38-61,132`).
  - Bundled plugins come from `//go:embed all:plan-review` (`plugins/embed.go:12`), today UI-only. Each start rewrites them into `<state>/plugins/<id>`; new builtins are added disabled (`internal/plugin/builtin.go:38-49`).
  - Commands resolve through `exec.LookPath` on the **daemon's** PATH, not the login PATH (`internal/plugin/manager.go:683-697`). ACP already uses `proc.LoginPATH` (`internal/agent/acp.go:111-116`).
  - Supervision: env carries `HIVE_SOCKET`, `HIVE_PLUGIN_ID`, `HIVE_PLUGIN_DIR`, `HIVE_PLUGIN_DATA_DIR` (`manager.go:659-665`). Restart backoff runs 1s→30s; more than 5 crashes in 60s marks the plugin failed (`manager.go:22-34,556-625`). The data dir survives reinstall (`manager.go:158-162`).
  - Socket: `pluginModeAllowed` admits control, attach and create (`internal/daemon/plugins.go:175-177`), with a token-bucket limiter (`plugins.go:21-109`).
- **Plugin SDK.** `plugins/sdk/hive-plugin.mjs` is one dependency-free file for Node 18+.
  - API: `connect()` (:436), then `conn.send(name, payload)` (:350), which is fire-and-forget with no request/reply matching, plus `conn.on`.
  - Its frame table is checked against Go by `internal/plugin/sdk_drift_test.go:30`.
- **ACP layer (spec 496).**
  - `internal/acp` implements new, load, prompt, update, request_permission and set_mode (`types.go:11-16`). It has **no `session/cancel`**. Capability detection reads only `loadSession` (`types.go:35-36`).
  - Agent catalog (`internal/agent/acp.go:73-74,84-106,136,152-160`): Claude runs through `@agentclientprotocol/claude-agent-acp@0.85.1`, Codex through `@agentclientprotocol/codex-acp@2.1.1`, and Pi through `pi-acp`, all via `npx`. Gemini and Copilot are refused.
  - Registry (`internal/registry/acp.go`): `onACPUpdate` (:447-492), `onACPPermission` (:540-560, auto-allows only this session's own `submit_result`), `PromptACP` / `ErrACPBusy` (:634,655), `SubmitResult` (:708), `SetKind` (:870).
  - Frames a plugin can use: `CREATE_SESSION{kind:"acp"}` (with `use_worktree`), `PROMPT_ACP`, `GET_ACP_TRANSCRIPT` → `ACP_TRANSCRIPT` (deltas carry `prompt_id`, `result_status`, `result`), `ANSWER_PERMISSION`, `SET_SESSION_KIND` (`docs/plugins.md:240-268`).
  - `submit_result` is built: `hived mcp-submit` runs as one MCP server per start with a nonce, and results are capped at 64 KiB. The result schema is fixed (`{status, summary?, data?}`); there is no per-node schema.
- **Provenance.** `CreateSpec.SpawnedBy` (`internal/wire/control.go:127-131`) and `principalOf` (`internal/daemon/daemon.go:2114-2121`) return only `plugin:<id>` or `""`. The **`workflow:<run-id>` principal is documented (`acp-workflows.md:228`) but never stamped.**
- **Worktrees.** `CreateSpec.UseWorktree/Branch/WorktreePath` (`wire/control.go:67-88`) → `worktree.CreateWorktreeAt` (`registry/create.go:1015`). Dirty refusal: `Kill` returns `ErrWorktreeDirty` (`registry/registry.go:1629-1718`).
- **Persistence.**
  - `StateDir` (`registry/paths.go:19-50`), `writeAtomic` (`registry/persist.go:100`).
  - No SQLite driver in `go.mod` and no event log anywhere.
  - Node 22.5+ ships `node:sqlite`; CI runs Node 24 (`.github/workflows/ci.yml:113`).
- **Doc-drift precedent.** `scripts/acp-probe/` has `probe.test.mjs` plus `check-doc.mjs`, which compares generated output against `<!-- capability-table:start/end -->` markers in `acp-workflows.md`. It runs from `ci.yml:351-359` on the `matrix.biome` leg. The new prototype's IR/Mermaid check slots in beside it.
- **GUI.** React 19, zustand, Vite 8, TypeScript 7 (`cmd/hivegui/frontend/package.json`). No graph library and no Mermaid anywhere yet.
- **Events.** `SessionEvent{kind, session}` (`wire/control.go:522-530`). No OpenTelemetry code; only an "OTel span" target in `RELIABILITY.md:12`.

### Constraints / dependencies

- **No off-the-shelf durable engine meets the hard requirement.** It must survive a crash, a restart and a reboot with no extra always-on service.

  | Option | Why it falls short |
  |---|---|
  | Temporal | Its local server (`start-dev`) is "not intended for production use", and Temporalite is archived. |
  | Restate | Needs its own server: a third stateful process (BSL 1.1). |
  | Inngest | Has a 2 h step cap and is SSPL. |
  | DBOS TS | Supports Postgres only; its SQLite PR #1288 is unmerged and scoped to dev/test. |
  | Vercel Workflow | Its local World is dev-only. |
  | Absurd | Postgres only. |

  A small SQLite checkpoint log, modelled on Absurd's step checkpoints, is the only fit. Restate is the runner-up if one spawned process is acceptable.
- **The frameworks are each weak where it matters here.**
  - Mastra: core is Apache-2.0, `ee/` is under a separate enterprise license (v2.0, 2026-09-22), and it is heavy. Local file-backed LibSQL is unconfirmed.
  - LangGraph.js: MIT. Its `SqliteSaver` uses native better-sqlite3. `interrupt()` re-runs the whole node on resume, which is unsafe if an ACP prompt runs before the interrupt. Its graph isn't fully static when it uses conditional edges or `Send`.
- **ACP today.**
  - Protocol v1. `session/cancel` is a baseline notification; `usage_update` and `session_info_update` are update kinds.
  - The Claude adapter was renamed to `@agentclientprotocol/claude-agent-acp`. The Codex adapter is `@agentclientprotocol/codex-acp`, built on the Codex App Server.
  - Gemini is native `--acp` (0.33+). Copilot CLI is native `copilot --acp`, in public preview since 2026-01-28. OpenCode is native. Pi goes through `pi-acp`.
- **Billing (fetched 2026-10-05).** The Anthropic support article (updated 2026-06-16) says the Agent SDK change is **paused**: "Claude Agent SDK, `claude -p`, and third-party app usage still draw from your subscription's usage limits". The promised credit "isn't available", and no new date is set. Zed's post (2026-05-14, updated 2026-06-16) agrees.
- **OTel GenAI.** The conventions now live in `open-telemetry/semantic-conventions-genai`, at Development status.
  - `gen_ai.operation.name` values include `invoke_workflow`, `invoke_agent` and `execute_tool`.
  - Span `invoke_workflow {gen_ai.workflow.name}` (INTERNAL), with metric `gen_ai.invoke_workflow.duration`.
- **Security finding (shipped code, not just design).**
  - `claude-agent-acp` defaults to `settingSources: ["user","project","local"]`. Claude Code docs say headless/SDK runs show no workspace-trust or `.mcp.json` prompt, yet project hooks, `env` and `.mcp.json` servers still run.
  - Hive passes no setting-source restriction today; `grep settingSources internal/` is empty.
  - So an ACP Claude session in an untrusted worktree runs that repo's hooks with no prompt.
  - Codex skips project `.codex/` layers unless the path is trusted. How codex-acp treats a never-trusted path is unverified.
- **Gaps the design must close:**
  - stamp `workflow:<run-id>`;
  - add `session/cancel`;
  - launch plugins with the login PATH or a bundled runtime;
  - give the SDK request/reply matching;
  - support a per-node `submit_result` schema;
  - add a headless bundled plugin to the embed.

### Prior lessons

No prior lessons matched (brain searches: "workflow engine durable plugin acp", "typescript dsl graph mermaid").

### Conventions card

- Build `./build.sh`; tests `scripts/test.sh [go|unit|dom|e2e]`; prototype tests `node --test 'scripts/<dir>/*.test.mjs'` (Node 24 in CI).
- Biome: `npx biome ci .` (formatting included) on JS/MJS under `scripts/`.
- Docs-only and `scripts/` changes need no changeset (`scripts/check-changeset.sh` exempts them). No `DaemonContract` bump, since nothing touches `internal/{wire,daemon,session,registry}`.
- Non-obvious architecture goes in `docs/design-docs/`. `DESIGN.md` changes only if a structural rule changes; this spec ships no product code.
- Doc-drift checks follow the `scripts/acp-probe/check-doc.mjs` marker pattern and run in `ci.yml` next to it.

## Approach

Ship one design doc, `docs/design-docs/workflow-engine.md`, and a TypeScript prototype under `scripts/workflow-proto/`. The prototype runs on Node 24's built-in type stripping, so it needs no build step, no npm install and no new dependency. It is type-checked by the TypeScript 7 compiler the frontend already pins. Every IR and Mermaid example in the doc is generated by the prototype and kept honest by a CI drift check. That check copies `scripts/acp-probe/check-doc.mjs`: generated output sits between HTML-comment markers in the doc.

The prototype also proves the round-B "user's Node plus Hive's checker" answer on a small scale. The same `.ts` files run unbuilt on stock Node and type-check with `tsc --erasableSyntaxOnly`.

**DSL design.** Nodes declare typed output schemas, and conditions are data, not closures. That is how the graph stays static and inspectable:
- **Output schemas.** A tiny schema builder, `s.object/s.enum/s.string/s.number/s.boolean/s.array`, gives TS inference for `node.out.<field>` and emits JSON Schema into the IR. That JSON Schema is also what a node's `submit_result` will validate against (gap: today's fixed `{status, summary, data}`).
- **Conditional edges.** `edge(a, b, when(a.out.verdict, 'eq', 'approve'))` stores `{ref:"a.verdict", op:"eq", value:"approve"}`. TS rejects a ref to a field the schema lacks, or a value outside an enum. Operators are `eq`, `ne`, `in`, `truthy`, `lt`, `gt`. There are no arbitrary predicates.
- **Loops.** `loop(id, {maxIters, until: cond}, body)` stays one static subgraph. The IR records a bounded back-edge with `maxIters`; the iteration count is runtime state, not graph shape. Runaway is capped by construction.
- **Dynamic fan-out.** `map(id, {over: ref-to-array-output, concurrency}, template)` holds one static template subgraph. Cardinality is known only at run time; runtime events key instances as `<mapId>[<index>]/<nodeId>`, so the overlay shows N instances under the one reviewed template.
- **Subgraph composition.** Library constructs are higher-order functions that take node factories and return a `Subgraph` with typed `in` and `out` ports. Composition prefixes ids (`review/worker`), so ids stay unique and stable across rebuilds.
- **Node kinds.** `agent` (ACP: agent id, prompt template, mode, worktree policy, output schema, timeout), `check` (argv, cwd, timeout; result is exit code plus captured output), `human` (prompt, output schema). There is no `llm` kind.

**The doc's sections, mapped to the success criteria:**

| Criterion | Doc section |
|---|---|
| SC1 | DSL + IR: builder API, IR JSON Schema (embedded in a `<!-- wf:schema:start/end -->` block that check-doc regenerates, and committed as `ir.schema.json`), worked examples for loops, conditional edges, fan-out and composition, each with generated IR and Mermaid, plus a "why it stays static" paragraph per construct |
| SC3 | Durability: a comparison table. Rows: Temporal, Restate, Inngest, DBOS, SQLite event log (Vercel Workflow and Absurd as notes). Columns: exactly the spec's eight — local-only single machine; needs an always-on service; TS SDK quality; determinism constraints vs hour-long agent calls; waiting on a human; cost; lock-in; fit with a data-defined graph. Choice: a plugin-owned `node:sqlite` checkpoint log in `HIVE_PLUGIN_DATA_DIR`. Tables `runs`, `node_attempts`, `events`; node states `pending→running→done/failed/waiting/skipped`. Resume marks `running` as interrupted and retries; completed nodes never re-run; how this survives a plugin crash, a `hived` restart and a reboot |
| SC4 | User workflow files: `<repo>/.hive/workflows/*.ts`, discovered on project open; run on the user's Node ≥22.6 via the login PATH; type-checked with a Hive-shipped `tsgo` against a shipped `@hive/workflow` `.d.ts`; disabled with a clear message when Node is missing. The doc must reconcile this with SC4's "without the user installing a toolchain". The argument: Node is already a prerequisite of every ACP node, since Claude, Codex and Pi all launch through `npx` (`internal/agent/acp.go:84-102`, `ACPAvailable` :152-160). So workflows add no toolchain beyond what ACP sessions already need, and the user installs no TypeScript compiler, bundler or package. A run pins the IR snapshot plus a source hash at start, so in-flight runs keep their snapshot and editing a file affects new runs only |
| SC5 | Runtime events: the schema `{run_id, node_id, instance?, attempt, seq, ts, type, data}`; the OTel mapping run→`invoke_workflow`, node→an INTERNAL `workflow.node {node_id}` span (distinct from the turn span), agent turn→`invoke_agent` child span, tool call→`execute_tool`, plus `gen_ai.*` attributes at Development status; the GUI overlay keyed by node id, live from the event stream and replayed from the `events` table |
| SC6 | ACP node layer: capability detection (`initialize` caps plus the catalog); `session/update` → node events table; one worktree per agent node via `CreateSpec.UseWorktree`; cancellation (adds `session/cancel`, a gap today), per-node timeout, `maxIters`, a global run budget; F2 Codex write lock (sequential takeover, `acp_writer_locked`), F5 Pi ignores `mcpServers` (Hive's Pi extension), F4 Codex default permission mode (least-permissive mode first). Also lists the gaps the engine spec must close: `workflow:<run-id>` stamping, SDK request/reply, plugin login PATH, per-node result schema, headless bundled plugin |
| SC7 | Risks: billing (quotes the Anthropic support article updated 2026-06-16 and Zed's post; paused, draws from subscription limits; hedge = per-run budget, vendor-mixing, check nodes cost nothing); security (untrusted worktree hooks and MCP: cites #505; permission defaults; secrets never in IR or events; all inside 492's trust model) |
| SC8 | Go/no-go: (1) no engine, (2) Mastra or LangGraph.js inside the plugin, (3) custom. A concrete threshold, an explicit verdict, and a paragraph on how 492's follow-up 2 changes on no-go (written conditionally if the verdict is go). `acp-workflows.md` follow-up 2 links the doc and states the verdict |

**Why custom over Mastra or LangGraph.js, before the verdict** (the doc argues this with evidence):
- LangGraph's `interrupt()` re-runs the whole node, which is unsafe around an ACP prompt.
- Its conditional edges and `Send` aren't visible statically.
- Its SQLite saver is a native module.
- Mastra is heavy, and its file-backed LibSQL durability is unconfirmed.
- Neither gives an IR keyed for overlay without an adapter layer that costs about as much as the custom core.
The doc still states the threshold that would flip the verdict, in concrete terms: the adapter code needed to fit a framework (IR export, node-id event keying, interrupt-safe ACP nodes) versus the custom core's lines of code, measured on the prototype. A framework wins if its adapter is under half the custom core. Usage evidence feeds gate C/D, not this verdict.

### Files to change

1. `docs/design-docs/acp-workflows.md`: rewrite follow-up 2 (:294-298) to link the new doc and state its verdict (SC8).
1a. `docs/design-docs/agent-orchestration.md`: the parallel-track paragraph (:24-25) links the new doc next to `acp-workflows.md`.
2. `docs/design-docs/index.md`: add a "Workflow engine" entry.
3. `.github/workflows/ci.yml`: extend the `matrix.biome` leg with a step "Workflow prototype tests + design-doc drift check (spec 495)" that runs the three commands from Verification.
4. `docs/product-specs/495-…md`: stage transitions only.
5. `docs/exec-plans/active/495-design-workflow-engine.md`: plan sections, decision log, progress.

### New files

- `docs/design-docs/workflow-engine.md`: the design doc (SC1, SC3–SC8).
- `scripts/workflow-proto/schema.ts`: the output-schema builder with TS inference and JSON Schema emission.
- `scripts/workflow-proto/dsl.ts`: the builder (`workflow`, `agent`, `check`, `human`, `edge`, `when`, `loop`, `map`, `subgraph`), the IR types, `build()` → IR, and `validate(ir)` (unique ids, edges resolve, refs resolve against schemas, loops bounded, map `over` is an array output, no cycles outside `loop`).
- `scripts/workflow-proto/library.ts`: `reviewLoop(worker, reviewer, {maxIters})`, `fanOutThenVerify(items, step)` and `planImplementVerify(spec)`.
- `scripts/workflow-proto/mermaid.ts`: IR → Mermaid `flowchart`. Loops render as subgraphs labelled `≤N×`, map nodes as subgraphs labelled `for each`, and edges carry their condition label.
- `scripts/workflow-proto/ir.schema.json`: the IR JSON Schema. It is generated and checked: a test asserts the committed file equals `irSchema()`.
- `scripts/workflow-proto/examples.ts`: named examples used by both the doc and the tests (`loop`, `conditional`, `fanout`, `composition`, plus the three library constructs).
- `scripts/workflow-proto/check-doc.ts`: regenerates every `<!-- wf:<example>:<ir|mermaid>:start/end -->` block and the `wf:schema` block, and fails on a diff. It never passes vacuously. It also fails when:
  - the doc has zero `wf:` blocks;
  - an example exported from `examples.ts` lacks its `ir` or `mermaid` block (coverage is over the examples, not the blocks present);
  - a block names an unknown example or kind;
  - a `:start` has no matching `:end`;
  - blocks are duplicated or nested;
  - the `wf:schema` block is missing.

  `--write` rewrites the blocks.
- `scripts/workflow-proto/emit.ts`: a CLI, `node scripts/workflow-proto/emit.ts <example> [--ir|--mermaid]`, that prints the generated output. It makes SC2's "emits IR JSON plus Mermaid" directly runnable.
- `scripts/workflow-proto/tsconfig.json`: `strict`, `noEmit`, `erasableSyntaxOnly`, `allowImportingTsExtensions`, `module`/`moduleResolution: nodenext`, `"types": []`, and `include` covers `*.ts` plus `node-shim.d.ts`. No `@types/node` is installed; `types: ["node"]` fails with TS2688, which the reviewer reproduced.
- `scripts/workflow-proto/node-shim.d.ts` — minimal ambient declarations for the `node:*` modules used (no `@types/node` dependency).
- `scripts/workflow-proto/README.md`: how to run it; what it is not (it executes no agents); and that Biome does not lint `scripts/`, because CI runs Biome only from the frontend directory, the same as `acp-probe`.
- Tests, listed below.

### Tests

All under `scripts/workflow-proto/`, run by `node --test`:
- `dsl.test.ts`
  - `build emits stable ids and edges`: the same workflow built twice gives byte-identical IR.
  - `when() stores data conditions`: the condition is JSON, not a function; `JSON.parse(JSON.stringify(ir))` round-trips equal.
  - `validate rejects an unbounded cycle`: a back-edge outside `loop` is an error.
  - `validate rejects a ref to an unknown output field`: covers refs built through an `as any` cast.
  - `validate rejects map over a non-array output`.
  - `validate rejects duplicate ids after composition`.
  - `loop records maxIters and its until condition`.
  - `map records concurrency and its template subgraph`.
- `library.test.ts`
  - `reviewLoop builds worker→reviewer with a bounded back-edge on revise`.
  - `fanOutThenVerify maps step over items, then joins into verify`.
  - `planImplementVerify chains plan→implement→check→review with a human approval node`.
  - `library outputs validate`: every example passes `validate`.
- `mermaid.test.ts`
  - `renders condition labels on edges`.
  - `renders loop and map as labelled subgraphs`.
  - `output is deterministic`.
- `emit.test.ts`: `emit prints IR and Mermaid for every example; an unknown example exits non-zero`.
- `schema.test.ts`
  - `the committed ir.schema.json equals irSchema()`.
  - `every example IR validates against ir.schema.json`, using a ~40-line JSON Schema subset validator in the test, with no dependency.
- `check-doc.test.ts`
  - `passes on the committed doc`.
  - `fails when an IR block is edited`: mutate one character in memory; expect an error naming the example.
  - `fails when the doc has no wf blocks`.
  - `fails when an example has no ir block`: both markers of one block deleted.
  - `fails on an unknown example name or kind`.
  - `fails on a start without an end, and on duplicate blocks`.
  - `fails on nested blocks`.
  - `fails when an example has no mermaid block`.
  - `fails when the wf:schema block is missing`.
  - `fails when the schema block drifts from irSchema()`.
  - `--write regenerates blocks`: on a temp copy.
- Type-level test `types.check.ts`, compiled by `tsc` only: `// @ts-expect-error` lines for a ref to a missing field, an enum value outside the enum, and `map` over a non-array. `tsc` fails if any of them stops erroring.

### Verification

```bash
node --test 'scripts/workflow-proto/*.test.ts'
node scripts/workflow-proto/check-doc.ts docs/design-docs/workflow-engine.md
cmd/hivegui/frontend/node_modules/.bin/tsc -p scripts/workflow-proto/tsconfig.json
# Negative: corrupt one IR line in a temp copy; the check must exit non-zero.
T=$(mktemp) && python3 -c 'import sys;s=open(sys.argv[1]).read();open(sys.argv[2],"w").write(s.replace("\"kind\": \"agent\"","\"kind\": \"agnt\"",1))' docs/design-docs/workflow-engine.md "$T" && ! node scripts/workflow-proto/check-doc.ts "$T"
grep -q 'workflow-engine.md' docs/design-docs/acp-workflows.md   # fails today
grep -q 'workflow-engine.md' docs/design-docs/index.md           # fails today
grep -q 'workflow-engine.md' docs/design-docs/agent-orchestration.md  # fails today
node scripts/workflow-proto/emit.ts reviewLoop --mermaid | grep -q '^flowchart'
```

### Risks

- **`node --test` with a `.ts` glob on Node 24.** Type stripping is on by default since 23.6/22.18, and CI pins Node 24. Risk: an import using a non-erasable TS feature. Mitigation: `erasableSyntaxOnly` in the tsconfig, which `tsc` enforces.
- **`@types/node` is not installed in the frontend** (checked). The prototype ships a minimal `node-shim.d.ts` declaring only the `node:*` APIs it imports (`node:test`, `node:assert/strict`, `node:fs`, `node:path`, `node:url`, `process.argv/exit`). No new dependency.
- **The frontend's `node_modules` in CI.** The `matrix.biome` leg already installs them (typecheck step), so the step goes after that. Locally, run `npm ci` first.
- **Typed-ref ergonomics in TS.** Getting `a.out.verdict` inference with enum value narrowing is the trickiest part. Fallback: string refs checked by `validate` at build time, with type-level tests limited to field existence.
- **Doc length.** The doc covers SC1 and SC3–SC8, so it will be long. Keep each section tight; generated blocks live in collapsible `<details>` blocks.
- **Mermaid rendering on GitHub.** Each marker pair wraps the whole ```mermaid fence (inside `<details>`), so `--write` regenerates the fence itself, not just its body. check-doc pins `JSON.stringify(ir, null, 2)`, which keeps the negative check stable.
- **Out of scope.** No SQLite resume simulator in the prototype; the spec says it executes no agents, and the durability design is argued in prose. Add one if the reviewer or the gate wants executable proof.

## Second opinion

- **Round 1:** revise, confidence 8. Five must-fix items, all applied:
  - tsconfig `types: []` plus `node-shim.d.ts`; `types: ["node"]` failed with TS2688, which the reviewer reproduced.
  - check-doc can no longer pass vacuously: zero blocks, coverage per example, unknown name or kind, unmatched or duplicate or nested markers.
  - The IR schema sits in a checked `wf:schema` block.
  - The SC8 no-go paragraph is mapped.
  - The SC3 table names the spec's eight columns.

  The reviewer verified locally that Node 24 runs `node --test` on a `.ts` glob, that `tsc` is 7.0.2 and accepts `erasableSyntaxOnly`, and that the CI `matrix.biome` leg has `node_modules` before the new step.
- **Round 2:** revise, confidence 7. Two must-fix items, both applied:
  - Tests for nested blocks, a missing schema block and a missing mermaid block.
  - The doc reconciles SC4's "without the user installing a toolchain" with the user's-Node choice: Node is already required by every ACP agent through `npx`.

  Nice-to-haves applied: a distinct `workflow.node` span, a concrete go/no-go threshold, a pinned stringify format, and markers that wrap the whole fence.
- **Disposition:** the skill allows at most two rounds, so this plan is presented after round 2's fixes without a third review.

## Decision log

- **2026-10-05** — Started from a fresh branch on `origin/main` instead of rebasing `feature/495-design-workflow-engine`. Why: that branch carried only the spec, which merged in #504.
- **2026-10-05** — Durable run log is plugin-owned: SQLite through Node's built-in `node:sqlite` in `HIVE_PLUGIN_DATA_DIR`. Why: user choice (round B). It keeps 492's split (no daemon change, no contract bumps), needs no extra service, and the static IR needs checkpoints, not replay.
- **2026-10-05** — TS workflow files run on the user's Node (22.6+ type stripping), found through the login PATH, and are type-checked by a Hive-shipped native checker (TypeScript 7 / tsgo) before each run. Workflows are disabled with a clear message when no suitable Node exists. Why: user choice (round B). Bundling Node costs 40–100 MB per platform plus security tracking.
- **2026-10-05** — The ACP Claude untrusted-hooks gap is filed as #505, not fixed here. Why: user choice (round B). It affects shipped code, and 495 ships no product code; the design doc's security section cites #505.
- **2026-10-05** — The IR's JSON Schema lives in its own `scripts/workflow-proto/ir-schema.ts`, not in `dsl.ts`. Why: it is data the doc, the test and `emit.ts --schema` all render, and keeping it apart keeps `dsl.ts` to the builder and validator.
- **2026-10-05** — Agent nodes take `worktree: 'own' | { of: <agent node> }`, and `reviewLoop` puts the reviewer in the worker's worktree. Why: one worktree per node (SC6) with an `own`-only model would have the reviewer read an empty checkout. Sharing a worktree never shares an ACP session, so F2 still holds.
- **2026-10-05** — Id segments are `[a-z][a-z0-9]*` joined by single hyphens. Why: Mermaid node names are derived from ids (`-` → `_`, `/` → `__`). With doubled or trailing hyphens allowed, `a--b` and `a/b` would collide. A test pins it.
- **2026-10-05** — `fanOutThenVerify(items, step, verify)` takes a third argument, the verify check. Why: the pass/fail claim after a fan-out must be a check node, not an agent's word. The spec's example signature was illustrative (it says "e.g.").
- **2026-10-05** — `scripts/workflow-proto/package.json` is `{"type":"module"}` only. Why: without it `tsc`'s `nodenext` would treat the `.ts` files as CommonJS and reject `import.meta`. There is no dependency and no install.
- **2026-10-05** — Go/no-go verdict: go, custom, gated on gate B and #505. Why: the framework adapter (data conditions, node-id keys, interrupt-safe ACP nodes, IR export, checkpoint mapping) is about one-half to one-third of the custom core. That sits at the "under half" threshold, not under it, and it adds a native module and license terms. Detail in the doc.
- **2026-10-05** — Moved the OpenTelemetry spec file a research subagent left untracked at the repo root (`a.md`) into the session scratchpad. It was not part of this change.
- **2026-10-05** — The Node floor for workflow files is 22.18 (or 23.6), not 22.6. Why: type stripping is unflagged only from those versions, and the engine should not depend on `--experimental-strip-types`. This corrects the round-B entry above.

## Progress

- **2026-10-05** — Exec plan created; research started.
- **2026-10-05** — Research done (three passes); clarifying round B answered; #505 filed. Stage → PLAN.
- **2026-10-05** — Plan approved (chat, round 1) after two second-opinion rounds.
- **2026-10-05** — Implemented: design doc, prototype (42 tests), CI step, cross-links. Mermaid output rendered in headless Chromium with mermaid@11 (all three examples render).

## PR convergence ledger

- **2026-10-05 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 9f6af028cb224522171916e05fbed7867474265eff7ecc29548c973bd47879b3; threads_open: 0; action: escalated:risky fix needs human decision; head_sha: 3a6e5561.

## Gate verdict

## Open questions
