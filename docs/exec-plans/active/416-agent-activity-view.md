# Agent activity view: see the plan and the tools, not just the state

- **Spec:** [docs/product-specs/416-agent-activity-view.md](../../product-specs/416-agent-activity-view.md)
- **Issue:** — (locally allocated number; **not** a GitHub issue. PR #416 on GitHub is
  `feat: add Alucard and Hex theme presets`, an unrelated merged PR. Never write `Fixes #416`.)
- **Design:** [docs/design-docs/agent-activity.md](../../design-docs/agent-activity.md)
- **Phase:** 3 of 4 (Pi tier; Phase 1 shipped in #417, Phase 2 in #420)
- **PR:** —
- **Branch:** —
- **Mocks:** https://claude.ai/artifact/7RjZw99RbKNV1iS13r2dtb (placement study — pie vs ring)
- **Status:** active

## Summary

Stop discarding the tool name, tool arguments and plan that the Claude hook
tier already delivers (Phase 1; the Pi extension tier sends none of this today
and gains it in Phase 3), keep a bounded per-session ring of
them in the daemon, and render the result in three placements from one component.
The *why* lives in the spec and design doc; this file is the *how*.

## Research

### Conventions card

Commands, verbatim from `AGENTS.md`:

```
build:  ./build.sh
test:   scripts/test.sh          # layers: go · unit · dom · e2e
e2e-real (separate): npm run test:e2e:real
ui lint: scripts/ui-lint.sh
```

Conventions this feature touches:

- **TDD is mandatory** — no behaviour change ships without the test that would have
  caught it. "Boil the lake": golden path *and* edge cases in the same PR.
- **Wire JSON is `snake_case` on the wire, `CamelCase` in Go.** JS readers use
  `snake_case ?? camelCase` at the boundary — but only for fields that have a
  camelCase bridge variant. Daemon-owned fields (`state`, `state_source`,
  `last_prompt`) are single-spelled snake_case, and the new activity fields follow
  *that* pattern.
- **A wire change moves all three clients in lock-step:** `cmd/hivegui/app_*.go`,
  `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient`.
- **`buildinfo.DaemonContract` must be bumped** for a daemon-side behaviour change;
  `scripts/check-daemon-contract.sh` fails CI without it.
- **Keybinding changes update seven surfaces** — see *Keymap surfaces* below.
- **No raw hex, no literal `font-size`/`border-radius` px outside `tokens.css`**;
  `scripts/ui-lint.sh` enforces it. No icon-shaped Unicode outside the SVG sprite.
- **Every user-visible change adds `.changesets/<slug>.md`**; never edit
  `CHANGELOG.md` or `docs/product-specs/index.md` (both generated, both CI-blocked).
- A `bump: minor` feature also adds an entry to `site/features.json` with
  `since: "Unreleased"`.

### Relevant code — Go / daemon

- `cmd/hived/hook.go:139-145` — **the collapse point.** One
  `case "PreToolUse", "PostToolUse", "PostToolUseFailure":` sets
  `ev.Kind = wire.AgentEventPermissionResolved` and never reads `tool_name` /
  `tool_input` out of the payload. This is the single site that splits into
  `tool_start` / `tool_end` (+ `plan` when `tool_name == "TodoWrite"`).
- `cmd/hived/hook.go:94` — `func mapHookPayload(raw []byte) wire.AgentEvent`, the exact
  signature the spec names. Payload is a loose `hookPayload map[string]any`
  (`hook.go:85`), not a fixed struct — so malformed/missing `tool_input` is a live
  case, not a hypothetical.
- `cmd/hived/hook.go:165` — `firstString(p, keys...)` caps at `wire.MaxSummaryLen`
  (512, `internal/wire/control.go:240`). Reusable for capping a derived label;
  label *derivation* (basename / command head / URL host) is new code.
- `cmd/hived/hook.go:180` — `sendHookEvent` dials the socket, sends `HELLO{mode:event}`
  + one `FrameAgentEvent`. New kinds ride this unchanged; no new envelope.
- `cmd/hived/hook_test.go` — table-driven `TestHookMapsEveryEvent` over fixtures in
  `cmd/hived/testdata/hooks/*.json`, one JSON file per case. Existing fixtures
  already include `pre_tool_use.json` / `post_tool_use.json` /
  `post_tool_use_failure.json`. `cmd/hived/hook_integration_test.go` covers the
  socket round-trip — read it before adding integration coverage.
- `internal/agentstate/machine.go` — the whole package is this one file (+ its test).
  `HookStaleAfter = 30s` (`:58`) is the staleness threshold the spec reuses.
  `Kind*` constants at `:65-86`; `Event` struct at `:89` (has only
  `Kind/Source/At/Text` — no room for `tool`, `target`, `call_id`, `ok`, `items`);
  `Snapshot` at `:97`; `Machine` at `:105`; `Apply(ev) bool` at `:269` with an
  existing out-of-order / clock-skew guard every new kind must pass through.
  **No ring or eviction code exists anywhere yet.**
- **Lifecycle — the ring belongs on `agentstate.Machine`.** `Entry.state` is created
  lazily at `internal/registry/registry.go:190`, replaced wholesale on create /
  restart / revive at `:401-407`, and freed with the entry at `:1387` (`kill()`).
  A ring hanging off `Machine` is created, reset and freed at those three existing
  sites for free — which is exactly the spec's "no second place to leak". Hanging it
  off `Entry` as a sibling field instead would require its own reset call.
- **Adding an event kind touches three mirrored places** that `internal/wire/control.go:486-489`
  documents as needing to stay byte-identical: `agentstate.Kind*`
  (`machine.go:65-86`), the `wire.AgentEvent*` consts
  (`control.go:491-499`), and the `wire.AgentEventKinds` allowlist
  (`control.go:507-517`) — plus the `Apply` switch (`machine.go:332-370`) and
  `registry.ApplyAgentEvent` (`registry.go:474-482`), which maps the string 1:1 with
  no translation table.
- `internal/wire/control.go:468-482` — `AgentEvent` struct. Needs new `omitempty`
  fields; all-optional means old and new JSON round-trip harmlessly in both
  directions on the same `FrameAgentEvent`.
- `internal/wire/control.go:152-223` — `SessionInfo`. The existing daemon-owned
  `omitempty` fields (`state`, `state_source`, `last_prompt`, `last_summary` at
  `:198-223`) are the pattern `plan_done` / `plan_total` / `current_tool` follow.
- `internal/wire/frame.go:39-161` — the frame-type registry is this one const block.
  Highest value today is `FrameSetWorktreeLabel = 0x2a`; **next free is `0x2b`**.
  Each new frame also needs a `case` in `FrameType.String()` (`frame.go:163-252`).
- `internal/registry/events.go:21-60` — the broadcast path. `Subscribe()` returns a
  buffered `chan wire.SessionEvent` (cap 64); `broadcast` drops slow listeners.
  `SubscribeProjects` / `SubscribeIdeas` are the parallel precedents to copy.
- `internal/daemon/daemon.go:840-885` — `serveControl`'s fan-out `select`. A new
  activity listener adds a `case` here. Idea events show the scoping idiom at
  `:874-878` (`if restricted && ev.Idea.ProjectID != ownProjectID { continue }`);
  the activity equivalent keys off `hello.SessionID` directly.
- `internal/daemon/daemon.go:1113` — `handleControlFrame`, one big `switch ft`, is
  where a `GET_ACTIVITY` case goes. `FrameListClosed` is the request/response shape
  to copy.
- `internal/daemon/daemon.go:1118-1122` + `:983-987` — the `ModeSession` gate.
  `sessionModeFrames` is a two-entry allowlist; `restricted := hello.Mode == wire.ModeSession`
  (`:791`) is the single boolean threaded everywhere.
- `internal/buildinfo/contract.go:105` — `DaemonContract = 10`, **bumps to 11**.
  Entries 4 (added `ModeEvent`/`FrameAgentEvent`) and 6 (added `ModeSession`) are the
  directly analogous precedents, including the history-paragraph format.

### Relevant code — the three wire clients

- `internal/wire/client.go:117-146` — `controlEvents` map + `ControlEventName()`.
  **One entry here (`FrameActivity: "activity:event"`) buys the broadcast fan-out in
  both hivegui (`app_control.go:450-453`) and ws-bridge (`main.go:545`) at zero
  further cost.** Neither client needs a per-frame branch.
- `cmd/hivegui/app_calls.go:601-622` — `ListIdeas`/`AddIdea` are the ~6-line template
  for a bound `GetActivity(sessionID string) error`. `app_attach.go` needs nothing.
- `cmd/hived-ws-bridge/main.go:319-450` — string-RPC `switch req.Method`; one
  ~6-line `case "GetActivity":`.
- `internal/wire/testclient/client.go:203-204` — one ~3-line request wrapper. Its
  `readLoop` has a `default: c.sendSnapshot(...)` fallthrough (`:566-569`), so a new
  frame is already observable in tests without a readLoop change; a typed
  `AwaitActivity` helper (mirroring `AwaitIdeaEvent` at `:216-238`) is optional
  ergonomics.

### Relevant code — frontend (React 18 + zustand)

- `src/components/SessionRow.tsx` — `<li className="hv-session-row">`. `agentCode()`
  at `:90-92` takes the first two lowercase chars of `session.agent`; it renders at
  `:246-259` as `<span className="hv-session-row__agent">` inside
  `.hv-session-row__meta`, tinted by an `--agent-color` custom property.
- `src/theme/components/session-row.css:3-23` — **the row is `min-height: 40px`, not
  32px.** The spec's "~32px text column" is the grid-row-1 slice the meta cell
  occupies, not the row height. `--font-mono` / `--text-xs` are applied to
  `.hv-session-row__agent` at `:193-204`; `--fg-subtle` (the stale-ring colour) at
  `:44,119,152,206,359`.
- `src/theme/components/session-row.css:301-361` — **density variants.**
  `[data-density='tight']` drops the row to **34px** and `'compact'` to **28px**, and
  both re-lay the grid. A fixed 30px ring cannot fit the compact row. See Open
  questions.
- `src/app/state.ts:23-73` — the `SessionInfo` interface. Dual-spelled fields declare
  both spellings; daemon-owned single-spelled ones are commented as such at `:44-66`.
- `src/lib/session-state.ts:32-58` — `StateCarrier`, a structural echo of
  `SessionInfo` kept import-independent because `app/state.ts` touches `localStorage`
  at import time and must stay loadable from the node unit suite. If activity fields
  feed state resolution they need an echo here, not an import.
- **No side panel or inspector exists anywhere in the frontend.** `src/theme/layout.css:6-25`
  makes `#app` a two-column grid (`var(--sidebar-width,220px) 1fr`) with `#terms`
  in column 2; `#terms.single .term-host` is `position:absolute; inset:6px`
  (`:64-86`). An inspector needs a new grid column or a split inside `#terms.single`.
- `src/components/TileChrome.tsx:1-25` — React **never** owns `.term-host` /
  `.term-body` (live xterm + PTY must not be unmounted or reparented). React portals
  only into `.tile-header` and `.tile-overlays`. A panel has no PTY constraint and can
  be an ordinary React component.
- `src/app/grid-layout.ts:60,135` — `applySingle()` / `applyGridLayout()`, driven from
  `src/components/GridView.tsx:60-71`. Tiles are reparented, never recreated. The
  activity-tile swap is therefore an overlay sibling inside the same host (the
  `.tile-overlays` idiom), not a replacement of the terminal DOM.
- `src/app/focus.ts:164` — `setFocusedTile()` is the sole writer of `.term-focused`
  and re-asserts DOM focus through an 8-frame rAF retry.
- `src/lib/focus.ts:32-54` — pure `decideFocusAction()`. It returns `ACTION_PRESERVE`
  only when `document.activeElement` is a real `<INPUT>`/`<TEXTAREA>`
  (`isRealInput()` at `:52-54`). **A `<button>` is not a real input**, so a focusable
  disclosure triangle in the panel loses focus back to the terminal on the next
  `setFocusedTile()`. See Open questions.
- `src/app/keyboard.ts` — bindings are an if/else chain on `e.key` in the window
  keydown handler (grid toggle at `:456-462`); menu actions map at `:828-829`.
- **Keymap surfaces (seven):** `src/app/keyboard.ts` (the branch);
  `src/lib/shortcuts.ts` (`shortcutGroups()` at `:163-164` feeds the ⌘/ overlay,
  `paletteShortcuts()` at `:244-289` feeds the palette hint);
  `src/components/modals/HelpOverlay.tsx` (pure renderer, no literals);
  `src/main.tsx:~200-220` (palette command registry);
  `cmd/hivegui/menu_darwin.go:113-114` (if menu-exposed); `README.md:244` Keybinds
  table; `.changesets/<slug>.md`.
- `src/theme/tokens.css` — `--fg-subtle:11`, `--accent:12`, `--state-running:18`,
  `--state-attention:19`, `--state-error:22`, `--state-info:27`, `--font-mono:44`,
  `--text-xs:45`. `--state-info`'s own comment says it was picked hue-distinct
  specifically so a panel could show "occupied" vs "needs you" — this UI is what it
  was reserved for.

### Relevant code — Pi extension

- `internal/agent/pi/hive.ts` (243 lines) — default-exported factory
  `function (pi: ExtensionAPI)` at `:77`. Connects per report with a fire-and-forget
  `net.createConnection` over `HIVE_SOCKET`, keyed by `HIVE_SESSION_ID` (`:21-40,
  92-102`); without either env var it is inert (`:78-80`). Frames are hand-encoded —
  `[1B type][4B BE len][JSON]` (`:10-40`) — making this **the only non-Go encoder of
  Hive frames**, kept in sync with `internal/wire/frame.go` by comment alone.
  `truncate()` at `:69-75` caps text at 512 bytes, UTF-8-safe.
- `internal/agent/pi.go:16` — `//go:embed pi/hive.ts`. **There is no build or bundle
  step**: the raw TypeScript is embedded verbatim and written to
  `<stateDir>/pi/hive.ts` by `EnsurePiExtension` (`:31`, atomic temp+rename, 0600,
  content-compare before rewrite). `piSpawnArgs` (`:67`) passes `-e <path>` only when
  the file exists, else Pi silently degrades to the heuristic tier.
- `internal/agent/pi/hive.test.ts` — **`node:test` + `node:assert/strict`, not
  vitest.** No socket mocking: tests bind a real `net.createServer` on a temp Unix
  socket and parse real frames (`collectFrames()` at `:32-69`); handler tests use a
  `handlerPi()` fake that records `pi.on()` registrations. Socket tests are `unixOnly`
  (`:148-152`). The Go side cross-checks the encoder at `internal/agent/pi_test.go:157`.
- Currently subscribed Pi events (`hive.ts:104-195`, asserted at `hive.test.ts:100-115`):
  `session_start`, `input`, `agent_start`, `agent_end`, `agent_settled`,
  `ui_prompt_start`, `ui_prompt_end`, `session_shutdown`. **No tool events today** —
  exactly the gap this spec closes.
- **Pi API verified against the real package**, `@earendil-works/pi-coding-agent`
  `dist/core/extensions/types.d.ts`:
  `ToolExecutionStartEvent{ type, toolCallId, toolName, args }` (`:608-613`) and
  `ToolExecutionEndEvent{ type, toolCallId, toolName, result, isError }` (`:623-629`)
  — the design doc's table is correct field-for-field, including `isError` (not
  `is_error`). `registerTool<TParams,TDetails,TState>(tool: ToolDefinition): void`
  exists at `:944`; `ToolDefinition` (`:344-377`) requires `name`, `label`,
  `description`, `parameters` (TypeBox `TSchema`) and `execute(...)`. The blocking
  `tool_call` / `tool_result` handlers at `:678-771` are real, confirming the design
  doc's warning to stay out of that path. Pi ships no todo tool.
  **Two caveats:** the installed version is **0.85.0**, not the 0.85.1 the design doc
  cites; and the package is **not present in this repo** at all — it lives in a
  separate `pi-devkit` checkout, so `hive.ts` gets no local typecheck against it.
- **No settings mechanism exists for the Pi extension.** Its only configuration is the
  two env vars. The Settings screen's Agents tab
  (`src/components/modals/Settings.tsx` + `cmd/hivegui/app_calls.go:69-73`) edits
  custom *agent launch definitions* (`ID`/`Name`/`Cmd`/`Color`, persisted whole-list to
  `agents.json`), not per-agent behaviour toggles; theme settings go to `localStorage`
  with no Go round-trip. The spec's "todo tool on by default, with a setting to
  disable it" therefore needs a new path end-to-end: a persisted field, a Wails
  binding, a UI control, and a channel `hive.ts` can actually read (a third env var,
  or a config file `EnsurePiExtension` also writes).

### Test harness

- `test/e2e/wails-mock.ts` (1351 lines) — in-memory state seeded with project `p1` /
  session `s1` (`:160-201`); `broadcast()` (~`:210`) emits `session:list` and
  `session:event`. `MockSession = SessionInfo & {...}` (`:22-29`) **intersects the real
  `SessionInfo`**, so the new activity fields become settable on the mock with no
  separate edit. An `ACTIVITY` push adds an `emit('activity:event', ...)` here plus an
  `EventsOn` reader in `src/app/events.ts` (the `session:event` consumer pattern is at
  `events.ts:264,306,456,673,859,925`).
- `test/e2e/hive-global.d.ts:29-53` — `HiveTestApi`; a mock-only activity setter goes here.
- `test/e2e/sidebar-density.spec.ts:33-56` — **the direct template for the spec's
  "ring does not change row height" criterion**: a `rowHeight()` helper doing
  `getBoundingClientRect().height` on the first `.hv-session-row`, asserted with
  `toBeCloseTo(40, 0)`. jsdom cannot do this, which is why the spec calls vitest
  CSS-blind.
- `test/dom/ui-session-row.test.tsx` — renders `<SessionRow>` via RTL over a minimal
  `SessionInfo` fixture. This is the layer for "no ring when `plan_total` is absent",
  not for pixel geometry.
- `playwright.config.js` — boots real `vite dev` with `VITE_WAILS_MOCK=1`. Run local
  Playwright with `CI=1` or it reuses a stale dev server and green means nothing.

### Constraints / dependencies

- `scripts/check-daemon-contract.sh` fails this PR without a `DaemonContract` bump.
- `block-generated-edits` fails the PR if it touches `CHANGELOG.md` or
  `docs/product-specs/index.md`.
- `scripts/ui-lint.sh` rejects raw hex and literal px font-size/border-radius outside
  the token files, and icon-shaped Unicode outside the SVG sprite.
- `hive.ts` has no typecheck and no bundler; every edit to it is verified only by its
  own `node:test` suite and the Go-side encoder cross-check.
- `internal/agentstate.Machine` is deliberately **not** concurrency-safe — the
  registry mutex is the single guard. The ring inherits that contract.

### Prior lessons

`brain-search` over the feature's terms returned no qualifying entries — no prior
lessons matched.

### Phase 3 research (2026-09-16, after #420)

Two Explore workers (Pi wire path; settings path) against `origin/main` at 8a4b761d.

**Pi extension today.** `internal/agent/pi/hive.ts` has no tool events and no
`registerTool`. `encodeFrames(sessionId, kind, text, at)` (:45-59) builds HELLO + exactly
one AGENT_EVENT and cannot carry `tool`/`target`/`call_id`/`ok`/`items`. `truncate()`
(:69-75) is hard-wired to 512 bytes. `post(kind, text)` (:85-102) dials once per event and
stamps `at` at call time. Test fakes `fakePi()`/`handlerPi()` (`hive.test.ts:16-26`) have no
`registerTool`, so the first call throws in every existing test until both grow one;
`collectFrames` is typed `Record<string,string>`; :100-115 pins the exact subscription list.

**Wire shape the daemon already accepts** (Phases 1-2, no daemon change needed):
`wire.AgentEvent` (`internal/wire/control.go:505-551`) — `tool`, `target`, `call_id`,
`ok *bool`, `items []PlanItem`; `agent_id`/`agent_type` must stay unset for Pi (a set
`agent_id` stops the event moving state/plan, `agentstate/machine.go:401`). `PlanItem`
(:555-575) `{id?, text, status, tools?}` — reporter never sends `tools`. Caps (:597-611):
target 120, plan text 200, 100 items, tool name / call_id 128. `applyEventFrame`
(`daemon.go:788-825`) truncates, allows 8 frames per connection. `setPlan`
(`activity.go:328-363`) replaces wholesale and carries tallies forward by ID then text;
`setPlan(nil)` clears, so an empty todo list (dropped by `omitempty`) still clears.
A late wholesale `plan` is dropped by the ordering guard (`activity.go:172-197`);
`tool_end` falls back to the start's tool/target (:295-300).

**Label derivation to port** (`cmd/hived/toollabel.go`): key allowlist in order
`command`→`commandHead`; `file_path`/`path`/`notebook_path`→`baseName`; `url`→`urlHost`.
`baseName` = text after the last `/` or `\` (:216-221). `commandHead` (:88-125): cut at
`; | & \n \r < > ( )`, split whitespace, drop leading `NAME=value`, reject heads with
`` = $ " ' @ : ` ``, append a subcommand only when `isSubcommand` accepts it (:188-209).
`capLabel` 120 bytes rune-safe. No shared vectors — the table is inline Go
(`toollabel_test.go:13-86`). Pi built-in arg keys (`bash`/`powershell` `command`;
`read`/`write`/`edit`/`ls`/`grep`/`find` `path`) all hit the existing allowlist. JS
`new URL().host` drops default ports and lowercases; Go `url.Parse` does not — parity is
close, not exact. Byte vs UTF-16 lengths and `\s` vs `strings.Fields` differ at the edges.

**Nothing keys off tool names** for display (`SessionRow.tsx:251,258` renders
`current_tool` as text); lowercase Pi names show as `bash · npm test`.

**Pi API** (pi-coding-agent 0.85.0 in `~/checkout/pi-devkit`; running pi 0.85.1):
`registerTool(ToolDefinition)` (`types.d.ts:944`); `ToolDefinition` needs `name`, `label`,
`description`, `parameters` (TypeBox schema), `execute(toolCallId, params, signal, onUpdate,
ctx) → {content, details}`. `tool_execution_start` fires **before** the blocking
`tool_call` hook/permission `confirm()`; `tool_execution_end` fires for blocked and aborted
calls too; parallel mode ends in any order (`pi-agent-core/dist/agent-loop.js`).
**TypeBox cannot be value-imported**: Pi's jiti loader aliases `typebox`, but
`node --test` loads `hive.ts` with no node_modules. A plain JSON-Schema object as
`parameters` validates under typebox 1.3.7 `Compile` (checked). Pi's own example todo
extension is also named `todo` — a name clash if the user loads both.

**Settings path — reuse Phase 1's.** `agent-settings.json` (`internal/agent/settings.go:22`)
with pointer-`omitempty` fields defaulting on (:43-62), read at every spawn by
`spawnSettings()` (:118-126). `Def.SpawnEnv` (`agent.go:66-72`) is wired only for Claude
(:144); create (`registry/create.go:128`), revive (`registry.go:1139`) and restart
(`:1256`) all apply it via `applyAgentSpawn` (:790-807), and custom `pi …` agents inherit it
(`custom.go:168-175`). Wails `AgentSettings` maps fields by hand (`app_calls.go:114-135`);
UI checkbox at `Settings.tsx:668-684` (label "Show Claude's plan progress in the sidebar"),
local state, save guarded when load failed (:576-582). Mocks carry the field at
`test/e2e/wails-mock.ts:1048-1056` and `test/e2e-real/wails-bridge.ts:327-334`.
`TestOnlyClaudeHasSpawnEnv` (`settings_test.go:212`) and the `SpawnEnv != nil` check at
:205 break once Pi gets a `SpawnEnv`.

**Tests / CI.** `node --test` runs via Go `TestPiExtensionRunsNodeTests`
(`internal/agent/pi_test.go:300-322`, node 24 in CI). `TestPiExtensionFramesAreValidWireFrames`
(:161-236) expects exactly 2 frames. `TestPiExtensionKindsAreOnTheAllowlist` (:248-295)
scrapes string literals inside `post(...)` — an object literal there would feed its keys
into the allowlist check. Opt-in real probe `cmd/hived/pi_probe_test.go` (`HIVE_PROBE_PI=1`).

**Daemon contract.** `DaemonContract = 12`. `check-daemon-contract.sh` watches
`internal/{wire,daemon,session,registry}` and `cmd/hived` only. A Pi-extension + settings
change touches none → no bump required (contract 10's note shows an extension-behaviour
bump has precedent; judgement call).

**Docs touched.** `README.md:27-34`, `site/features.json:3-6` ("a Claude session"),
`docs/design-docs/agent-activity.md:59-71,104-123` ("planned, phase 3"), code comments
"nil for every agent but Claude" (`agent.go:71`, `custom.go:170-172`), plus a new changeset.

**Phase 3 prior lessons.**
- Verify the Pi API sketch against the installed package's `docs/extensions.md` before
  building; smoke-test loading with `pi -e <path> --list-models` (no tokens).
- pi-devkit's node_modules can trail the running pi (0.85.0 vs 0.85.1) — compare versions
  before relying on a newer API.

## Phase split (operator-approved)

- **Phase 1 (this plan)** — data plane + sidebar. wire frames/kinds/fields,
  agentstate ring + plan, hook.go split + label derivation, 3 wire clients,
  DaemonContract 10→11, SessionRow plan pie.
- **Phase 2 (follow-up to Phase 1, own PR; was "1b")** — subagent attribution. See
  [Phase 2](#phase-2--subagent-attribution-follow-up). Lands after Phase 1 merges and
  before Phase 3, so Pi's wire work is built on the attributed shape.
- **Phase 3** — Pi tier: `tool_execution_*` → tool_start/tool_end, `registerTool('todo')`,
  and the settings path (persisted field → Wails → UI → a channel hive.ts can read).
- **Phase 4** — inspector panel + activity grid.

Phase 1 ships standalone value: Claude sessions get a live plan indicator in the sidebar.

### Phase 2 — subagent attribution (follow-up)

**Problem.** Claude fires `PreToolUse` / `PostToolUse` for tool calls made *inside*
subagents, and those payloads carry `agent_id` (present only inside a subagent) and
`agent_type` (code.claude.com/docs/en/hooks, common input fields). Phase 1 reads neither,
so once a session fans out (Agent tool, parallel subagents, background workflows):

1. `current_tool` is one value that parallel subagents overwrite — the sidebar flickers.
2. The parent's `Agent` call stays open for the whole fan-out while subagent events land
   in the ring as if the main thread ran them.
3. The per-step tally stamps subagent tool calls onto the parent's active plan item.
4. A subagent's `TaskCreate` / `TaskUpdate` may merge into the parent's plan.
5. Background agents may fire hooks after the parent's `Stop`, flipping state back to
   `working`.

Phase 1 is not blocked by this: single-thread sessions are correct, and the failure is
cosmetic/misleading rather than a state or privacy regression.

**Step 0 — capture before building (gate).** Same rule that caught the TodoWrite error:
no shape from docs or memory. Run a real Claude session with a capture hook on
`PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `SubagentStart`, `SubagentStop`,
`Stop`, and drive (a) two parallel foreground subagents, (b) a background agent that
outlives the parent turn, (c) a subagent that calls `TaskCreate`. Record in the decision
log:
- whether `session_id` is the parent's for subagent tool calls;
- the exact `agent_id` / `agent_type` fields and the `SubagentStart` / `SubagentStop` shapes;
- whether subagent task tools share the parent's task list (answers risk 4);
- whether hooks arrive after the parent's `Stop` (answers risk 5).
Commit the captured payloads as fixtures. Anything below that the capture contradicts is
revised before code.

**Step 0 results (captured 2026-09-16, Claude Code 2.1.273, `claude -p` with a capture
hook; fixtures in `cmd/hived/testdata/hooks/subagent/`, paths and message bodies
redacted).**
- `session_id` on every subagent event is the **parent's**. Subagent tool events add
  `agent_id` (17-char hex, e.g. `a5cb8f30deab1983f`) and `agent_type`
  (`general-purpose`). Main-thread events carry neither key.
- `SubagentStart` = `{agent_id, agent_type}` + common fields. `SubagentStop` adds
  `agent_transcript_path`, `last_assistant_message`, `stop_hook_active`,
  `background_tasks`.
- **Risk 4 does not occur in this build:** a general-purpose subagent has no TaskCreate /
  TaskList (its `ToolSearch select:TaskCreate,TaskList` returned nothing), and the
  parent's TaskList afterwards showed only the parent's task. The main-thread-only plan
  rule stays as a one-line guard (TodoWrite availability in subagents was not captured).
- **Risk 5 confirmed:** the parent's `Stop` fires while subagents still run, and their tool
  events arrive afterwards (13 s later for a parallel pair, 21 s for a background agent).
  `Stop` carries `background_tasks: [{id, type:"subagent", status:"running", agent_type,
  description}]`, but it lists only subagents running *at that Stop*.
- **Risk 2 depends on how the model launched the agent:** a foreground Agent call's
  `PostToolUse` arrived only after `SubagentStop` (sonnet run). In the haiku run, the two
  calls launched without `run_in_background` still got `PostToolUse` within 50 ms, and a
  `Stop` followed while they ran. Hive must not assume either.

**Design (option B, operator-approved 2026-09-16).**
- **Wire.** `tool_start` / `tool_end` gain optional `agent_id` and `agent_type`, omitted
  for main-thread calls. Additive fields only — verify a Phase-1 daemon decodes them
  without error. New *kinds* are a different matter: `serveEvent` (`daemon.go`, "unknown kind" log) drops an unknown
  `AGENT_EVENT` kind, so skew becomes a state regression. No new kind is added unless
  Step 0 shows `SubagentStart` / `SubagentStop` are needed for the count (then
  `subagent_start` / `subagent_end` kinds, with the `DaemonContract` bump and skew note
  that implies).
- **`current_tool` and the plan come from the main thread only** — events with no
  `agent_id`. Subagent planning calls never mutate the parent's plan (unless Step 0
  shows a shared list, in which case they mutate it and that is correct).
- **Ring.** Subagent tool events are still stored, tagged with `agent_id` / `agent_type`,
  so Phase 4's panel can nest them under the parent's `Agent` call. The tally is stamped
  only for main-thread events.
- **`SessionInfo` gains `subagents_running`** (count of open subagents), and the sidebar
  row shows it without growing the row. Placement decided against a mock before coding.
- **State.** If Step 0 confirms post-`Stop` hooks, a subagent tool event must not flip a
  settled session back to `working`; the session reads settled with a running-subagent
  count instead. If it does not confirm, no state change.
- **Label derivation and privacy rule unchanged** — `agent_type` is a type name, not an
  argument, and is the only new string that crosses.
- **All three wire clients move in lock-step**, as in Phase 1.

**Tests (TDD, before implementation).**
- `mapHookPayload` table: captured subagent payloads emit `agent_id` / `agent_type`;
  main-thread payloads omit them.
- Machine: interleaved main + two subagent streams leave `current_tool` on the main
  thread's call; subagent `TaskCreate` leaves the parent plan unchanged (or per Step 0);
  tally counts only main-thread events; `subagents_running` rises and falls, and is
  cleared on session close.
- State: post-`Stop` subagent event does not reopen `working` (only if Step 0 confirms).
- Playwright (`CI=1`): the subagent count does not change row height.

**Out of scope for Phase 2.** Rendering subagent trees (Phase 4 panel), cross-session views
(still gated on agent-orchestration phases), Pi subagents (Pi has none today).

**Approved plan (operator, 2026-09-16, via plan-html round 1).** Supersedes the Design bullets
above wherever they differ.

Supersedes the "Design (option B)" bullets in the exec plan's Phase 2 section, using the
Step 0 capture (fixtures: `cmd/hived/testdata/hooks/subagent/*.jsonl`) and two operator
decisions: the count comes from new `subagent_start` / `subagent_end` kinds, and its
placement A was picked from the mock at https://claude.ai/artifact/G8wkTR3asbCuyoUNuwPcqE.

#### Phase 2 — Approach

One rule, applied once at the machine: **an event carrying `AgentID` belongs to a
subagent. It is recorded, but it never moves the session's state, `current_tool`, the plan,
or the plan-step tally.** The main thread is every event without `AgentID`.

Scope of the rule: only `tool_start`, `tool_end`, `plan`, `plan_item`, `subagent_start` and
`subagent_end` ever carry `AgentID`. `hook.go` never sets it on `PermissionRequest`,
`Notification` or any other kind, so a permission prompt raised inside a subagent still sets
`waiting_permission` on the session, which is correct: the user has to answer it.

1. **Hook → wire (`cmd/hived/hook.go`).**
   - `toolEvents`: copy `agent_id` / `agent_type` into `AgentEvent.AgentID/AgentType`.
     `planEvent` builds a fresh event, so it must copy them too, or a subagent's planning
     call would reach the plan as main-thread.
   - New cases: `SubagentStart` → `subagent_start`, `SubagentStop` → `subagent_end`. Both
     carry AgentID/AgentType.
   - `Stop` → `turn_end` also carries `RunningAgents *[]string`: the ids in
     `background_tasks` with `type == "subagent"` and `status == "running"`. It is nil when
     the key is absent (older Claude), so absence never clears anything.
   - `internal/agent/claude.go` `claudeHookEvents` adds `SubagentStart`, `SubagentStop`.
     Sessions spawned before the upgrade keep the old hook set until restarted, so they
     show no count. Documented, not migrated.
2. **Wire (`internal/wire/control.go`).** `AgentEvent` gains `AgentID`, `AgentType`
   (omitempty) and `RunningAgents *[]string` (`running_agents,omitempty`). `ToolEvent`
   (ring entry) gains `AgentID` / `AgentType`, so Phase 4 can nest subagent calls under
   the parent's `Agent` call. New kinds `AgentEventSubagentStart` / `AgentEventSubagentEnd`
   go in `AgentEventKinds`. `SessionInfo` gains `SubagentsRunning int
   json:"subagents_running,omitempty"`.
3. **Daemon trust boundary (`internal/daemon/daemon.go` `applyEventFrame`).** Cap
   `AgentID` at `MaxActivityIDLen` and `AgentType` at `MaxToolNameLen` with `capBytes`.
   Cap `RunningAgents` at `activityOpenCap`-sized length (32) with each id capped. Without
   this, a local writer could store a frame's worth per field.
4. **Machine (`internal/agentstate`).**
   - `Event` gains `AgentID`, `AgentType`, `RunningAgents`. `registry.ApplyAgentEvent`
     copies all three, since that copy is field-by-field and silently drops new fields.
   - `openCall` gains `agentID` / `agentType`. `toolEnd` inherits them from the open call,
     as it already does Tool/Target.
   - **Ordering guard:** today `Apply` sets `hookSeenAt = ev.At` for every event, and the
     guard sends anything behind it to `applyLateActivity`, which drops every non-tool kind.
     Subagent hooks race the parent's (Step 0: subagent A's PostToolUse and SubagentStop land
     milliseconds before the parent's Stop). A subagent event applied first would push
     `hookSeenAt` past the Stop's stamp and get the parent's `Stop` dropped. Split the clock:
     - `orderAt`: the guard's reference, advanced by main-thread events only.
     - `hookSeenAt`: tier liveness for `trusted()`. A main-thread event that passes the guard
       **assigns** `hookSeenAt = ev.At`, as today, which keeps the documented clock-step
       recovery ("a gap of HookStaleAfter or more is a clock that moved, and the newest
       report wins"). Only subagent-tagged events use `max(hookSeenAt, ev.At)`. Subagent
       activity keeps the hook tier trusted, so screen repaints from a running subagent do
       not reopen the heuristic tier.
     The guard's gate becomes `!m.orderAt.IsZero()`. Subagent-tagged events skip the guard
     and never reach `applyLateActivity`. Their handling is order-independent: set
     semantics for the count, CallID pairing for tools. The bypass still refreshes
     `source` / `hookSeenAt` first and still honours the `StateExited` early return, so a
     `subagent_start` after `session_end` cannot repopulate a dead session's count.
   - **State:** in `Apply`, `KindToolStart` / `KindToolEnd` set `StateWorking` only when
     `ev.AgentID == ""`. This fixes Risk 5: a subagent tool event after the parent's `Stop`
     no longer flips `waiting_input` back to `working`.
   - **Tally:** `toolStart` skips `plan[idx].Tools++` and stamps `PlanIdx = -1` for
     subagent events. (`recordFinishedTurnStart` never sees one: subagent events skip the
     ordering guard, so they never reach the late path.) `toolEnd`'s unpaired fallback
     (`out.PlanIdx = currentPlanIdx()`) is skipped for them too. It would otherwise stamp the
     main plan's step on an unpaired subagent end: an evicted call, one forgotten at
     `subagent_end`, or a PostToolUse racing past its SubagentStop.
   - **`current_tool`:** `planSummary` skips open calls with `agentID != ""`.
   - **`endTurn`** forgets main-thread open calls only. Subagent calls outlive the parent's
     turn (Step 0), so forgetting them loses their durations. Subagent calls are forgotten
     at their `subagent_end`, at `session_end`, and by the existing open cap. The cap's
     eviction prefers subagent calls, so a fan-out cannot evict the main thread's
     running call.
   - **Finished-turn rule:** a subagent `tool_start` stamped at or before `turnEndedAt`
     still opens normally. The "belongs to a finished turn" rule in the live path and in
     `recordFinishedTurnStart` applies to main-thread starts only, because a subagent is not
     bound to the parent's turn.
   - **Plan:** `setPlan` / `mergePlanItems` are skipped when `AgentID != ""`. In this build
     subagents have no task tools (Step 0), so this is a one-line guard for TodoWrite
     configurations, which were not captured.
   - **Count:** `activity.subagents map[string]time.Time` (agent id → start `At`) plus a
     small `ended` set (bounded to 32, FIFO) so that a late `subagent_start` arriving after
     its `subagent_end` does not resurrect the agent. `Snapshot` gains
     `SubagentsRunning int` (comparable, so `announceStateLocked` repaints on change). The
     map is capped at 32 entries.
     - `subagent_start`: add, unless ended.
     - `subagent_end`: delete, mark ended, forget that agent's open calls.
     - `turn_end` with `RunningAgents != nil`: **reconcile** by removing every tracked
       agent that started before the Stop's `At` and is not in the list. This heals a
       missed `SubagentStop` (interrupts were not captured) without racing a subagent that
       started after the Stop was generated. A removed agent is treated exactly like
       `subagent_end`: its open calls are forgotten, and it joins the ended set so a late
       start cannot resurrect it. Reconcile runs on `turn_end` only, never on
       `subagent_end`. `SubagentStop`'s own `background_tasks` still lists the stopping
       agent as `running` (fixture line 17), so `hook.go` does not read it there.
     - `session_end`, `Exit()`, and a fresh `Machine` clear it. `Exit()` has to do it
       explicitly, because it does not touch `act` today.
     - `prompt` does **not** clear it: a background subagent spans parent turns (Step 0).
5. **Clients.** `app_control.go` and `hived-ws-bridge` forward raw payloads, and
   `testclient` and `hivebar` decode `wire.SessionInfo`, so nothing needs a code change.
   Only the frontend types (`src/app/state.ts`: `SessionInfo.subagents_running`, plus
   `agent_id` / `agent_type` on the ToolEvent type for Phase 4 parity) and the sidebar row
   change. `registry/events.go` `broadcastActivityLocked` sends no ACTIVITY for
   `subagent_start` / `subagent_end`: the count travels on SESSION_EVENT, and forgotten open
   calls have no outcome to report (the same rule `endTurn` follows). Phase 4 revisits it
   if the panel needs lifecycle rows.
6. **Sidebar (`SessionRow.tsx`, `session-row.css`): placement A, picked by the operator
   from the mock.** A numeral badge sits on the pie's bottom-right corner. It is
   absolutely positioned inside the pie's grid cell (column 1 / row 2 at comfortable,
   row 1 at compact), so it cannot change the row's height. With no plan and a count > 0,
   a hollow placeholder ring carries the badge. With no plan and count 0, nothing renders,
   as today. The indicator's `aria-label` and `title` gain ", N subagents running". Above
   9 the badge reads `9+`. The badge takes the same stale treatment as the pie.
7. **`buildinfo.DaemonContract` 11 → 12**, with a history entry. A new GUI works against an
   old daemon (the count is just absent), but new `hived hook` binaries send kinds an old
   daemon drops at `daemon.go:793` and then closes the connection. That is the same hazard
   entry 11 describes.

##### Phase 2 — Why this beats the obvious alternative

The obvious alternative is filtering subagent events in `hook.go` and never sending
them. That loses the ring entries Phase 4 needs, and the daemon would have no count.
Filtering in the registry instead would split the rule across two packages. The machine
already owns state, tally, and `current_tool`, so the rule lives in the one place all
three are computed.

#### Phase 2 — Files to change

1. `cmd/hived/hook.go`: AgentID/AgentType on tool and plan events; SubagentStart/Stop
   cases; `RunningAgents` from `Stop.background_tasks`.
2. `internal/agent/claude.go`: two hooks added to `claudeHookEvents`.
3. `internal/wire/control.go`: AgentEvent and ToolEvent fields, `RunningAgents`, two
   kinds plus the allowlist, `SessionInfo.SubagentsRunning`, doc comments.
4. `internal/daemon/daemon.go`: caps for the new fields.
5. `internal/agentstate/machine.go`: Kind mirrors, `Event` fields, `Snapshot` field,
   `Apply` state guard, new kind cases, `Exit()` clears the count.
6. `internal/agentstate/activity.go`: `openCall` fields, subagent set and ended set,
   tally, `current_tool`, `endTurn`, late path, eviction preference, reconcile.
7. `internal/registry/registry.go`: `ApplyAgentEvent` copies the new fields; `Entry.Info()`
   copies `SubagentsRunning`.
8. `internal/buildinfo/contract.go`: bump to 12, history entry.
9. `cmd/hivegui/frontend/src/app/state.ts`: `subagents_running?: number`.
10. `cmd/hivegui/frontend/src/components/SessionRow.tsx`: count rendering, labels.
11. `cmd/hivegui/frontend/src/theme/components/session-row.css`: the chosen placement at
    both densities.
12. `cmd/hivegui/frontend/test/e2e/wails-mock.ts`: `setSessionSubagents(id, n)`.
13. `docs/design-docs/agent-activity.md`: replace "Subagents — planned, phase 2" with the
    shipped behaviour; update the wired-hooks list at :35, and fix the stale "collapses all
    three to permission_resolved" text next to it (:35-40).
13a. `cmd/hived/claude_probe_test.go`: the opt-in live probe gains a one-subagent case.
14. `docs/design-docs/daemon-contract.md`: only if its history table restates entries
    (checked during implementation).
15. `docs/product-specs/416-agent-activity-view.md`: add Phase 2 success criteria (the
    gate validates against the spec) and the hook list at :30.
16. `docs/exec-plans/active/416-agent-activity-view.md`: this plan, Progress, Decision
    log.

#### Phase 2 — New files

- `.changesets/agent-activity-subagents.md` (a new file, because `agent-activity-plan-pie.md`
  already shipped in #417's release notes path and its `pr: 417` is fixed): `type: changed`, `bump: minor`. Sidebar
  shows running subagents; subagent tools no longer hijack the session's current tool,
  state or plan tally.
- `cmd/hivegui/frontend/test/e2e/sidebar-subagents.spec.ts`: Playwright row-height and
  label checks. It could instead extend `sidebar-plan-pie.spec.ts` if it stays small; the
  choice is made while writing.

#### Phase 2 — Tests (written first, each seen failing)

Go, `cmd/hived/hook_test.go`:
- `TestHookSubagentToolEventsCarryAgent`: replays every tool line of
  `parallel-and-background.jsonl`. Subagent lines emit `AgentID`/`AgentType`; main-thread
  lines emit neither.
- `TestHookSubagentStartStop`: SubagentStart/Stop lines map to the new kinds with
  AgentID/AgentType.
- `TestHookStopRunningAgents`: a Stop line with `background_tasks` yields the running
  subagent ids. A Stop with `[]` yields a non-nil empty list. A Stop without the key
  yields nil.
- `TestHookSubagentPlanEventCarriesAgent`: a synthetic subagent TaskCreate PostToolUse
  gives a plan event whose AgentID is set.

Go, `internal/agent/claude_test.go`:
- `TestClaudeSettingsRegistersSubagentHooks`: decode the generated settings JSON and assert
  `SubagentStart` and `SubagentStop` **by name**. The existing test loops over
  `claudeHookEvents` itself, so it would pass whatever the list contains.

Go, `internal/wire/agent_event_test.go`:
- Update `TestAgentEventKindsAllowlist`.
- `TestAgentEventRunningAgentsRoundTrip`: nil stays absent, and empty stays present
  (`running_agents:[]`).

Go, `internal/agentstate/activity_test.go`:
- `TestSubagentToolKeepsMainCurrentTool`: main `Agent` start, then two subagents'
  interleaved starts and ends. `CurrentTool` stays `Agent`.
- `TestSubagentToolDoesNotTallyPlanStep`: tally counts only main-thread starts; subagent
  ring entries have `PlanIdx -1` and AgentID set, including an **unpaired** subagent
  tool_end while a plan step is active.
- `TestSubagentEventDoesNotDropParentStop`: subagent tool_end At=t+5ms applied, then the
  parent's turn_end At=t. State is `waiting_input`, and the reconcile ran.
- `TestSubagentEventsKeepHookTierTrusted`: after turn_end, subagent events 25 s and 50 s
  later keep `trusted()` true at 55 s.
- `TestClockStepBackAfterSubagentEvent`: subagent event at T, then a main-thread event at
  T−40 s applies, and `trusted()` is measured from T−40 s.
- `TestSubagentStartAfterSessionEndIgnored`: count stays 0 on an exited session.
- `TestPermissionInsideSubagentStillWaits`: a waiting_permission event (never tagged)
  arriving between subagent tool events sets `waiting_permission`.
- `TestSubagentToolAfterStopKeepsWaiting`: turn_end, then a subagent tool_start and
  tool_end. State stays `waiting_input`, the ring records both, and the call pairs with a
  duration.
- `TestTurnEndKeepsSubagentOpenCalls`: a subagent call open across `turn_end` still pairs.
- `TestLateSubagentStartIsOpenNotFinishedTurn`: a subagent start stamped before
  `turnEndedAt` is paired, not recorded as a finished-turn start.
- `TestSubagentPlanEventIgnored`: plan and plan_item with AgentID leave the plan
  unchanged.
- `TestSubagentsRunningCount`: start A, start B → 2; end A → 1; a duplicate start B stays
  1; end B → 0.
- `TestLateSubagentStartAfterEndStaysEnded`.
- `TestTurnEndReconcilesSubagents`: tracked {A (before Stop), C (after Stop)}, Stop lists
  [] → A removed, C kept; A's open call is forgotten; a late subagent_start for A does not
  bring it back. A nil list changes nothing.
- `TestSubagentsClearedOnSessionEndAndExit`.
- `TestOpenCapEvictsSubagentCallsFirst`.
- `TestSubagentCountIsCapped`.

(`TestApplyMapsEveryKind` is deliberately not extended: unknown kinds already change no
state, so a "no state change" row would pass on unmodified code.)

Go, `internal/daemon/activity_test.go`:
- `TestSessionInfoCarriesSubagentsRunning`: subagent_start over the events socket lands
  in SESSION_EVENT's `subagents_running`. A turn_end with `running_agents: []` sent over
  the same socket reconciles it to 0. That proves the registry copies `AgentID`,
  `AgentType` and `RunningAgents`.
- Extend `TestEventModeCapsActivityFields` with oversized AgentID, AgentType and
  RunningAgents.
- `TestFixtureTimelineParallelAndBackground` (in `cmd/hived`, where `mapHookPayload`
  lives): replay the captured fixture line by line into `agentstate.Machine.Apply`
  **directly**, with a hand-built `wire.AgentEvent` → `agentstate.Event` copy. Going through
  `Registry.ApplyAgentEvent` would clamp the synthetic future-dated stamps to
  `time.Now()` (registry.go:475-477) and collapse the inversion pass. Stamp `At` =
  `time.Now().Add(-time.Minute)` + 10 ms × line (the fixtures keep arrival order but not
  receive times). The registry's copy of the new fields is covered separately by the
  daemon socket tests below. Sample after chosen lines (1-based):
  - count 1 after line 10, 2 after 14, 1 after 17, 0 after 24, 1 after 26, 0 after 37;
  - after line 23 (subagent B's tool events, following the Stops at 18-19): state still
    `waiting_input`. Today's `main` shows `working` here, which is the Risk 5 regression.
  - after line 39 (SessionEnd): `exited`, count 0.
  A second pass swaps the `At` stamps of lines 17 and 18, so the parent's Stop applies
  after a later-stamped SubagentStop. The line-23 assertion must still hold; this pass
  fails under the unsplit ordering clock.

Frontend, vitest `test/dom/ui-session-row-plan.test.tsx` (or a sibling file):
- The count renders for `subagents_running > 0` with and without a plan, not for 0 or
  absent. The pie label includes the count.

Playwright (`CI=1`), `sidebar-subagents.spec.ts`:
- Row height is identical with count 0 and 3, at comfortable and compact density.
- The count element is visible and not clipped (`elementFromPoint` at its centre hits
  it).

#### Phase 2 — Verification

```
go test ./cmd/hived ./internal/agent ./internal/wire ./internal/agentstate ./internal/daemon ./internal/registry
scripts/test.sh go
scripts/test.sh unit && scripts/test.sh dom
cd cmd/hivegui/frontend && CI=1 npx playwright test sidebar-subagents sidebar-plan-pie
scripts/check-daemon-contract.sh origin/main HEAD
scripts/ui-lint.sh
./scripts/ci-bootstrap.sh   # fresh worktree: generates the wailsjs bindings typecheck needs
cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck
```

Each new test is run against the unmodified code first and must fail. The fixture
timeline test is the end-to-end proof: on today's `main` it shows `working` after the
mid-timeline Stop.

Manual: `HIVE_PROBE_CLAUDE=1` live probe (`cmd/hived/claude_probe_test.go`) extended with
a one-subagent prompt that asserts `subagents_running` rises and returns to 0. It runs
by hand like the Phase 1 probe, not in CI.

#### Phase 2 — Open questions / risks

- **An interrupted subagent may never fire `SubagentStop`** (not captured). The next
  parent `Stop` reconcile heals it. Until that Stop, the count can read high.
- **Interactive vs `-p`:** both captures were headless. The interactive TUI could order
  hooks differently. The machine rules are order-independent (set semantics,
  late-path admission), so this should not matter, and the live probe re-checks it.
- **Older Claude builds and unknown hook names:** `minHooksVersion` is 2.1.0; only 2.1.273
  was captured. Before registering the new hooks, check Claude Code's changelog for when
  `SubagentStart` appeared, and whether an older build rejects a `--settings` file naming a
  hook it does not know. If it rejects the file, every hook breaks. If so, gate the two
  names on a version constant in `claude.go`, mirroring `minHooksVersion`, and record it in
  the decision log.
- **Old sessions** keep old hook settings until restarted: no count, and subagent tool
  events are still tagged, because tagging reads fields on hooks already registered.
- **Open-call cap 32** is shared. With eviction preferring subagent calls, a very wide
  fan-out loses subagent durations first. That is acceptable.
- **Ended-set bound 32:** a session with more than 32 subagents ending between a late
  start and its end could resurrect one. The count stays capped and the next Stop's
  reconcile heals it.
- Ruled out: counting from `Stop.background_tasks` alone (operator decision); dropping
  subagent events in `hook.go` (loses Phase 4's nesting data).

#### Phase 2 — Second opinion

- **Round 1:** verdict revise, confidence 8. Seven must-fix items, all applied:
  - the ordering-guard hole (subagent events advancing `hookSeenAt` got the parent's Stop dropped);
  - the `toolEnd` unpaired-fallback plan index;
  - a wrong fixture timeline test (it ends in SessionEnd, and its line-18 sample passed on main);
  - two vacuous tests (the hook list looping over itself, and the unknown-kind state row);
  - the contract-check command missing its args;
  - reconcile not forgetting calls or marking agents ended.
- **Round 2:** verdict revise, confidence 8. All round-1 items were confirmed resolved. Two new
  must-fix items, both applied but not re-reviewed (the loop allows one re-review):
  - `max()` on `hookSeenAt` for main-thread events broke clock-step recovery. Main-thread
    events now assign it, and only subagent events use `max`.
  - The fixture test's synthetic future stamps would be clamped by the registry. It now
    drives `Machine.Apply` directly with past stamps, and the registry copy is covered by
    the socket test.
  - Nice-to-haves applied: stale late-path text, exited-session bypass, and the guard gating
    on `orderAt`.


## Decisions taken at the clarifying round

1. **Phase split = 3.**
2. **The indicator moves under the state icon, and it is a filled pie.** Placement is
   `grid-column: 1 / grid-row: 2` — the cell beneath the state icon, empty in the app
   today. That column is **14px**, so the mark is `--hv-plan-size`: 12px normal / 11px
   tight / 10px compact. A filled pie, not a hollow ring: at 12px a ring's arc is a
   hairline whose hole eats more than half the mark, while the pie uses every pixel.
   Decided against the mockup at https://claude.ai/artifact/7RjZw99RbKNV1iS13r2dtb.
2b. **At compact density the indicator moves into row 1**, beside the state icon, because
   compact has no row 2 (`session-row.css:317-334` lifts the title into row 1). Column 1
   widens 14px → 24px at compact only, so both marks fit. **This takes 10px from the
   title's track at the density where the title is the only line** — the accepted cost,
   chosen over hiding the indicator at compact.
3. **Disclosure triangles are non-focusable** (Phase 3 concern, recorded now): a div with
   `onClick` + `aria-expanded`, no tab stop, so `lib/focus.ts` is never touched.
4. **`GET_ACTIVITY` is refused outright for `ModeSession`** — not added to
   `sessionModeFrames`. Zero new code; the existing check at `daemon.go:1117` refuses it.

## Approach

> **Revised 2026-09-16 (operator-approved) — read the Decision log first.** The plan
> source below was designed around `TodoWrite`, which current Claude Code disables by
> default and, on current models, does not provide at all. Phase 1 now takes the plan
> from `TaskCreate` / `TaskUpdate` (per-item `plan_item`, merged by ID in the daemon)
> and `TaskList` (wholesale resync), keeps `TodoWrite` as a fallback, and opts Claude
> sessions into the task tools via `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` — on by default,
> Settings toggle, shared gate with the hooks, never overriding the user. Everything
> else in this section stands. Verified end-to-end by `TestClaudeProbeTaskToolsOptIn`
> against a real Opus 5 session, with a negative control (setting off → `plan_total`
> 0) run once and discarded.

### Why this shape

The data is already in flight and already discarded at one line
(`cmd/hived/hook.go:139-145`). The whole feature is: stop discarding it, derive a safe
label at the reporter, keep a bounded ring beside the state machine that already has the
right lifecycle, and ride the pub/sub and snapshot paths that already exist.

The obvious alternative — a new subscription protocol for activity, and a new per-session
store in the registry — buys nothing. The design doc already rules out subscription
gating (frames are ~100 bytes and the activity grid wants every session anyway). And a
store on `registry.Entry` would need its own reset at three call sites; hanging the ring
on `agentstate.Machine` inherits create/replace/free for free at
`registry.go:190,401-407,1387`, which is exactly the spec's "no second place to leak".

### `call_id` is Claude's `tool_use_id`

Verified against the current hook reference (code.claude.com/docs/en/hooks): both
`PreToolUse` and `PostToolUse` carry **`tool_use_id`** (e.g. `"toolu_01ABC123…"`) alongside
`tool_name` and `tool_input`, and the docs state those three are the event-specific fields.
That is the key the spec calls `call_id`, and it is what makes pairing safe while Claude
runs tools in parallel — `tool_name` alone is not a fallback, because two concurrent
`Bash` calls are indistinguishable by name.

**When it is absent** (an older Claude, or a payload shape change): the event is still
emitted with an empty `call_id`. A `tool_start` with no id is recorded but never pairs; a
`tool_end` with no id is pushed to the ring unpaired, with no duration. Activity degrades
to "a tool ran" rather than being dropped, and the state effect is preserved either way.
The existing fixtures do not carry the field, so new ones do — see New files.

### `mapHookPayload` returns a slice

The spec says a `TodoWrite` `PostToolUse` *"additionally emits `plan`"* — two events from
one payload. Today the signature is `func mapHookPayload(raw []byte) wire.AgentEvent`
(`hook.go:94`). It becomes `[]wire.AgentEvent`, and `sendHookEvent` becomes
`sendHookEvents`, writing N frames on the **one** existing connection rather than dialing
per event. Callers: `runHook` (`hook.go:36`) and `hook_test.go`.

### Label derivation is a key-driven allowlist, not a tool-name switch

New file `cmd/hived/toollabel.go`. It reads only a known set of `tool_input` keys and
emits nothing for anything else — so an unrecognised tool leaks nothing by default, and
a new Claude tool cannot silently start sending arguments over the socket.

| Key | Rule | Example |
|---|---|---|
| `command` | **first token, plus the second iff it does not start with `-` and contains no `/`, `=` or quote** | `npm test` → `npm test`; `curl -H "Authorization: Bearer sk-…"` → `curl`; `sleep 12; touch x` → `sleep 12` |
| `file_path`, `path`, `notebook_path` | separator-agnostic basename (splits on **both** `/` and `\`) | `internal\agentstate\machine.go` → `machine.go` |
| `url` | host only; empty on parse failure | `https://api.x.com/v1?token=…` → `api.x.com` |
| anything else | **no label** | — |

The command rule is deliberately stricter than "the segment before the first shell
metacharacter", which would have leaked the whole of `curl -H "Authorization: …"`.
Result capped at 120 bytes via the existing `strings.ToValidUTF8` + cap idiom
(`hook.go:165`).

### Plan extraction is defensive about TodoWrite's shape

`tool_input.todos` → items. Text reads `content`, falling back to `activeForm`. Status
maps `pending→pending`, `in_progress→active`, `completed→done`, and **any unrecognised
status falls back to `pending`** rather than being dropped. Capped at 100 items,
200 bytes of text each. TodoWrite's schema is Claude-internal and undocumented, so the
table-driven test covers both field spellings and an unknown status.

### Ring and plan on `agentstate.Machine`

```go
const ActivityRingCap = 200
const activityOpenCap = 32

type PlanItem struct { Text, Status string; Tools int }      // Tools = the tally
type ToolEvent struct {
    Tool, Target, CallID string
    StartedAt, EndedAt   time.Time    // daemon clock, both
    OK                   *bool        // nil while running
    PlanIdx              int          // -1 when no plan was active
}
```

On `Machine`: `ring []ToolEvent` (evicting at cap), `plan []PlanItem`, `planAt time.Time`,
`open map[string]ToolEvent` (call_id → in-flight, capped at 32). A Go map has no order, so
"oldest" is defined explicitly: when inserting into a full map, scan the 32 entries and
drop the one with the smallest `StartedAt`. At that size the scan is cheaper than
maintaining a second index.

- `tool_start` → record in `open`, stamp `StartedAt` from the **daemon's** clock,
  increment `plan[current].Tools`. Incrementing at start means a tool that never
  completes still counts.
- `tool_end` → look up by `call_id`, stamp `EndedAt`, set `OK`, push into the ring,
  delete from `open`. Duration is `EndedAt - StartedAt`, both daemon-side, never from
  reporter timestamps. An end with no matching start is pushed with a zero `StartedAt`
  and no duration rather than dropped.
- `plan` → **replace wholesale, but carry tallies forward by matching item text**, with
  **first-match-wins** on duplicates: TodoWrite can emit two items with the same text, and
  the 200-byte cap can make two long items identical after truncation. Each old item is
  consumed at most once.
  TodoWrite fires repeatedly with an evolving list; naive wholesale replacement would
  reset every tally on each fire, which would make the spec's "tally stays correct after
  eviction" criterion pass its own test and still be wrong in practice.

`Machine` stays non-concurrency-safe; the registry mutex remains the single guard.

### Wire

- Frames: `FrameActivity = 0x2b`, `FrameGetActivity = 0x2c` (next free after
  `FrameSetWorktreeLabel = 0x2a`), plus a `String()` case each.
- **One payload struct for both directions**, because two shapes on one frame is muddy
  and the JS reader would need to branch:
  ```go
  type ActivityMsg struct {
      SessionID string      `json:"session_id"`
      Events    []ToolEvent `json:"events,omitempty"`
      Plan      []PlanItem  `json:"plan,omitempty"`
      Full      bool        `json:"full,omitempty"`   // true = GET response, false = delta
      // Reserved for Phase 4's age display. Set by the daemon from its own
      // clock; nothing in Phase 1 reads it. Declared now so the frame shape
      // does not change between phases.
      StaleAt   string      `json:"stale_at,omitempty"`
  }
  ```
- `AgentEvent` gains `Tool`, `Target`, `CallID string`, `OK *bool`, `Items []PlanItem`,
  all `omitempty`, so old and new JSON round-trip on the unchanged `FrameAgentEvent`.
  `OK` is a pointer so "absent" and "false" stay distinguishable.
- `SessionInfo` gains `plan_done`, `plan_total int`, `current_tool string`, all
  `omitempty`, single-spelled snake_case like `state`/`state_source`.
- Three mirrored kind registries updated together: `agentstate.Kind*`
  (`machine.go:65-86`), `wire.AgentEvent*` consts (`control.go:491-499`),
  `wire.AgentEventKinds` allowlist (`control.go:507-517`).

### Daemon

`SubscribeActivity` in `internal/registry/events.go` mirroring `SubscribeIdeas`; a fan-out
case in `serveControl`'s select (`daemon.go:840-885`) carrying `if restricted { continue }`;
a `case wire.FrameGetActivity:` in `handleControlFrame` (`daemon.go:1113`). **Not** added
to `sessionModeFrames`.

### Clients

One entry — `FrameActivity: "activity:event"` in `internal/wire/client.go:117-138` — buys
the broadcast in both hivegui (`app_control.go:450-453`) and ws-bridge (`main.go:545`).
Plus `App.GetActivity` (~6 lines, `app_calls.go`), a ws-bridge `case "GetActivity"`
(~6 lines), and a testclient wrapper + `AwaitActivity` helper.

`buildinfo.DaemonContract` 10 → 11 with a history paragraph, following entries 4 and 6.

The paragraph must state the **specific** skew risk, which is worse than "activity is
missing": `internal/daemon/daemon.go:758` drops an `AGENT_EVENT` outright when its `Kind`
is not in `wire.AgentEventKinds`. A new `hived hook` reporting `tool_start`/`tool_end` to
an older still-running daemon therefore loses the working-state effect that
`permission_resolved` used to deliver — a **state regression**, not merely absent activity.
That is exactly the class history entry 2 documents.

### Sidebar plan pie — CSS conic-gradient, not SVG

A new `<span class="hv-session-row__plan">` placed at `grid-column: 1; grid-row: 2`, whose
child is the mark:

```css
background: conic-gradient(var(--hv-plan-color) calc(var(--hv-plan-pct) * 1%),
                           color-mix(in srgb, var(--hv-plan-color) 22%, transparent) 0);
border-radius: 50%;
width: var(--hv-plan-size); height: var(--hv-plan-size);
```

`--hv-plan-pct` and `--hv-plan-color` are set inline from React; `--hv-plan-size` is a
token redefined per density (12 / 11 / 10px). No mask — a **filled pie**, per the mockup.

Chosen over an inline `<svg><circle stroke-dasharray>` because `scripts/ui-lint.sh`'s glyph
rule polices icon-shaped markup and expects icons to come from the SVG sprite — a
data-driven arc cannot be a static sprite symbol, so conic-gradient sidesteps the question
and is less code.

Unlike the spec's original placement, this cell is **empty today**, so the mark needs no
`position: absolute` trick to stay out of the layout — it simply occupies an unused grid
cell and cannot change the row's height at normal or tight density.

**Compact is the exception.** With no row 2, the mark moves to `grid-row: 1` and column 1
widens from `14px` to `24px` in the compact block so the state icon and the pie sit side by
side. That is the one place this placement costs something: 10px off the title's track.

Colour is the session-state token, desaturating to `--fg-subtle` when the plan is stale.

**Staleness rides `state_source` in Phase 1 — a deliberate partial.** The row cannot see
`HookStaleAfter`, and `plan_done`/`plan_total`/`current_tool` do not carry it. Phase 1 uses
the existing `state_source` as a proxy: `state_source !== 'hook'` (and, in Phase 3,
`!== 'extension'`) with a non-zero `plan_total` means "plan present but not live", and the
pie takes `--fg-subtle`.

**What that proxy does and does not cover**, because the demotion is not time-driven:
`Output` returns early for a session in a `wantsUser` state (`machine.go:167-170`) and
`Tick` returns early for anything that is not `working` (`machine.go:247-249`) — and a
session at rest emits no output to trigger `Output` in the first place.

- **Covered:** the hook dies mid-turn while bytes keep arriving. The session is `working`,
  `Output` fires, `trusted()` fails, the tier is taken back, the pie desaturates. This is
  the case the spec's staleness paragraph is actually about.
- **Not covered:** a hooked session parked at idle / waiting_input / waiting_permission
  keeps `state_source: "hook"` indefinitely, so a plan left behind by a hook that died at
  rest renders as live however old it is.

That gap is accepted for Phase 1: staleness appears in the spec's *Desired behavior*, not
its *Success criteria*, and the panel-and-tile age display it describes is Phase 4. The
real fix is `ActivityMsg.StaleAt`, carried from the daemon's own clock and consumed by the
Phase 4 renderers.

## Files to change

1. `internal/wire/frame.go` — `FrameActivity = 0x2b`, `FrameGetActivity = 0x2c` + two `String()` cases.
2. `internal/wire/control.go` — `AgentEvent` new fields; `PlanItem`, `ToolEvent`, `ActivityMsg`, `GetActivityReq`; `SessionInfo` +3 fields; `AgentEvent*` consts + `AgentEventKinds` entries.
3. `internal/wire/client.go` — one `controlEvents` entry.
4. `internal/agentstate/machine.go` — `Kind*` consts; `Event` gains the tool/plan fields; ring + plan + open-call map on `Machine`; `Apply` cases; `Snapshot` gains the plan summary; staleness accessor.
5. `internal/registry/registry.go` — `ApplyAgentEvent` passes the new fields through; `SessionInfo` population reads the plan summary.
6. `internal/registry/events.go` — `SubscribeActivity` + `broadcastActivity`.
7. `internal/daemon/daemon.go` — fan-out case; `handleControlFrame` case for `FrameGetActivity`.
8. `cmd/hived/hook.go` — split `:139-145` into tool_start / tool_end / plan; `mapHookPayload` → slice; `sendHookEvents`.
9. `cmd/hivegui/app_calls.go` — `GetActivity`.
10. `cmd/hived-ws-bridge/main.go` — `case "GetActivity"`.
11. `internal/wire/testclient/client.go` — `GetActivity` + `AwaitActivity`.
12. `internal/buildinfo/contract.go` — 10 → 11 + history paragraph.
13. `cmd/hivegui/frontend/src/app/state.ts` — three `SessionInfo` fields.
14. `cmd/hivegui/frontend/src/components/SessionRow.tsx` — `.hv-session-row__plan` element + inline custom props; rendered only when `plan_total > 0`.
15. `cmd/hivegui/frontend/src/theme/components/session-row.css` — the new grid placement (`grid-column:1; grid-row:2`), `--hv-plan-size` per density, and the compact block's `grid-template-columns` widening + `grid-row:1` move.
16. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — activity fields + a mock-only setter.
17. `cmd/hivegui/frontend/test/e2e/hive-global.d.ts` — the setter's type.
18. `DESIGN.md` — wire protocol changed → required by AGENTS.md.
19. `docs/design-docs/agent-activity.md:162` — "all six theme presets work without a new token" → **"every preset"**. `themes.css` defines 20; writing a number is what made this line wrong twice already (`icon.css:28` says 18), so it gets no number.
20. `docs/design-docs/index.md` — hand-maintained; add the entry for the new design doc (already staged locally). **`docs/product-specs/index.md` is NOT touched** — it is generated by `scripts/regen-generated.py` and hard-blocked by the `block-generated-edits` job. Only the spec file itself changes.
21. `site/features.json` — one entry. The schema is `title` / `blurb` / `status` / `since` only (no id, no category); `status: "shipped"` with `since: "Unreleased"`, stamped at release time per AGENTS.md:258.

## New files

- `cmd/hived/toollabel.go` — label derivation (command head, basename, URL host).
- `cmd/hived/toollabel_test.go`
- `cmd/hived/testdata/hooks/` — new fixtures, all carrying `tool_use_id` except where its absence is the point: `pre_tool_use_todowrite.json`, `post_tool_use_todowrite.json`, `post_tool_use_malformed_input.json`, `post_tool_use_oversized.json`, `pre_tool_use_windows_path.json`, `pre_tool_use_url.json`, `pre_tool_use_secret_header.json`, `pre_tool_use_token_url.json`, `pre_tool_use_no_call_id.json`
- `internal/agentstate/activity.go` + `activity_test.go` — ring, plan, tally (kept out of the 394-line `machine.go`).
- `cmd/hivegui/frontend/test/dom/ui-session-row-plan.test.tsx`
- `cmd/hivegui/frontend/test/e2e/sidebar-plan-pie.spec.ts`
- `.changesets/agent-activity-plan-pie.md` — `type: added`, `bump: minor`.

## Tests

**Go — `cmd/hived/hook_test.go`**
- `TestMapHookPayloadToolEvents` (table): PreToolUse→`tool_start`; PostToolUse→`tool_end` `ok=true`; PostToolUseFailure→`tool_end` `ok=false`; TodoWrite PostToolUse→**two** events (`tool_end` + `plan`); missing `tool_input`; `tool_input` as a string not an object; oversized argument truncated; unknown tool → empty `target`.
- `TestMapHookPayloadPreservesWorkingState` — every tool event still drives the state the collapsed `permission_resolved` did.
- `TestMapHookPayloadCallID` — `tool_use_id` is carried through to `call_id`; a payload without it yields an empty `call_id` and the event is still emitted.
- `TestHookNeverLeaksToolInput` — two assertions, because a blanket "no `tool_input` value appears" is **false-failing**: `post_tool_use.json` is `{"command":"ls"}` and `ls` is the correct derived label.
  1. **Structural:** the marshalled `AgentEvent` JSON contains no `tool_input` key; the only non-scalar field allowed is the plan's `items`. Derived labels (`target`) are permitted.
  2. **Secret-bearing:** fixtures whose arguments contain material that must never appear — `curl -H "Authorization: Bearer sk-live-xxx"`, `https://api.example.com/v1?token=SECRET`, a long absolute path with a username — assert the secret substring is absent from the frame while the expected label (`curl`, `api.example.com`, the basename) is present.
  Together these are neither vacuous nor false-failing, and satisfy the spec's "asserted in a test, not by inspection".

**Go — `cmd/hived/toollabel_test.go`**
- `TestDeriveLabel` (table): unix path→basename; **Windows path→basename** (the spec's separator-agnostic criterion); `npm test`→`npm test`; `curl -H "Authorization: Bearer …"`→`curl` (no leak); `sleep 12; touch x`→`sleep 12`; URL→host; unparseable URL→empty; unknown key→empty; over-long input capped.

**Go — `internal/agentstate/activity_test.go`**
- `TestRingEvictsAtCap` — 250 events, 200 retained, oldest gone.
- `TestTallySurvivesEviction` — tally correct after its events are evicted.
- `TestPlanReplacementPreservesTallies` — replacing the plan keeps per-item tallies matched by text; a genuinely new item starts at 0; **duplicate item texts are consumed first-match-wins**.
- `TestToolEndWithoutStart` — recorded, not dropped, no duration.
- `TestDurationUsesDaemonClock` — reporter timestamps skewed by an hour; duration still correct.
- `TestOpenCallsBounded` — 50 unmatched starts, map stays at 32, and the entry dropped is the one with the smallest `StartedAt`.
- `TestParallelToolsPairByCallID` — two concurrent `Bash` starts with different `tool_use_id`s end out of order and still pair correctly; pairing by tool name would fail this.
- `TestUnpairedToolEnd` — a `tool_end` with an empty `call_id` is recorded with no duration rather than dropped.
- `TestActivityClearedOnNewMachine` — `agentstate.New` yields an empty ring and plan.

**Go — daemon**
- `TestActivityBroadcastReachesControlClients` — an agent event produces an `ACTIVITY` frame on a control client.
- `TestGetActivityReturnsRing`.
- `TestSessionModeCannotGetActivity` — a `ModeSession` connection sending `GET_ACTIVITY` gets `ErrCodeModeNotAllowed`.
- `TestSessionInfoCarriesPlanSummary` — `plan_done`/`plan_total`/`current_tool` on the snapshot.

**Frontend — dom** (`ui-session-row-plan.test.tsx`)
- no plan element when `plan_total` is absent or 0; present when set; `plan_done`/`plan_total` reach `--hv-plan-pct`; **a session no longer on the hook tier uses the subtle colour** — phrased as what it checks (`state_source`), not as "stale", which it only approximates; `aria-label` states the fraction.

**Frontend — e2e** (`sidebar-plan-pie.spec.ts`, the `sidebar-density.spec.ts:33-56` pattern)
- row height is **unchanged** with and without a plan, at **all three densities** (40 / 34 / 28px) — the criterion the spec calls out as unanswerable by vitest;
- the agent code's computed `font-size` is unchanged (it is no longer overlapped, but the assertion is cheap and pins the regression);
- at normal and tight the mark sits in **grid-row 2** and its box does not intersect the state icon's box (`getBoundingClientRect`);
- at compact the mark sits in **grid-row 1**, column 1 measures 24px, and the row is still 28px;
- **compact title truncation**: the title's `scrollWidth`/`clientWidth` ratio is asserted against a no-plan baseline row, so the 10px the pie takes is measured rather than assumed.

## Verification

```bash
scripts/test.sh go          # also runs the Pi extension's node:test suite (internal/agent/pi_test.go:296)
scripts/test.sh unit dom
(cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/sidebar-plan-pie.spec.ts test/e2e/sidebar-density.spec.ts)
scripts/ui-lint.sh
scripts/check-daemon-contract.sh origin/main HEAD
```

`check-daemon-contract.sh` takes **two required refs** and exits 2 on anything else
(`scripts/check-daemon-contract.sh:22-28`) — a bare invocation would have checked nothing.
The playwright line needs the frontend cwd, which is what `scripts/test.sh:57-58` does;
there is no `test/e2e/` at the repo root. `node --test internal/agent/pi/` is dropped as
redundant: `internal/agent/pi_test.go:296` already runs it inside `scripts/test.sh go`.

Each fails on a wrong implementation: the row-height assertion fails the moment the pie
changes the row's height; the structural half of `TestHookNeverLeaksToolInput` fails if a
`tool_input` object ever reaches a frame and the secret-bearing half fails if a derived
label over-captures; `check-daemon-contract.sh` fails without the bump; `ui-lint.sh` fails
on a raw hex.

## Second opinion

Two rounds, one `general-purpose` reviewer.

**Round 1 — `revise`, confidence 8.** Four must-fix items, three of them real defects in
the draft; all four applied:

1. **`call_id` had no named source.** No fixture carried a tool-call id and neither doc
   named the key. Fixed by verifying `tool_use_id` against the live hook reference,
   specifying absence behaviour, and adding pairing tests. Load-bearing, because Claude
   runs tools in parallel and `tool_name` is not a fallback.
2. **The leak test would have failed a correct implementation.** `{"command":"ls"}` derives
   the label `ls`, so "no `tool_input` value appears anywhere" was false-failing. Restated
   as a structural assertion plus secret-bearing fixtures.
3. **`scripts/check-daemon-contract.sh` takes two required refs** and exits 2 otherwise
   (`:22-28`). The bare invocation would have checked nothing while appearing to pass.
4. **The stale-desaturation had no data source.** Fixed without a new wire field, via the
   existing `state_source`.

The most valuable catch arrived as a *nice-to-have*: `internal/daemon/daemon.go:758` drops
an `AGENT_EVENT` whose `Kind` is unknown, so version skew is a **state regression**, not
merely absent activity. That is now required content for the `DaemonContract` 11 history
paragraph. Eight further nice-to-haves were applied (open-map eviction ordering, tally
tie-break, generated-index wording, `site/features.json` schema, playwright cwd, redundant
`node --test`, resolved ui-lint risk).

**Round 2 — `approve`, confidence 8.** All four must-fix items verified closed. One
residual correction applied before the operator saw the plan: the claim that the machine
"hands the session back to the heuristic tier" past `HookStaleAfter` is **wrong on elapsed
time alone** — `Output` bails on `wantsUser` (`machine.go:167-170`) and `Tick` bails unless
`working` (`:247-249`), and a session at rest emits no output to trigger `Output`. Verified
independently at those lines before accepting it. The Approach section now states exactly
what the `state_source` proxy does and does not cover.

**Applied: 4 of 4 must-fix, 9 of 9 nice-to-have.**

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.

- **2026-09-16 iter 1** — verdict: (not reported); mergeable: MERGEABLE; findings_hash: empty; threads_open: 5; action: escalated:operator-stopped-premise-invalidated; head_sha: 44735aef. Stopped by the operator mid-iteration after the TodoWrite premise was disproven. The worker had already pushed one autofix commit (44735aef, 5 safe fixes, all verified correct — including a real bug: GET_ACTIVITY on a closed session returned `true`, which closes the GUI's whole control connection) and resolved 5 of 10 CodeRabbit threads. The remaining 5 are untouched. Re-run from scratch after the plan-source revision.
- **2026-09-16 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 2297d0b1bf382db08dcfeae0920dd06542ad98df504143fe8aaecc64a3975856; threads_open: 3; action: escalated:ci-check-failed+risky-fix+new-threads; head_sha: c5daa840.
- **2026-09-16 iter 3** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 85a363d22763ab80cbe89d9d6939c16920ea4dabb5bbbcd5f1df54f2c1d5ad14; threads_open: 0; action: escalated:risky-fix-needs-decision; head_sha: ca66e6df.
- **2026-09-16 iter 4** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 345a723ef25dfd8d97e4f226782da4cdd97c77db20aead390ffb3692eacd87ca; threads_open: 0; action: escalated:risky-fix-needs-decision; head_sha: b9160bd6.
- **2026-09-16 iter 5** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 3778d8dca35b6ce7a656c0ef37ea0dd54431646bc942c5b5e8ba1e5483e9f9ae; threads_open: 2; action: escalated:max-iterations+risky-fix-needs-decision; head_sha: 3ca14f68. Max iterations (5) reached. Both findings resolved by operator decision in the following commit; operator chose to proceed to /hs-merge-gate without a further review round.
- **2026-09-16 iter 6** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 7113525c098795eefc4565e01971a2d60f9a6b58ed0ae3225d29dd535dcd0de1; threads_open: 0; action: escalated:risky-fix-needs-decision; head_sha: 19294278. Operator-approved extra round past the 5-iteration cap. One IMPORTANT finding (Tool/CallID uncapped at the daemon boundary), fixed in the following commit. CI: macOS failed on a proxy.golang.org module-download timeout (no test ran); Linux failed on worktrees.spec.ts:247 toBeFocused (passed on retry). That spec has failed twice on this branch and not in main's last 40 failed runs, but passed 160/160 locally including 120 repeats at 12 workers, and this PR touches no focus code: recorded as likely flaky, not proven.
- **2026-09-16 iter 7** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: d2ffdc87. Converged. Four reviewers clean; CI passed on Linux, macOS and Windows with no retries (worktrees.spec.ts:247 did not recur). Two MINOR items noted, not applied before the gate: stale 'reads exactly one frame' comments (serveEvent now drains up to eventMaxFrames), and Text/Target still byte-cut rather than via capBytes.
- **2026-09-16 iter 1 (PR #420, phase 1b)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 972d54d7a2a78481d345f79edaf3146595204465154c10b0048c853b73cfe32d; threads_open: 3; action: escalated:ci-check-failed; head_sha: 891b5b02. Autofix added a DOM test for the no-plan stale badge and fixed a CSS comment. Linux CI failed `ui-lint --strict` on the badge's raw radius and font size, which were in the PR head before autofix; fixed by the orchestrator in 9c70f0a1 (`--radius-sm`, and a documented allow for the 8px digit, below the 11px type scale). Three CodeRabbit threads arrived after the push.
- **2026-09-16 iter 2 (PR #420)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 71240f8d1caf9c1ff0fc669564fe52167bcdf485b0541f060f83cc4bc7adf1b9; threads_open: 1; action: escalated:risky-fix-needs-decision; head_sha: dae4c033. Autofix: `setPlan` now stamps omitted steps so a late `plan_item` cannot resurrect them (IMPORTANT, with a new test), and the spec's no-plan badge wording was clarified. CI passed. Escalated on CodeRabbit r4030941237 (an inverted subagent tool pair leaves an orphan open call); operator chose not to open a start for an already-ended subagent, fixed in the following commit and the thread resolved.
- **2026-09-16 iter 3 (PR #420)** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 7ad3e398. Converged. Four dimension reviewers were clean. CI passed on Linux, macOS and Windows once the worker's pending checks finished (orchestrator re-checked). One low-confidence doc drift, not filed, was fixed in the GATE commit: the Tally bullet credited `recordFinishedTurnStart` with skipping subagent events, but that path never sees them.

## Gate verdict

Append-only. The latest entry is authoritative.

- **2026-09-16** — verdict: NEEDS_FOLLOWUP; phase: 1/3; checks: 2 passed / 0 failed / 1 followups / 3 deferred (pass/fail/followup count dimensions; deferred counts acceptance criteria); followups: none; one-line: every Phase 1 success criterion and every non-goal passes, but three code comments still say a ModeEvent connection reads exactly one frame.
  - 2026-09-16 dimensions:
    - acceptance — PASS — every Phase 1 criterion verified by running its tests (hook, agentstate, daemon, wire, agent, registry spawn-gate; Playwright sidebar-plan-pie 7/7; DOM 11/11). DEFERRED: Pi extension tests (phase 2); the TypeScript half of separator-agnostic labels (phase 2); the panel and activity-grid Playwright checks (phase 3). The real-Claude opt-in probe skips outside `HIVE_PROBE_CLAUDE=1` by design; it was run by hand on 2026-09-16 and passed, with a negative control confirming plan_total 0 with the setting off.
    - non-goals — PASS — activity is memory-only (agent-settings.json holds only the task-tools boolean); no raw tool arguments on any wire type; no transcript view, token/cost accounting, plugin loading or cross-session view; no Pi tier, inspector panel or activity grid in the diff.
    - doc accuracy — NEEDS_FOLLOWUP — changeset (type added, pr 417), site/features.json, README, DESIGN.md, design doc and spec all accurate; CHANGELOG.md and the generated spec index untouched. Three comments are false: internal/wire/control.go:19-23, internal/wire/frame.go:131-135 and internal/daemon/daemon.go:654-658 say a ModeEvent connection reads exactly one frame, but serveEvent drains up to eventMaxFrames (8). regression_of: n/a (changeset type is added). Also noted, not from this PR: AGENTS.md cites `.changesets/README.md` as the changeset schema, and that file does not exist.

- **2026-09-16** — verdict: PASS; phase: 1/3; checks: 3 passed / 0 failed / 0 followups / 6 deferred (pass/fail/followup count dimensions; deferred counts acceptance criteria); followups: none; one-line: re-run after fixing the doc-accuracy follow-up on this branch — every Phase 1 criterion, every non-goal and every doc check passes.
  - 2026-09-16 dimensions:
    - acceptance — PASS — confirmed the only code change since the previous run (f8b92755) is comment-only; re-ran hook, agentstate, daemon, wire, agent and registry spawn-gate tests, Playwright sidebar-plan-pie 7/7, DOM 11/11, and the DaemonContract check. DEFERRED: Pi extension tests; the TypeScript half of separator-agnostic labels; the channel hive.ts will read the Pi todo-tool setting through (phase 2); panel and activity-grid Playwright checks (phase 3). The real-Claude opt-in probe skips outside HIVE_PROBE_CLAUDE=1 by design and was run by hand on 2026-09-16 (passed, with a negative control).
    - non-goals — PASS — unchanged from the previous run and re-verified against the full PR diff.
    - doc accuracy — PASS — the four corrected comments match serveEvent (drains up to eventMaxFrames, invalid frame closes, read deadline refreshed per frame); a repo-wide sweep found no remaining false one-frame claim; changeset, site/features.json, README, DESIGN.md, design doc and spec re-confirmed; CHANGELOG.md and the generated spec index untouched. Noted, not this PR: AGENTS.md cites a nonexistent `.changesets/README.md` (predates the branch).
- **2026-09-16** — verdict: PASS; phase: 2/4; checks: 3 passed / 0 failed / 0 followups / 3 deferred; followups: none; one-line: all six Phase 2 (subagent attribution) criteria verified by running their tests, no Phase 1 regression, every non-goal held, docs accurate to the code.
  - 2026-09-16 dimensions:
    - acceptance — PASS — each Phase 2 criterion ran green: hook mapping (four `TestHook*` subagent tests), machine rule and count (17 agentstate tests), the Stop race (`TestSubagentEventDoesNotDropParentStop`, `TestFixtureTimelineParallelAndBackground` with the inverted-stamp pass), daemon caps and `SessionInfoCarriesSubagentsRunning`, `DaemonContract = 12`, and Playwright `sidebar-plan-pie` 11/11 including the badge row-height and no-clip checks. Phase 1 criteria: the Go suites, DOM 16/16 and the pie Playwright tests are green; the outlined-pie restyle is an operator decision, not a regression. DEFERRED: Pi extension tests and the TypeScript half of separator-agnostic labels (Phase 3); panel and grid Playwright checks (Phase 4).
    - non-goals — PASS — no persistence, and no raw tool arguments on any wire type (new fields are only agent_id, agent_type, running_agents and subagents_running). No transcript view, cost accounting, plugin API, cross-session view, subagent tree, Pi tier, panel or grid in the diff.
    - doc accuracy — PASS — changeset valid (type added, pr 420). Design doc, spec sidebar text and Phase 2 criteria, and contract entry 12 all match the code. CHANGELOG.md and the generated index are untouched, and README/DESIGN/site hold nothing stale. Two source comments still said "phase 1b" after the renumbering; fixed in 451702b9 before this verdict.

## Phase 3 plan (approved 2026-09-16 via HTML review, round 1)

Scope (from the plan's operator-approved phase split): Pi `tool_execution_*` → `tool_start`/`tool_end`,
a Hive-registered todo tool → `plan`, and the settings path that turns it off. Operator decisions:
tool name `hive_todo`; separate Pi checkbox (`pi_todo_tool`); shared JSON label vectors.

Spec criteria this phase closes:
- (C1) Pi extension tests: `tool_execution_start` / `tool_execution_end` post the right events;
  `registerTool` produces a plan; the todo tool disabled posts nothing and registers nothing.
- (C2) Label derivation is separator-agnostic: a Windows path renders as its basename in **both** reporters.
- (C3, carried) No raw tool arguments cross the socket — only `target` and plan `items` — asserted in a test.
- (C4, carried) Hive adds a tool to the user's agent only as a setting, on by default, disableable.
- (C5, carried) Durations come from the daemon clock — the extension sends no duration/timestamps beyond `at`.

### Approach

**No daemon or wire change.** Phases 1–2 already accept `tool_start`/`tool_end`/`plan` with
`tool`, `target`, `call_id`, `ok`, `items` from `source: "extension"`. Phase 3 is reporter + settings only.

1. **Encoder generalised to N events per connection.** `encodeFrames(sessionId, events, at)` takes an
   array of `{kind, text?, tool?, target?, call_id?, ok?, items?}` and emits HELLO + one AGENT_EVENT per
   entry (daemon allows 8 per connection). `post(kind, text?, fields?)` becomes a one-element wrapper over
   `send(events)`. Why: a successful `hive_todo` call must deliver `tool_end` and `plan` together with the
   same `at`, exactly as `hook.go:484-515` does for Claude — two dials would let them race.
   **Sends are serialized.** Today every `post` dials its own connection, so a parallel tool's `tool_end`
   stamped a few ms after a `plan` can reach the daemon first, and `applyLateActivity`
   (`activity.go:169-172`) then drops the wholesale `plan` silently — fatal if it was the final "all done"
   update. `send` appends to a FIFO and the next dial starts only on the previous connection's `close`
   (the daemon applies frames before closing, `daemon.go:755-781`), so delivery order = stamp order. The
   existing 2 s socket timeout bounds a wedged daemon; the queue is capped at 64 pending reports and the queue is bounded (fire-and-forget contract unchanged). A connect error, a timeout, or a
   synchronous throw from `net.createConnection` (the `catch` at `hive.ts:99`) all advance the queue — it
   never stalls. `send` rejects batches over 8 events (the daemon's `eventMaxFrames`, `daemon.go:742`).
   Full-queue policy: drop the **oldest** pending report, not the newest — a wedged daemon's backlog of
   64 × 2 s would otherwise deliver state older than `HookStaleAfter` (30 s) while the freshest report is
   discarded.
   Every field is capped in TS before encoding: text 512 B, target 120 B, tool / call_id 128 B,
   plan text 200 B, ≤100 items (a generalised `truncateBytes(s, max)`; `truncate` stays as the 512 wrapper).

2. **Tool events.** Subscribe to the **non-blocking** `tool_execution_start` / `tool_execution_end`
   (never `tool_call` / `tool_result`, per the design doc).
   - start → `tool_start {tool: toolName, target: deriveTarget(args), call_id: toolCallId}`
   - end → `tool_end {tool, call_id, ok: !isError}` (target omitted; the daemon falls back to the start's,
     `activity.go:295-300`). `ok` is always sent explicitly, including `false`.
   - Raw `args` / `result` are never put in a frame: the frame body is built from an explicit field list,
     never a spread of the Pi event.

3. **`deriveTarget(args)` — a TS port of `cmd/hived/toollabel.go`**, same key allowlist and order
   (`command` → commandHead; `file_path`/`path`/`notebook_path` → baseName; `url` → urlHost), same
   `isEnvAssignment` / `isCommandName` / `isSubcommand` rules, byte-length checks via `Buffer.byteLength`,
   `baseName` = after last `/` or `\`, 120-byte rune-safe cap. Non-object input → "".

4. **Shared label vectors.** Move `TestDeriveLabel`'s inline table to
   `cmd/hived/testdata/toollabel/vectors.json` (`[{name, input, want, want_go?, want_ts?}]`).
   `toollabel_test.go` and `hive.test.ts` both iterate it; `want_go` / `want_ts` exist only for pinned
   URL-parser divergences (JS `new URL` strips default ports and lowercases hosts). The Windows-path vector
   has a single `want` — it must agree in both languages (C2). The other Go-only tests
   (`TestIsSubcommandKnownCeiling`, `TestCommandWordCredentialCeiling`, …) stay in Go; their vectors are
   added to the JSON too so the TS port inherits the same documented ceilings.

5. **`hive_todo` tool.** Registered with `pi.registerTool` only when `process.env.HIVE_PI_TODO_TOOL !== "0"`.
   - `parameters`: a **plain JSON-Schema object** (`{type:"object", properties:{todos:{type:"array",
     maxItems:100, items:{type:"object", properties:{text:{type:"string"}, status:{type:"string",
     enum:["pending","active","done"]}}, required:["text","status"]}}}, required:["todos"]}`), because
     `hive.ts` is loaded by `node --test` with no node_modules and a value import of `typebox` would crash
     the module. `import type` only. Verified research-time that typebox 1.3.7 `Compile` accepts it.
   - Semantics: wholesale replace (the whole list each call), matching `plan` kind and Claude's `TodoWrite`.
     No IDs; the daemon carries tallies forward by text.
   - `execute` returns `{content: [text summary], details: {todos}}` — state lives in the tool result
     details, as Pi's own `examples/extensions/todo.ts` does, so it survives branch/resume.
   - `execute` does **not** post and keeps no side state. The plan is posted from `tool_execution_end` when
     `toolName === "hive_todo" && !isError`, together with that `tool_end`, read from
     `event.result?.details?.todos` (`ToolExecutionEndEvent.result` is the `AgentToolResult` carrying
     `details`, `types.d.ts:623-629` — re-verified at step 0). No call-ID map, so nothing can leak when an
     end never fires. Missing/malformed `details` → only `tool_end`. `result` itself never enters a frame
     (C3). A failed or blocked call changes no plan. Why not post from `execute`: it runs before end and
     would land a plan for a call Pi later reports as errored/aborted.
   - An empty `todos` list posts `plan` with `items: []`; `omitempty` drops it and `setPlan(nil)` clears —
     a test pins that the frame is still a `plan` kind.

6. **Resume/branch reconstruction.** One helper `planFromBranch(ctx)` walks
   `ctx.sessionManager.getBranch()` for the last `toolResult` message with `toolName === "hive_todo"`, `isError !== true` and
   `details.todos` (an `afterToolCall` hook can flip `isError` while keeping `execute`'s `details`,
   `agent-loop.js:504-512,549-552`; the live end handler posts no plan for such a call, so the rebuild
   must skip it too). It runs on `session_start` (every `reason`: startup/reload/new/resume/fork) and on
   `session_tree` (a `/tree` jump changes branch without `session_start`, `types.d.ts:506,918`). It
   **always** posts a `plan` when the tool is enabled — the found list, or `items: []` when none — so `/new`,
   a fork, or a jump to a branch without a todo call clears the previous conversation's plan instead of
   leaving it on screen (`setPlan` is the only thing that clears it, `machine.go:484-487`). On
   `session_start` it rides the same send as the existing `ping`. Wrapped in try/catch like
   `lastAssistantText`; a throw posts `items: []`. Disabled tool ⇒ no plan frame at all. Without this a Pi `/resume` or restart shows no plan until the next call — the same
   symptom the operator reported for Claude, avoidable here because Pi keeps the state in the session.

7. **Setting path — reuse Phase 1's `agent-settings.json` / `Def.SpawnEnv`.**
   - `internal/agent/settings.go`: `PiTodoTool bool json:"pi_todo_tool"` (default true, pointer on disk),
     `const PiTodoToolEnv = "HIVE_PI_TODO_TOOL"`, `piSpawnEnv(sp)`.
   - `piSpawnEnv` shares `piSpawnArgs`' gate (extension file present) and returns an **explicit**
     `HIVE_PI_TODO_TOOL=1` or `=0` every time. Explicit both ways because the variable is Hive's own and an
     inherited value (hived launched from inside a Hive session) must not override the setting — unlike
     `CLAUDE_CODE_ENABLE_TODO_TOOLS`, which is the user's variable and is left alone.
   - `IDPi` Def gets `SpawnEnv: piSpawnEnv`. Create, revive and restart already apply it via
     `applyAgentSpawn` / `resolveAgentEnv`; custom `pi …` agents inherit it (`custom.go:168-175`).
   - Wails `AgentSettings` gains `PiTodoTool` (mapped both ways in `GetAgentSettings`/`SaveAgentSettings`).
   - Settings → Agents: second checkbox "Show Pi's plan progress in the sidebar", hint: adds a `hive_todo`
     tool to Pi sessions Hive starts, uses some context, newly started sessions only.
     Loaded/saved in the same guarded block as the Claude one (both fields saved together — `SaveSettings`
     writes the whole struct).
   - Extension side: disabled ⇒ no `registerTool`, no plan reconstruction, no plan frames. Tool events for
     other tools still flow (they are not what the setting governs).

8. **Daemon contract: no bump.** Nothing under `internal/{wire,daemon,session,registry}` or `cmd/hived`
   non-test code changes (the only `cmd/hived` changes are `_test.go` + testdata, which
   `check-daemon-contract.sh` ignores). An old daemon + new GUI writes a key nobody reads (harmless).
   The extension file is embedded in hived and rewritten at daemon start, so new behaviour arrives with the
   daemon binary — same as any other hived change that isn't a protocol change.

Alternative ruled out: posting plan items (`plan_item`) with IDs. Wholesale `plan` is simpler, needs no ID
stability, and the list is already whole in the tool call. A late wholesale `plan` would be dropped by the
ordering guard; the serialized send queue (step 1) delivers in stamp order so it never arrives late.

### Files to change

1. `internal/agent/pi/hive.ts` — `truncateBytes`; `encodeFrames(sessionId, events, at)`; `send`/`post`;
   `deriveTarget` + helpers (exported for tests); `tool_execution_start`/`_end` handlers; `hive_todo`
   registration gated on `HIVE_PI_TODO_TOOL`; plan from `tool_execution_end` `result.details`; `planFromBranch` on `session_start` / `session_tree`; header
   comment ("one AGENT_EVENT" → "one or more").
2. `internal/agent/pi/hive.test.ts` — fakes gain `registerTool` (recording); `collectFrames` typed
   `Record<string, unknown>`; subscription-list assertion gains `tool_execution_start`, `tool_execution_end`,
   `session_tree`; vectors path resolved from `import.meta.url` (the Go runner's cwd is `internal/agent`);
   new tests (below).
3. `internal/agent/pi_test.go` — `TestPiExtensionFramesAreValidWireFrames` calls the new signature and
   adds a two-event case (HELLO + 2 AGENT_EVENTs, all decoded by `wire.ReadFrame`, `ok:false` survives as
   `*bool` false, `items` decode to `[]wire.PlanItem`); `TestPiExtensionKindsAreOnTheAllowlist` required
   list gains `tool_start`, `tool_end`, `plan`; the scraper is widened to also collect every
   `kind:\s*"…"` literal in the file (so kinds inside `send([...])` are checked, not just `post("…")`), and
   to assert that **every** collected kind is on the allowlist; `TestPiDefUsesSpawnArgs` also asserts `SpawnEnv`.
4. `cmd/hived/toollabel_test.go` — `TestDeriveLabel` reads `testdata/toollabel/vectors.json`
   (honouring `want_go`); fails if the file has zero vectors.
5. `internal/agent/settings.go` — `PiTodoTool`, `PiTodoToolEnv`, `piSpawnEnv`, resolve/save.
6. `internal/agent/agent.go` — `IDPi.SpawnEnv`; `SpawnEnv` doc comment "nil for every agent but Claude" → "Claude and Pi".
7. `internal/agent/custom.go` — comment at :170-172.
8. `internal/agent/settings_test.go` — replace `TestOnlyClaudeHasSpawnEnv` with `TestOnlyClaudeAndPiHaveSpawnEnv`; fix the `SpawnEnv != nil` expectation in `TestCustomAgentInheritsSpawnEnv`; new tests (below).
9. `internal/registry/agent_env_test.go` — extend `TestEverySpawnPathCarriesHooksAndOptInTogether` with a Pi entry (create + restart + revive carry `-e` and `HIVE_PI_TODO_TOOL` together).
10. `cmd/hivegui/app_calls.go` — `AgentSettings.PiTodoTool` both directions.
11. `cmd/hivegui/frontend/src/components/modals/Settings.tsx` — second checkbox + hint, same load/save guard.
12. `cmd/hivegui/frontend/test/dom/settings.test.tsx` — Pi toggle describe block.
13. `test/e2e/wails-mock.ts`, `test/e2e-real/wails-bridge.ts`,
    `cmd/hivegui/frontend/test/dom/settings-updates.test.tsx:49` — `pi_todo_tool: true` in agent settings mocks.
14. `README.md:27-34` — Pi gets a plan via Hive's `hive_todo` tool; toggle location.
15. `site/features.json` "Agent plan progress" blurb — Claude and Pi sessions.
16. `docs/design-docs/agent-activity.md:59-71,104-123` — drop "planned, phase 3"; tool is `hive_todo`;
    setting and env var; plan reconstructed from tool-result details on resume.
17. `cmd/hived/pi_probe_test.go` — opt-in (`HIVE_PROBE_PI=1`) load check (see Tests).

### New files

- `cmd/hived/testdata/toollabel/vectors.json` — shared label vectors (Go + TS).
- `.changesets/pi-tool-activity-and-plan.md` — `bump: minor`, user-visible: Pi sessions show the current
  tool and plan progress; Settings → Agents Pi toggle.

### Tests

TS — `internal/agent/pi/hive.test.ts` (node:test, run via Go `TestPiExtensionRunsNodeTests`):
- `tool_execution_start posts tool_start with tool, derived target and call_id` — args `{command:"FOO=1 npm test --secret=x"}` → `{kind:"tool_start", tool:"bash", target:"npm test", call_id}`; asserts the frame's JSON contains neither `--secret` nor any `args` key (C3).
- `tool_execution_end posts tool_end with explicit ok` — `isError:false` → `ok:true`; `isError:true` → `ok:false` present (not omitted); no `result` content in the frame (C3).
- `hive_todo success posts tool_end and plan on one connection` — fire start, call the registered `execute` with 2 todos, fire end → exactly one connection carrying `[tool_end, plan]`, same `at`, items `{text,status}` with no `tools`/`id`.
- `hive_todo failure changes no plan` — end with `isError:true` → only `tool_end`, no `plan`.
- `hive_todo end without details posts only tool_end` — `result` undefined / `details` missing / `todos` not an array.
- `hive_todo empty list clears the plan` — `todos:[]` → a `plan` frame is sent.
- `plan caps: 100 items, 200-byte text on a rune boundary`.
- `todo tool disabled registers nothing and posts no plan` — `HIVE_PI_TODO_TOOL=0` → `registerTool` never called; a `tool_execution_end` for `hive_todo` and a `session_start` with a hive_todo branch post no `plan` (C1).
- `todo tool enabled by default` — env var unset and `=1` → registered once, name `hive_todo`, parameters is a plain object with `required:["todos"]`.
- `session_start reconstructs the plan from the last hive_todo tool result`.
- `session_start reason "new" with no hive_todo result posts an empty plan` (clears the previous conversation's).
- `session_tree rebuilds the plan for the new branch` — branch A has todos, jump to branch B without → `items: []`; back to A → A's list.
- `malformed branch entries post ping and an empty plan`.
- `rebuild skips an errored hive_todo result` — last result `isError:true` with `details.todos` → the previous good list (or `[]`).
- `sends are serialized in stamp order` — test server delays closing connection 1; a second `post` fired meanwhile does not connect until connection 1 closes, and payloads arrive with non-decreasing `at`.
- `a refused connection does not stall the queue` — first dial errors (socket path removed then restored), second report still arrives.
- `queue is capped and drops the oldest` — with a server that never closes, pending stays ≤64 (exported `pendingSends()` test hook) and after the server closes the newest report is among those delivered.
- `a synchronous connect throw advances the queue` — invalid socket path that throws synchronously, next report still sent.
- `send rejects a batch over 8 events`.
- `deriveTarget matches the shared vectors` — iterates `vectors.json` using `want_ts ?? want`; includes the Windows path vector (C2).
- `encodeFrames caps every string field` — oversized tool/target/call_id/text.

Go:
- `cmd/hived/toollabel_test.go` `TestDeriveLabel` — now vector-driven; fails on an empty vector file.
- `internal/agent/pi_test.go` `TestPiExtensionFramesAreValidWireFrames` — two-event decode as above.
- `internal/agent/pi_test.go` `TestPiExtensionKindsAreOnTheAllowlist` — requires `tool_start`/`tool_end`/`plan`.
- `internal/agent/settings_test.go`: `TestSettingsPiTodoToolMissingKeyDefaultsOn`, `TestSettingsRoundTrip` (extended with the Pi field), `TestPiSpawnEnvExplicitBothWays` (on → `=1`, off → `=0`), `TestPiSpawnEnvIgnoresInheritedValue` (lookupEnv reports `HIVE_PI_TODO_TOOL=0`, setting on → still `=1`), `TestPiSpawnEnvNeedsTheExtension` (no file → nil), `TestOnlyClaudeAndPiHaveSpawnEnv`.
- `internal/registry/agent_env_test.go` `TestEverySpawnPathCarriesHooksAndOptInTogether` — Pi rows.
- `cmd/hived/pi_probe_test.go` `TestPiProbeExtensionLoadsWithTodoTool` (`HIVE_PROBE_PI=1`) — `pi -e <written hive.ts> --list-models` with env set exits 0 with no extension load error, with the tool on and off (no tokens spent; per prior lesson).

DOM — `cmd/hivegui/frontend/test/dom/settings.test.tsx` `describe('settings: Pi plan progress toggle')`:
loads `pi_todo_tool` (default on when missing), saves both fields together, disabled + not saved when the
settings file failed to load, hint mentions `hive_todo`, "newly started sessions only", aria-describedby.

### Verification

```
node --test internal/agent/pi/                      # TS suite incl. vectors + disabled-tool tests
go test ./internal/agent/... ./cmd/hived/... ./internal/registry/...
scripts/test.sh                                      # go · unit · dom · e2e
scripts/ui-lint.sh
scripts/check-daemon-contract.sh origin/main         # expect: no bump required
HIVE_PROBE_PI=1 go test ./cmd/hived -run TestPiProbe  # manual, real pi
```
Non-vacuity checks run once during implementation and recorded in Progress:
- flip one vector's `want` → both Go and TS fail;
- put a bogus kind inside `send([{kind:"bogus"}])` → `TestPiExtensionKindsAreOnTheAllowlist` fails;
- revert the queue to per-post dials → the serialization test fails;
- remove the empty-plan post on `session_start` → the `reason "new"` test fails;
- make `tool_execution_end` spread the event into the frame → the C3 no-raw-args test fails;
- drop the `HIVE_PI_TODO_TOOL` gate → the disabled test fails;
- post the plan from `execute` instead of end → the failure-changes-no-plan test fails.

### Open questions / risks

- **Pi API drift**: installed pi-devkit is 0.85.0, running pi 0.85.1. Step 0 of implementation: verify
  `registerTool` / `tool_execution_*` / tool-result `details` shape against the running pi's
  `docs/extensions.md` and the load probe before writing handlers; log deviations in the decision log.
- **CI actually runs the TS half**: `TestPiExtensionRunsNodeTests` skips when node can't strip types
  (`pi_test.go:286-289`); confirm the CI job running `go test ./internal/agent/...` is the node-24 job
  (`ci.yml:113`), or C2's "both reporters" could pass with the TS half skipped.
- **Mixed-version downgrade**: an older GUI's `SaveAgentSettings` rewrites the file without
  `pi_todo_tool`, silently turning a user's "off" back on. Harmless; recorded in the decision log.
- **Permission windows**: `tool_execution_start` fires before a blocking `confirm()`, so order is
  tool_start → waiting_permission → ui_prompt_end → tool_end; correct because `at` is stamped in the handler.
  A parallel tool starting *inside* another extension's wait flips state to working — accepted, matches Claude.
- **Go/JS URL parity** is close, not exact; divergences are pinned, not hidden.
- **Restart after disabling**: a resumed Pi conversation that used `hive_todo` then has it unregistered;
  the model sees a missing tool. Accepted, documented in the hint ("newly started sessions").
- **Tool name**: `hive_todo` shows in the sidebar as the current tool while it runs — acceptable.
- **Scraper coupling**: `TestPiExtensionKindsAreOnTheAllowlist` checks literal kinds in `post("…")` and
  `kind: "…"`; a kind built from a variable is invisible to it. Documented in a comment beside `send`.
- **`/reload`** creates a new extension instance with its own queue, so the old instance's
  `session_shutdown` idle and the new `session_start` ping+plan can still race. Harmless: a late idle is
  dropped by the ordering guard and the plan is newer.

### Second opinion

- **Round 1 — revise (confidence 7).** Five must-fix items, all applied: (1) the kind-allowlist scraper
  was blind to kinds inside `send([...])` → widened to `kind: "…"` literals with a bogus-kind non-vacuity
  step; (2) `/new` / fork left the previous plan on screen → every `session_start` posts a plan, empty when
  none; (3) `session_tree` jumps ignored → same rebuild helper; (4) a wholesale `plan` could arrive after a
  later-stamped event and be dropped silently → serialized send queue; (5) the call-ID stash map could leak
  and was unverified → removed, plan read from `tool_execution_end` `result.details`.
- **Round 2 — revise (confidence 7).** Round-1 items confirmed resolved (`session_tree` fires after the
  leaf moves; same-`at` frames are not "late", `machine.go:403`; `result` carries `details`,
  `agent-loop.js:532-539`). One new must-fix, applied without a third review (the loop runs at most two):
  the branch rebuild must skip `hive_todo` results with `isError: true`, with a test. Nice-to-haves also
  applied: stale "stash-by-call-id" and scraper-coupling text removed; queue advances on synchronous
  connect throws; full queue drops the **oldest** report (a 64 × 2 s backlog would otherwise exceed
  `HookStaleAfter`); `send` rejects batches over 8 frames; `/reload` race noted as harmless.

## Decision log

- **2026-09-16** — **Command-word credential leak accepted and documented (operator
  decision).** Four review rounds each found a new secret shape slipping past the label
  heuristic (bare second word → env-assignment first word → Unicode look-alikes → a
  credential typed as the command word itself). Offered an allowlist of known command
  words, a further first-word shape check, or a comment fix; the operator chose the
  comment fix. So a secret typed AS the command (`sk-live-… run`) becomes the local label.
  It never leaves the machine. Pinned by `TestCommandWordCredentialCeiling`; the upgrade
  path is an allowlist.
- **2026-09-16** — **Late tool events record activity only; turns clear running calls
  (operator decision).** A late tool_end used to be dropped whole by the ordering guard,
  orphaning its call and pinning `current_tool`. Late tool events now pair and time but
  never move state or the staleness clock; late plan events stay dropped (an older plan
  update would regress a step). Every turn-ending event clears calls still marked
  running. Chosen over activity-only without a clear, and over an age-based sweep.
- **2026-09-16** — **A late tool_start from a finished turn is never marked running
  (operator decision).** The turn-end clear left a hole: a start stamped inside a turn
  but arriving after it ended reopened a call in a session waiting for the user. The
  machine now records the reporter timestamp of the last turn end; a late start stamped
  at or before it goes to the timeline only. Chosen over a state-based guard, which would
  let a previous turn's late start run into the next turn.
- **2026-09-16** — **Plan revision (operator-approved): the plan comes from Claude's Task
  tools, not TodoWrite.** Why: TodoWrite is disabled by default in favour of
  TaskCreate/TaskGet/TaskList/TaskUpdate, and on current models (Opus 5 / Sonnet 5)
  Claude Code provides NEITHER unless the session opts in
  (code.claude.com/docs/en/tools-reference, "Task tool availability"). As originally
  built, the pie would never have appeared on the default model. The spec and design
  doc named TodoWrite and it was carried into the plan without being verified — the
  hook events were checked against a live session, the tool the plan depends on was
  not. Every Task-tool shape below is from a captured payload, not from docs or memory.
- **2026-09-16** — Task tools are INCREMENTAL, so the daemon assembles the plan.
  TaskCreate carries no id in its input — Claude assigns it, and it appears only in the
  PostToolUse `tool_response.task.id`. TaskUpdate is `{taskId, status?, subject?}` and
  sends only what changed; `status: "deleted"` removes a task. `hived hook` is a new
  process per event and cannot hold the list, so a new `plan_item` event upserts one
  task by id and the daemon merges only the fields present. `TaskList`'s response is
  the complete list and is used as a free wholesale resync. TodoWrite stays as a
  wholesale `plan` for older configurations.
- **2026-09-16** — Only a successful PostToolUse mutates the plan. Why: a failed
  TaskUpdate did not change Claude's list, so it must not change Hive's.
- **2026-09-16** — **Hive opts Claude sessions into the task tools by default, with a
  Settings toggle (operator decision).** `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` at spawn,
  unless the user's own environment already sets that variable — an explicit choice is
  never overridden. Why: without it the feature is dead on the default model. Mirrors
  the spec's existing rule for Pi's Hive-registered todo tool (Hive adds a tool, on by
  default, disableable). Cost, stated in the docs: the tools spend context on every
  session — the toggle is the mitigation. Read at spawn, so the toggle affects newly
  started sessions only.
- **2026-09-16** — The setting lives in `internal/agent`, persisted beside
  `agents.json`, written by the GUI and read by the daemon at spawn. Why: that is
  exactly how `agents.json` already works (GUI `SaveCustomAgents` writes it,
  `cmd/hived/main.go` reads it), it is agent config rather than registry state so the
  "registry is the only writer" hard rule is not in play, and it needs no wire change.

- **2026-09-16** — Hook fixtures now mirror payloads captured from a live Claude
  session rather than hand-written guesses. Why: the hand-written
  `post_tool_use_failure.json` carried only `error`, which made the `ok=false`
  arm look unreachable and hid that the failure path carries `tool_input` and
  `tool_use_id` like every other tool event.

- **2026-09-16** — `serveEvent` drains its ModeEvent connection (bounded at 8
  frames) rather than reading exactly one. Why: a TodoWrite payload is two
  events and both ride one connection; dialing twice would double the handshake
  on the daemon's hottest path. An invalid frame still drops the connection, so
  the existing "refused input ends the conversation" contract is unchanged.
- **2026-09-16** — The command-head rule cuts at a shell metacharacter FIRST and
  then keeps at most two tokens. Why: neither half is sufficient alone —
  metacharacter-only would ship `curl -H "Authorization: …"` whole, and
  two-tokens-only rendered `sleep 12;` with the separator glued on.
- **2026-09-16** — The compact-density column widening is scoped with `:has()`.
  Why: unscoped, it cost every row 10px of title at that density, including the
  sessions with no plan at all. Caught by the truncation assertion.
- **2026-09-16** — The ACTIVITY delta comes from an explicit `lastDelta` field,
  not from the last ring entry. Why: a `tool_start` carrying a call_id goes into
  the pending map rather than the ring, so a ring-derived delta broadcast
  *nothing at all* when a tool began — clients would only ever learn that tools
  had ended.

- **2026-09-16** — Split into 3 phases; this plan is Phase 1 (data plane + sidebar).
  Why: the spec spans Go daemon, hand-encoded TypeScript, a new settings round-trip and
  three React surfaces — far past one reviewable PR.
- **2026-09-16** — The plan indicator sits under the state icon (`grid-column:1/grid-row:2`),
  not behind the agent code, and is a **filled pie**, not a hollow ring. Why: that cell is
  empty today, so the mark cannot affect row height and never collides with
  `--agent-color`; but the column is 14px, and at 12px a ring's arc is a hairline while a
  pie stays legible. Decided against the placement mockup.
- **2026-09-16** — At compact density the mark moves to row 1 and column 1 widens 14px→24px.
  Why: compact has no row 2. Cost: 10px off the title track, asserted by a truncation test
  rather than eyeballed.
- **2026-09-16** — `GET_ACTIVITY` is refused outright for `ModeSession` rather than scoped
  to the caller's own session. Why: the only `ModeSession` dialer is `hive idea`, nothing
  would consume activity there, and `ownSessionOnly` already sets a "bare minimum"
  precedent. Opening it later is ~5 lines; withdrawing it later is a breaking change.
- **2026-09-16** — Phase 1 staleness rides `state_source` and is knowingly partial: a plan
  orphaned by a hook that died while the session was at rest renders as live. Why:
  staleness is in the spec's Desired behavior, not its Success criteria, and the age
  display is Phase 3, whose `ActivityMsg.StaleAt` is the real fix.
- **2026-09-15** — Ring and plan snapshot live on `agentstate.Machine`, not as a
  sibling field on `registry.Entry`. Why: `Machine` is already created, replaced and
  freed at the three lifecycle sites (`registry.go:190,401-407,1387`), so the spec's
  "trimmed on session close, no second place to leak" costs zero new code.
- **2026-09-16** — **Subagent attribution deferred to a Phase 1b follow-up (operator
  decision, option B).** Claude's tool hooks fire inside subagents with `agent_id` /
  `agent_type`, which Phase 1 ignores. Chosen: tag subagent events on the wire, drive
  `current_tool` and the plan from the main thread only, and surface a running-subagent
  count. Rejected: adding the fields now with no behaviour (A — Phase 1 is nearly done,
  and fields without the semantics would need a second pass anyway) and deferring
  entirely (C — Phase 2 would shape Pi's wire work around a single-agent assumption).
  Compared against herdr (HEAD 2026-09-16): it tracks lifecycle state only, detects
  Claude by screen manifest, and has no plan/tool/subagent view, so it offers no prior
  art for this layer.
- **2026-09-16** — **Phase 1b Step 0 run headless (operator decision).** `claude -p
  --settings <tmp>` with a capture hook, so the operator's settings and sessions stayed
  untouched. Two runs cost $0.12 (haiku) and one sonnet run. Findings are in the Phase 1b
  section.
- **2026-09-16** — **Phase 1 follow-ups ship as their own PR (#419), not inside 1b
  (operator decision).** These are the rune-safe Text/Target cap, the registry-test
  `SHELL` isolation and the missing `.changesets/README.md`. They don't depend on 1b.
- **2026-09-16** — **`subagents_running` comes from new `subagent_start` / `subagent_end`
  kinds (operator decision).** Rejected: counting from `Stop.background_tasks`. It never
  sees foreground subagents, and it stays stale-high between Stops. The new kinds need a
  `DaemonContract` bump, because an old daemon drops unknown kinds.
- **2026-09-16** — **Count placement is picked from a mock at the 1b plan stop (operator
  decision).**
- **2026-09-16** — **Placement A: numeral badge on the plan pie (operator decision).**
  Rejected B (chip in row 1, takes width from the name), C (orbit segments, hard to count
  past 4) and D (line-2 prefix, costs subtitle characters). Mock:
  https://claude.ai/artifact/G8wkTR3asbCuyoUNuwPcqE.
- **2026-09-16** — **No version gate for the subagent hooks.** `SubagentStart` shipped
  in Claude Code 2.0.43, which predates `minHooksVersion` 2.1.0, so both hooks exist
  wherever Hive registers hooks at all.
- **2026-09-16** — **Plan pie redrawn as an outlined pie (operator decision, from
  screenshots).** The operator reported the pip as misaligned and odd. It measured
  centred under the icon. The actual faults were two: the 22% colour-mix track turned
  into a slate-grey disc on dark themes (Hex), and centring in row 2 sank the pip to the
  row's bottom edge when the row had no title. The pip is now a 1.5px outline in the
  state colour with a solid fill, top-aligned in row 2. Both faults are pinned by
  Playwright tests that fail on the old CSS.
- **2026-09-16** — **Late plan updates are judged per step, and the fix ships in the 1b
  PR (operator decision).** Found live: this session was 3 of 4 steps through its task
  list and the sidebar showed 1/4. Claude runs one message's task-tool calls in
  parallel, and their `plan_item` events arrive inverted. The late path dropped every
  plan item, losing completions until the next `TaskList`. Each step now keeps the stamp
  of its newest update, and a late update applies unless it is older for that same
  step. Rejected: accepting late items unconditionally, which would let a completed step
  regress. A late wholesale `plan` is still dropped.
- **2026-09-16** — **Phase 1b renumbered to Phase 2 of 4 (operator decision).** The merge
  gate needs an integer `Phase: N of M`. Pi moves to Phase 3, and the inspector panel and
  activity grid to Phase 4. Forward-looking text in this plan, the spec and the design
  doc was renumbered. Append-only history (decision log, ledger, gate verdicts, progress)
  keeps the "1b" name it was written with.

- **2026-09-16** — **Phase 3 reset to RESEARCH, not IMPLEMENT (operator decision).** Phase 2 merged in #420. The Approach covers Phases 1–2 only; Phase 3 (Pi tier + settings path) has a scope line and no approved design, so it gets its own research and plan approval.

- **2026-09-16** — Phase 3 clarifying round (operator answers):
  - Pi tool name **`hive_todo`**, not `todo` — Pi's example extension registers `todo`; loading both would clash.
  - **Separate Pi checkbox**: new `pi_todo_tool` key in `agent-settings.json` (default on) + its own env var, not a relabelled `claude_task_tools` (key and Claude-specific hint already shipped).
  - **Shared JSON label vectors** read by both `toollabel_test.go` and `hive.test.ts`; known Go/JS URL divergences pinned per language.
  - Plan pie empty after a daemon restart (operator report) is a **separate follow-up**, not Phase 3 scope. Live Claude sessions revived at 16:14:08 carry `CLAUDE_CODE_ENABLE_TODO_TOOLS=1`, so the flag is not reset; hypothesis is the in-memory plan being lost while resumed conversations never re-send it. Unconfirmed.

- **2026-09-16** — Phase 3 wedged-daemon queue policy: drop the **oldest** pending report when 64 are queued (a 64 × 2 s backlog would otherwise outlive `HookStaleAfter` and discard the freshest state). Mixed-version downgrade (older GUI rewrites `agent-settings.json` without `pi_todo_tool`, turning a user's "off" back on) accepted as harmless.

- **2026-09-16** — Phase 3 implementation deviations from the approved plan:
  - **Load probe replaced by live probes.** `pi -e <ext> --list-models` exits 0 even when an extension throws on load (checked with a deliberately throwing extension), so the planned no-token `TestPiProbeExtensionLoadsWithTodoTool` would have been vacuous. Replaced by `TestPiProbeTodoToolPlan` (real pi calls `hive_todo` → plan 1/2 + a completed `hive_todo` in the activity ring) and `TestPiProbeTodoToolOff` (setting off → plan_total 0), one API call each. The existing probe body became the shared `startPiProbe` helper.
  - **Step 0 verified against the running pi 0.85.1** (global install, not pi-devkit's 0.85.0): `tool_execution_end.result` carries `details` (`agent-loop.js` `emitToolExecutionEnd`), the toolResult message keeps `isError` and `details`, `session_start.reason` and `session_tree` exist as planned. No API deviation.
  - **Shared vectors carry more than the Go table did**: non-object inputs, a non-string value falling through to the next key, look-alike hyphens, U+0085 / U+FEFF word-splitting, the two documented ceilings, and one pinned URL divergence (`want_go` / `want_ts`). The ceilings now fail rather than skip if raised; the Go `Skip` tests remain as the documented escape hatch.

## Progress

- **2026-09-15** — RESEARCH complete; three-way fan-out (Go daemon, frontend, Pi
  extension + brain) reconciled against the spec and design doc.
- **2026-09-16** — PLAN approved via the HTML plan review (round 1, no revisions
  requested) after two rounds of adversarial second opinion. Stage → IMPLEMENT.
- **2026-09-16** — Phase 1 implemented and pushed as PR #417. Stage → REVIEW.
- **2026-09-16** — Review loop stopped by the operator: TodoWrite premise disproven.
  Plan revised to the task tools + opt-in; implemented on the same branch. Real-Claude
  probe passes (plan_total 2, plan_done ≥1 in 8.7s on Opus 5); negative control with
  the setting off confirmed plan_total 0.

- **2026-09-16** — Gate NEEDS_FOLLOWUP (phase 1/3); doc accuracy: three stale "reads exactly one frame" comments — internal/wire/control.go:19-23, internal/wire/frame.go:131-135, internal/daemon/daemon.go:654-658.
- **2026-09-16** — Gate follow-up fixed on this branch in f8b92755 (operator decision: hold at GATE, fix, re-gate). Four comments corrected to match serveEvent draining up to eventMaxFrames: internal/wire/control.go (ModeEvent, AgentEvent), internal/wire/frame.go (FrameAgentEvent), internal/daemon/daemon.go (eventReadDeadline). The fourth (the AgentEvent doc) was found by sweeping for the same claim. Comment-only — verified no non-comment line changed — so it was NOT put through another review-loop round.
- **2026-09-16** — Phase 1b implemented on `feature/416-phase-1b`: hook mapping, wire
  fields and kinds, daemon caps, machine subagent rule with split ordering clock, count
  and reconcile, registry copy, sidebar badge, contract 12, docs, changeset. Mutation
  checks confirmed that the split-clock, subagent-state and clock-step tests each fail
  when their fix is reverted. Live probes `TestClaudeProbeTaskToolsOptIn` and
  `TestClaudeProbeSubagentCount` passed (`HIVE_PROBE_CLAUDE=1`).
- **2026-09-16** — Phase 2 merged (#420). Phase reset: `Phase: 3 of 4`, PR/Branch cleared, spec stage GATE → RESEARCH on `feature/416-phase-3`.

- **2026-09-16** — Phase 3 PLAN approved via the HTML plan review (round 1, no feedback) after two second-opinion rounds (revise → revise, all must-fix applied). Stage → IMPLEMENT.

- **2026-09-16** — Phase 3 implemented on `feature/416-phase-3`: Pi extension tool events, `hive_todo` tool + plan (live and rebuilt on `session_start` / `session_tree`), serialized bounded send queue, TS label port over shared vectors, `pi_todo_tool` setting → `HIVE_PI_TODO_TOOL`, Settings checkbox, docs, changeset. Checks: `scripts/test.sh` green (go · 517 unit · 809 dom · 341 e2e), `scripts/ui-lint.sh`, `biome ci`. Mutation checks, each confirmed failing its test: vector flip (Go and TS), `eventBody` spreading the event, removed env gate, failed call posting a plan, bogus kind inside `send([...])`, unserialized queue, `session_start` skipping the empty plan, rebuild ignoring `isError`, queue dropping newest, synchronous throw stalling; plus raw `args` added at the call site still dropped by `eventBody` (test passes, as intended). Live probes `TestPiProbeReportsThroughTheExtension`, `TestPiProbeTodoToolPlan`, `TestPiProbeTodoToolOff` pass (`HIVE_PROBE_PI=1`, pi 0.85.1); the Off probe fails (plan_total 1) with the env gate removed.

## Open questions / risks

- **TodoWrite's payload shape is undocumented.** Mitigated by reading `content` with an
  `activeForm` fallback and defaulting unknown statuses to `pending`, both covered by the
  table test. If the real shape differs, the fallback degrades to an empty plan rather
  than a wrong one — but it will need a fixture refresh against a live session.
- **Compact column widening is the one regression risk in the UI.** 10px off the title
  track at the density whose whole point is the title. Measured by the truncation
  assertion above rather than eyeballed; if it reads badly, hiding at compact is the
  fallback and costs one CSS line.
- **`--agent-color` collision is now moot** — the mark no longer sits behind the agent
  code. Contrast still checked via `scripts/ui-lint.sh --contrast`.
- **`ui-lint.sh` has no opinion on `conic-gradient`, `color-mix` or `width: 12px`** —
  it polices raw hex, `font-size:<n>px`, `border-radius:<n>px` and icon glyphs only.
  Confirmed, so this is no longer a risk and needs no `--hv-plan-track` fallback.
- **Theme preset count in the design doc is wrong.** `themes.css` defines **20** presets;
  the doc says six and `icon.css:28` says 18. Fix the doc to say "every preset" rather
  than a number that keeps going stale.
- ~~**`PostToolUseFailure` may be dead weight.**~~ **RESOLVED against a live session
  (2026-09-16).** Ran `exit 3` through a real Claude session with a capture hook on all
  three tool events. `PostToolUseFailure` fired and `PostToolUse` did **not** — they are
  mutually exclusive, so the `ok=false` arm is reachable and `ok` is not permanently true.
  The capture also corrected an assumption: the failure payload carries **`tool_input` and
  `tool_use_id`**, not just `error`, so it derives a label and pairs like any other tool
  event. It additionally carries `error`, `is_interrupt` and `duration_ms`.
  The three hand-written fixtures were unrealistic (no `tool_use_id`, no `tool_input` on
  the failure) — that is what made this look like a risk — and have been replaced with the
  captured shapes, plus `TestHookFailureCarriesToolAndCallID`.
  Claude's own `duration_ms` is deliberately **not** used: durations stay on the daemon's
  clock, per the spec.
- **Phase 1 emits `plan` only from Claude.** Pi sessions show the agent code alone until
  Phase 3, which is the designed empty state, not a regression.
- **Phase 1 misattributes subagent activity.** Sessions that fan out (subagents,
  background workflows) show a flickering `current_tool`, subagent calls tallied on the
  parent's plan step, and possibly subagent tasks in the parent plan. Known and accepted
  for Phase 1; fixed by [Phase 2](#phase-2--subagent-attribution-follow-up).

