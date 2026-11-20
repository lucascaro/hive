# Pin important agent responses to the session's inspector panel

- **Spec:** [docs/product-specs/566-pin-important-agent-responses-to-the-session-s-ins.md](../../product-specs/566-pin-important-agent-responses-to-the-session-s-ins.md)
- **Issue:** #566
- **Status:** active
- **PR:** [#568](https://github.com/lucascaro/hive/pull/568)
- **Branch:** feature/566-pinned-responses

## Summary

Agents (Claude, Pi) pin important responses — title + body — to a per-session "Pinned" section of the ⌘J inspector, with a sidebar badge, activity-grid display, copy, unpin and unpin-all. Pins persist with the session.

## Research

FE = `cmd/hivegui/frontend`.

### Relevant code — daemon / agents
- **Claude launch.** `claudeSpawnArgs` (`internal/agent/claude.go:319-348`) passes only `--settings <json>` (hooks → `hived hook`, additive to user hooks). There is no `--append-system-prompt` and no `--mcp-config` for PTY Claude. The only MCP precedent is ACP's `hived mcp-submit` (`cmd/hived/mcpsubmit.go:24-58`, wired at `internal/registry/acp.go:248-285`). `claudeHooksAvailable` (`claude.go:388-396`) gates everything Hive adds to a Claude launch.
- **Claude instructions.** The SessionStart `additionalContext` comes from `sessionStartOutput` plus `taskToolsNudge` (`cmd/hived/hook.go:199-234`). It fires again on resume and on compaction. The task-tools nudge is gated on `CLAUDE_CODE_ENABLE_TODO_TOOLS`, so a pin nudge needs its own Hive-owned env var.
- **Settings.** Stored in `agent-settings.json` (`internal/agent/settings.go:44-203`): `*bool` fields with omitempty, so a missing key means the default. `SaveSettings` copies fields one by one, so a new field must be added there.
  - Env injection lives in `claudeSpawnEnv` (`:236-256`) and `piSpawnEnv` (`:267-285`). Hive-owned vars are set explicitly both ways. The "never override the user" check is the `lookupEnv` test at `:248`.
  - GUI mirror: `cmd/hivegui/app_calls.go:148-190`. Settings UI: `FE/src/components/modals/Settings.tsx` (`:218`, `:437`, `:779`, `:929-962`).
- **Session to daemon.** Every spawn gets `HIVE_SESSION_ID`, `HIVE_SOCKET` and `HIVE_SESSION_TOKEN` (`internal/registry/registry.go:1106-1150`). The token rotates on every spawn.
  - `ModeSession` verbs are listed in `sessionModeFrames` (`internal/daemon/daemon.go:1412-1426`).
  - `hived idea` (`cmd/hived/idea.go:255-287`) is the template for a session-callable subcommand: no grant needed, and the session id is taken from the HELLO.
  - `hived` is **not** on PATH inside sessions. Hooks use its absolute path (`claude.go:402`).
- **Pi.** The extension is `internal/agent/pi/hive.ts`.
  - `hive_todo` is registered at `:698-717` and gated by `HIVE_PI_TODO_TOOL` (`:651`).
  - `submitResult` (`:327-385`) is a tool `execute` that round-trips a ModeSession frame and waits for OK/ERROR. That is the model for a `hive_pin` tool.
  - System-prompt append: `before_agent_start` (`:789-796`). ACP Pi forces the todo tool off (`registry/acp.go:274`).
- **Storage.**
  - Activity lives in memory in agentstate (`internal/agentstate/activity.go:20-30`) and is replaced on restart or revive. **Pins must not live there.**
  - Per-session `sessions/<id>/session.json` holds `MetaFile` (`internal/registry/persist.go:16-60`), written with `writeAtomic` (fixed `.tmp`, no fsync, `:111-128`). `writeDurable` is in `archive.go:48-77`.
  - Ideas are stored one file per item (`persist.go:83-109`).
  - Close (`kill`, `registry.go:1835+`) writes a tombstone from `MetaFile`, then `RemoveAll(sessions/<id>)` (`:1985`, `:2009`).
  - Reopen restores from the tombstone (`closed.go:361-371`). Tombstones are pruned at 20 entries or 7 days (`closed.go:30-37`, `293-303`), and `discardRecord` (`:314-324`) removes the record and patch.
  - Archive writes `archived/<id>.json` (`archive.go:83-95`). Unarchive is at `:158`; `DeleteArchived` is about `:208`.
- **Wire.**
  - Highest frame id is 0x59 (`internal/wire/frame.go:324`). Every new frame must be classified (`frame.go:335-368`, `TestEveryFrameClassified`).
  - `ActivityMsg` / `GetActivityReq` are at `control.go:850-878`. `SessionInfo.PlanDone/PlanTotal` (`control.go:324-333`) is the model for `PinCount`.
  - Fan-out: `registry/events.go:24,128`; daemon loop `daemon.go:1272-1291`; GET_ACTIVITY `daemon.go:1918-1931`.
  - Wire changes go to three clients in lock-step: `cmd/hivegui/app_calls.go`, `cmd/hived-ws-bridge/main.go` (`:465`) and `internal/wire/testclient` (`:448`).
- **Trust-boundary caps.**
  - `MaxPayload` is 1 MiB per frame (`frame.go:38`). Activity caps are at `control.go:805-823`.
  - Ideas reject over the cap rather than truncate (`MaxIdeaText` 4 KiB, `ErrCodeIdeaTooLong`, `control.go:1021-1024`). `MaxMessageLen` is 64 KiB (`:1872`).
- **DaemonContract.** Currently 38 (`internal/buildinfo/contract.go:329`). This feature must bump it.

### Relevant code — GUI
- **Activity plumbing.**
  - Wire to Wails event map: `internal/wire/client.go:162`. Handler: `FE/src/app/events.ts:823`.
  - `forgetActivity` on restart (`events.ts:897-904`) must not touch pins.
  - Store: `FE/src/store/activity.ts`, a separate zustand store with a snapshot/delta protocol. `useNow()` and `formatAge` (`FE/src/lib/activity.ts:232`) are reusable for pin ages.
- **Panel** (`FE/src/components/activity/ActivityPanel.tsx:33-79`).
  - Layout: head, then plan `<ol>`, then the Timeline header and `<ol>`. The empty state replaces everything below the head.
  - CSS is global BEM (`FE/src/theme/components/activity.css`). The plan is `max-height:50%` (`:112`) and the timeline is `flex:1; min-height:0` (`:167`).
  - No focus steal is enforced by `keepFocus` (`shared.tsx:62-64`, mousedown preventDefault).
  - The host remounts on session switch (`ActivityPanelHost.tsx:19-31`, `key={sessionId}`).
  - `.hv-activity` has `user-select:none`.
- **Tile** (`ActivityTile.tsx:31-138`, `activity.css:54-79`). Tiles are inert by design and must stay non-interactive.
- **Mutations.**
  - Pattern: `requireControl` then `WriteJSON` (`app_calls.go`). Models: `RemoveIdea` (`:1193`), `SetSessionOrchestrator` (`:1027`), `GetActivity` (`:915`).
  - FE: explicit re-exports in `FE/src/bridge.ts`.
  - Mocks: `FE/test/e2e/wails-mock.ts` (`ArchiveSession` `:647`, `GetActivity` `:1413`, `__hive.setActivity` `:2329-2351`) and `FE/test/e2e-real/wails-bridge.ts`.
  - Every dom test's `vi.mock('../../src/bridge.js')` export list must grow too.
- **Confirm.** `openChoiceDialog` (`FE/src/app/modals/choice-dialog.ts:100`), example `idea-inbox.ts:172-185`.
  - **Gap:** `ChoiceDialog.tsx:96-118` passes no `hints` to `ModalShell`, so no confirm shows its keys today, which violates AGENTS.md's design rule. `ModalShell` supports `hints` (`ModalShell.tsx:53-54`).
- **Clipboard.** `SetClipboardText` (Go `cmd/hivegui/app.go:195`; used at `FE/src/app/actions.ts:432`). Never `navigator.clipboard`. The mock clipboard (`wails-mock.ts:79-87`) has no `__hive` getter.
- **Sidebar row.**
  - `FE/src/components/SessionRow.tsx` reads SessionInfo only. The idea indicator (`:318-327`) is the badge pattern.
  - Grid and densities: `FE/src/theme/components/session-row.css:20-39`, `:415-490`.
  - Height tests to mirror: `FE/test/e2e/sidebar-plan-pie.spec.ts:118`, `:275`.
- **Markdown.** `FE/src/components/Markdown.tsx` is safe for untrusted text (lexer only, no innerHTML, links are inert). The `hv-md-*` styles live only in `theme/components/acp.css`.
- **Icons.** Sprite-only (`FE/src/lib/icons.svg`, `ICON_NAMES` in `FE/src/lib/icon-sprite.ts:21-54`, `docs/design-docs/ui/icons.md`). There is no pin, copy or trash icon yet.
- **Commands.** The inspector has no per-item commands today (`FE/src/app/commands.ts:263-270`). A new key scope needs a FIXTURES entry in `every-shortcut.spec.ts`.
- **Tests to extend.**
  - `FE/test/unit/activity.test.ts`
  - `FE/test/dom/activity-panel.test.tsx`
  - `FE/test/dom/settings.test.tsx:268-406`
  - `FE/test/e2e/activity-panel.spec.ts` (`:151` focus template)
  - `FE/test/e2e/activity-grid.spec.ts` (`:341`, `:401`)
  - `FE/test/e2e/sidebar-plan-pie.spec.ts`

### Constraints / dependencies
- **Pin lifetime.** Pins must outlive restart and revive, and so live outside agentstate. A close or archive `RemoveAll`s the session dir, so pins must be carried into the tombstone or archive record and restored on reopen/unarchive. `discardRecord` must also delete them, or they leak.
- **Size.** 1 MiB frame cap versus "no user-facing caps". A full pin snapshot must fit in one frame, or be paged or sent per pin.
- **Claude channel.** Hooks cannot add a tool. Two options:
  - A Bash CLI: absolute path, quoting, possible permission prompt per pin.
  - An MCP server via `--mcp-config`: a new surface for PTY Claude, deferred-tool discovery.
- **ACP sessions.** ACP strips `HIVE_*` (`internal/acp/agent.go:61-80`).
- **Collisions.** `PlanStep`'s local `pinned` state (`ActivityPanel.tsx:96`) and the launcher's `pinned` agents (`Settings.tsx:426`). Rename the former.

### Prior lessons
- *narrowing-a-capability-means-scoping-data-not-just-verbs*: a verb on the session allowlist is not enough.
  - Overwrite the payload's session id with the HELLO's.
  - Scope reads, writes and subscriptions.
  - Test with a second session that neither read nor write crosses.
- *driving-agent-clis-headlessly-from-an-agent-session*: when probing real claude or pi, strip only `CLAUDE_CODE_CHILD_SESSION` and `HIVE_SOCKET`. Reuse `scripts/acp-probe/`. Mutation-check the guards, because tests passed vacuously.

### Conventions card
- **Build:** `./build.sh`
- **Test:** `scripts/test.sh` (layers go · unit · dom · e2e); `npm run test:e2e:real` for real-daemon e2e (isolated `HIVE_SOCKET` + `HIVE_STATE_DIR`).
- **Toolchain:** run Go under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- **Static analysis:** `for os in darwin linux windows; do GOOS=$os staticcheck ./...; GOOS=$os go vet ./...; done`.
- **Lint:** `biome ci .` and `scripts/ui-lint.sh --strict --contrast`; `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree).
- **Gates:** `scripts/check-daemon-contract.sh`, `scripts/check-changeset.sh`.
- **Rules:**
  - TDD: a test comes with every change.
  - Playwright runs with `CI=1`; vitest is CSS-blind.
  - Wire JSON is snake_case.
  - Shell out only via `proc.Command`.
  - Keys are data (no hard-coded key labels).
  - Destructive actions go through the confirm overlay, with its keys shown.
  - A user-visible change adds `.changesets/<slug>.md` and a `site/features.json` entry (`since: "Unreleased"`).
  - Update README, help and `DESIGN.md` if wire or persistence rules change.

## Approach

Pins live on the registry `Entry`, not in agentstate. They are persisted one file per pin and reach the GUI through their own frames. Agents add a pin through a single session-mode verb, `ADD_PIN`, carried by one of two channels:

- **Claude:** a new stdio MCP server, `hived mcp-pin`, exposing one tool named `pin`.
  - PTY Claude gets it through `--mcp-config`.
  - ACP Claude gets it as a second `acp.MCPServer`.
- **Pi:** a native `hive_pin` tool in the Hive extension.

The GUI reads `pin_count` from `SessionInfo` for the sidebar badge. Pin headers are fetched in pages, and a body is fetched when its pin is expanded.

**Why this design.**

- **Why not fold pins into the activity ring.** The ring is in memory and is wiped on restart and revive. The spec requires pins to survive both.
- **Why not one `pins.json` file.**
  - `writeAtomic` uses a fixed `.tmp` name.
  - Pins are append-mostly.
  - A pin is up to 64 KiB, so rewriting the whole file on every add costs O(total).
  - One file per pin follows the ideas precedent (`persist.go:83-109`).
- **Why headers-in-pages plus body-on-demand instead of one full snapshot frame.** There is no count limit (operator decision), so a full snapshot can exceed `MaxPayload` (1 MiB).
- **Why MCP and not a Bash CLI.** The CLI cannot work under ACP, which strips `HIVE_*`. It would also need an absolute path with spaces, heredoc quoting, and a Bash permission prompt (operator decision, after seeing these tradeoffs).

### Data model and wire (`internal/wire`)

- **`Pin` type.**
  - Fields: `{ID string; Title string; CreatedAt int64 /*unix ms, daemon clock*/; BodyLen int}`. These are the headers.
  - `PinBody{SessionID, ID, Body}`.
  - `ID` is daemon-generated, time-sortable and unique: 16 hex chars of unix-nanos followed by 8 random hex.
- **New frames.** Each is classified in `frame.go`, and `TestEveryFrameClassified` enforces that.
  - `FrameAddPin` (session → daemon, `ModeSession` only): `AddPinReq{Title, Body}`, answered by `FrameAddPinOK{ID}` or `ERROR`.
  - `FrameGetPins` (control): `{SessionID}`. The answer is one or more `FramePins{SessionID, Pins []Pin, Done bool}` pages of at most 200 headers each, newest first.
  - `FrameGetPinBody` (control): `{SessionID, ID}`, answered by `FramePinBody`.
  - `FrameUnpin` (control): `{SessionID, ID}`. An empty `ID` is **not** allowed. Unpin-all is its own frame, so a missing field can never mean "all" (prior lesson).
  - `FrameUnpinAll` (control): `{SessionID}`.
  - `FramePinsChanged` (control event): `{SessionID, Added *Pin, RemovedIDs []string, Cleared bool}`. This is the delta broadcast.
- **`SessionInfo.PinCount`** (`pin_count,omitempty`). A session update is broadcast on every pin change.
- **Limits.**
  - `MaxPinTitleLen = 256` and `MaxPinBodyLen = 64 << 10`.
  - Empty title, empty body, invalid UTF-8, or over the limit is rejected with `ErrCodePinInvalid` / `ErrCodePinTooLong`. There is no truncation.
  - These limits are trust-boundary bounds, not the user-facing caps the spec ruled out.

### Daemon and registry

- **`internal/registry/pins.go` (new).**
  - `AddPin(id, title, body)`, `Pins(id)`, `PinBody(id, pinID)`, `Unpin(id, pinID)` and `UnpinAll(id)`.
  - Storage is `sessions/<id>/pins/<pinID>.json`, written with `writeAtomic` to a per-pin temp name. Each file holds `{header, body}` with the header first.
  - **Concurrency invariant.**
    - Every pin mutation and the lifecycle moves hold a per-entry `pinMu`. That covers AddPin, Unpin, UnpinAll, and the pins moves in kill, archive, unarchive and reopen.
    - `kill` takes `pinMu` and sets `e.pinsClosed = true` before renaming `pins/`. A later AddPin or Unpin sees the flag and returns `not_found`.
    - Disk I/O happens under `pinMu` but outside `r.mu`, so a pin write can never recreate a removed `sessions/<id>/` directory or leak into `closed/<id>.pins`.
    - **Lock order: `r.mu` before `pinMu`, never nested the other way.**
      - AddPin and Unpin look up the entry under `r.mu`, release it, then take `pinMu`.
      - `kill` takes `pinMu` only after releasing `r.mu`.
      - The `pins:changed` and SessionInfo broadcasts run after `pinMu` is released.
    - **`pinsClosed` is set only after kill's point of no return**, that is, after the entry has been removed from `r.entries`. A refused kill (dirty worktree, or `force=false`) leaves pinning working.
    - Reopen and unarchive build a fresh `Entry` with the flag clear.
  - `load()` reads only headers into memory, before revive. Bodies stay on disk and are read on `GetPinBody`.
  - **Startup sweep.** `closed/*.pins` and `archived/*.pins` dirs whose record no longer exists are removed. A crash between the rename and `RemoveAll` therefore cannot leak them.
- **Lifecycle.**
  - **Close.** In `kill`, before `RemoveAll(sessions/<id>)`, rename `sessions/<id>/pins` to `closed/<id>.pins` (`archived/<id>.pins` when `archive=true`).
  - **Restore.** Reopen (`closed.go:361`) and unarchive (`archive.go:158`) rename it back and reload.
  - **Discard.** `discardRecord` adds `os.RemoveAll(filepath.Join(dir, id+".pins"))`. That covers both tombstone pruning and `DeleteArchived`.
  - **Revive, restart and `/clear`** do nothing to pins.
- **`internal/daemon/daemon.go`.**
  - `FrameAddPin` goes in `sessionModeFrames`. The handler overwrites the session with `hello.SessionID`, because the payload has no session field at all.
  - Two text updates in `daemon.go`: the allowlist comment (`:1412`) and the hard-coded verb list in the `ErrCodeModeNotAllowed` message (`:1727`) both gain `ADD_PIN`.
  - The control frames are refused on session-mode and plugin connections (`pluginRefusedFrames`).
  - `FramePinsChanged` is dropped on session-mode connections, the same way activity is.
  - `GET_PINS` and `GET_PIN_BODY` check that the session exists.
  - **Cross-session isolation.** `Unpin` / `PinBody` with a pin id belonging to another session return `not_found`. The lookup is by `(sessionID, pinID)`, never by `pinID` alone.
- **`internal/buildinfo/contract.go`.** Bump `DaemonContract` from 38 to 39.

### Agent channels

- **`cmd/hived/mcppin.go` (new).** It runs `hived mcp-pin`.
  - The JSON-RPC loop is extracted from `mcpsubmit.go` into a shared `serveMCP(in, out, tools, call)` helper in `cmd/hived/mcpserve.go`. `mcp-submit` keeps its tests green on top of the helper.
  - Tool `pin`:
    - Schema: `{title: string, body: string (markdown)}`.
    - The description carries the "when to pin" guidance, so ACP Claude gets the instruction without a SessionStart hook.
  - It dials `ModeSession` with `HIVE_SESSION_ID`, `HIVE_SOCKET` and `HIVE_SESSION_TOKEN` from its environment, sends `ADD_PIN`, and waits for OK or ERROR with a 10 s deadline. A daemon error comes back as an `isError` tool result, so the model sees the reason.
- **Settings (`internal/agent/settings.go`).**
  - `PinTool bool` (default true; `*bool` in the file) and env `HIVE_PIN_TOOL`, set explicitly both ways in `claudeSpawnEnv` and `piSpawnEnv`, like `HIVE_PI_TODO_TOOL`.
  - `SaveSettings` writes the field.
  - Hive does not override a user setting here, because the variable is Hive-owned. Hive never touches the user's own MCP config or `CLAUDE_CODE_*` variables, and `--mcp-config` is additive: no `--strict-mcp-config`.
- **PTY Claude (`internal/agent/claude.go`).**
  - When `claudeHooksAvailable && settings.PinTool`, `claudeSpawnArgs` also returns `--mcp-config <json>`. The JSON declares server `hive-pin` as `{command: <hived>, args:["mcp-pin"], env: {"HIVE_SESSION_ID":"${HIVE_SESSION_ID}", "HIVE_SOCKET":"${HIVE_SOCKET}", "HIVE_SESSION_TOKEN":"${HIVE_SESSION_TOKEN}"}}`.
    - The variables are passed explicitly because Claude gives stdio MCP servers a filtered environment.
    - They use `${VAR}` expansion, never literal values, so the rotating token never appears in argv or `ps`.
  - The `--settings` JSON gains `permissions.allow: ["mcp__hive-pin__pin"]`, so pinning never prompts.
    - **This assumes Claude merges permission rules across settings sources, as it does hooks.** If it replaced them instead, the user's own allow and deny rules would silently vanish.
    - The required probe asserts that a user-level rule survives next to Hive's rule.
    - If that probe fails, the allow rule is dropped and the tool prompts once instead. That fallback is a one-line change and is recorded in the decision log.
  - The SessionStart `additionalContext` (`cmd/hived/hook.go`) gains `pinNudge` when `HIVE_PIN_TOOL=1`. It is independent of the task-tools gate.
  - **Off means none of the three:** no `--mcp-config`, no allow rule, no nudge.
- **ACP Claude (`internal/registry/acp.go`).**
  - When `PinTool` is on, `acpExtras` appends a second `acp.MCPServer{Name: as.server+"-pin", Command: hived, Args: ["mcp-pin"], Env: hive env}`.
  - The auto-allow check (`acp.go:641`) also accepts `acp.PinIdentities(server)`.
  - ACP Pi forces `HIVE_PIN_TOOL=0` (non-goal: ACP Pi).
- **Pi (`internal/agent/pi/hive.ts`).**
  - `pinTool = HIVE_PIN_TOOL !== "0"` registers `hive_pin`, with a `promptGuidelines` line for proactive pinning.
  - Its `execute` round-trips `ADD_PIN` through `sessionHello`, the same way `submitResult` does, and returns the error text when the daemon refuses.

### GUI (`cmd/hivegui`)

- **Go bindings.**
  - `app_calls.go`: `GetPins`, `GetPinBody`, `Unpin`, `UnpinAll`.
  - `AgentSettings.PinTool`.
  - `internal/wire/client.go`: map `FramePins` to `pins:page`, `FramePinBody` to `pins:body`, and `FramePinsChanged` to `pins:changed`.
- **Lock-step clients.** `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient`, `FE/src/bridge.ts`, `test/e2e/wails-mock.ts` (with `__hive.setPins` / `emitPinsChanged` / `clipboard()` helpers), `test/e2e-real/wails-bridge.ts`, and the `vi.mock` export lists in the touched dom tests.
- **Store: `FE/src/store/pins.ts` (new).**
  - A zustand store keyed by session: `headers` (newest first), `bodies` (Map), `loaded`, plus one global `collapsed` boolean.
  - It handles page, changed and body events, and requests pins on first view.
  - It is deliberately separate from `store/activity.ts`, so `forgetActivity` can never clear pins.
- **`ActivityPanel.tsx` — new Pinned section.**
  - **Placement and gating.**
    - It sits between the head and the plan, rendered outside the `!empty` gate.
    - It renders nothing when the session has no pins.
    - It renders the same component whether or not the agent supports pins. Agents without pin support never have any, which is the spec's "no affordance" for other agents.
  - **Header.** A disclosure, the label "Pinned", the count, and an "Unpin all" action.
  - **Rows.**
    - Each row shows the title and its age (`formatAge`).
    - Clicking a row expands it. The body is fetched on first expand and rendered with `Markdown`.
    - Each row has copy and unpin icon actions.
  - **Copy.** Uses `SetClipboardText` (body fetched first if needed).
  - **Unpin all.** Calls `openChoiceDialog({title:"Unpin all N pins?", choices:[Cancel, {label:"Unpin all", danger:true}]})`.
  - **Focus.**
    - Every action is a `tabIndex={-1}` element under `keepFocus`, so the terminal keeps focus.
    - The `PlanStep` `pinned` local state is renamed to `held` to avoid the name collision.
- **`activity.css`.**
  - `.hv-activity__pins` is `flex-shrink:0; max-height:30%; overflow-y:auto`.
  - The plan's 50% cap and the pins' 30% cap leave the timeline at least 20%.
  - Pin-body markdown styles: the `hv-md-*` rules move from `acp.css` to a shared `theme/components/markdown.css`, imported by both.
- **`ActivityTile.tsx`.** An inert, title-only pins list between the plan and the feed (`flex:0 1 auto; max-height:30%`). It has no buttons and no focusable elements.
- **`SessionRow.tsx` + `session-row.css`.** A `pin` icon plus count, rendered in the indicator cell next to the idea icon, from `pin_count`. It is hidden at 0 and stays within the row grid at every density.
- **`ChoiceDialog.tsx`.** It passes `hints` to `ModalShell` for every caller: `[enter] choose` and `[esc] cancel`. Enter and Esc are hard-coded overlay keys per AGENTS.md, so no binding derivation is needed.
- **Icons.** Add `hv-pin` and `hv-copy` symbols to `icons.svg`, to `ICON_NAMES`, and to `docs/design-docs/ui/icons.md`. Unpin reuses `hv-x`.
- **`Settings.tsx` (Agents tab).** A checkbox "Agents pin important responses (Claude, Pi)", id `settings-pin-tool`, with a hint that it applies to new sessions.

### Docs

- `DESIGN.md`: the new persisted per-session pins, the pin frames, and the `hived mcp-pin` subcommand.
- `docs/design-docs/agent-activity.md`: a short Pins section, including the reasoning on lifecycle and size limits.
- `README.md`: the feature mention. No keybinding is added, so the Keybinds table does not change.
- `.changesets/pinned-responses.md`: `type: added`, `bump: minor`, `issue: 566`.
- `.changesets/confirm-dialog-key-hints.md`: `type: fixed`, `bump: patch`. It covers the confirm-dialog key hints, which affect every dialog.
- `docs/design-docs/daemon-contract.md`: a line for contract 39, if that doc keeps a history.
- `site/features.json`: an entry with `since: "Unreleased"`.

## Files to change

1. `internal/wire/frame.go`, `internal/wire/control.go`: frames, types, limits, `SessionInfo.PinCount`.
2. `internal/wire/client.go`: Wails event mapping.
3. `internal/wire/testclient/*`: GetPins, Unpin, UnpinAll, AddPin helpers.
4. `internal/registry/registry.go`, `closed.go`, `archive.go`, `persist.go`: Entry pins, load, kill/restore move, `discardRecord`.
5. `internal/registry/acp.go`: second MCP server and auto-allow.
6. `internal/acp/permission.go`: `PinToolName`, `PinIdentities`.
7. `internal/daemon/daemon.go`: verb allowlist, handlers, broadcast filtering, plugin refusal.
8. `internal/buildinfo/contract.go`: bump to 39.
9. `internal/agent/settings.go`, `claude.go`: `PinTool`, env, `--mcp-config`, allow rule.
10. `cmd/hived/hook.go`: `pinNudge`.
11. `cmd/hived/main.go`: dispatch `mcp-pin`.
12. `cmd/hived/mcpsubmit.go`: move the JSON-RPC loop to `mcpserve.go`.
13. `internal/agent/pi/hive.ts`: `hive_pin` tool.
14. `cmd/hived-ws-bridge/main.go`: new frames.
15. `cmd/hivegui/app_calls.go`: bindings and `AgentSettings.PinTool`.
16. `FE/src/bridge.ts`, `FE/src/app/events.ts`: re-exports and event wiring.
17. `FE/src/components/activity/ActivityPanel.tsx`, `ActivityTile.tsx`, `shared.tsx`.
18. `FE/src/components/SessionRow.tsx`, `FE/src/app/state.ts` (`pin_count` reader).
19. `FE/src/components/modals/ChoiceDialog.tsx` (hints).
20. `FE/src/components/modals/Settings.tsx`.
21. `FE/src/theme/components/activity.css`, `session-row.css`, `acp.css`.
22. `FE/src/lib/icons.svg`, `FE/src/lib/icon-sprite.ts`.
23. `FE/test/e2e/wails-mock.ts`, `FE/test/e2e-real/wails-bridge.ts`.
24. `DESIGN.md`, `docs/design-docs/agent-activity.md`, `docs/design-docs/ui/icons.md`, `README.md`, `site/features.json`.

## New files

- `internal/registry/pins.go`: the pin store and persistence.
- `internal/registry/pins_test.go`
- `cmd/hived/mcpserve.go`: the shared MCP stdio loop.
- `cmd/hived/mcppin.go`
- `cmd/hived/mcppin_test.go`
- `FE/src/store/pins.ts`
- `FE/src/components/activity/PinnedSection.tsx`
- `FE/src/theme/components/markdown.css`
- `FE/test/unit/pins-store.test.ts`
- `FE/test/dom/pinned-section.test.tsx`
- `FE/test/e2e/pinned.spec.ts`
- `internal/daemon/pins_restart_test.go`
- `.changesets/pinned-responses.md`

## Tests

**Go**

- `internal/registry/pins_test.go`
  - `TestAddPinPersistsAndOrdersNewestFirst`
  - `TestAddPinRejectsInvalid`: covers empty title, empty body, invalid UTF-8, title over 256 B, and body over 64 KiB. Each must return an error and write no file.
  - `TestPinsSurviveRegistryReload`: a new Registry on the same state dir loads them.
  - `TestPinsSurviveRestartAndRevive`
  - `TestUnpinRemovesOnlyThatPin`
  - `TestUnpinAllClears`
  - `TestUnpinOtherSessionsPinIsNotFound`
  - `TestPinsFollowCloseAndReopen`
  - `TestPinsFollowArchiveAndUnarchive`
  - `TestDiscardRecordRemovesPins`: covers both tombstone pruning and `DeleteArchived`.
  - `TestPermanentCloseDropsPins`
  - `TestPinCountOnSessionInfo`
- `internal/daemon/*_test.go`
  - `TestAddPinOnSessionModeUsesHelloSession`: a payload naming another session has no effect.
  - `TestAddPinRequiresValidToken`
  - `TestPinControlFramesRefusedOnSessionMode`
  - `TestPinControlFramesRefusedForPlugins`
  - `TestGetPinsPagesUnderMaxPayload`: 5,000 pins, each with a 256 B title, which is more than 1 MiB of headers. It asserts:
    - more than one page arrives;
    - each page is at most 200 headers and under `MaxPayload`;
    - only the last page has `Done`;
    - the pages concatenate to every id, newest first.
  - `TestGetPinsDoneOnExactPageMultiple`: 400 pins produce exactly two pages, with no empty trailing page, and the second page has `Done`.
  - `TestPinsChangedBroadcastToControlNotSession`
  - `TestSessionCannotReadOtherSessionPins`: a second session, per the prior lesson.
- `cmd/hived/mcppin_test.go`
  - `TestMCPPinListsTool`
  - `TestMCPPinCallsAddPin`: uses a fake submit function.
  - `TestMCPPinSurfacesDaemonError`: returns `isError` with the reason.
  - The existing `mcpsubmit` tests stay green after the loop extraction.
- `internal/agent/claude_test.go`
  - `TestClaudeSpawnArgsAddsPinMCPWhenOn`: asserts the `--mcp-config` JSON names the hived path and `mcp-pin`. Its `env` block must use exactly the three `${HIVE_SESSION_*}` references, with no literal token. `--settings` must carry `permissions.allow` with `mcp__hive-pin__pin`.
  - `TestClaudeSpawnArgsNoPinWhenOff`: no `--mcp-config` and no allow rule.
  - `TestClaudeSpawnArgsNoPinWithoutHooks`
- `internal/agent/settings_test.go`
  - `TestPinToolDefaultOn`
  - `TestPinToolRoundTrip`: `SaveSettings` then `LoadSettings`.
  - `TestClaudeAndPiSpawnEnvPinTool`: sets `HIVE_PIN_TOOL` both ways.
- `cmd/hived/hook_test.go`
  - `TestSessionStartPinNudgeGatedOnEnv`: present when on; absent when off; independent of task tools.
- `internal/registry/acp_test.go`
  - `TestACPExtrasAddsPinServerForClaude`
  - `TestACPExtrasNoPinForPi`
  - `TestACPAutoAllowsPinTool`
- `internal/acp/permission_test.go`
  - `TestPinIdentities`
- `internal/wire`
  - `TestEveryFrameClassified` (existing; it must cover the new frames).
  - `TestPinFramesRoundTrip`
- **Required real-Claude probe** (a manual pre-merge gate: skipped in CI behind `HIVE_PROBE_CLAUDE=1`, and its result is recorded in the decision log with the Claude Code version). In `cmd/hived/claude_probe_test.go`, `TestProbeClaudePinsViaMCP` runs a real `claude -p` told to pin against an isolated daemon (temp `HIVE_SOCKET` and `HIVE_STATE_DIR`). It asserts:
  - a pin arrives;
  - no permission prompt appears;
  - the MCP server received `HIVE_SESSION_*` through `${VAR}` expansion;
  - a user-level `permissions` rule in an isolated `CLAUDE_CONFIG_DIR` still applies next to Hive's rule.
- `internal/registry/pins_test.go` (run with `-race`)
  - `TestAddPinConcurrentWithKillLeavesNoOrphanDir`: concurrent AddPin and kill leave no `sessions/<id>` dir and no stray pin in `closed/<id>.pins`.
  - `TestUnpinConcurrentWithArchive`
  - `TestKillRefusedKeepsPinning`
  - The concurrency tests run under a 10 s timeout and assert they complete, which proves there is no deadlock.
  - `TestStartupSweepsOrphanPinsDirs`
  - `TestLoadReadsHeadersOnly`: bodies are not held in memory after load.
- `internal/daemon/pins_restart_test.go` (new; uses `testclient.RequireIsolation`)
  - `TestPinsSurviveDaemonRestart`: a real daemon on isolated temp dirs. Add pins through the testclient, stop the daemon, start a new one on the same state dir, then GET_PINS returns them.
  - `TestSessionModeRefusesPinControlVerbs`: GET_PINS, GET_PIN_BODY, UNPIN and UNPIN_ALL on a session connection each return `ErrCodeModeNotAllowed`.
- **Mutation checks.** Each of these must turn the named test red:
  - Revert the HELLO override: `TestAddPinOnSessionModeUsesHelloSession`.
  - Drop the `discardRecord` line: `TestDiscardRecordRemovesPins`.
  - Look pins up by `pinID` alone: `TestUnpinOtherSessionsPinIsNotFound` and `TestSessionCannotReadOtherSessionPins`.
  - Always emit `--mcp-config`: `TestClaudeSpawnArgsNoPinWhenOff`.
  - Send one page, or set `Done` on every page: `TestGetPinsPagesUnderMaxPayload`.
  - Remove the `pinsClosed` check: `TestAddPinConcurrentWithKillLeavesNoOrphanDir`.

**Pi**

- `internal/agent/pi/hive.test.ts`
  - `hive_pin registered when HIVE_PIN_TOOL unset`
  - `not registered when "0"`
  - `execute posts ADD_PIN and returns daemon error text`

**JS unit and dom**

- `FE/test/unit/pins-store.test.ts`
  - Pages merge newest first.
  - A changed add, remove or clear applies.
  - A `pins:changed` add or remove arriving between two pages is neither lost nor double-counted: dedupe by id, and a remove wins over a later page.
  - `forgetActivity` leaves pins intact.
  - A reconnect re-requests pins. That is the app-relaunch path.
- `FE/test/dom/pinned-section.test.tsx`
  - Hidden with no pins.
  - Count is shown in the header.
  - A collapsed row is one line, title plus age.
  - Click expands and renders the markdown body.
  - Unpin calls `Unpin(session, id)`.
  - Unpin-all opens a choice dialog, and Cancel calls nothing.
  - Copy calls `SetClipboardText(body)`.
  - The section renders for an empty-activity session.
- `FE/test/dom/settings.test.tsx`: the pin-tool checkbox loads, defaults on, and is saved.
- `FE/test/dom/choice-dialog.test.tsx`: the hints show `[esc]` and `[enter]`.
- `FE/test/dom/ui-session-row-plan.test.tsx`: the pin badge shows `pin_count` and is hidden at 0.

**Playwright** (Wails mock, run with `CI=1`): `FE/test/e2e/pinned.spec.ts`

- `overflowing pins scroll inside a capped section and the timeline keeps height`
- `clicking pins, copy and unpin never takes the keyboard`: uses `PAST_FOCUS_GUARD_MS`, `isTermFocused` and `stdinText`.
- `unpin all confirms, shows keys, cancel keeps pins, terminal refocused`
- `copy puts body on clipboard`
- `sidebar pin badge does not change row height at any density`: 40 / 34 / 28.
- `badge count matches inspector count`: polls.
- `activity-grid tile lists pins without focus or keystrokes`

**Daemon restart.** Covered by the Go integration test `TestPinsSurviveDaemonRestart`, on isolated temp dirs. The e2e-real harness has no daemon-restart helper, so no e2e-real test is added.

## Verification

- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) scripts/test.sh go unit dom e2e`
- `cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/pinned.spec.ts test/e2e/activity-panel.spec.ts test/e2e/activity-grid.spec.ts test/e2e/sidebar-plan-pie.spec.ts test/e2e/every-shortcut.spec.ts`
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -race ./internal/registry ./internal/daemon -run 'Pin'`
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done`
- `cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck`, then `scripts/ui-lint.sh --strict --contrast`
- `scripts/check-daemon-contract.sh origin/main HEAD && scripts/check-changeset.sh`
- `HIVE_PROBE_CLAUDE=1 go test ./cmd/hived -run TestProbeClaudePinsViaMCP`: the required manual gate. Its result goes in the decision log.
- The mutation checks listed under Tests: apply each one, confirm the named test goes red, then revert by re-applying the staged fix. Never use `git checkout --` on a file with uncommitted work.

- **2026-11-19** — PR #568 opened.

## Open questions / risks

- **Will Claude Code defer the MCP tool behind ToolSearch?** If so, the nudge names `mcp__hive-pin__pin` so the model can select it. The probe proves end to end that a pin arrives. If the probe fails, pins for PTY Claude are broken; stop and escalate rather than ship.
- **`${VAR}` expansion in `--mcp-config`.** This is the decided approach, and the probe verifies it. If the probe fails, stop and escalate.
- **`--settings` permission merge.** The probe verifies it. If it fails, the fallback is to drop the allow rule, so the tool prompts once.
- **The `HIVE_PIN_TOOL` override criterion is satisfied only trivially.** The variable is Hive-owned, as `HIVE_PI_TODO_TOOL` is. What Hive respects is the user's own MCP servers and settings: `--mcp-config` is additive, and permissions merge.
- **Window between close and tombstone prune** (7 days or 20 entries). Pins stay recoverable through Reopen during this window. "Permanently closed" means the tombstone has been discarded. `DESIGN.md` says so.
- **`hivebar`** decodes `SessionInfo` only, so `pin_count,omitempty` is harmless there and needs no change.
- **Body selection.** `keepFocus` cancels mousedown, so pin text cannot be mouse-selected. Copy is the route. This is accepted, because selection would steal focus.
- **No count bound.** A misbehaving agent can grow pins without limit: each is at most 64 KiB, and it can be paged. Unpin-all is the escape hatch. This was an explicit operator decision.
- **Archive and close carry pins** as `<id>.pins` siblings. If a rename fails, it is logged and the pins are lost. The session close itself must not fail because of pins.

## Second opinion

The independent reviewer (`hs-reviewer` subagent) ran two rounds.

- **Round 1: revise, confidence 7, with 7 must-fix items.** All seven were applied:
  - The paging test was vacuous, because headers alone never exceed 1 MiB.
  - AddPin could race kill and leave an orphan session directory.
  - The `--mcp-config` env block was missing; `${VAR}` expansion is now decided.
  - The session-mode error text and allowlist comment were stale.
  - The `--settings` permission merge was unverified; the probe is now a required gate with a fallback.
  - Mutation checks were missing for the scoping, the off switch and the paging.
  - The e2e-real test was unnamed; it became a Go integration test.
- **Round 2: approve, confidence 8, with 2 clarifications.** Both were applied:
  - The lock order between `r.mu` and `pinMu` is now stated.
  - `pinsClosed` is set only after kill's point of no return, with a test for a refused kill.
- **Nice-to-haves.** Applied: orphan sweep, header-only load, the exact-multiple `Done` page test, the hivebar note, the close-window note, and a second changeset.

## Decision log

- **2026-11-19** — Scope set at brainstorm: agent-authored pins only (manual pinning deferred), persist with session, per-session, Claude + Pi, proactive on by default with Settings toggle, no user-facing caps, extras = copy, sidebar badge, grid tiles. Why: operator answers in /hs-brainstorm.

- **2026-11-19** — Claude pins through a Hive MCP tool (`hived mcp-pin`). PTY Claude gets it via `--mcp-config` plus an allow rule; ACP Claude gets it as a second MCP server. Why: hooks cannot add a tool, and Claude has no built-in pin tool to observe (tasks work by observing built-ins). A Bash CLI would break under ACP, which strips `HIVE_*`. Operator chose this.
- **2026-11-19** — ACP Claude is in scope; ACP Pi is out. Why: operator decision. ACP Pi already forces Hive's tools off.
- **2026-11-19** — Pins are rejected, not truncated, above title 256 B or body 64 KiB. There is no count limit; headers are paged and bodies are fetched on demand. Why: operator decision. The 1 MiB frame cap rules out a single full snapshot.
- **2026-11-19** — Every confirm dialog shows its `[enter]`/`[esc]` hints, not just unpin-all. Why: operator decision. All dialogs had broken AGENTS.md's overlay-keys rule.
- **2026-11-19** — Assumed: pins follow the session through close/reopen and archive/unarchive as `<id>.pins` sibling directories. They are dropped when the tombstone or archive record is discarded. Why: this matches "persist with session". Spec Notes had asked whether archived sessions keep their pins; the answer is yes.
- **2026-11-19** — The real-Claude probe (`HIVE_PROBE_CLAUDE=1 TestProbeClaudePinsViaMCP`) is a required manual gate before merge. Its result and the Claude Code version get recorded here. Why: `${VAR}` expansion in `--mcp-config` and the `--settings` permission merge cannot be verified in CI.

- **2026-11-19** — Plugins may read pins (`GET_PINS`, `GET_PIN_BODY`) but not change them (`UNPIN`, `UNPIN_ALL` are in `pluginRefusedFrames`). This deviates from the plan, which refused every pin control frame to plugins. Why: parity with `GET_ACTIVITY`, which plugins can read; only curation is the user's call.
- **2026-11-19** — Pin-body Markdown styles reuse `acp.css`'s agent-Markdown rules through an `:is(.acp-item--agent, .hv-activity__pin-body)` selector. The plan called for a new `markdown.css`. Why: one selector list, with no move and no second copy.
- **2026-11-19** — `TestPinsSurviveDaemonRestart` lives in `internal/daemon/pins_test.go` rather than a separate `pins_restart_test.go`. It uses a temp state dir and socket via `shortTempDir`, like every daemon test; `testclient.RequireIsolation` is for testclient, which this test does not use.
- **2026-11-19** — The real-Claude probe asserts the permission merge with a project **deny** rule, not an allow. Why: on a machine whose own Claude config is permissive, an unlisted `touch` already runs unprompted, so an allow rule passed vacuously. This was confirmed by a mutation. A deny shows "merged" versus "replaced" on every machine. A second mutation, a deny on a different command, made the probe fail with "the project-denied command ran".
- **2026-11-19** — **Manual pre-merge gate: PASSED** on Claude Code 2.1.329 (`HIVE_PROBE_CLAUDE=1 go test ./cmd/hived -run TestProbeClaudePinsViaMCP`).
  - It confirmed three things: `${VAR}` expansion in the `--mcp-config` env, the pin arriving with no permission prompt, and the user's project deny still applying beside Hive's allow.
  - The probe harness flakes at Claude's folder-trust dialog. About half the starts sit there ignoring Enter, before any assertion runs. That dialog appears only because the probe writes a project `.claude/settings.json`.
  - Every run that got past the dialog passed.
- **2026-11-19** — `AddPin` was split into a lookup and `addPin(e, …)`, so `TestAddPinOnClosedEntryIsRefused` can exercise the closed-entry branch deterministically. The race test alone did not catch the mutation that removed the `closed` check.

- **2026-11-19** — `--mcp-config` is variadic and is now among the spawn args. An argv opening prompt cannot be swallowed by it: `resolveAgentCmd` appends the prompt after a literal `--` (`internal/registry/create.go:766-774`), and `--` ends option parsing.
- **2026-11-19** — Pins-store reconnect fix, found in the advisor review. Before it, a reload merged its pages into the headers the client already held, so a pin removed while the client was disconnected stayed in the inspector while the badge (the daemon's `pin_count`) disagreed. Now a load's first page replaces every header except pins a delta added during the load. The fix is covered by a test, mutation-checked.
- **2026-11-19** — Added `TestPiExtensionFrameConstantsMatchWire`. Nothing pinned `hive.ts`'s hand-written `FRAME_*` numbers to the wire, so a renumbering would have broken Pi pins silently. The test is mutation-checked.

## Progress

- **2026-11-19** — Spec triaged (enhancement / L / P2); research started.
- **2026-11-19** — Research done; plan drafted. Second opinion: revise, then approve 8/10. Operator approved the plan via HTML, first pass.
- **2026-11-19** — Implemented. Go: full suite green, plus per-GOOS staticcheck and vet. Frontend: unit and dom green, full Playwright e2e green (597), biome, typecheck and ui-lint clean. Mutation checks all fail as expected, and the real-Claude probe passed.

## Open questions
