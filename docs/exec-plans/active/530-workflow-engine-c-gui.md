# Workflow engine C: GUI

- **Spec:** [docs/product-specs/530-workflow-engine-c-gui-workflow-list-trust-dialog-ru.md](../../product-specs/530-workflow-engine-c-gui-workflow-list-trust-dialog-ru.md)
- **Design:** [workflow-engine.md](../../design-docs/workflow-engine.md) (The overlay, Security), [acp-workflows.md](../../design-docs/acp-workflows.md)
- **Depends on:**
  - A (#528), planned at `origin/feature/528-daemon-sdk-prereqs:docs/exec-plans/active/528-workflow-engine-a-daemon-sdk-prereqs.md`;
  - B (#529), planned at `origin/feature/529-engine-core:docs/exec-plans/active/529-workflow-engine-b-headless-engine-core.md`, section "Contract for C".
- **Issue:** #530
- **Branch:** `feature/530-workflow-gui` (research and plan). Each phase PR gets its own branch.
- **Status:** active. Drafted, but **not handoff-ready**: Open questions is non-empty and the required changes to A and B are unapproved.

## Summary

C is the GUI for the workflow engine. In app code under `cmd/hivegui/frontend/`, it adds a project-scoped **Workflows** modal:
- the workflow list, with the plugin's own state ("off", "requires Node 26.0.0", "failed");
- the trust view;
- review and start, with the graph and an input form generated from the schema;
- a run view, with the live or replayed overlay, node detail, human answers and permission answers.

Every byte goes over A's `PLUGIN_MESSAGE` to B's `workflows` plugin. The graph is rendered natively in React, not with Mermaid (see Decisions). C adds no frame, no daemon code and no DaemonContract bump.

## Research

Verified against `origin/main` at `0bf4e725` on 2026-10-25. Line numbers are from that commit.

### Relevant code

| Claim | Where | Consequence for C |
|---|---|---|
| UI plugin surfaces are per **session**: `sessionView.modal/panel/banner`, `badge`, `commands`, `settings` | `docs/plugins.md:395-428` | There is no project-scoped surface. Workflows belong to a project. |
| UI plugin commands only get `keys: {key, shift?}` with ⌘ forced, and appear only in the palette and the help overlay | `docs/plugins.md:410-417` | They can't meet criterion 7: no `commands.ts` entry, no `MENU_COMMANDS`, no `menu_darwin.go` item, no accelerators fixture, no README row. |
| The UI `hive` object has no way to send to a plugin | `docs/plugins.md:371-393`. A adds only Node-side `hive.postMessage` (A plan, PR2 "SDK"). | A UI plugin would need a plugin API change. A plugin loads only on an exact `api_version` match (`docs/plugins.md:23-27`), so that change breaks every third-party plugin. |
| A bundled plugin "uses nothing a third-party plugin cannot" | `docs/plugins.md:117-121` | A bundled UI plugin with private powers is ruled out. |
| B's manifest is headless (`no ui`) | B plan, Shape | C is app code that talks to B's process. |
| Every app chord lives in `DEFAULT_APP_BINDINGS` | `src/lib/bindings.ts:62-180` | Off macOS, plain `Ctrl+letter` is a terminal control byte. `toggle-activity` shows the `{mac, other}` pattern (`:89-95`). `RESERVED_CHORDS` is at `:182`. |
| Modal scopes and their precedence | `src/app/key-scopes.ts:65-82` (`modal()`), `KEY_SCOPES` at `:358` | Two new scopes: `workflow-trust`, placed before `workflows`. |
| Every scope needs a sweep fixture | `test/e2e/every-shortcut.spec.ts:61` (`FIXTURES`), `:49` (`UNBOUND_SCOPES`), `:244` (`NOT_IN_MENU`) | `menu:workflows` must be a real menu item, not a `NOT_IN_MENU` entry. |
| Menu default accelerators are pinned on both sides | `cmd/hivegui/menu_darwin.go:40-118`, `cmd/hivegui/testdata/menu-default-accelerators.json`, `menu_darwin_test.go:259-272` | Add `"workflows"` to the fixture and the File item. |
| Commands, palette and menu table | `src/app/commands.ts` (`PALETTE`, `MENU_COMMANDS` at `:387`) | Add one command id, `workflows`. |
| Help rows | `src/lib/shortcuts.ts:306` (the Worktrees row is the template) | One row. |
| ModalShell renders the footer hints, which every overlay must show | `src/components/modals/ModalShell.tsx:53-54`; precedent `Worktrees.tsx:132-135` | Hints use the `[key]` / `(key)` format. |
| Dialog widths top out at 720px | `src/theme/components/dialog.css:30-32` | Too narrow for a graph plus node detail. Add `data-size='xl'` (see open question 1). |
| The choice dialog is the confirm overlay; its first choice is the safe one | `src/app/modals/choice-dialog.ts:94-108` | Cancelling a run goes through it. It shows **no key hints**, which is an existing AGENTS.md gap, noted and not fixed here. |
| ACP permission card | `src/components/acp/AcpTranscript.tsx:215-250` (`AnswerPermission(sessionId, request_id, option_id)`) | Permission waits reuse the existing `AnswerPermission` binding (`src/bridge.ts`). The node's session also shows `waiting_permission` in the sidebar already. |
| Nonce correlation pattern | `src/app/plugins.ts:30-35` (`newNonce`), `:95-105` (`relistPluginsIfWanted` after a reconnect) | Reused for requests, and for resync after a reconnect. |
| Plugin state reaches the store | `src/app/events.ts:767-788` (`plugin:list`, `plugin:event`); `PluginInfo.status / status_detail` at `src/app/state.ts:197-219` | The "requires Node" text is A PR3's `refused` `status_detail`. |
| Control frames become events by name | `internal/wire/client.go:159-170` `ControlEventName`; used by `cmd/hived-ws-bridge/main.go:755` | Mapping `FramePluginMessage → "plugin:message"` gives the GUI and the bridge the event in one place. |
| The ws-bridge dispatches by **Wails method name** | `cmd/hived-ws-bridge/main.go:353-692` (`case "AnswerPermission"`, …); `test/e2e-real/wails-bridge.ts` `call('<Method>')` | A's plan calls the op `send_plugin_message`, which doesn't fit this convention (A-2). |
| Banner slots | `src/store/store.ts:292` (`BannerSlot`), `src/components/Banners.tsx` | One new slot, `workflow`, for "a run is waiting on you". |
| Mock harness | `test/e2e/wails-mock.ts` (2374 lines, `emit()` at `:43`, `window.__hive` pokes); `test/unit/bridge-harness-parity.test.ts` | A scripted fake engine for `SendPluginMessage`. |
| The e2e-real leg runs Node **24** | `.github/workflows/ci.yml:110-113`, `:380-392` | B's floor is 26, so the plugin would be `refused` there. C moves this leg to Node 26. |
| Real `hived` has no fake-ACP hook reachable from e2e-real | `registry.SetACPCommandForTest` is Go-only (B plan) | The e2e-real workflow uses only human and check nodes. |
| IR shape | `scripts/workflow-proto/dsl.ts:53-118`; schema subset in `schema.ts:30-45`; `condLabel` in `mermaid.ts:30-48` | `edges` stay inside one body (`workflow-engine.md:683`), so each body is a DAG and a native layered layout is enough. |
| Frontend dependencies | `cmd/hivegui/frontend/package.json` (React 19, zustand, marked; no graph library) | — |

### Graph rendering evidence (Mermaid vs native)

Measured 2026-10-25 on `mermaid@12.2.0` (latest, published 2026-10-13) with `vite@8.2.2`, the frontend's pinned bundler. Method: a bare `mermaid.render` of a flowchart, with the manifest walked from the entry through the flowchart chunk; scratch files are in `/tmp/mmd-measure`.

| Criterion | Mermaid | Native (React, `hv-*`, tokens) |
|---|---|---|
| Bundle | The flowchart path is **≥ 733 KB raw / 188 KB gzip** across 23 chunks. That is a lower bound: the lazily loaded layout engine isn't counted. The package emits 5.09 MB (93 chunks) in all. Today's app JS is about 1.04 MB / 293 KB gzip (a local build from 2026-10-20), so Mermaid adds at least about 64% to gzip. | About 300 lines, no dependency |
| Theming | "The theming engine will only recognize hex colors and not color names" ([theming](https://mermaid.js.org/config/theming.html), accessed 2026-10-25). It can't use `var(--…)` tokens; it would need hex values resolved per theme and a re-render on every theme change. That works against principle 3 of `docs/design-docs/ui/README.md`. | Tokens directly; presets apply for free |
| Overlay | The overlay would colour generated SVG by derived ids (`flowchart-<mid>-N`). Those ids belong to Mermaid's output, not ours. | `data-node-id` on our own element, and state is a React prop |
| A11y | One SVG with text. Nodes are not focusable and carry no state. | Each node is a `<button>` whose `aria-label` includes kind, agent, state and attempt; Tab reaches it, Enter opens its detail. State is shape + colour + words (principle 5). |
| Testability | jsdom has no `getBBox`, so Mermaid can't render in the dom layer. | Structure in dom tests; geometry in Playwright |
| Contract | B omits `mermaid` when the reply is over 48 KiB (`mermaid_omitted`), so C must render from `ir` either way. | Always from `ir` |

**Decision: native.**

### Prior lessons

None matched (`brain-search 'workflow gui graph overlay plugin message'`, `'mermaid keybinding modal scope'`).

### Conventions card

```
build:  ./build.sh
test:   scripts/test.sh                       # go · unit · dom · e2e
mock:   (cd cmd/hivegui/frontend && CI=1 npx playwright test)
real:   (cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real)
lint:   ./scripts/ci-bootstrap.sh && (cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
ui:     scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast
```

- **TDD.**
  - Pure modules (`lib/workflow-*.ts`, `lib/schema-form.ts`) get unit tests.
  - Components get dom tests.
  - Layout is checked only in real Chromium (Playwright). vitest is CSS-blind.
- **Keybindings Policy.** All five updates apply. Every label goes through `shortcutLabel` / `useShortcutLabel`. Modal-local confirm/cancel keys are the AGENTS.md hard-coded exception.
- **The GUI never opens a PTY.** Everything goes through the Wails bindings (`SendPluginMessage`, `AnswerPermission`, `SetPluginEnabled`).
- **Untrusted text.** Prompts, outputs, agent messages, file names and errors from the engine or the workflow file render as **plain text**: never Markdown, never HTML.
- **Docs.**
  - Each user-visible PR gets a changeset.
  - `site/features.json` gets an entry with `since: "Unreleased"`.
  - README gets the Keybinds row and a short Workflows section.
  - Never edit `CHANGELOG.md` or `docs/product-specs/index.md`.

## Required changes to A

- **A-1: `req_id` on GUI→plugin `PLUGIN_MESSAGE` (A PR2). Optional.**
  - **This reverses a user decision.** A's decision log records that PLUGIN_MESSAGE "has no `req_id`", with "Why: user decision". C does not need it:
    - the plugin-state gate sends only while the plugin is `running`;
    - every request has a timeout;
    - a `control:error` with code `plugin_not_running` rejects **every** pending `workflows` request. C is the only sender to that plugin, so nothing is misattributed.
  - What it would buy: an oversize or bad-topic refusal names its request, instead of surfacing on the generic error line until the timeout.
  - Wording, if approved:
    > A GUI→plugin `PLUGIN_MESSAGE` may carry `req_id` (`json:"req_id,omitempty"`). The daemon echoes it, through the existing `req_id` peek, on any ERROR it raises for that message (`plugin_not_running`, `plugin_message_too_large`, `invalid_topic`). It is not forwarded to the plugin. The binding is `App.SendPluginMessage(pluginID, topic, dataJSON, reqID string) error`. Test: `TestPluginMessage_ErrorEchoesReqID`.
- **A-2: name the bridge op after the binding (A PR2). Blocking.**
  - Wording:
    > `cmd/hived-ws-bridge/main.go` handles `case "SendPluginMessage"`, like every other op (`main.go:353-692`). `test/e2e-real/wails-bridge.ts` and `test/e2e/wails-mock.ts` export `SendPluginMessage`, and `src/bridge.ts` re-exports it, so `bridge-harness-parity.test.ts` holds.
- **A-3: the event name through `ControlEventName` (A PR2). Confirm only.**
  - Wording:
    > `internal/wire/client.go` `ControlEventName` maps `FramePluginMessage` to `"plugin:message"`, so the GUI and `hived-ws-bridge` emit it from one table.

## Required changes to B

- **B-1: an event that ends a permission wait (B3). Blocking.**
  - B says the wait "clears when the permission goes away" (B plan, Agent nodes, item 4), but no `RunEvent` type says so. Without one, the overlay would show "waiting" until the turn ends.
  - Wording:
    > Add `node.resumed` `{reason: "permission" | "human"}`, emitted when a `node.waiting` ends without the node finishing. For a permission wait that is when the transcript's pending permission clears; a human answer ends with `node.output` + `node.finished` in one transaction, so it emits none. Test: `permission wait emits node.resumed after ANSWER_PERMISSION`.
- **B-2: find waiting runs across projects (B2). Blocking for criterion 5 after a restart.**
  - The design says a waiting human node "shows in the GUI after any restart" (`workflow-engine.md:1338`). The GUI must find those runs at boot without one `runs.list` per project.
  - Wording:
    > `runs.list` takes an optional `project_id` (omitted means every project) and an optional `state` filter (`"waiting"`, …). Each run in the reply carries `project_id`.
- **B-3: a seeded tsgo tarball for tests (B4). Recommended, not blocking.**
  - Without it, e2e-real downloads about 10 MB from npm on every fresh state dir. That works in CI, but it is a flake source.
  - Wording:
    > Before fetching, `tsgo.ts` looks for `plugin-data/workflows/tsgo/cache/typescript-<platform>-7.0.2.tgz` and verifies it against the same pinned integrity. It adds no new trust, because a seeded tarball passes the same check as a download. The e2e-real `globalSetup` fills it with `npm pack @typescript/typescript-<platform>@7.0.2`.
- **B-4: where a check runs with no agent upstream (B2). Clarification.**
  - The design says "in the worktree of its upstream node" (`workflow-engine.md:60`). C's e2e-real fixture needs a check with no agent upstream (human → check).
  - Wording:
    > A check with no agent upstream runs in a clean worktree of the run's pinned base (`hive/wf/<run>/<key>`).

    The other answer is "it is rejected by `validate`". In that case the e2e-real fixture is human-only.

## Approach

### Where the UI lives: app code

The UI is a first-party modal in the app, not a bundled UI plugin. The evidence is in the Research table:
- criterion 7 needs `commands.ts`, the menu, the accelerators fixture and the README, and plugin commands can't reach any of them;
- plugin surfaces are per session;
- the UI plugin API has no send call, and adding one breaks every plugin through the exact `api_version` match;
- the bundled-plugin rule forbids private powers.

Rejected: a UI half on B's plugin. **Cost of the choice:** the app knows the plugin id `workflows` and B's topics. Both live in this repo, so a contract change moves in lock-step, as wire changes already do.

### Transport (`src/app/workflows.ts`)

- **`request(topic, data, {timeoutMs})`.** It sends a nonce from `newNonce()` (`plugins.ts`, now exported) as `data.nonce`, calls `SendPluginMessage('workflows', topic, JSON.stringify(data))`, and resolves on the `plugin:message` whose topic is `<topic>.reply` and whose `data.nonce` matches.
  - It rejects on `ok:false` with `{code, message}`, or on a timeout.
  - A `control:error` with code `plugin_not_running` rejects every pending request and re-reads the plugin state. If A-1 lands, a `req_id` echo rejects just its own request.
  - Timeouts: 15 s, except `workflow.build` at 180 s. The first build may download tsgo, and the UI says so.
  - Replies to other windows' nonces are ignored. A reply reaches every UI client (A plan, PLUGIN_MESSAGE), so this is the multi-window rule.
- **Envelope reader.** `plugin_id ?? pluginId`, `topic`, `data` (snake_case ?? camelCase at the Go boundary). `data` is plugin JSON relayed verbatim, so it is read as B's snake_case only.
- **Pushes.** `run.event {run_id, events}` is dispatched to the subscribers of that `run_id`. The waiting tracker sees every run.
- **Gaps.** A subscriber keeps `last_seq`. If a batch starts past `last_seq + 1`, it pages `run.events {after_seq: last_seq, limit: 500}` until `more` is false, then applies the buffered batch, dropping `seq` values it already has.
- **Reconnect.** The control-reconnect path in `events.ts` (beside `relistPluginsIfWanted()`, `:219`) calls `resyncWorkflows()`. That re-pages every open run from its `last_seq` and re-runs the waiting scan.
- **Waiting scan.** At boot and after a reconnect, while the plugin is `running`, the app calls `runs.list {state: "waiting"}` (B-2). Live `node.waiting` / `node.resumed` / `node.finished` / `run.finished` events keep the set current.
  - A non-empty set raises the `workflow` banner: "Workflow <name> is waiting on you" with **Open**, which opens the run view.

### Plugin state gate

The modal header reads `pluginInfo('workflows')` from the store.

| Plugin state | Panel shows |
|---|---|
| Absent | "This Hive daemon has no workflow engine. Update Hive." |
| `enabled: false` | "Workflows are off" with **Turn on**, which calls `SetPluginEnabled('workflows', true)`. A builtin needs no trust prompt (`docs/plugins.md:117-119`). |
| `refused` | `status_detail` verbatim, e.g. "requires Node.js 26.0.0 or newer (found v24.11.0 at …)", with A PR3's **Restart**. **This is criterion 1's required version.** |
| `failed` / `crashed` | `status_detail` and **Restart** |
| `running` | The list |

### Views (`src/components/modals/Workflows.tsx`)

The modal is `#workflows` with `data-size='xl'`. It is opened by the `workflows` command for the active project. It has four views; the view state is in the store slice `workflows`.

1. **List.**
   - `workflows.list {project_id}` on open and on **Refresh** (a button; no watcher, as criterion 1 asks).
   - Shows the folder, a trust chip (`none` / `trusted` / `changed` / `invalid` with `invalid_reason`), one row per file (`name` when known), and **Recent runs** from `runs.list {project_id, limit: 20}`.
   - Every run row has a state icon, the workflow name, the time, and a **source changed** chip when `source_changed`.
   - When trust is `none` or `changed`, **Review & trust…** opens view 2. Otherwise clicking a file opens view 3 and a run opens view 4.
2. **Trust.**
   - `workflow.trust.describe` shows the folder path, the short hash, and the file list with sizes.
   - The text says what trusting means:
     - "Building a workflow runs these files as you, inside Node's permission sandbox (defence in depth, not a security boundary)";
     - "Hive asks again whenever any file here changes";
     - "Nothing has been built yet".
   - Footer hints: `(y)/[enter] trust`, `(n)/[esc] cancel`. Buttons: **Trust and continue** and **Cancel**.
   - Confirm sends `workflow.trust.confirm {hash}`. Error `changed` re-describes, with the note "The files changed while you were reading". `symlink` / `too_large` / `missing` show their reason and offer no confirm.
3. **Review & start.**
   - `workflow.build {project_id, file}` shows a spinner. On success it renders `WorkflowGraph(ir)` and `SchemaForm(input_schema)`; **Start run** sends `run.start {project_id, file, input, source_hash}`, then opens view 4.
   - Errors:
     - `type_errors` as a `file:line:col message` list;
     - `errors[]` for `invalid_ir` / `build_failed`;
     - `sandbox_unavailable` with its message;
     - `untrusted` / `changed` go to view 2;
     - `changed` on `run.start` rebuilds and says the file changed since review.
4. **Run.**
   - `run.get {run_id}` provides `ir` and node states. The events are paged with `run.events` from seq 0 (replay), then followed live.
   - The header shows the run state icon and words, the workflow, the started time, and a **source changed since this run started** chip. It has **Cancel run** (confirm overlay, choice dialog, safe first choice "Keep running") and **Retry**, which is shown only for `failed` / `cancelled`.
   - Below it are the overlaid graph and the `NodeDetail` pane for the selected node (or key).

### Graph (`src/lib/workflow-graph.ts`, `src/components/workflows/WorkflowGraph.tsx`)

- **Pure layout.** `layoutBody(body)`:
  - rank 0 for entry nodes, otherwise `1 + max(rank(pred))`;
  - rows by rank, in IR order within a row;
  - edges get a `condLabel` port, a 25-line copy of `mermaid.ts:30-48`.
  - It recurses into `group` / `loop` / `map`.
  - Ponytail: no crossing minimization; add a barycentre pass if real workflows tangle.
- **Rendering.**
  - Each body is a `.hv-wf-body` (CSS grid, one row per rank). A subgraph is a bordered `.hv-wf-sub` box with a header:
    - `loop`: "≤3× until review.verdict = approve", plus the live "iteration 2 / 3";
    - `map`: "for each split.tasks, 4 at once", plus the live "3 of 5 done".
    - The `loop` header likewise reads "at most 3 rounds until review.verdict = approve", without `≤` or `×` (see Glyphs).
  - Nodes are `<button class="hv-wf-node" data-node-id data-state>` showing `local id · agent` / `· check` / `· human`, a state icon and a state word.
  - Edges are drawn in one `aria-hidden` SVG per body. Its paths are computed from `getBoundingClientRect` in `useLayoutEffect`, plus a `ResizeObserver`. Condition labels are `.hv-wf-edge-label` chips at the midpoint.
- **Map copies.** The template node shows the aggregate state and a disclosure, "5 copies". Expanding it lists one row per key (`work/each[2]/do`) with its state icon. Selecting a row selects that key.
- **Loop copies.** Each node shows its latest iteration's state. Its detail lists the per-iteration keys.

### Overlay (`src/lib/workflow-overlay.ts`)

`fold(state, events) → state` is pure, and live and replay use the same function.
- `runState`;
- `nodes[node_id] = {aggregate, instances: {key: {state, attempt, waiting?: {reason, …}, session_id?, error?}}}`;
- `loops[node_id] = {iteration, max_iters}`;
- `maps[node_id] = {count, done}`;
- `lastSeq`.

The aggregate state ranks as `waiting > running > failed > interrupted > pending > cancelled > skipped > done`, so attention wins. `agent.*` and `check.output` events are kept per key, capped at the last 200 per key, for the detail pane only.

### Node detail (`src/components/workflows/NodeDetail.tsx`)

- **Facts.** Kind, key, state, attempt, branch, worktree, error, and conflict (`{branch, files, worktree}` plus the recovery text).
- **Session.** **Open session** calls `switchTo(session_id)`, closing the modal.
- **Output.** `node.get {run_id, key}` on select, with a "truncated" mark.
- **Activity.** The recent `agent.message` and `check.output` text, plain.
- **Human wait.**
  - The prompt is shown as plain text.
  - `SchemaForm(output_schema)` and **Answer** send `human.answer {run_id, key, answer}`.
  - `invalid_answer` shows its path-qualified message inline; `not_waiting` refreshes.
- **Permission wait.**
  - The title, plus one button per `options[]` (`name`, with a `kind` tone), calling `AnswerPermission(session_id, request_id, option_id)`.
  - `permission_stale` is ignored, as in `AcpTranscript.tsx:243-249`, and **Open session** is offered.

### Schema form (`src/lib/schema-form.ts`, `src/components/workflows/SchemaForm.tsx`)

| Schema | Field |
|---|---|
| `string` | `<input>` |
| `enum` | radio group (≤ 4 values) or `<select>` |
| `number` | `type=number` |
| `boolean` | checkbox |
| `array` of scalars | textarea, one value per line |
| `array` of objects | JSON textarea, parsed on submit |
| `object` | `<fieldset>`, recursive |

- `required` comes from the schema.
- `toValue(schema, raw)` coerces the raw values. Client checks stop at native required/number validity; the engine is authoritative (`invalid_answer`, and `run.start` errors).

### Keys and commands

- **Command `workflows`.**
  - Title "Workflows…", in `PALETTE`. It **toggles** the modal for the active project, from any view, trust included.
    - On macOS the native ⇧⌘R accelerator takes the key before the webview, so the scope's `mirror` close never fires there. `menu:workflows` runs the command unconditionally (`commands.ts:435-439`).
    - The command itself must therefore close an open modal, as `openSettings` does for ⌘, (`modals/settings.ts:60-68`).
  - Default chord `{mac: 'Mod+Shift+R', other: 'Ctrl+Shift+R'}` (open question 2). It is free: it is not in `DEFAULT_APP_BINDINGS` or `RESERVED_CHORDS`, and `session-notes` uses ⇧⌘O.
  - It has a help row in `shortcuts.ts`, beside the Worktrees row (`:306`).
  - `menu:workflows` is a File menu item, "Workflows…", with `accel("workflows", keys.Combo("r", keys.ShiftKey, keys.CmdOrCtrlKey))`. The fixture gets `"workflows": "cmdorctrl+shift+r"`.
  - README gets a Keybinds row.
- **Scope `workflow-trust`.**
  - Exclusive, trap `workflows`. It is active only while the modal is open and the view is `trust`, which has no text input, so a bare `y`/`n` can't fire while the user types.
  - Bindings:
    - `Shift?+Y` and `Enter` → `workflow-trust.confirm`;
    - `Shift?+N` and `Any+Escape` → `workflow-trust.cancel`.
  - `Enter` on a focused **Cancel** cancels (the confirm command reads `document.activeElement`). Initial focus is on **Trust** (open question 3).
- **Scope `workflows`.**
  - Exclusive, trap `workflows`. Bindings: `Any+Escape` → `workflows.close` (in the run or review view it goes back one view first), and `mirror('workflows', 'workflows.close')`.
  - No bare letters, because the views contain text inputs.
- **Labels.**
  - The modal title suffix and the banner's Open hint use `useShortcutLabel('workflows')`.
  - Modal-local hints are the dialog confirm/cancel exception and follow the `Worktrees.tsx:132` pattern.
- **Sweep fixtures** (`every-shortcut.spec.ts`).
  - `FIXTURES` (`:61`): `workflows: (page) => run(page, 'workflows')`, plus a `workflow-trust` fixture that opens the modal on a mock project whose trust is `none` and clicks **Review & trust…**.
  - `OPENS` (`:128`): `workflows: 'workflows'`.
  - `workflows.close` and `workflow-trust.cancel` match the `.close` / `.cancel` closers rule in `expectEffect` (`:144`), so no other table needs an entry.
  - `workflow-trust.confirm` is dispatch-only there. Its effect is proven in the dom test.
- **Glyphs.** `×` is on `ui-lint.sh`'s glyph denylist (`scripts/ui-lint.sh:143`), and that rule scans `src/components`.
  - Headers say "at most 3 rounds", not "≤3×".
  - Every label is built in `src/lib/workflow-graph.ts`, which is outside the glyph rule's targets (`:85`).

### UI system

- **Tokens.** Use existing tokens only; add one if a role is missing (`tokens.md` + `tokens.css`).
- **Icons.** Add `state-pending` (hollow circle, `--fg-muted`), `state-skipped` (dash, `--fg-muted`) and `state-interrupted` (broken ring, `--state-attention`) to the sprite and `icons.md`.
- **State mapping.**

  | Node state | Icon |
  |---|---|
  | `running` | `state-working` |
  | `waiting` (human) | `state-attention` |
  | `waiting` (permission) | `state-waiting-permission` |
  | `done` | `hv-check` |
  | `failed` | `state-failed` |
  | `cancelled` | `state-exited` |
  | `pending`, `skipped`, `interrupted` | the new icons |

- **Dialog size.** `dialog.css` gains `[data-size='xl'] { max-width: min(1120px, 94vw) }`, with `ModalShell`'s `size` union extended. The graph pane scrolls inside itself (`overflow: auto`), never the page.
- **Docs.** The `docs/design-docs/ui/README.md` decisions table gains a row: "Workflow graph (2026-10-25, spec 530): native React layered layout, not Mermaid". `components.md` documents `.hv-wf-*`.

### Phases

**DaemonContract: none for any phase.**
- C touches `cmd/hivegui/**` (including the ungated Go `menu_darwin.go`), `.github/workflows/ci.yml` and docs.
- `scripts/check-daemon-contract.sh origin/main HEAD` proves it in each PR.

**Merge order.**
- Every phase needs A PR2 merged (with A-2), and B5 merged so the plugin exists.
- C phases merge **after B5**. Otherwise a release between them shows a Workflows command whose panel says "no workflow engine".

| Phase | Scope | Changeset |
|---|---|---|
| **C1** | Transport, plugin gate, the list, the trust view, the command, the two scopes, the menu, the help row, the README row, the mock engine | `added` / `minor`: "Workflows panel lists a project's workflows and asks before trusting them" |
| **C2** | Graph layout and rendering, schema form, build, review and start | `added` / `minor` |
| **C3** | Run view: overlay, replay, gap and reconnect resync, node detail, human and permission answers, cancel and retry, the waiting banner. e2e-real, the CI Node 26 move, the `site/features.json` entry, the README Workflows section, the design-system docs. | `added` / `minor` |

### Files to change

- `cmd/hivegui/frontend/src/lib/bindings.ts`: the `workflows` chord (C1).
- `cmd/hivegui/frontend/src/app/commands.ts`:
  - C1: `workflows`, `workflows.close`, `workflow-trust.confirm` and `workflow-trust.cancel`, the `PALETTE` row and `MENU_COMMANDS['menu:workflows']`;
  - C3: `workflow-run.cancel`.
- `cmd/hivegui/frontend/src/app/key-scopes.ts`: the `workflowTrust` and `workflows` scopes, placed before `whatsNew` in `KEY_SCOPES` (C1).
- `cmd/hivegui/frontend/src/lib/shortcuts.ts`: the help row (C1).
- `cmd/hivegui/frontend/src/store/store.ts`: `ModalId` gets `'workflows'`, `BannerSlot` gets `'workflow'`, and a new `workflows` slice: view, projectId, list, trust, build, the open run's folded state, the waiting set (C1–C3).
- `cmd/hivegui/frontend/index.html`: the `#workflows` dialog root (C1).
- `cmd/hivegui/frontend/src/components/App.tsx`: the portal (C1).
- `cmd/hivegui/frontend/src/components/Banners.tsx`: the `workflow` banner (C3).
- `cmd/hivegui/frontend/src/components/modals/ModalShell.tsx`: `size` gains `'xl'` (C1).
- `cmd/hivegui/frontend/src/theme/components/dialog.css`: `xl` (C1).
- `cmd/hivegui/frontend/src/app/events.ts`:
  - C1: subscribe to `plugin:message`; route a matching `control:error` `req_id` to `workflows.ts`; call the reconnect resync;
  - C3: the waiting scan on boot.
- `cmd/hivegui/frontend/src/app/plugins.ts`: export `newNonce` (C1).
- `cmd/hivegui/frontend/src/lib/icons.svg`: three state icons (C2).
- `cmd/hivegui/menu_darwin.go`: the File › "Workflows…" item (C1).
- `cmd/hivegui/testdata/menu-default-accelerators.json`: `"workflows": "cmdorctrl+shift+r"` (C1).
- `cmd/hivegui/frontend/test/e2e/wails-mock.ts`:
  - C1: the scripted engine behind `SendPluginMessage` (topics answered from `window.__hive.seedWorkflows(spec)`), the `workflows` builtin in the plugin list, and `__hive.pushRunEvents(run_id, events)`;
  - C2, C3: further topics.
- `cmd/hivegui/frontend/test/e2e/every-shortcut.spec.ts`: the `workflows` and `workflow-trust` `FIXTURES`, and `OPENS.workflows` (C1).
- `.github/workflows/ci.yml`: the e2e-real leg's `node-version` becomes `'26'`, or the leg moves onto A PR3's Node 26 leg (C3).
- `cmd/hivegui/frontend/test/e2e-real/globalSetup.mjs`: seed tsgo if B-3 lands (C3).
- `README.md`: the Keybinds row (C1) and a "Workflows" paragraph under Plugins (C3).
- `site/features.json`: "Run multi-agent workflows", `status: shipped`, `since: "Unreleased"` (C3).
- `docs/design-docs/ui/README.md`, `components.md`, `icons.md` (C2, C3).
- `docs/plugins.md`: one line under Workflows (B5's section) saying the app's Workflows panel is the client of these topics (C3).

### New files

- `cmd/hivegui/frontend/src/app/workflows.ts`: transport, subscriptions, resync, the waiting tracker, open/close.
- `cmd/hivegui/frontend/src/lib/workflow-types.ts`: B's IR subset, the reply types and `RunEvent`.
- `cmd/hivegui/frontend/src/lib/workflow-graph.ts`: pure layout and `condLabel`.
- `cmd/hivegui/frontend/src/lib/workflow-overlay.ts`: the pure fold.
- `cmd/hivegui/frontend/src/lib/schema-form.ts`: pure field descriptors and coercion.
- `cmd/hivegui/frontend/src/components/modals/Workflows.tsx`: the modal and its views.
- `cmd/hivegui/frontend/src/components/workflows/WorkflowGraph.tsx`, `NodeDetail.tsx`, `SchemaForm.tsx`.
- `cmd/hivegui/frontend/src/theme/components/workflows.css`.
- `cmd/hivegui/frontend/test/unit/fixtures/workflows/{reviewLoop,fanOutThenVerify,planImplementVerify}.ir.json`.
  - Copied from B's `tools/emit.ts` output.
  - `workflow-graph.test.ts` asserts they parse against `workflow-types.ts`. When B changes the IR, `emit.ts` regenerates them.
- `.changesets/530-workflows-list-trust.md`, `530-workflows-review-start.md`, `530-workflows-run-overlay.md`.
- The tests below.

### Tests

**Unit**
- `test/unit/workflow-graph.test.ts`:
  - `ranks entry nodes 0 and joins at 1+max`
  - `edges never leave their body`
  - `condLabel matches the prototype for eq ne in truthy lt gt all any not`
  - `the three worked examples lay out without throwing and keep IR order within a rank`
- `test/unit/workflow-overlay.test.ts`:
  - `node.* events drive instance state`
  - `map copies aggregate under the template as k of n done`
  - `loop.iteration sets iteration of max_iters`
  - `permission node.waiting then node.resumed clears the wait`
  - `human node.waiting carries prompt and output_schema`
  - `aggregate ranks waiting over running over failed`
  - `replay of all events equals incremental live fold`
  - `events of another run_id are ignored`
  - `firstGap reports the missing seq`
- `test/unit/schema-form.test.ts`:
  - `descriptors for string enum number boolean array object`
  - `coerces numbers and one-per-line arrays`
  - `array of objects parses JSON and reports a parse error`
  - `required follows the schema`

**DOM**
- `test/dom/workflows-transport.test.ts`:
  - `reply settles only its own nonce`
  - `a reply for another window is ignored`
  - `timeout rejects`
  - `plugin_not_running control:error rejects every pending request`
  - (only if A-1 lands) `control:error with matching req_id rejects only that request`
  - `gap triggers run.events paging and dedupes seq`
  - `reconnect re-pages open runs from last_seq`
- `test/dom/workflows-modal.test.tsx`:
  - `absent plugin says update Hive`
  - `off plugin offers Turn on and calls SetPluginEnabled`
  - `refused shows status_detail with the required Node version`
  - `failed shows Restart`
  - `list shows trust chip and files`
  - `trust view shows folder, hash, files, consequences and both hints`
  - `y and Enter confirm; n and Escape cancel; Enter on Cancel cancels`
  - `confirm changed re-describes`
  - `type_errors render file:line:col`
  - `start sends coerced input and source_hash`
  - `source_changed chip on run rows and run header`
  - `cancel run asks through the choice dialog and keeps running on Escape`
  - `engine text renders as plain text`
- `test/dom/workflow-graph.test.tsx`:
  - `every IR node renders once with data-node-id and an aria-label naming its state`
  - `map template expands to one row per key`
  - `loop header shows iteration`
- `test/dom/workflow-node-detail.test.tsx`:
  - `human answer submits human.answer`
  - `invalid_answer shows the path message`
  - `permission options call AnswerPermission with session_id request_id option_id`
  - `Open session switches to the node session`
- `test/dom/workflows-modal.test.tsx` (also): `menu:workflows while the modal is open closes it, from the trust view too`.
- `test/dom/key-scopes.test.ts` (extend):
  - `workflow-trust is inactive outside the trust view`
  - `workflow-trust precedes workflows`
- Existing parity tests cover the new binding: `keymap-parity`, `shortcut-labels-parity` and `bindings.test.ts` (menu fixture).

**E2E (mock), `test/e2e/workflows.spec.ts`**
- `list, trust, review, start, live overlay, human answer, permission answer, finished replay`.
- `waiting banner opens the run`.
- `layout: graph pane scrolls inside the xl dialog with no page horizontal scroll; every edge path endpoint lies inside its source and target rects; elementFromPoint at each node centre hits that node`. It runs on all three worked examples under `hive-dark` and `hive-light`, in real Chromium. This is the CSS validation, since vitest is CSS-blind.
- `every-shortcut.spec.ts`: the new FIXTURES; the menu-parity test sees `menu:workflows` in `menu_darwin.go`.

**E2E-real, `test/e2e-real/workflows.spec.ts`**
- Through `hived-ws-bridge` with A's `PLUGIN_MESSAGE`:
  1. Enable the builtin.
  2. Write `.hive/workflows/hello.ts` (human `approve` → check `["git","rev-parse","HEAD"]`, or human-only per B-4) in the isolated project.
  3. Trust, review and start it.
  4. Answer the human node and see `done`.
  5. Reload the page and see the same run replayed as `done`.
- It needs Node 26 on the leg (the CI change) and `HIVE_SOCKET` / `HIVE_STATE_DIR` isolation, which the harness already provides.

**Go**
- `cmd/hivegui/menu_darwin_test.go` `TestMenuDefaultsMatchFixture` (existing): passes with the fixture row.

## Verification

```
./scripts/ci-bootstrap.sh
(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci . && npx vitest run)
(cd cmd/hivegui/frontend && CI=1 npx playwright test workflows every-shortcut)
(cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real -- workflows)      # Node >= 26 on PATH
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./cmd/hivegui/...
scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast
scripts/check-daemon-contract.sh origin/main HEAD                          # expect: no bump required
scripts/check-changeset.sh
```

## Spec corrections (proposed; not yet applied to 530)

- **SC-1, criterion 1.** "No usable Node" is the plugin's `refused` state with A PR3's `status_detail`, not a topic reply. Proposed: "the panel shows the plugin's state: off (with Turn on), refused with the required Node version, failed (with Restart)". The plugin ships **disabled** (B5), which the spec doesn't mention.
- **SC-2, criterion 4.** "Mermaid or equivalent" is decided: native React (see the evidence above).
- **SC-3, criterion 6.** e2e-real can't produce a permission wait: real `hived` has no fake adapter reachable from a browser test. Permission waits are covered at the mock layer here, and daemon-side by B's `TestPermission_BecomesNodeWaiting`. They also depend on B-1.
- **SC-4, criterion 7.** Modal-local `y` / `Enter` / `n` / `Esc` are the AGENTS.md "dialog confirm/cancel" exception. They are not rebindable app commands and get no README row. Only `workflows` takes all five Keybindings Policy updates.
- **SC-5, criterion 8.** "All new frames work under the lock-step wire rule" is A's obligation. C adds no frame. Proposed: "C's e2e-real run exercises A's `PLUGIN_MESSAGE` through `hived-ws-bridge`". The e2e-real workflow uses human and check nodes only.
- **SC-6, criterion 4.** The design's "a run can be scrubbed after the fact" (`workflow-engine.md:1396`) isn't in the criteria. Replay shows the final state; a scrubber is open question 5.

## Decision log

- **2026-10-25:** App code, not a bundled UI plugin. Why: criterion 7, project scope, the plugin API's exact-version rule and the bundled-plugin rule (Research).
- **2026-10-25:** Native graph, not Mermaid. Why: tokens, overlay ids, a11y, jsdom and `mermaid_omitted`; size is secondary (≥ 188 KB gzip, a lower bound).
- **2026-10-25:** Trust is a view inside the Workflows modal, not a stacked dialog. Why: no second root or z-index deviation, and the scope can be gated on the view, so `y`/`n` never meet a text input.
- **2026-10-25:** Engine text is plain text. Why: it is untrusted (agent outputs, workflow files) and runs in the app's origin.
- **2026-10-25:** No client-side schema validator beyond native constraints. Why: the engine validates authoritatively and returns path-qualified errors; a second validator would drift.
- **2026-10-25:** IR fixtures are copied JSON, regenerated with B's `emit.ts`. Why: the frontend doesn't import plugin source across trees.

## Progress

- **2026-10-25:** Research complete. Plan drafted against A at `origin/feature/528-daemon-sdk-prereqs` and B at `origin/feature/529-engine-core`. Awaiting the user's answers and approval of A-2, B-1 and B-2. A-1 and B-3 are optional; B-4 is a clarification.
- **2026-10-25:** Revised after review:
  - A-1 is now optional, because it would reverse a user decision;
  - the menu path toggles the modal;
  - the sweep's `OPENS` entry is added;
  - `×` is avoided because it is on the glyph denylist.

## Open questions

UX and design first.

1. **Surface.** One exclusive `xl` modal (recommended) blocks the terminals while someone watches a live run. It ships with the least new chrome, and agent nodes are ordinary ACP sessions in the sidebar anyway. The alternative is a modal for list/trust/start plus a **non-modal run view**: a grid tile or main-area view, like the ACP tile. That costs new layout code and its own scope. Which?
2. **Default chord and menu.** Is ⇧⌘R / Ctrl+Shift+R and File › "Workflows…" right?
3. **Trust Enter semantics.** With initial focus on **Trust**, a stray Enter trusts, but the spec's `[enter] trust` hint is true. Recommended: initial focus on Trust, and Enter on a focused Cancel cancels. The alternative, focus on Cancel, makes the hint wrong until the user tabs.
4. **Human-wait attention.** Recommended: the `workflow` banner. Should a desktop notification (`Notify`) also fire when a node starts waiting?
5. **Replay scrubber.** Should C include one? Recommended no: final-state replay only, scrubber deferred to monitoring (follow-up 3).
6. **Approve A-2, B-1, B-2** as worded, so the A and B plans can absorb them.
   - A-1 (`req_id` on PLUGIN_MESSAGE) would reverse your earlier decision. C works without it. Do you want to reverse it?
   - Should B-3 (tsgo seed) be required or optional?
