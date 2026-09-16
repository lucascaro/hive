# Agent activity view: see the plan and the tools, not just the state

- **Spec:** [docs/product-specs/416-agent-activity-view.md](../../product-specs/416-agent-activity-view.md)
- **Issue:** — (locally allocated number; **not** a GitHub issue. PR #416 on GitHub is
  `feat: add Alucard and Hex theme presets`, an unrelated merged PR. Never write `Fixes #416`.)
- **Design:** [docs/design-docs/agent-activity.md](../../design-docs/agent-activity.md)
- **Phase:** 1 of 3
- **PR:** #417
- **Branch:** feature/416-agent-activity-view
- **Mocks:** https://claude.ai/artifact/7RjZw99RbKNV1iS13r2dtb (placement study — pie vs ring)
- **Status:** active

## Summary

Stop discarding the tool name, tool arguments and plan that the Claude hook
tier already delivers (Phase 1; the Pi extension tier sends none of this today
and gains it in Phase 2), keep a bounded per-session ring of
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

## Phase split (operator-approved)

- **Phase 1 (this plan)** — data plane + sidebar. wire frames/kinds/fields,
  agentstate ring + plan, hook.go split + label derivation, 3 wire clients,
  DaemonContract 10→11, SessionRow plan pie.
- **Phase 2** — Pi tier: `tool_execution_*` → tool_start/tool_end, `registerTool('todo')`,
  and the settings path (persisted field → Wails → UI → a channel hive.ts can read).
- **Phase 3** — inspector panel + activity grid.

Phase 1 ships standalone value: Claude sessions get a live plan indicator in the sidebar.

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
      // Reserved for Phase 3's age display. Set by the daemon from its own
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
the existing `state_source` as a proxy: `state_source !== 'hook'` (and, in Phase 2,
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
its *Success criteria*, and the panel-and-tile age display it describes is Phase 3. The
real fix is `ActivityMsg.StaleAt`, carried from the daemon's own clock and consumed by the
Phase 3 renderers.

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

## Decision log

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
  Phase 2, which is the designed empty state, not a regression.

