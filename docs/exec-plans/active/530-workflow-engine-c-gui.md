# Workflow engine C: GUI

- **Spec:** [docs/product-specs/530-workflow-engine-c-gui-workflow-list-trust-dialog-ru.md](../../product-specs/530-workflow-engine-c-gui-workflow-list-trust-dialog-ru.md)
- **Design:** [workflow-engine.md](../../design-docs/workflow-engine.md) (The overlay, Security), [acp-workflows.md](../../design-docs/acp-workflows.md)
- **Depends on:** A (#528, [plan](528-workflow-engine-a-daemon-sdk-prereqs.md)) and B (#529, [plan](529-workflow-engine-b-headless-engine-core.md), section "Contract for C"). See **Dependencies on A and B** below.
- **Issue:** #530
- **Branch:** `feature/530-gui` (research and plan). Each implementation PR gets its own branch.
- **Status:** active. Drafted and plan-reviewed on 2026-11-08; **awaiting user approval**. Four open questions (below) must be answered first; two of them belong to B.

## Summary

C is the GUI for the workflow engine. In app code under `cmd/hivegui/frontend/`, it adds a project-scoped **Workflows** modal:
- the workflow list, gated on the `workflows` plugin's own state ("off", "requires Node 26.0.0", "failed");
- the trust view;
- review and start, with the graph and an input form generated from the schema;
- a run view, with the live or replayed overlay, node detail, human answers and permission answers;
- a "waiting on you" banner and desktop notification for human and permission waits.

Every byte goes over A's `PLUGIN_MESSAGE` to B's `workflows` plugin, through the plugin-message plumbing A already merged (`src/app/plugins.ts`). The graph is rendered natively in React, not with Mermaid. C adds no frame, no daemon code and no DaemonContract bump.

This plan supersedes the 2026-10-25 draft on `origin/feature/530-workflow-gui`. The user decisions recorded there stand; this revision re-grounds every claim on `origin/main` at `78ce3ace` (A PR1 #532 and PR2 #539 merged, B1 #538 merged) and adds the mockups, the B dependency list and the refreshed graph evidence.

## Research

Verified against `origin/main` at `78ce3ace` on 2026-11-08. Paths are relative to `cmd/hivegui/frontend/` unless they start with `cmd/`, `internal/`, `plugins/`, `docs/`, `scripts/` or `.github/`.

### Relevant code

| Claim | Where | Consequence for C |
|---|---|---|
| The plugin-message plumbing is **merged** (A PR2): `onPluginMessage`, `deliverPluginMessage`, `sendPluginMessage(pluginId, topic, data)` | `src/app/plugins.ts:88-130` | C's transport is a thin nonce layer on top. No new subscription in `events.ts`. |
| `events.ts` already decodes `plugin:message` and calls `deliverPluginMessage` | `src/app/events.ts:794-806` | Nothing to add there for messages. |
| Nonce helper exists, unexported | `src/app/plugins.ts:32` `newNonce()` | Export it; don't write a second one. |
| The Wails binding, the bridge op and the event name all landed (A-2, A-3) | `cmd/hivegui/app_calls.go:420` `SendPluginMessage`; `cmd/hived-ws-bridge/main.go:657`; `internal/wire/client.go:152`; `test/e2e-real/wails-bridge.ts:282`; `test/e2e/wails-mock.ts:2141` (records into `sentPluginMessages`, answers nothing) | The mock needs a scripted engine behind `SendPluginMessage`. |
| `plugin_not_running` is a plain `control:error` with no `req_id` and no plugin id field | `internal/daemon/pluginmsg.go:217`, code at `internal/wire/request.go:58` | C claims it like `claimPluginInstallError` does (`events.ts:1348`): reject every pending `workflows` request, then stop the generic error line. C is the only app code that sends to a plugin. |
| Reconnect hook | `events.ts:221` calls `relistPluginsIfWanted()` | `resyncWorkflows()` goes beside it. |
| UI plugin surfaces are per **session**, and UI plugins cannot send | `docs/plugins.md` (UI plugins section) | The workflows UI is app code, not a UI plugin (Decision log). |
| Plugin state in the store | `src/app/state.ts:211-219` (`status: 'stopped' \| 'running' \| 'crashed' \| 'failed' \| 'refused'`, `status_detail`, `builtin`) | The plugin gate reads it. `refused` + `status_detail` with the Node version come from **A PR3 (not merged)**. |
| App chords | `src/lib/bindings.ts:62` `DEFAULT_APP_BINDINGS`; `{mac, other}` pattern at `:89-95`; `RESERVED_CHORDS` at `:182` | `Mod+Shift+R` / `Ctrl+Shift+R` is unbound and not reserved. |
| Modal scopes | `src/app/key-scopes.ts:70` `modal()`, `:88` `mirror()`, `:199` `worktrees` (the template), `:358` `KEY_SCOPES` | Two new scopes, `workflow-trust` before `workflows`. |
| Sweep fixtures | `test/e2e/every-shortcut.spec.ts:49` `UNBOUND_SCOPES`, `:61` `FIXTURES`, `:128` `OPENS`, `:144` `expectEffect` (`.close`/`.cancel` closers), `:244` `NOT_IN_MENU` | New fixtures; `menu:workflows` is a real menu item. |
| Menu defaults | `cmd/hivegui/menu_darwin.go:118` (File › Settings… is the template); `cmd/hivegui/testdata/menu-default-accelerators.json` | Add the item and `"workflows": "cmdorctrl+shift+r"`. |
| Commands | `src/app/commands.ts:113` `PALETTE`, `:387` `MENU_COMMANDS`, `:435` menu dispatch | One palette command, `workflows`. |
| Help rows | `src/lib/shortcuts.ts:306` (Worktrees row) | One row. |
| Menu path vs open modal | `src/app/modals/settings.ts:60-74` (`openSettings` guards re-entry) | The native ⇧⌘R accelerator takes the key before the webview, so the `workflows` command itself must toggle (close if open). |
| ModalShell | `src/components/modals/ModalShell.tsx:52` (`size?: 'sm' \| 'md' \| 'lg'`), footer `hints`, `onEscape`; precedent `Worktrees.tsx:120-135` | Add `'xl'`; Escape in a sub-view goes back via `onEscape`. |
| Dialog widths top out at 720px | `src/theme/components/dialog.css:30-32` | Add `[data-size='xl']`. |
| Choice dialog = the confirm overlay; first choice is the safe one and takes focus | `src/app/modals/choice-dialog.ts:47-108` | Cancel-run goes through it. |
| ACP permission card | `src/components/acp/AcpTranscript.tsx:218-262` `PermissionCard` (private), calls `AnswerPermission(sessionId, request_id, option_id)` | Extract to its own file and reuse in node detail. No second copy. |
| Banner slots | `src/store/store.ts:301` `BannerSlot = 'undo-close' \| 'daemon' \| 'update'`; `src/components/Banners.tsx` | Add `'workflow'`. |
| ModalId | `src/store/store.ts:191` | Add `'workflows'`. |
| Dialog roots | `index.html:139-140` (`#worktrees`), portals in `src/components/App.tsx:115` | Add `#workflows`. |
| Notifications | `events.ts:341` (attention, tag = session id), `:362` (idle), `Notify(title, subtitle, body, tag)` | A live wait notifies; a permission wait reuses the session-id tag so the OS collapses it with the session's own attention notification. |
| `bell-click` | `events.ts:1315-1327`; returns early when the tag is not a known session | The `wf:` branch must run **before** the session lookup. |
| Untrusted text precedent | `AcpTranscript.tsx` renders agent text; `marked` is in `package.json` | Engine text (prompts, outputs, errors, file names) renders as **plain React text**, never Markdown or HTML. |
| **No CSP** in the app | No `Content-Security-Policy` in `index.html` or `cmd/hivegui/*.go` | Any `innerHTML` of engine-derived markup would rest on a sanitizer alone. Native React text escapes by construction. |
| IR types and `condLabel` live in B's tree | `plugins/workflows/sdk/dsl.ts:17-125` (`IR`, `Body`, `Node`, `Cond`, …); `plugins/workflows/ir/mermaid.ts:30` `condLabel`; worked examples `plugins/workflows/tools/examples.ts` | Import them; don't copy (Decision log). Probe on 2026-11-08: a frontend-options program (`moduleResolution: bundler`, ES2022 lib) importing `IR`, `Cond` and `condLabel` type-checks with tsc 7.0.2 **once `allowImportingTsExtensions: true` is set** (TS5097 without it). |
| Cross-tree import precedent | `src/lib/whats-new.ts:19` imports `site/features.json`; `vite.config.*:104-105` `server.fs.allow: [__dirname, site/]`, deliberately not the repo root | Add `plugins/workflows` to `fs.allow` (dev server only; `vite build` is unaffected). |
| WebView2 browser accelerators are off | `cmd/hivegui/window_options.go:115` | `Ctrl+Shift+R` (hard reload in a browser) reaches the app on Windows. Linux WebKitGTK gets a manual check (Verification). |
| CI e2e Node version | `.github/workflows/ci.yml:111-113` (`node-version: '24'`, one setup step for the whole frontend job, which runs both e2e legs at `:381-397`) | B's floor is Node 26, so on this job the plugin is `refused`. A PR3 adds a Node 26 leg; C's e2e-real needs the frontend job itself on 26. |
| Mock harness | `test/e2e/wails-mock.ts` (2389 lines; `emit()`, `window.__hive` at `:2165`); parity test `test/unit/bridge-harness-parity.test.ts` | Scripted fake engine, seeded per test. |
| Glyph denylist | `scripts/ui-lint.sh:143` (`×`, `▶`, `✓`, …), scoped to `src/app`, `src/components`, `src/theme`, `index.html` (`:85`) | Labels with symbols are built in `src/lib/`; headers say "at most 3 rounds", not "≤3×". |
| Icons | `src/lib/icons.svg:45-52` (`state-running`, `-working`, `-attention`, `-waiting-permission`, `-starting`, `-exited`, `-error`, `-failed`) | Add `state-pending`, `state-skipped`, `state-interrupted`. |
| Brain lesson (macOS menu vs webview keys, PR #488) | `ModalShell`'s native Escape listener runs before React | Sub-view Escape uses `onEscape`; the trust view adds no key capture, so no menu suspension is needed. |

### Graph rendering evidence (Mermaid vs xyflow vs native)

Measured 2026-11-08 with the frontend's pinned `vite@8.2.2` and `react@19.2.8`. Versions read from the npm registry the same day (`npm view <pkg> version time.modified`): `mermaid@12.3.0` (2026-11-06), `@xyflow/react@12.12.1` (2026-11-08), `@dagrejs/dagre@3.1.3`, `elkjs@0.12.1`. Scripts: scratchpad `mmd/setup.sh`, `mmd/walk.mjs` (walks the Vite manifest from the entry through the flowchart chunk's static imports).

| Criterion | Mermaid 12.3.0 | xyflow 12.12.1 + dagre 3.1.3 | Native (React, `hv-*`, tokens) |
|---|---|---|---|
| Bundle (gzip -9) | Flowchart render path: **29 chunks, 819 KB raw / 218 KB gzip**. The package emits 108 chunks, 5.17 MB raw in all. | **~74 KB gzip** over bare React (133 KB − 59 KB) | ~300 lines, no dependency |
| Theming | Hex-only theme variables ("The theming engine will only recognize hex colors", mermaid.js.org/config/theming). Needs per-theme resolution and a re-render on theme change. Against UI principle 3. | CSS variables work | Tokens directly; presets apply for free |
| Untrusted labels | Labels go through Mermaid's DOMPurify-based sanitizer into `innerHTML`; with no CSP in the app that sanitizer is the only defence | React text | React text |
| Overlay | Colour generated SVG by Mermaid's derived ids (`flowchart-<mid>-N`) | Node components, our data | `data-node-id` on our element; state is a prop |
| A11y | One SVG; nodes not focusable, carry no state | Focusable nodes, but its own keyboard model (pan/zoom, selection) to reconcile with key scopes | Each node is a `<button>` with an `aria-label` naming kind, agent, state, attempt (principle 5) |
| Testability | jsdom has no `getBBox`; can't render in the dom layer | Needs `ResizeObserver` / measured DOM; partly testable | Structure in dom tests; geometry in Playwright |
| Contract | B omits `mermaid` when the reply would pass 48 KiB (`mermaid_omitted`) — C must render from `ir` anyway | From `ir` | From `ir` |
| Fit | Subgraphs for group/loop/map are native | Subflows exist, but nested layout means dagre per body | Edges never leave their body (`workflow-engine.md`, IR), so each body is a DAG and a ranked layout per body is enough |

**Decision: native** (user decision 2026-10-25, re-confirmed by this evidence). xyflow is the credible equivalent and is recorded as the fallback if real workflows tangle beyond a ranked layout plus a barycentre pass.

### Prior lessons

- `webview-key-capture-needs-menu-suspension` (rank 21, PR #488): on macOS a native accelerator beats the webview; `ModalShell`'s native Escape runs before React. Applied: the `workflows` command toggles, and sub-view Escape goes through `onEscape`.

### Conventions card

```
build:  ./build.sh
test:   scripts/test.sh                       # go · unit · dom · e2e
mock:   (cd cmd/hivegui/frontend && CI=1 npx playwright test)
real:   (cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real)
lint:   ./scripts/ci-bootstrap.sh && (cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
ui:     scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast
```

- **TDD.** Pure modules (`lib/workflow-*.ts`, `lib/schema-form.ts`) get unit tests; components get dom tests; layout is checked only in real Chromium (Playwright, `CI=1`). vitest is CSS-blind.
- **Keybindings Policy.** All five updates apply to `workflows`. Every label goes through `shortcutLabel` / `useShortcutLabel`. Modal-local confirm/cancel keys are the AGENTS.md hard-coded exception.
- **The GUI never opens a PTY.** Everything goes through Wails bindings (`SendPluginMessage`, `AnswerPermission`, `SetPluginEnabled`, `Notify`).
- **Untrusted text** from the engine renders as plain text.
- **Docs.** Changeset per user-visible PR; `site/features.json` with `since: "Unreleased"`; README Keybinds row and a Workflows paragraph; never edit `CHANGELOG.md` or `docs/product-specs/index.md`.

## Dependencies on A and B

C can be built and fully tested at the mock layer now. It can only **merge** once everything below is on `main`; otherwise a release would ship a Workflows command whose panel says "no workflow engine".

### From A (#528)

| Need | Where | Status |
|---|---|---|
| `PLUGIN_MESSAGE`, `SendPluginMessage` binding, `plugin:message` event, bridge op (A-2), shared event name (A-3) | PR2 | **Merged** (#539) |
| `plugin_not_running` error | PR2 | **Merged** |
| `refused` with a `status_detail` naming the required Node version; the manifest `node` field; **Restart** on failed/refused rows (`SET_PLUGIN_ENABLED true`) | PR3 | **Not merged.** Criterion 1's "required version" text is this string. |
| A Node 26 CI leg | PR3 | Not merged. C additionally moves the frontend job to Node 26 for e2e-real (Files to change). |

### From B (#529): topics and shapes C uses

All on `PLUGIN_MESSAGE` with `plugin_id: "workflows"`; requests carry `data.nonce`; replies on `<topic>.reply` with `{nonce, ok, error?: {code, message}, …}`; every message ≤ 48 KiB.

| Topic | C uses | B phase |
|---|---|---|
| `workflows.list {project_id}` → `{folder, trust, invalid_reason?, workflows: [{file, name?}]}` | List view | B4 (trust state) |
| `workflow.trust.describe {project_id}` → `{folder, hash, files: [{path, bytes}]}`; errors `symlink`, `too_large`, `missing` | Trust view | B4 |
| `workflow.trust.confirm {project_id, hash}` → `{}`; error `changed` | Trust view | B4 |
| `workflow.build {project_id, file}` → `{name, ir, mermaid?, mermaid_omitted?, source_hash, input_schema}`; errors `untrusted`, `changed`, `type_errors` (`type_errors: [{file, line, col, message}]`), `build_failed`, `invalid_ir` (`errors: string[]`), `sandbox_unavailable`, `typecheck_unavailable` | Review view. C ignores `mermaid`. | B4 |
| `run.start {project_id, file, input, source_hash}` → `{run_id}`; error `changed` | Review view | B2 |
| `run.cancel {run_id}`, `run.retry {run_id}` → `{}` | Run view | B2 |
| `runs.list {project_id?, state?, before?, limit ≤ 50}` → `{runs: [{run_id, project_id, workflow, file, state, created_at, updated_at, source_hash, source_changed}]}` (B-2) | List view (recent runs); waiting scan at boot | B2 |
| `run.get {run_id}` → `{run: {…, input, base_sha, kept_worktrees, error}, ir, nodes: [{key, node_id, state, attempt, session_id?, branch?, worktree?, error?, started_at?, ended_at?}]}` | Run view | B2 — **shape open, see Q-B1** |
| `node.get {run_id, key}` → `{key, node_id, state, attempt, output?, truncated?, error?, conflict?}` | Node detail | B2 — **paging open, see Q-B2** |
| `run.events {run_id, after_seq, limit ≤ 500}` → `{events, last_seq, more}` | Replay, gap fill, reconnect | B2 |
| `human.answer {run_id, key, answer}` → `{}`; errors `invalid_answer` (path-qualified), `not_waiting` | Human wait | B2 |
| Push `run.event {run_id, events: RunEvent[]}`, contiguous `seq` per batch | Live overlay | B5 (embedding) — event schema from B2/B3 |
| `RunEvent` types: `run.started`, `run.finished`, `node.ready`, `node.started`, `node.waiting` (`human`: `prompt`, `output_schema`; `permission`: `session_id`, `request_id`, `title`, `options`), `node.resumed` (B-1), `node.output`, `node.finished` (incl. `conflict`), `loop.iteration`, `map.expanded`, `agent.*`, `check.output` | Overlay fold, node detail | B2 (core), B3 (`agent.*`, permission waits, `node.resumed`) |
| Node states `pending running waiting done failed skipped interrupted cancelled`; run states `running waiting done failed cancelled` | Icons and words | B2 |
| Overlay keys: `node_id` = IR id; `key` = instance (`<map>[<i>]/<node>`, `<loop>[<k>]/<node>`) | Overlay | B2 |
| Plugin embedded in `hived`, ships **disabled**, DaemonContract bump | Plugin gate, e2e-real | B5 |
| Seeded tsgo tarball (B-3) at `plugin-data/workflows/tsgo/cache/typescript-<platform>-7.0.2.tgz` | e2e-real `globalSetup` | B4 |
| A check with no upstream head runs in its own clean worktree at the pinned base (B-4, decided) | e2e-real fixture: human → check | B2/B3 |

### B open questions and what C needs from them

From B's plan, `## Open questions`:

- **Q-B1 — `run.get` size (must be settled before C's run view).** A run with 32 map copies of many nodes can exceed the message cap in `nodes`. This changes the reply shape C parses, its transport loop and its tests. C's recommendation: B adds `{nodes_after?: key}` to the request and `{more: boolean, next?: key}` to the reply, byte-budgeted under 48 KiB, with `run` and `ir` only on the first page. C then pages with the same helper it uses for `run.events`. The alternative (C derives node states from `run.events` alone and `run.get` drops `nodes`) also works but costs a full replay on every open.
- **Q-B2 — full output retrieval (must be settled, or truncation accepted as final).** `node.get` truncates at 16 KiB. If B adds `offset`/`limit`, NodeDetail gets a **Load more** button and one dom test; if not, NodeDetail shows "Output truncated at 16 KiB" and, for agent nodes, **Open session** (the transcript has the full text). C's recommendation: accept truncation for now; the session link covers agents, and check output is a log tail.
- **Outputs vs `redact()`** — does not change C's files or tests: C displays whatever the topics return.
- **B3 spawn-to-record gap** and **B3 group survivors** — do not change C. A `failed` node with `kept_worktrees` already renders through `error` and the run header.

## Approach

### Where the UI lives: app code, one modal

A first-party modal, not a bundled UI plugin: criterion 7 needs `commands.ts`, the menu, the accelerators fixture and the README, which plugin commands can't reach; plugin surfaces are per session; the UI plugin API cannot send; a bundled plugin may use nothing a third-party one cannot (`docs/plugins.md`). Cost: the app knows the plugin id `workflows` and B's topics. Both live in this repo, so a contract change moves in lock-step like wire changes.

Not the sidebar (it is sessions and projects; a run is neither) and not Settings (a run is work, not configuration). One `xl` blocking modal, opened by the command for the active project (user decision).

### Transport (`src/app/workflows.ts`)

- **`request<T>(topic, data, {timeoutMs})`.** Adds `nonce: newNonce()` (exported from `plugins.ts`), calls `sendPluginMessage('workflows', topic, {...data, nonce})`, and resolves on the first message from `onPluginMessage` with `plugin_id === 'workflows'`, `topic === `${topic}.reply`` and `data.nonce` equal. Rejects on `ok: false` with a `WorkflowError {code, message, type_errors?, errors?}`, or on timeout.
  - Timeouts: 15 s; `workflow.build` 180 s (the first build may download tsgo — the spinner says so after 5 s).
  - Replies for other windows' nonces are ignored (every UI client receives every reply).
- **`plugin_not_running`.** `claimWorkflowsNotRunning(e)` in `workflows.ts`, called in the `control:error` handler right after `claimPluginInstallError` (`events.ts:1348`): when the code is `plugin_not_running` and requests are pending, reject them all and return true, so the generic error line stays quiet. The plugin gate then shows the real state from the store.
- **`pageAll(topic, req, more)`.** One helper for `run.events` (and `run.get` if Q-B1 lands as paging).
- **Pushes.** `run.event` batches go to the subscribers of that `run_id`, and every batch goes to the waiting tracker.
- **Gaps.** A subscriber keeps `lastSeq`. If a batch starts past `lastSeq + 1`, it pages `run.events {after_seq: lastSeq, limit: 500}` until `more` is false, then applies the buffered batch, dropping `seq ≤ lastSeq`.
- **Reconnect.** `resyncWorkflows()` beside `relistPluginsIfWanted()` (`events.ts:221`): re-pages every open run from its `lastSeq` and re-runs the waiting scan.
- **Waiting scan.** At boot (after the plugin list arrives with `workflows` running) and after a reconnect: `runs.list {state: 'waiting'}` (B-2), then `run.get` per run to find which keys wait. Live `node.waiting` / `node.resumed` / `node.finished` / `run.finished` keep the set current. A non-empty set raises the `workflow` banner.
- **Desktop notification** (user decision). A **live** `node.waiting` calls `Notify(workflow name, project name, body, tag)`:
  - human: body "Waiting for your answer — click to open.", tag `wf:<run_id>:<key>`;
  - permission: body "Waiting for permission — click to open.", tag = the node's `session_id`, so the OS collapses it with the ACP session's own attention notification (`events.ts:341`).
  - The boot/reconnect scan never notifies, so a restart doesn't re-announce old waits.
  - `bell-click` (`events.ts:1315`): a tag starting `wf:` is handled **before** the session lookup and opens the modal on that run with the key selected; any other tag keeps today's behaviour (switch to the session, where `PermissionCard` answers it).

### Plugin state gate

The modal body reads `plugins.find(p => p.id === 'workflows')` from the store.

| Plugin state | Panel shows |
|---|---|
| Absent | "This Hive has no workflow engine. Update Hive." |
| `enabled: false` | "Workflows are off." **Turn on** → `SetPluginEnabled('workflows', true)` (a builtin needs no trust prompt). |
| `refused` | `status_detail` verbatim (A PR3: "requires Node.js 26.0.0 or newer (found v24.11.0 at …)") and **Restart**. **Criterion 1's required version.** |
| `failed` / `crashed` | `status_detail` and **Restart** |
| `stopped` while enabled | spinner "Starting the workflow engine…" |
| `running` | The list |

**Restart** reuses whatever A PR3 exports for PluginsPanel's Restart (it sends `SET_PLUGIN_ENABLED true`); C does not write a second implementation.

### Views (`src/components/modals/Workflows.tsx`)

`#workflows`, `ModalShell size='xl'`, title "Workflows · <project>" with the `workflows` shortcut hint. View state in the store slice `workflows`: `{view: 'list' | 'trust' | 'review' | 'run', projectId, file?, runId?, selectedKey?}`. Escape in `trust`/`review`/`run` goes back to `list` (`onEscape`); in `list` it closes.

1. **List.** `workflows.list` and `runs.list {project_id, limit: 20}` on open and on **Refresh** (a button only: the `workflows` scope binds no bare letters, because other views hold text inputs). Trust chip (`trusted` / `none` / `changed` / `invalid` + `invalid_reason`). When trust is `none` or `changed`, file rows are inert and **Review & trust…** is the primary action. Otherwise a file row opens Review; a run row opens Run.
2. **Trust.** `workflow.trust.describe`; folder path, short hash (12 hex), file list with sizes; what trusting means; footer hints; **Trust and continue** and **Cancel**. Confirm sends `workflow.trust.confirm {project_id, hash}`; `changed` re-describes with "The files changed while you were reading — review them again."; `symlink` / `too_large` / `missing` show their reason and no confirm button. Initial focus and button style: **Q-C1**.
3. **Review & start.** `workflow.build` with a spinner; on success `WorkflowGraph(ir)` (no overlay) beside `SchemaForm(input_schema)` and **Start run** → `run.start {project_id, file, input, source_hash}` → Run view. Errors: `type_errors` as `file:line:col  message` rows; `errors[]` for `invalid_ir` / `build_failed`; `sandbox_unavailable` / `typecheck_unavailable` with their message; `untrusted` / `changed` go to Trust; `changed` on `run.start` rebuilds and says "The workflow changed since you reviewed it."
4. **Run.** `run.get` provides `ir` and node states; `run.events` from seq 0 replays, then the live stream continues. Header: run state icon + word, workflow name, start time, **source changed since this run started** chip when `source_changed`, **Cancel run** (choice dialog, safe first choice "Keep running"), **Retry** only for `failed`/`cancelled`. Body: the overlaid graph and `NodeDetail` for the selected key.

### Mockups (ASCII; the real layout is validated in Chromium, see Tests)

Plugin gate (one of five bodies shown):

```
┌ Workflows · hive ─────────────────────────────── ⇧⌘R ─ [x] ┐
│                                                             │
│   (!) The workflow engine needs a newer Node.js.            │
│       requires Node.js 26.0.0 or newer                      │
│       (found v24.11.0 at /usr/local/bin/node)               │
│                                                             │
│       [ Restart ]                                           │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                                              [esc] close    │
└─────────────────────────────────────────────────────────────┘
```

List:

```
┌ Workflows · hive ───────────────────────────────────── [x] ┐
│ .hive/workflows   ( trusted )                  [ Refresh ]  │
│ ─────────────────────────────────────────────────────────── │
│  review-loop.ts        Review loop                      >   │
│  fan-out.ts            Fan out and verify               >   │
│  plan.ts               (not built yet)                  >   │
│                                                             │
│ Recent runs                                                 │
│  <> waiting   Review loop     2 min ago                 >   │
│  .. running   Fan out         10 min ago                >   │
│  ok done      Review loop     yesterday  (source changed) > │
├─────────────────────────────────────────────────────────────┤
│                                              [esc] close    │
└─────────────────────────────────────────────────────────────┘
untrusted variant: chip "( not trusted )", rows dimmed, primary [ Review & trust… ]
```

Trust:

```
┌ Workflows · hive › Trust ──────────────────────────── [x] ┐
│ Trust the workflows in /Users/me/hive/.hive/workflows?     │
│ Contents a1b2c3d4e5f6                                       │
│   review-loop.ts      2.1 KB                                │
│   fan-out.ts          1.4 KB                                │
│                                                             │
│ Building a workflow runs these files as you, inside Node's  │
│ permission sandbox. The sandbox is defence in depth, not a  │
│ security boundary. Hive asks again whenever any file here   │
│ changes. Nothing has been built yet.                        │
│                                                             │
│                       [ Cancel ]  [ Trust and continue ]    │
├─────────────────────────────────────────────────────────────┤
│        (y)/[enter] trust  ·  (n)/[esc] cancel               │
└─────────────────────────────────────────────────────────────┘
```

Review & start:

```
┌ Workflows · hive › Review loop ───────────────────── [x] ┐
│ ┌ graph (scrolls) ────────────────┐ ┌ Input ────────────┐ │
│ │ ┌ fix · at most 3 rounds ─────┐ │ │ task *            │ │
│ │ │ until review.verdict = ok   │ │ │ [_______________] │ │
│ │ │  [ work · claude ]          │ │ │ strict            │ │
│ │ │        │                    │ │ │ [x]               │ │
│ │ │  [ review · codex ]         │ │ │ max_files         │ │
│ │ └─────────────────────────────┘ │ │ [ 10 ]            │ │
│ │        │ review.verdict = ok    │ │                   │ │
│ │  [ approve · human ]            │ │   [ Start run ]   │ │
│ └─────────────────────────────────┘ └───────────────────┘ │
├───────────────────────────────────────────────────────────┤
│                                  [esc] back               │
└───────────────────────────────────────────────────────────┘
build error variant: graph pane replaced by "Type errors" list:
  review-loop.ts:12:7  Type 'number' is not assignable to type 'string'.
```

Run (live), with a map disclosure, a loop header and a human wait selected:

```
┌ Workflows · hive › Review loop · run 7f3a ─────────────── [x] ┐
│ <> waiting · started 14:02 · (source changed)  [Cancel run]  │
│ ┌ graph ──────────────────────────┐ ┌ approve · human ──────┐ │
│ │ ┌ fix · round 2 of 3 ─────────┐ │ │ <> waiting for you    │ │
│ │ │ [ok work · claude  done   ] │ │ │ Approve the change?   │ │
│ │ │ [ok review · codex done   ] │ │ │ (plain text prompt)   │ │
│ │ └─────────────────────────────┘ │ │ verdict *             │ │
│ │ ┌ each · for each split.tasks ┐ │ │ (o) approve           │ │
│ │ │ 3 of 5 done, 2 at once      │ │ │ ( ) reject            │ │
│ │ │ [.. do · claude  running  ] │ │ │ note                  │ │
│ │ │  v 5 copies                 │ │ │ [_______________]     │ │
│ │ │    each[0]/do  ok done      │ │ │            [ Answer ] │ │
│ │ │    each[1]/do  .. running   │ │ └───────────────────────┘ │
│ │ │    …                        │ │                           │
│ │ └─────────────────────────────┘ │                           │
│ │ [<> approve · human waiting]    │                           │
│ └─────────────────────────────────┘                           │
├───────────────────────────────────────────────────────────────┤
│                                    [esc] back                 │
└───────────────────────────────────────────────────────────────┘
```

Node detail, permission wait (reuses `PermissionCard`):

```
┌ review · codex ────────────────────────┐
│ ?  waiting for permission               │
│ Run `go test ./...`?                    │
│ [ Allow once ] [ Always ] [ Reject ]    │
│ attempt 1 · branch hive/wf/7f3a/fix_2/… │
│ [ Open session ]                        │
└─────────────────────────────────────────┘
```

Banner and cancel confirm:

```
<> Workflow "Review loop" in hive is waiting on you.   [ Open ]  ⇧⌘R
```
```
┌ Cancel this run? ─────────────────────────────┐
│ Running agents are stopped. Worktrees with     │
│ changes are kept.                              │
│        [ Keep running ]  [ Cancel run ]        │
└────────────────────────────────────────────────┘
```

(`<>`, `..`, `ok`, `?`, `(!)` stand for the sprite's state icons; nothing is a Unicode glyph in the code.)

### Graph (`src/lib/workflow-graph.ts`, `src/components/workflows/WorkflowGraph.tsx`)

- **Pure layout.** `layoutBody(body)`: rank 0 for entry nodes, otherwise `1 + max(rank(pred))`; rows by rank in IR order; edges carry `condLabel(cond)` imported from `plugins/workflows/ir/mermaid.ts`; recurses into `group` / `loop` / `map`. `ponytail:` no crossing minimisation; add a barycentre pass if real workflows tangle.
- **Headers** (strings built in `lib/`): loop "at most N rounds until <cond>" + live "round k of N"; map "for each <ref>, C at once" + live "k of n done".
- **Rendering.** Each body is a `.hv-wf-body` (CSS grid, one row per rank). A container is a bordered `.hv-wf-sub` with its header. Nodes are `<button class="hv-wf-node" data-node-id data-state>` with `local id · agent|check|human`, a state icon and a state word; `aria-label` "<id>, <kind> <agent>, <state>, attempt <n>". Edges are one `aria-hidden` SVG per body, paths computed from `getBoundingClientRect` in `useLayoutEffect` plus a `ResizeObserver`; condition labels are `.hv-wf-edge-label` chips at the midpoint.
- **Map copies.** Template node shows the aggregate state and a "N copies" disclosure; expanding lists one row per key (`each[2]/do`) with its icon; selecting a row selects that key.
- **Loop copies.** Each node shows its latest iteration; detail lists per-iteration keys.

### Overlay (`src/lib/workflow-overlay.ts`)

`fold(state, events) → state`, pure; live and replay use the same function: `runState`; `nodes[node_id] = {aggregate, instances: {key: {state, attempt, waiting?, session_id?, error?, conflict?}}}`; `loops[node_id] = {iteration, max_iters}`; `maps[node_id] = {count, concurrency, done}`; `lastSeq`. Aggregate rank: `waiting > running > failed > interrupted > pending > cancelled > skipped > done` (attention wins). `agent.*` and `check.output` kept per key, last 200, for the detail pane only. Events for another `run_id` are ignored. Replay shows the final state; no scrubber (spec non-goal).

### Node detail (`src/components/workflows/NodeDetail.tsx`)

Facts (kind, key, state, attempt, branch, worktree, error, `conflict {branch, files, worktree}` with the recovery text); **Open session** (`switchTo(session_id)`, closes the modal); output via `node.get` on select, with the truncation note per Q-B2; recent `agent.message` / `check.output` as plain text; human wait = prompt (plain text) + `SchemaForm(output_schema)` + **Answer** → `human.answer` (`invalid_answer` inline, `not_waiting` refreshes); permission wait = the extracted `PermissionCard` (`permission_stale` handling unchanged) + **Open session**.

### Schema form (`src/lib/schema-form.ts`, `src/components/workflows/SchemaForm.tsx`)

| Schema (B's subset, `plugins/workflows/sdk/schema.ts`) | Field |
|---|---|
| `string` | `<input>` |
| `enum` | radio group (≤ 4 values) or `<select>` |
| `number` / `integer` | `type=number` |
| `boolean` | checkbox |
| `array` of scalars | textarea, one value per line |
| `array` of objects | JSON textarea, parsed on submit |
| `object` | `<fieldset>`, recursive |

`required` from the schema; `toValue(schema, raw)` coerces. Client checks stop at native validity; the engine is authoritative (`invalid_answer`, `run.start` errors). Fields follow `components.md` › Form fields (label above, 28px input, tokens), as ProjectEditor and Settings do; there is no form-field component to reuse, and C adds none.

### Keys and commands (Keybindings Policy checklist)

**Command `workflows`** — all five updates:
1. **Binding + command.** `src/lib/bindings.ts`: `{ keys: { mac: 'Mod+Shift+R', other: ['Ctrl+Shift+R', 'Ctrl+Shift+[KeyR]'] }, command: 'workflows' }`. `src/app/commands.ts`: `PALETTE` entry `{ id: 'workflows', title: 'Workflows…', run: toggleWorkflows }` and `MENU_COMMANDS['menu:workflows'] = 'workflows'`. `toggleWorkflows()` closes when open (the native accelerator beats the scope's `mirror`), else opens for the active project (no project → flash "Open a project first").
2. **Native menu default.** `cmd/hivegui/menu_darwin.go`: File › "Workflows…" with `accel("workflows", keys.Combo("r", keys.ShiftKey, keys.CmdOrCtrlKey))`, `emit("menu:workflows")`. Fixture `cmd/hivegui/testdata/menu-default-accelerators.json`: `"workflows": "cmdorctrl+shift+r"`.
3. **Help overlay.** `src/lib/shortcuts.ts` row beside Worktrees (`:306`): `{ command: 'workflows', label: 'Workflows in the active project' }`. This also lists it in Settings › Shortcuts.
4. **README.** Keybinds table row: `| ⇧⌘R | Workflows in the active project (list, trust, run, answer) |`.
5. **Changeset.** Yes (Phases).
- **`RESERVED_CHORDS`:** no change. C adds no terminal-owned predicate.

**Scope `workflow-trust`** (exclusive, trap `workflows`), active only while the modal is open and `view === 'trust'` (no text input there, so bare letters are safe): `Shift?+Y`, `Enter` → `workflow-trust.confirm`; `Shift?+N`, `Any+Escape` → `workflow-trust.cancel`. `Enter` on a focused **Cancel** cancels (confirm reads `document.activeElement`). These are the AGENTS.md dialog confirm/cancel exception: not in the help overlay, not rebindable, no README row. Placed before `workflows` in `KEY_SCOPES`.

**Scope `workflows`** (exclusive, trap `workflows`): `Any+Escape` → `workflows.close` (back one view, or close in `list`), `mirror('workflows', 'workflows.close')`. No bare letters (views have inputs). Placed before `whatsNew` in `KEY_SCOPES`.

**Unlisted commands** (`commands.ts`, no `title`): `workflows.close`, `workflow-trust.confirm`, `workflow-trust.cancel`. Cancel run is a button that opens the choice dialog, not a command (nothing binds it).

**Labels.** Title suffix and banner hint use `useShortcutLabel('workflows')`. Modal-local hints follow `Worktrees.tsx:131-135`.

**Sweep** (`every-shortcut.spec.ts`): `FIXTURES.workflows` (open via `run(page, 'workflows')` on a mock project with the engine running) and `FIXTURES['workflow-trust']` (trust `none`, click **Review & trust…**); `OPENS.workflows = 'workflows'`. `workflows.close` and `workflow-trust.cancel` satisfy the `.close`/`.cancel` closer rule; `workflow-trust.confirm` is dispatch-only there and proven in a dom test. `menu:workflows` is emitted by `menu_darwin.go`, so it is **not** added to `NOT_IN_MENU`.

### UI system

- **Tokens.** Existing only; if a role is missing, add it to `tokens.md` + `tokens.css` in the same PR.
- **Icons.** Add `state-pending` (hollow circle), `state-skipped` (dash), `state-interrupted` (broken ring) to `src/lib/icons.svg` and `icons.md`.
- **State → icon.** `running` → `state-working`; `waiting` human → `state-attention` (pulses, per patterns.md); `waiting` permission → `state-waiting-permission`; `done` → `check`; `failed` → `state-failed`; `cancelled` → `state-exited`; `pending` / `skipped` / `interrupted` → the new icons. Always icon + word.
- **Dialog size.** `dialog.css`: `.hv-dialog__panel[data-size='xl'] { max-width: min(1120px, 94vw); }`; `ModalShell` `size` gains `'xl'`. The graph pane scrolls inside itself; the page never scrolls horizontally.
- **Docs.** `docs/design-docs/ui/README.md` decisions table: "Workflow graph (2026-11-08, spec 530): native React ranked layout, not Mermaid or xyflow — tokens, a11y, overlay ids, no sanitizer dependency; mockups in exec plan 530". `components.md` documents `.hv-wf-*` and `WorkflowGraph`; `icons.md` the three icons.

### Phases (PR split)

All C PRs merge **after** A PR3 and B5. Recommended split (Q-C2): **two PRs**.

| PR | Scope | Changeset |
|---|---|---|
| **C1** | Transport (`workflows.ts`, `newNonce` export, `plugin_not_running` claim, `pageAll`), plugin gate, list, trust, build/review/start, `WorkflowGraph` (static), `SchemaForm`, `xl` size, the command + both scopes + menu + fixture + help row + README Keybinds row, three icons, mock engine for these topics, `vite.config` / `tsconfig` cross-tree import | `added`/`minor`: "Workflows panel (⇧⌘R) lists a project's workflows, asks before trusting them, and starts a run from a typed form" |
| **C2** | Run view: overlay fold, replay, gap and reconnect resync, node detail, `PermissionCard` extraction, human and permission answers, cancel/retry, banner, live-wait notifications and `bell-click` routing; e2e-real + CI Node 26; `site/features.json`, README Workflows paragraph, design-system docs, `docs/plugins.md` line | `added`/`minor`: "Watch a workflow run live on its graph and answer its questions" |

Alternative: one PR (≈ the same reviewable surface as C1+C2, but one review loop). Three PRs (the 2026-10-25 split) gave no shippable intermediate.

**DaemonContract:** none. C touches `cmd/hivegui/**` (incl. the ungated Go `menu_darwin.go`), `.github/workflows/ci.yml`, `cmd/hivegui/frontend/vite.config.*`/`tsconfig.json` and docs. `scripts/check-daemon-contract.sh origin/main HEAD` proves it per PR.

### Files to change

- `cmd/hivegui/frontend/src/lib/bindings.ts` — the `workflows` chord (C1).
- `cmd/hivegui/frontend/src/app/commands.ts` — `workflows` in `PALETTE`, unlisted `workflows.close`, `workflow-trust.confirm`, `workflow-trust.cancel`; `MENU_COMMANDS['menu:workflows']` (C1).
- `cmd/hivegui/frontend/src/app/key-scopes.ts` — `workflowTrust` and `workflows` scopes in `KEY_SCOPES` (C1).
- `cmd/hivegui/frontend/src/lib/shortcuts.ts` — help row (C1).
- `cmd/hivegui/frontend/src/store/store.ts` — `ModalId` + `'workflows'` (C1), `BannerSlot` + `'workflow'` (C2), `workflows` slice (C1, extended C2).
- `cmd/hivegui/frontend/index.html` — `#workflows` dialog root (C1).
- `cmd/hivegui/frontend/src/components/App.tsx` — the portal (C1).
- `cmd/hivegui/frontend/src/components/Banners.tsx` — `workflow` banner (C2).
- `cmd/hivegui/frontend/src/components/modals/ModalShell.tsx` — `size` gains `'xl'` (C1).
- `cmd/hivegui/frontend/src/theme/components/dialog.css` — `xl` (C1).
- `cmd/hivegui/frontend/src/components/acp/AcpTranscript.tsx` — import the extracted `PermissionCard` (C2).
- `cmd/hivegui/frontend/src/app/plugins.ts` — export `newNonce` (C1).
- `cmd/hivegui/frontend/src/app/events.ts` — `claimWorkflowsNotRunning` after `claimPluginInstallError` (`:1348`) and `resyncWorkflows()` beside `:221` (C1); waiting scan trigger on `plugin:list`/`plugin:event`, `wf:` branch first in `bell-click` (`:1315`) (C2).
- `cmd/hivegui/frontend/src/lib/icons.svg` — three icons (C1).
- `cmd/hivegui/frontend/tsconfig.json` — `allowImportingTsExtensions: true` (C1).
- `cmd/hivegui/frontend/vite.config.js` — `server.fs.allow` (`:104-105`) gains `plugins/workflows` (C1).
- `cmd/hivegui/menu_darwin.go` — File › Workflows… (C1).
- `cmd/hivegui/testdata/menu-default-accelerators.json` — `"workflows": "cmdorctrl+shift+r"` (C1).
- `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — scripted engine behind `SendPluginMessage` (`__hive.seedWorkflows(spec)`), the `workflows` builtin in `mockPlugins` (default disabled), `__hive.pushRunEvents(run_id, events)` (C1, C2).
- `cmd/hivegui/frontend/test/e2e/every-shortcut.spec.ts` — `FIXTURES.workflows`, `FIXTURES['workflow-trust']`, `OPENS.workflows` (C1).
- `cmd/hivegui/frontend/test/e2e-real/globalSetup.mjs` — `npm pack @typescript/typescript-<platform>@7.0.2` into the isolated state dir's `plugin-data/workflows/tsgo/cache/` (C2).
- `.github/workflows/ci.yml` — frontend job `node-version: '26'` (or the e2e-real step onto A PR3's Node 26 leg, whichever A PR3 leaves) (C2).
- `README.md` — Keybinds row (C1); Workflows paragraph under Plugins (C2).
- `site/features.json` — "Run multi-agent workflows", `status: shipped`, `since: "Unreleased"` (C2).
- `docs/design-docs/ui/README.md`, `components.md`, `icons.md` (C1 icons, C2 rest).
- `docs/plugins.md` — one line in B5's Workflows section: the app's Workflows panel is the client of these topics (C2).

### New files

- `cmd/hivegui/frontend/src/app/workflows.ts` — transport, subscriptions, resync, waiting tracker, open/close/toggle.
- `cmd/hivegui/frontend/src/lib/workflow-types.ts` — reply types and `RunEvent` (B's contract tables); IR types re-exported from `plugins/workflows/sdk/dsl.ts`.
- `cmd/hivegui/frontend/src/lib/workflow-graph.ts` — pure layout and header labels.
- `cmd/hivegui/frontend/src/lib/workflow-overlay.ts` — pure fold.
- `cmd/hivegui/frontend/src/lib/schema-form.ts` — field descriptors and coercion.
- `cmd/hivegui/frontend/src/components/modals/Workflows.tsx` — modal and views.
- `cmd/hivegui/frontend/src/components/workflows/WorkflowGraph.tsx`, `NodeDetail.tsx`, `SchemaForm.tsx`.
- `cmd/hivegui/frontend/src/components/acp/PermissionCard.tsx` — extracted from `AcpTranscript.tsx`, unchanged behaviour.
- `cmd/hivegui/frontend/src/theme/components/workflows.css`.
- `cmd/hivegui/frontend/test/e2e-real/fixtures/hello-workflow.ts` — human `approve` → check `["git","rev-parse","HEAD"]` (B-4 decided).
- `.changesets/530-workflows-panel.md` (C1), `.changesets/530-workflows-run-view.md` (C2).
- The tests below.

No IR fixture files: tests build IR from `plugins/workflows/tools/examples.ts` (the same worked examples B's tests use), so the GUI can't drift from B's builder.

### Tests

**Unit**
- `test/unit/workflow-graph.test.ts`: `ranks entry nodes 0 and joins at 1+max`; `edges never leave their body`; `the three worked examples lay out without throwing and keep IR order within a rank`; `loop and map headers read at most N rounds and for each ref, C at once`.
- `test/unit/workflow-overlay.test.ts`: `node.* events drive instance state`; `map copies aggregate under the template as k of n done`; `loop.iteration sets round k of N`; `permission node.waiting then node.resumed clears the wait`; `human node.waiting carries prompt and output_schema`; `aggregate ranks waiting over running over failed`; `replay of all events equals incremental live fold`; `events of another run_id are ignored`; `firstGap reports the missing seq`; `node.finished conflict is kept on the instance`.
- `test/unit/schema-form.test.ts`: `descriptors for string enum number boolean array object`; `coerces numbers and one-per-line arrays`; `array of objects parses JSON and reports a parse error`; `required follows the schema`.

**DOM**
- `test/dom/workflows-transport.test.ts`: `reply settles only its own nonce`; `a reply for another window is ignored`; `ok false rejects with code and type_errors`; `timeout rejects`; `plugin_not_running rejects every pending request and is claimed`; `gap triggers run.events paging and dedupes seq`; `reconnect re-pages open runs from lastSeq`; (if Q-B1 = paging) `run.get pages nodes until more is false`.
- `test/dom/workflows-notify.test.ts`: `live human node.waiting calls Notify with tag wf:<run_id>:<key>`; `live permission node.waiting calls Notify tagged with the session_id`; `the boot waiting scan raises the banner but never notifies`; `bell-click with a wf: tag opens the run with that key selected`; `bell-click with a session id still switches to the session`.
- `test/dom/workflows-modal.test.tsx`: `absent plugin says update Hive`; `off plugin offers Turn on and calls SetPluginEnabled`; `refused shows status_detail with the required Node version and Restart`; `failed shows Restart`; `list shows trust chip, files and recent runs`; `untrusted list offers Review & trust and no file opens`; `trust view shows folder, hash, files, consequences and both hints`; `y and Enter confirm; n and Escape cancel; Enter on Cancel cancels`; `confirm changed re-describes`; `symlink error offers no confirm`; `type_errors render file:line:col`; `start sends coerced input and source_hash`; `run.start changed rebuilds`; `source_changed chip on run rows and run header`; `cancel run asks through the choice dialog and keeps running on Escape`; `retry shows only for failed and cancelled`; `engine text renders as plain text` (a prompt containing `<img onerror>` and `**md**` renders literally); `menu:workflows while the modal is open closes it, from the trust view too`; `Escape in review goes back to the list`.
- `test/dom/workflow-graph.test.tsx`: `every IR node renders once with data-node-id and an aria-label naming its state`; `map template expands to one row per key`; `loop header shows round k of N`.
- `test/dom/workflow-node-detail.test.tsx`: `human answer submits human.answer`; `invalid_answer shows the path message`; `permission options call AnswerPermission with session_id request_id option_id`; `Open session switches to the node session`; `truncated output shows the truncation note` (or `Load more` per Q-B2).
- `test/dom/acp-transcript.test.tsx` (existing permission-card tests keep passing against the extracted component; no new test).
- `test/dom/key-scopes.test.ts` (extend): `workflow-trust is inactive outside the trust view`; `workflow-trust precedes workflows`.
- Existing parity tests cover the new binding: `keymap-parity`, `shortcut-labels-parity`, `bindings.test.ts` (menu fixture), `bridge-harness-parity.test.ts` (no new binding, so unchanged).

**E2E (mock), `test/e2e/workflows.spec.ts`**
- `turn on, list, trust, review, start, live overlay, human answer, permission answer, finished replay`.
- `waiting banner opens the run`.
- `layout: graph pane scrolls inside the xl dialog with no page horizontal scroll; every edge endpoint lies inside its source and target rects; elementFromPoint at each node centre hits that node` — on all three worked examples under `hive-dark` and `hive-light`, at 1280×800 and 900×700. This is the CSS validation (vitest is CSS-blind).
- `every-shortcut.spec.ts`: new fixtures; menu-parity sees `menu:workflows` in `menu_darwin.go`.

**E2E-real, `test/e2e-real/workflows.spec.ts`** (through `hived-ws-bridge`, isolated `HIVE_SOCKET` / `HIVE_STATE_DIR`): enable the builtin; write `.hive/workflows/hello.ts` from the fixture into the isolated project; trust, review, start; answer the human node; see `done` (the check ran `git rev-parse HEAD` in its own worktree); reload and see the same run replayed as `done`. Permission waits are mock-only (spec criterion 6; real `hived` has no fake ACP adapter reachable from a browser test).

**Go**
- `cmd/hivegui/menu_darwin_test.go` `TestMenuDefaultsMatchFixture` (existing) passes with the fixture row.

## Verification

```
./scripts/ci-bootstrap.sh
(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci . && npx vitest run)
(cd cmd/hivegui/frontend && CI=1 npx playwright test workflows every-shortcut)
(cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real -- workflows)      # Node >= 26 on PATH; after B5
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./cmd/hivegui/...
scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast
scripts/check-daemon-contract.sh origin/main HEAD                          # expect: no bump required
scripts/check-changeset.sh
```

Manual (per `docs/verifying-the-gui-by-hand.md`, driven by Playwright against `wails dev` at `http://localhost:34115`, not by eye): open with ⇧⌘R, trust, start, answer; confirm the File › Workflows… item fires. On Linux (WebKitGTK) and the Windows VM (`docs/testing-on-windows.md`): `Ctrl+Shift+R` opens the modal and does not reload the page.

## Decision log

- **2026-10-25:** App code, not a bundled UI plugin. Why: criterion 7, project scope, the plugin API's exact-version rule and the bundled-plugin rule.
- **2026-10-25:** Native graph, not Mermaid. Why: tokens, overlay ids, a11y, jsdom, `mermaid_omitted`; size secondary.
- **2026-10-25:** Trust is a view inside the Workflows modal, not a stacked dialog. Why: no second root, and the scope gates on the view so `y`/`n` never meet a text input.
- **2026-10-25:** Engine text is plain text. Why: untrusted, and it runs in the app's origin.
- **2026-10-25:** No client-side schema validator beyond native constraints. Why: the engine validates authoritatively; a second validator would drift.
- **2026-10-25, user decisions.** Surface: one `xl` blocking modal. Graph: native React. Command: `workflows` on ⇧⌘R / Ctrl+Shift+R, File › Workflows…. Trust view opens focused on **Trust** (see Q-C1). Waits: banner plus desktop notification. Replay: no scrubber. A-2, A-3, B-1, B-2, B-3 approved; A-1 rejected (no `req_id`; `plugin_not_running` rejects all pending).
- **2026-10-25:** Permission-wait notification tagged with the node's `session_id`; human wait `wf:<run_id>:<key>`; only live events notify.
- **2026-11-08:** Re-grounded on `78ce3ace`. A-2/A-3 are merged; the transport reuses `plugins.ts` instead of adding a `plugin:message` subscription. `plugin_not_running` is claimed (no `req_id` routing, consistent with A-1). B-4 is decided (clean worktree at the pinned base), so the e2e-real fixture is human → check.
- **2026-11-08:** Import `IR`/`Cond` types and `condLabel` from `plugins/workflows/` instead of copying them and IR fixtures. Why: one source, no regeneration step; precedent `whats-new.ts` → `site/features.json`; a tsc 7.0.2 probe compiles with `allowImportingTsExtensions`. Cost: one tsconfig flag and one `fs.allow` entry. Rejected: copied types + `emit.ts` JSON fixtures (drift, a manual regen step).
- **2026-11-08:** Extract `PermissionCard` from `AcpTranscript.tsx` and reuse it. Rejected: a second permission UI in NodeDetail.
- **2026-11-08:** Graph evidence refreshed: Mermaid 12.3.0 flowchart path 218 KB gzip; xyflow + dagre ~74 KB gzip; the app has no CSP, which makes Mermaid's sanitizer the only line for untrusted labels. Native stays; xyflow is the recorded fallback.

## Progress

- **2026-10-25:** First research and plan on `feature/530-workflow-gui`; user decisions folded in.
- **2026-11-08:** Rebased onto fresh `origin/main` (`78ce3ace`) as `feature/530-gui`; spec carried (stage `PLAN`, SC-1..SC-7). Plan re-grounded, mockups and Dependencies on A and B added, graph evidence re-measured, PR split reduced to two. Plan-reviewed (Review log). Awaiting user approval.

## Open questions

Each blocks the approval gate; none blocks C's mock-layer work.

- **Q-C1 — trust confirm style vs focus (user).** The brief asks for a destructive-style confirm; the 2026-10-25 decision put initial focus on **Trust**, while the choice dialog's convention is that the safe choice takes focus. Options: (a) keep focus on Trust and style it `kind="danger"`; (b) focus **Cancel**, Trust `kind="danger"` (an accidental Enter backs out; `y` still confirms); (c) keep focus on Trust, `kind="primary"`. Recommendation: (b) — trusting runs code, so the stray-Enter path should be the safe one, matching `choice-dialog.ts`. Changes one dom test's expectation only.
- **Q-C2 — PR split (user).** Recommendation: two PRs (C1, C2 above), merged back-to-back after B5. Alternative: one PR. Changes changeset and branch count, not the file list.
- **Q-B1 — `run.get` paging (B, before C2).** See Dependencies. Changes C's transport and one test.
- **Q-B2 — `node.get` output paging (B, before C2).** See Dependencies. Changes NodeDetail and one test.

### Risks

- **Cross-tree type-check.** The probe compiled `dsl.ts`, `schema.ts` and `mermaid.ts` under frontend options; `tools/examples.ts` and `sdk/library.ts` (pulled in by the tests) were not probed. If they fail under `lib: ES2022`, the fallback is to import only types + `condLabel` in `src/` and keep the examples import in tests only, where vitest needs no tsconfig change — first step of C1 is `npm run typecheck` with the import in place.
- **Merge order.** C is mergeable only after A PR3 and B5 (Dependencies). Building against the mock contract earlier risks rework if B2 changes a reply shape; Q-B1 and Q-B2 are the two known ones.
- **Ctrl+Shift+R on Linux WebKitGTK** is unverified (WebView2's browser accelerators are off on Windows). Manual check in Verification.
- **Waiting scan cost.** One `run.get` per waiting run at boot; bounded by `runs.list` limit 50.

## Review log

<!-- /hs-feature-plan-review appends here. -->
- **2026-11-08** — plan-review (inline, by the planning agent; the three dimensions were reviewed in sequence, not independently — the coordinator's Opus pass is the independent one).
  - Grounding: every path under Files to change exists (`vite.config.js`, not `vite.config.*`); New files do not exist; `Notify`, `AnswerPermission`, `SetPluginEnabled` exist in both test bridges, so `bridge-harness-parity` needs nothing; `TestMenuDefaultsMatchFixture` at `menu_darwin_test.go:266`; test files `key-scopes.test.ts`, `acp-transcript.test.tsx`, `keymap-parity.test.ts`, `shortcut-labels-parity.test.ts`, `bindings.test.ts` exist. `plugin_not_running` carries no `req_id` (`pluginmsg.go:217`), so "route by req_id" from the 2026-10-25 draft became a claim function. Form fields are CSS rules, not a component; wording fixed.
  - Gaps: `bell-click` returns early on an unknown session, so the `wf:` branch is placed first; `ModalId` moved to C1 (the modal ships there); Escape-goes-back via `onEscape` added with a test; `run.start changed`, `symlink`, `retry visibility` and plain-text-injection tests added; Linux/Windows `Ctrl+Shift+R` manual check added.
  - YAGNI: dropped the `workflow-run.cancel` command (a button suffices; nothing binds it); dropped copied IR fixtures and the copied `condLabel` in favour of importing B's source; dropped the third PR.
  - Open questions left for the user: Q-C1, Q-C2; for B: Q-B1, Q-B2. Not handoff-ready until they are answered.

## PR convergence ledger

<!-- /hs-review-loop appends one line per iteration here (append-only). -->

## Gate verdict

<!-- /hs-merge-gate appends one line per run here (append-only). -->
