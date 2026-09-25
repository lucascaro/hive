# Review and annotate agent plans in Hive before implementation

- **Spec:** [docs/product-specs/457-review-and-annotate-agent-plans-in-hive-before.md](../../product-specs/457-review-and-annotate-agent-plans-in-hive-before.md)
- **Issue:** #457
- **PR:** #459
- **Branch:** feature/457-review-and-annotate-agent-plans-in-hive-before
- **Status:** active

## Summary

Opt-in, native Hive plan review: a blocking gate on Claude's `ExitPlanMode` and on a Hive-provided Pi submit-plan tool, with passage-anchored comments sent back to the agent on deny. The full *why* lives in the spec; this file is about the *how*.

## Research

### Relevant code

**Claude hook path**
- `internal/agent/claude.go:232-287`: `claudeHookEvents` already includes `PermissionRequest`. `claudeSpawnArgs` wires one matcher-less group per event through `--settings`. `claudeHookEntry` (L238-241) has no `timeout` field, and Claude's default hook timeout is 600 s.
- `cmd/hived/hook.go`:
  - The invariant is no stdout and always exit 0 (L6-11). The precedent for structured stdout is `sessionStartOutput` (L97-113).
  - `PermissionRequest` maps to `waiting_permission`, and to `waiting_input` for AskUserQuestion (L181-189).
  - `sendHookEvents` (L527-558) is ModeEvent, fire-and-forget. Summaries are capped at `MaxSummaryLen` (L507-517).
- `internal/daemon/daemon.go`:
  - `serveEvent` (L810-837) never replies. It has a 2 s per-frame read deadline (L714) and allows 8 frames (L797).
  - `serveEventsOnly` (L735-791) accepts ModeEvent and ModeSession only.
  - ModeSession has a 30 s idle deadline (L722) and allows only the idea verbs (`sessionModeFrames` L1152).
- **Claude Code hooks contract** (docs fetched, Claude Code 2.1.282 installed):
  - PermissionRequest input carries `tool_name` and `tool_input`. For ExitPlanMode those are `plan` (markdown) and `planFilePath`.
  - Output shape: `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"|"deny", ...}}}`.
  - **Allow for ExitPlanMode must echo `updatedInput: tool_input`.** Without it, Claude silently falls back to the built-in dialog. plannotator does the same.
  - Deny carries a `message` that Claude reads.
  - Exit 0 with no stdout falls through to the normal permission flow. That is the fallback.
  - On timeout the output is discarded and the normal flow runs.
  - All matching hooks run in parallel, and there is **no documented precedence for conflicting PermissionRequest decisions**. So Hive must not answer at all when another reviewer is configured.
- **Live capture was not possible headless.** Under `claude -p --permission-mode plan`, ExitPlanMode is disabled ("ExitPlanMode disabled this session"). The payload capture has to run in an interactive PTY session (brain lesson below).

**Pi path**
- `internal/agent/pi.go:18-86` embeds `pi/hive.ts`, writes it to `<stateDir>/pi/hive.ts`, and passes `-e <path>`.
- Settings reach the extension through env: `piSpawnEnv` in `internal/agent/settings.go:179-190` always sets `HIVE_PI_TODO_TOOL=0|1` and `HIVE_PI_HEARTBEAT=1`.
- `internal/agent/pi/hive.ts`:
  - Fire-and-forget ModeEvent sender, one connection at a time, 2 s timeout, queue of 64 (L189-243).
  - Privacy rule: raw tool arguments and results never go over the socket (L23-27, L117-131). The plan body is a deliberate, named exception for this tool only.
  - Existing tool `hive_todo` (L358-375) uses plain JSON-Schema parameters so `node --test` runs without node_modules.
  - A tool blocking in `execute` raises no `ui_prompt_*`, so it would read as `working`. The review tool must post a keyed `waiting_permission` itself and make it `lastState` so the heartbeat replays it (L329-347, L469-485).
  - The heartbeat stops at `session_shutdown` (L510). A pending review must be cancelled there.
- Pi 0.85.1 extension API (installed `docs/extensions.md`):
  - `execute(toolCallId, params, signal, onUpdate, ctx)` is async, and a long-pending Promise is the blocking mechanism. Honor `signal` so Esc cancels.
  - Returning normally is a success. Throwing sets `isError`.
  - `promptSnippet` and `promptGuidelines` nudge tool use (L1373-1388). `before_agent_start` can append to the system prompt (L530-560).
  - A `tool_call` handler can block edit/write with a reason (L792-809).
  - `ctx.hasUI` / `ctx.ui.confirm` give a terminal fallback.
  - Prior art: `examples/extensions/plan-mode/`.
- Tests: `internal/agent/pi_test.go` covers wire-frame validity through node (L154), `node --test pi/hive.test.ts` (L346-365), and the kind allowlist `TestPiExtensionKindsAreOnTheAllowlist` (L285). `pi/hive.test.ts` has `fakePi` stubs and a real unix-socket server.

**Daemon parked-question pattern (spec 451), the model to copy**
- `SessionInfo.PendingWorktreeChoice` (`internal/wire/control.go:173-183`, type L975-1008, with `ParkID` against stale answers). `FrameResolveWorktreeChoice` 0x31 (`internal/wire/frame.go:187-196`, String L281).
- The daemon handler runs off the read loop (`daemon.go:1638`).
- Registry: `parkWorktreeChoice` / `ResolveWorktreeChoice` (`internal/registry/create.go:1058,1119`). `canAskUser()` (`registry.go:862-880`) is fed by `d.controlClients` (`daemon.go:73-78,666-685`).
- **Caveat:** hivebar and ws-bridge are also ModeControl clients, so the count means "some control client", not "a GUI that can answer". `Hello.Client` names the client ("hivegui/…", `cmd/hivegui/app_control.go:67`).
- Size: `SessionInfo` rides every broadcast and frames cap at 1 MiB (`frame.go:34`). Only a small `pending_plan_review` goes on SessionInfo. The markdown is fetched with a get/reply pair, like `FrameGetActivity`/`FrameActivity` 0x2b/0x2c (`daemon.go:1335`).
- Needs-attention derives from `waiting_permission` (`registry.go:239-253,283`; `agentstate/machine.go:566-574`). The GUI's `sessionState()` (`src/lib/session-state.ts:122`) and ⌘B `jumpToAttention` (`src/app/keyboard.ts:762`) already consume it.

**GUI**
- Daemon→GUI: `controlReadLoop` (`cmd/hivegui/app_control.go:427`) plus the `wire.ControlEventName` table (`internal/wire/client.go:119-151`). The ws-bridge shares that table.
- GUI→daemon: bound methods in `cmd/hivegui/app_calls.go` (`ResolveWorktreeChoice` :806) through `src/bridge.ts`.
- Worktree-choice flow to mirror:
  - `src/app/events.ts:340-466`: queues one question at a time, closes stale dialogs, and does not treat a dismiss as an answer.
  - The tile "Answer…" button (`TileChrome.tsx:80`, `TileOverlays.tsx:192`).
  - Enter on a blocked tile (`keyboard.ts:390`).
- Modals: `components/modals/ModalShell.tsx` (sizes sm/md/lg, focus trap), the store `ModalId` (`src/store/store.ts:148-175`), and a root element in `index.html:126-154`.
- **No markdown library** in `cmd/hivegui/frontend/package.json`. `WhatsNew.tsx` deliberately avoids innerHTML. `marked` 18.0.14 has zero dependencies.
- Theming: tokens in `src/theme/tokens.css`, component CSS in `src/theme/components/`. `scripts/ui-lint.sh --strict` bans raw hex and px font sizes.
- Settings: `internal/agent/settings.go` (`agent-settings.json`) uses pointer fields where a missing key means the default. The GUI writes the file and hived reads it at spawn. Mirrored in `cmd/hivegui/app_calls.go:110-138`, with the UI in the Agents tab of `components/modals/Settings.tsx`.
- **External-reviewer detection:** Hive reads no Claude settings today. Sources to check:
  - `~/.claude/settings.json`, `<proj>/.claude/settings.json` and `settings.local.json`, plus managed settings, each at `hooks.PermissionRequest[].matcher`, which is a regex on the tool name.
  - Enabled plugins: `~/.claude/plugins/installed_plugins.json` plus `enabledPlugins`, then `<installPath>/hooks/hooks.json`.
  - A `disableAllHooks` flag.

### Constraints / dependencies
- Blocking needs a new request/wait path on the events socket. Neither ModeEvent (no reply, 2 s) nor ModeSession (30 s idle) fits.
- A `buildinfo.DaemonContract` bump is required (currently 16, `internal/buildinfo/contract.go:167`), because an old GUI cannot answer the new pending state. A new frame does not bump `PROTOCOL_VERSION`.
- Wire changes go to all three clients in lock-step: hivegui, hived-ws-bridge and `internal/wire/testclient`.
- Hivebar-only attachment must not count as "a GUI can answer".

### Prior lessons
- Capture real agent payloads from a live run before trusting the plan's sketch. Spec 416 was built on a tool current models lack. Build fixtures from the capture and test both the success and failure paths.
- Read the installed pi `docs/` rather than memory. It caught two wrong API assumptions last time.
- `pi -e … --list-models` exits 0 even when the extension throws. Prove extension behavior with the live `HIVE_PROBE_PI=1` probe plus a negative control.
- Heal Pi state by replaying keyed edges only, and never heartbeat a snapshot.

### Conventions card
```
build:  ./build.sh          # macOS .app (GUI + daemon)
test:   scripts/test.sh     # all layers: go · unit · dom · e2e
        npm run test:e2e:real   # isolated real hived (HIVE_SOCKET + HIVE_STATE_DIR temp)
lint:   biome ci .  (frontend) · scripts/ui-lint.sh --strict
        for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done
gates:  scripts/check-daemon-contract.sh <base> <head>
toolchain: GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)
```
- TDD: every behavior change ships its test. Go tests sit beside the source, frontend tests under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- Wire JSON is snake_case with explicit tags. JS reads `snake_case ?? camelCase`.
- Wire changes update hivegui, ws-bridge and testclient in lock-step. Daemon-side changes bump `DaemonContract`.
- Shell out only through `internal/proc`. The GUI never opens a PTY.
- Frontend selectors use ids and `hv-*` classes, never `data-testid`. User-visible changes get a changeset through `/hs-changelog-update`.

## Approach

**Shape.** The agent side makes one long-held request on a new events-socket mode. The daemon parks a small `pending_plan_review` on the registry entry, where it rides every SessionInfo, and holds the plan text in the registry. The GUI fetches the markdown with GET and answers with RESOLVE. This is spec 451's parked-question model: `PendingWorktreeChoice` (`internal/wire/control.go:173-183`), `parkWorktreeChoice` (`internal/registry/create.go:1058`), and `ParkID` staleness. On top of that, the requester connection waits for the answer.

**Rejected alternative.** Having the hook poll, or opening a second GUI-to-hook channel. Both need another transport and have no disconnect signal. A held socket gives cancel-on-exit for free: when Claude kills the hook or Pi's user presses Esc, the connection closes and the daemon sees EOF.

### (a) Wire + daemon

- **Mode.** New `wire.ModePlanReview = "plan_review"`, served only by `serveEventsOnly` (`daemon.go:760-790`). The control socket keeps answering `unknown_mode` (`daemon.go:699`).
- **Request.** HELLO keeps its 2 s deadline. The client then sends one `PLAN_REVIEW_REQUEST` (0x32) with `{session_id, source:"claude"|"pi", plan, cwd}`. After that the daemon clears the read deadline and no frame cap applies. A goroutine blocks on `conn.Read` only to detect EOF.
- **Validation.**
  - `plan` must be non-empty and at most `wire.MaxPlanReviewLen` (128 KiB). JSON-escaped 128 KiB stays under `MaxPayload` 1 MiB (`frame.go:34`).
  - The session must exist and be alive.
- **Reply.** `PLAN_REVIEW_DECISION` (0x33) with `{status, message, comments, feedback}`. Status is one of `approve | deny | disabled | external | no_client | cancelled | invalid`.
- **Gate before parking, in this order:**
  1. `agent.LoadSettings()` is read live. If `plan_review` is off or the settings don't parse, reply `disabled`.
  2. If `source==claude` and `agent.ExternalPlanReviewer(home, cwd, managedPath)` is true, reply `external`, **unless this session was spawned with Hive as the reviewer.** The spawn-time `plan_reviewer` and the list of suppressed plugin IDs are stored on the registry entry (`Entry.planReviewerAtSpawn`), because plugin suppression can only happen at spawn. Only the on/off switch is read live. A mid-session switch from external to Hive therefore can't double-prompt: that session keeps deferring to the external reviewer.
  3. If there are no answerers, reply `no_client` (fail fast). **The check and the park happen atomically.** The answerer count moves into the registry (`reg.SetAnswerers(n)`, replacing `SetHasControlClient`), and `ParkPlanReview` checks it under the same `r.mu` that `CancelPlanReviews` holds. A reviewer that leaves between the check and the park can't strand a 4-day wait.
- **Park.** `reg.ParkPlanReview(id, source, plan)` returns `(reviewID, <-chan Decision, cancel)`.
  - One review per session. A newer request cancels the older one, which gets `cancelled`.
  - It sets `PendingPlanReview{review_id, source, created_at}` and broadcasts `SessionEventUpdated`.
  - The plan text is never placed on SessionInfo.
- **Wait.** `select` over the decision, EOF (cancel and clear), `time.After(345600s)`, and daemon stop.
- **Deny message.** Built once in Go by `agent.FormatPlanFeedback(comments, feedback)`. Every comment appears with its quoted passage. The hook and Pi forward the message verbatim, so the format has one owner.
- **GUI-facing frames.**
  - `GET_PLAN_REVIEW` (0x34) `{session_id, review_id}` replies `PLAN_REVIEW` (0x35) `{session_id, review_id, source, plan}`, or FrameError `plan_review_stale`.
  - `RESOLVE_PLAN_REVIEW` (0x36) `{session_id, review_id, decision, comments[{quote,text}], feedback}`. A mismatched or empty review_id is ignored silently, so two racing windows produce one action (as `ResolveWorktreeChoice` does, `daemon.go:1638`).
  - Caps: 200 comments, quote ≤ 4 KiB, text ≤ 4 KiB, feedback ≤ 16 KiB.
  - Both frames are control-mode only and are not in `sessionModeFrames`.
  - `wire.ControlEventName` gains `FramePlanReview: "planreview:plan"`.
- **Answerers.** HELLO client names tell clients apart: `hivebar/0.1` (`cmd/hivebar/client.go:104`), `hivegui/0.2` (`app_control.go:67`), `ws-bridge/control` (`hived-ws-bridge/main.go:544`), and `testclient/0`.
  - Rename `controlClients` to `answerers` and increment only when the name lacks the `hivebar/` prefix (`daemon.go:676-685`).
  - This also tightens #451's worktree park, which its own comment at `daemon.go:672-675` says hivebar cannot answer.
  - When the count reaches 0, call `reg.CancelPlanReviews(StatusNoClient)`. Every pending requester then gets **`no_client`**, not `cancelled`. `cancelled` is kept for supersede, requester EOF, and daemon stop. The Claude hook falls through on either status. Pi falls back to `ctx.ui.confirm` on `no_client`, and that holds both when the GUI was never attached and when it quit mid-review, as the Decision log requires.
  - Daemon stop: `servePlanReview` selects on the daemon ctx and replies `cancelled`. These connections aren't in `d.clients`, so `Close` won't hang them up on its own.
- **Session end.** `watchSessionExit` (`registry.go:1431`) and `Kill` (`registry.go:1483`) cancel the park and clear the field.

### (b) Claude hook (`cmd/hived/hook.go`)

- After `sendHookEvents`, call `planReviewOutput(raw, sock, sessionID)`.
- It acts only when all of these hold: `hook_event_name=="PermissionRequest"`, `tool_name=="ExitPlanMode"`, and `tool_input.plan` is a non-empty string within the cap. Otherwise it returns nil without dialling.
- It dials the events socket, sends HELLO `plan_review` plus the request with `cwd` from the payload, and blocks with a client read deadline of 345600 + 60 s.
- On approve: `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow","updatedInput":<tool_input verbatim>}}}`.
- On deny: `decision:{behavior:"deny", message:<reply.message>}`.
- After either decision, send a `permission_resolved` event (an existing kind) so `waiting_permission` clears.
- Any other status, any error, or a panic produces no stdout and exit 0, which falls through to Claude's own dialog. Update the header comment at L6-11 to name this second deliberate stdout.
- The settings and external-reviewer gates live in the daemon, so the hook needs no state dir.
- **Timeout.**
  - `claudeHookEntry` gains `Timeout int \`json:"timeout,omitempty"\`` (`claude.go:238`).
  - `claudeSpawnArgs` gives PermissionRequest its own matcher-less group with `Timeout: 345600`. Every other event keeps the shared group with no timeout.
  - Keeping it matcher-less avoids a second `matcher:"ExitPlanMode"` group, which would run the hook twice or collide in Claude's dedup-by-command.
  - Set it unconditionally, because the setting is read live.

### (c) External-reviewer detection

`agent.ExternalPlanReviewer(home, projectDir, managedPath string) bool` in `internal/agent/planreview.go`. It only reads files.

- **Sources:**
  - `home/.claude/settings.json`
  - `projectDir/.claude/settings.json`
  - `projectDir/.claude/settings.local.json`
  - `managedPath`: darwin `/Library/Application Support/ClaudeCode/managed-settings.json`, linux `/etc/claude-code/managed-settings.json`.
- `disableAllHooks` true in any source means no external reviewer.
- A `hooks.PermissionRequest[]` group counts when its matcher is non-empty, is not `*`, and compiles to a regex that matches `ExitPlanMode`.
- **Plugins.** Enabled means `enabledPlugins` true after merging the sources, joined with `home/.claude/plugins/installed_plugins.json` (`plugins[name][*].installPath`). The same group rule then applies to `<installPath>/hooks/hooks.json`.
- `// ponytail: matcher-less/"*" PermissionRequest hooks are observers, not reviewers; revisit if a reviewer ships catch-all.`
- **Suppress the external reviewer when Hive is selected (criterion 5, second half).** Claude runs every matching hook in parallel, so Hive can't win by precedence. When the spawn-time settings have `plan_review` on and `plan_reviewer=="hive"`:
  - `claudeSpawnArgs` adds `enabledPlugins: {<id>: false}` to its `--settings` blob for each **plugin** that `ExternalPlanReviewers(...)` reports. Detection returns the sources as `[]Reviewer{Kind: plugin|settings, ID, Path}`.
  - The live capture checks that `--settings` `enabledPlugins:false` really disables the plugin's hooks for that session.
  - A reviewer hook written directly in a settings.json file **cannot** be disabled from `--settings`, because hooks concatenate. For that case the Settings reviewer select shows an inline warning naming the file: "this reviewer will also prompt; remove it or pick External."
  - This is spawn-time, so switching to Hive affects new Claude sessions only. The Settings copy says so, and the daemon gate enforces it (step 2).
  - `enabledPlugins:false` disables the **whole plugin** for that session: its commands, skills and MCP, not just its hook. The Settings copy and README say so.
  - **This narrows criterion 5 and needs operator sign-off at the plan stop.**

### (d) Pi (`internal/agent/pi/hive.ts`)

- `piSpawnEnv` (`settings.go:179`) always sets `HIVE_PI_PLAN_REVIEW=0|1`.
- **Registration.** `hive_submit_plan` takes plain JSON-Schema `{plan: string}` and is registered only when the variable is `=1`. With the setting off, Pi behaves exactly as today.
- **Live changes.** The daemon re-checks the setting per request. If it was turned off mid-session the reply is `disabled`, and the tool returns "Plan review is off in Hive; proceed." Turning it on only affects new Pi sessions, and the Settings copy says so.
- **`execute(id, params, signal, _, ctx)`:**
  1. Post a keyed `waiting_permission`, which becomes `lastState` so the heartbeat replays it.
  2. Call `requestPlanReview(sock, sid, plan, process.cwd(), signal)`. This is a new export on its own connection, not the fire-and-forget sender. An abort destroys the connection, and the daemon then cancels.
  3. Post `permission_resolved` in a `finally`.
- **Results.**
  - `approve`: "Plan approved; implement it."
  - `deny`: `reply.message` plus "Revise and call hive_submit_plan again."
  - `no_client`: `ctx.hasUI ? ctx.ui.confirm(...)` as the terminal fallback, otherwise "No reviewer available; proceed."
  - `cancelled`: throw.
- The plan body is the one named exception to the privacy rule (L23-27). Update that comment.
- `session_shutdown` destroys any in-flight review socket.
- **Nudge only:** `promptSnippet`, `promptGuidelines`, and a `before_agent_start` system-prompt line while the tool is registered. No edit/write gate.

### (e) Settings

- `Settings.PlanReview bool`, default false.
- `Settings.PlanReviewer string` (`"external"|"hive"`), default `"external"`. An unknown value means external.
- Both are pointer fields in `settingsFile`.
- Mirror them in `AgentSettings` (`app_calls.go:110-138`).
- Settings.tsx Agents tab: a "Review plans in Hive" checkbox and a reviewer select, disabled while review is off.

### (f) GUI

- **events.ts.** `maybeAskPlanReview(info)` is modelled on `maybeAskWorktreeChoice` (`events.ts:340-466`).
  - It queues one review at a time, calls `GetPlanReview`, and on `planreview:plan` opens `openModal({id:'plan-review', sessionId, reviewId, source, plan})`.
  - When `pending_plan_review` disappears or its review_id changes, it closes the modal as a non-answer.
  - Esc is not an answer.
- **Re-open.** The tile gets a "Review plan…" button (`TileChrome.tsx:80` / `TileOverlays.tsx:192` pattern). There is **no Enter intercept**. The session has a live PTY, and Claude's own terminal dialog may need that keystroke; the Enter path at `keyboard.ts:378-392` assumes a parked session with no process. Re-raise through the button, auto-raise, and ⌘B jump-to-attention.
- **Tile state** comes from `pending_plan_review`, not from `PHASE.blocked`. `blocked` stays create-only (`create.go:1100`). The tile shows its own "Plan awaiting review" line, and attention already comes from `waiting_permission`.
- **PlanReview.tsx.** Uses ModalShell `lg`.
- **Markdown.tsx.** Walks `marked.lexer()` tokens and builds React elements for: heading, paragraph, list, code, blockquote, table, hr, and inline strong/em/codespan/link/del/br/text.
  - Unknown tokens render as text.
  - Nothing uses innerHTML.
  - Links are shown as text plus href and are not navigable.
- **Comments.**
  - A selection inside the body enables "Comment on selection". The composer captures `{quote, text}`, and comments can be deleted.
  - Approve is the primary action.
  - Deny has an overall feedback textarea and stays disabled until there is a comment or some feedback.
- **Theme.** `src/theme/components/plan-review.css` uses tokens only.

### (g) Contract, docs, changeset

- `DaemonContract` 16 → 17, with a history entry (`contract.go:167`).
- `docs/design-docs/control-plane.md`: the mode, the frames, and the answerer rule.
- README: a "Plan review" section covering the setting, reviewer precedence, and fallback.
- `.changesets/` entry via `/hs-changelog-update`.

### Files to change

1. `internal/wire/frame.go`: frames 0x32-0x36 and their `String()` names.
2. `internal/wire/control.go`:
   - `ModePlanReview` and `SessionInfo.PendingPlanReview`.
   - The types `PendingPlanReview`, `PlanReviewRequest`, `PlanReviewDecision`, `PlanComment`, `GetPlanReviewReq`, `PlanReviewMsg`, `ResolvePlanReviewReq`.
   - Caps, status constants, and `Validate()`.
3. `internal/wire/client.go`: `FramePlanReview` in `controlEvents`.
4. `internal/registry/registry.go`: `Entry.planReview`, the `Info()` copy, and cancel in `watchSessionExit` and `Kill`.
5. `internal/daemon/daemon.go`: the `answerers` rename, hivebar exclusion, and cancel-at-zero; the `ModePlanReview` arm in `serveEventsOnly`; the GET and RESOLVE arms in `handleControlFrame`.
6. `internal/agent/claude.go`: the `Timeout` field and the separate PermissionRequest group.
7. `internal/agent/settings.go`: the two settings, and `HIVE_PI_PLAN_REVIEW` in `piSpawnEnv`.
8. `internal/agent/pi/hive.ts`: `requestPlanReview`, the `hive_submit_plan` tool, the nudge, and shutdown cancel.
9. `cmd/hived/hook.go`: `planReviewOutput` and its wiring into `runHook`.
10. `cmd/hivegui/app_calls.go`: the settings mirror, `GetPlanReview`, and `ResolvePlanReview`.
11. `cmd/hived-ws-bridge/main.go`: dispatch cases for `GetPlanReview` and `ResolvePlanReview`.
12. `internal/wire/testclient/client.go`: `GetPlanReview`, `ResolvePlanReview`, and the requester-side `PlanReview(ctx, sock, req)`.
13. `cmd/hivegui/frontend/package.json` and lockfile: `marked` 18.0.14, pinned exact.
14. `cmd/hivegui/frontend/src/`:
    - `bridge.ts` and `store/store.ts` (the `'plan-review'` ModalId).
    - `app/events.ts` and `app/keyboard.ts`.
    - `components/TileChrome.tsx`, `TileOverlays.tsx`, and `components/modals/Settings.tsx`.
    - `index.html` (modal root, if needed) and `theme/components/index.css`.
14b. `cmd/hivegui/frontend/src/app/state.ts`: `pending_plan_review?` / `pendingPlanReview?` on the SessionInfo type (next to the worktree-choice fields at L102-110). Tile chrome shows "Plan awaiting review", derived from that field. `phase-steps.ts` and `PHASE.blocked` are unchanged.
14c. `cmd/hivegui/frontend/test/e2e-real/wails-bridge.ts`: `GetPlanReview` and `ResolvePlanReview` exports, next to `ResolveWorktreeChoice` (L228).
14d. `DESIGN.md`: the answerer rule, and the fact that a pending plan review is session data, not a phase (next to L112).
14e. Settings reviewer select: the inline warning for a settings.json reviewer that can't be suppressed. It uses a new `GetExternalPlanReviewers` bound call, which scans user and managed settings plus plugins only, because the Settings modal has no project cwd. Add it to wails-mock.ts and e2e-real wails-bridge.ts too; ws-bridge's `default:` would otherwise return empty success silently.
15. `cmd/hivegui/frontend/test/e2e/wails-mock.ts`: mocks, plus a `__mockPendingPlanReview(id, md)` helper.
16. `internal/buildinfo/contract.go`: 17 and the history entry.
17. `docs/design-docs/control-plane.md`, `README.md`, and the Progress section of this exec plan.

### New files

- `internal/agent/planreview.go`: `ExternalPlanReviewer` and `FormatPlanFeedback`.
- `internal/registry/planreview.go`: `ParkPlanReview`, `PlanReviewText`, `ResolvePlanReview`, `CancelPlanReviews`.
- `internal/daemon/planreview.go`: `servePlanReview(ctx, conn, hello)`.
- `cmd/hivegui/frontend/src/components/modals/PlanReview.tsx`, `src/components/Markdown.tsx`, and `src/theme/components/plan-review.css`.
- `cmd/hived/testdata/hooks/permission_request_exitplanmode.json`, taken from the live capture.
- `.changesets/457-plan-review.md`.

### Tests

### Go

**Agent**
- `internal/agent/claude_test.go` `TestClaudeSpawnArgsDisablesExternalPluginReviewer`: the plugin reviewer is set to `false` only when review is on and the reviewer is Hive.
- `internal/agent/claude_test.go` `TestClaudeSpawnArgsPlanReviewTimeout`: only PermissionRequest carries `"timeout":345600`, and no other event has a timeout key.
- `internal/agent/settings_test.go`:
  - `TestSettingsPlanReviewDefaults`: defaults are off and external.
  - `TestSettingsPlanReviewRoundTrip`.
  - `TestPiSpawnEnvPlanReview`: the variable is `0` or `1` to match the setting.
- `internal/agent/planreview_test.go`:
  - `TestExternalPlanReviewer`, a table over a fake HOME and project. Cases: a user `ExitPlanMode` matcher; regex `Exit.*`; project `.local`; managed settings; an enabled plugin's hooks.json; a disabled plugin; `disableAllHooks`; a matcher-less or `*` group; malformed JSON; no reviewer at all.
  - `TestFormatPlanFeedbackCarriesEveryQuote`.

**Wire and registry**
- `internal/wire/plan_review_test.go` (all tests named `TestPlanReviewWire*` so the `-list` gate counts them): `Validate` caps; SessionInfo omits the field when it is nil; the `String()` names.
- `internal/registry/planreview_test.go`:
  - `TestParkPlanReviewReplacesPrior`
  - `TestResolvePlanReviewStaleIDIgnored`
  - `TestKillCancelsPlanReview`
  - `TestSessionExitCancelsPlanReview`
  - `TestPlanReviewInfoIsSmall`

**Daemon** (`internal/daemon/plan_review_test.go`)
- `TestPlanReviewDisabledByDefault`
- `TestPlanReviewFailsFastWithoutClient` (under 1 s)
- `TestPlanReviewHivebarIsNotAnAnswerer`
- `TestPlanReviewCancelsOnRequesterDisconnect`
- `TestPlanReviewCancelsWhenLastAnswererLeaves`: the requester gets `no_client`, and a Claude hook driven through it gives empty stdout.
- `TestParkPlanReviewAtomicWithAnswerers`: a registry test hook sets answerers to 0 between the gate and the park, and the park refuses with `no_client`.
- `TestPlanReviewCancelledOnDaemonStop`: `servePlanReview` replies `cancelled` and closes `conn` on ctx done, so the EOF-reader goroutine exits (goleak-style check).
- `TestPlanReviewReviewerIsSpawnTime`: a session spawned with the external reviewer, with the setting later switched to Hive, still gets `external`.
- `TestPlanReviewExternalReviewerWins` and `TestPlanReviewHiveSelectedIgnoresExternal`
- `TestPlanReviewOversizeRefused`
- `TestPlanReviewModeRefusedOnControlSocket`
- `TestGetPlanReviewStale`
- `TestPlanReviewRoundTripApproveDeny` (via testclient)
- Extend `TestControlClientCountTracksRealConnections` with a hivebar case.

**Hook** (`cmd/hived/hook_test.go`, against a fake socket server)
- `TestPlanReviewOutputAllowEchoesUpdatedInput`: `decision.updatedInput` deep-equals the fixture's `tool_input`.
- `TestPlanReviewOutputDenyCarriesEveryQuote`.
- `TestPlanReviewOutputFallsThrough`, a table that expects empty stdout for each case:
  - a non-ExitPlanMode event (asserts no dial)
  - `disabled`, `external`, `no_client`, and `cancelled` replies
  - daemon down
  - an oversize plan
  - an empty plan

**Integration and probes**
- `cmd/hived/hook_integration_test.go` `TestHookPlanReviewRoundTrip`, against a real daemon:
  - writes the settings;
  - a testclient control connection answers approve and then deny through GET and RESOLVE;
  - `runHook` is driven from the captured fixture;
  - asserts stdout, and that `pending_plan_review` appears and then clears.
- `cmd/hived/claude_probe_test.go` `TestClaudeProbePlanReview` (runs only with HIVE_PROBE_CLAUDE=1): a PTY claude session in plan mode. A deny with a quoted sentinel must be echoed in the next plan, and approve must proceed.
- `cmd/hived/pi_probe_test.go` `TestPiProbePlanReview`, plus the negative control `TestPiProbePlanReviewOff` (runs only with HIVE_PROBE_PI=1).

### Pi (`internal/agent/pi/hive.test.ts`, node --test via `pi_test.go:346`)

- Not registered unless `HIVE_PI_PLAN_REVIEW=1`.
- Registered with promptSnippet and guidelines.
- approve returns the approved text.
- deny returns the daemon's message.
- An abort closes the socket, and the server sees the close.
- `no_client` falls back to `ctx.ui.confirm`, both for "GUI never attached" (immediate reply) and "GUI left mid-review" (`no_client` arrives after a delay).
- Posts a keyed `waiting_permission`, then `permission_resolved`.
- `session_shutdown` destroys an in-flight review.

### Frontend

**Unit and DOM**
- `test/unit/markdown.test.ts`: the token-to-element mapping. A `<script>` or raw-HTML token renders as literal text.
- `test/dom/plan-review.test.tsx`:
  - headings, lists, and code render;
  - a selection becomes a comment carrying the quote;
  - Deny sends the comments plus feedback;
  - Approve sends the review_id;
  - innerHTML is never used (spied).
- `test/dom/plan-review-events.test.ts`: auto-raise; reviews queue one at a time; the modal closes when the review goes stale; Esc is not an answer.
- `tile-overlays.test.tsx`: the "Review plan…" button and the "Plan awaiting review" line appear only while `pending_plan_review` is set.
- `blocked-tile-keyboard.test.ts` `enter reaches xterm while a plan review is pending`: no intercept.
- `settings.test.tsx`: both controls save.

**E2E**
- Mock (`test/e2e/plan-review.spec.ts`): raise, comment, the deny payload, approve, and theme tokens.
- Real (`test/e2e-real/plan-review.spec.ts`):
  - write `agent-settings.json`;
  - dial the events socket via `requestPlanReview` imported from `internal/agent/pi/hive.ts`;
  - the modal appears;
  - deny with a comment, and the requester's message contains the quote;
  - re-request, approve, and the requester gets `approve`.

## Verification

```
# Named tests must EXIST (go test -run passes vacuously on zero matches):
# Named, non-probe tests must exist (probes excluded from the count):
test "$(GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -list '^Test(PlanReview|ParkPlanReview|ResolvePlanReview|KillCancelsPlanReview|SessionExitCancelsPlanReview|GetPlanReviewStale|ExternalPlanReviewer|FormatPlanFeedback|PiSpawnEnvPlanReview|SettingsPlanReview|ClaudeSpawnArgs(PlanReview|DisablesExternal)|HookPlanReviewRoundTrip)' ./internal/... ./cmd/hived/... | grep '^Test' | grep -vc Probe)" -ge 28
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/agent/... ./internal/wire/... ./internal/registry/... ./internal/daemon/... ./cmd/hived/...
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/agent -run TestPiExtensionRunsNodeTests -v   # node ≥23.6; the test skips on older node, so check -v output shows it RAN
( cd cmd/hivegui/frontend && npx vitest run test/unit/markdown.test.ts test/dom/plan-review.test.tsx test/dom/plan-review-events.test.ts test/dom/settings.test.tsx test/dom/tile-overlays.test.tsx test/dom/blocked-tile-keyboard.test.ts \
  && CI=1 npx playwright test test/e2e/plan-review.spec.ts && npm run test:e2e:real -- plan-review \
  && npx biome ci . && npm run typecheck )
scripts/ui-lint.sh --strict
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done
scripts/check-daemon-contract.sh origin/main HEAD
scripts/test.sh
```

**Live checks, run first during IMPLEMENT**

1. **Capture the Claude payload** in a real PTY (not `-p`, which disables ExitPlanMode). Run `claude --permission-mode plan` with a capture-only PermissionRequest/ExitPlanMode hook and ask for a two-step plan.
   - Commit the scrubbed JSON as the fixture.
   - Record whether Claude's terminal dialog renders while the hook blocks.
1b. **Plugin override check.** With plannotator installed as a user plugin (`enabledPlugins:true`), spawn claude with `--settings '{"enabledPlugins":{"plannotator@plannotator":false}}'` and confirm its ExitPlanMode hook does not fire. If `--settings` can't override the user-level value, the Hive-selected plugin path fails: stop and re-plan.
2. **Claude probe:** `HIVE_PROBE_CLAUDE=1 go test ./cmd/hived -run TestClaudeProbePlanReview -v`.
3. **Pi probe:** `HIVE_PROBE_PI=1 go test ./cmd/hived -run 'TestPiProbePlanReview' -v`. The negative control must show no `hive_submit_plan` when the variable is `=0`.
4. **Manual run with `./build.sh`:**
   - Plan, comment, deny, see the revised plan, approve.
   - Quit the GUI mid-review: the terminal dialog appears.
   - With plannotator's hook installed and default settings, only plannotator prompts. After switching the reviewer to Hive, only Hive prompts.

## Risks

- **Terminal dialog while the hook blocks is unverified.** Live check 1 answers it. If Claude does show its dialog and the user answers there, Claude kills the hook, EOF cancels the park, and the modal closes.
- **Hook dedup.** One matcher-less PermissionRequest group avoids a double run. Re-verify the timeout is honoured on the installed 2.1.282.
- **Detection heuristic.**
  - A catch-all reviewer (matcher-less or `*`) is missed.
  - Project settings are read from the payload `cwd` only; there is no walk up to the git root.
  - The managed path on Windows is unknown.
- **#451 side effect.** With hivebar excluded from answerers, a worktree create with only hivebar attached now fails instead of parking. That matches the comment at `daemon.go:672`; call it out in the PR.
- **Pi may ignore the nudge.** This is a named failure signal. The probe measures it, and there is deliberately no gate.
- **Plans over 128 KiB** fall through to the terminal. Truncating is wrong because a partial plan would get approved.
- **Session-id trust.** Any child process can send `plan_review` for another session's id and replace that session's pending review. The existing `serve_event` path trusts `session_id` the same way, so this is the same model, not a new hole. Accepted and documented.
- **ExitPlanMode inside a subagent** raises a review against the parent session. One review per session, so the newer one replaces the older.

## Criteria coverage

| # | Approach | Files | Tests |
|---|---|---|---|
| 1 off → unchanged | (a) `disabled`, (d) env gate, (b) fallthrough | settings.go, hook.go, hive.ts | DisabledByDefault, FallsThrough, hive.test not-registered, PiProbeOff |
| 2 blocks + attention + review opens | (a) park + waiting_permission, (f) auto-raise | daemon/planreview.go, events.ts, PlanReview.tsx | HookPlanReviewRoundTrip, plan-review-events, e2e-real |
| 3 approve / deny with quotes | FormatPlanFeedback, allow updatedInput | planreview.go, hook.go | AllowEchoes, DenyCarriesEveryQuote, ClaudeProbe |
| 4 Pi tool | (d) | hive.ts, settings.go | hive.test.ts, PiProbe |
| 5 one reviewer | (c) + `external`; spawn-time reviewer on the entry; plugin override when Hive is selected (settings.json reviewer gets a warning, not suppression) | agent/planreview.go, daemon | ExternalPlanReviewer, ExternalReviewerWins / HiveSelected, manual |
| 6 no GUI → no hang | no_client, cancel at 0, hivebar excluded, 4-day cap | daemon.go | FailsFast, CancelsWhenLastAnswererLeaves, HivebarNotAnswerer, CancelsOnDisconnect |
| 7 themed markdown | marked lexer → React, token CSS | Markdown.tsx, plan-review.css | markdown.test, plan-review.test, e2e mock, ui-lint |

## Second opinion

- **Round 1: revise (confidence 7), 5 must-fix items, all applied.**
  1. Criterion 5 when Hive is selected: suppress a plugin reviewer through `--settings`.
  2. The race between the no-client check and the park: the answerer count now sits in the registry under the park lock.
  3. Pi now gets `no_client` when the GUI leaves, so it falls back to its terminal confirm.
  4. Missing files: `state.ts`, the e2e-real bridge, and `DESIGN.md`.
  5. Vacuous verification: the frontend commands ran from the wrong directory, and the `-list` count included the skipped probe tests.
- **Round 2: revise (confidence 7).** The reviewer confirmed fixes 2, 3 and 5 hold. It found 2 new must-fix items, both applied after the round. The pipeline allows only one reviewer re-run, so no third round.
  1. A mid-session switch of the reviewer setting could double-prompt. The reviewer choice is now fixed at spawn and stored on the registry entry, so a live switch cannot double-prompt.
  2. Reusing `PHASE.blocked` plus the Enter intercept would steal keystrokes from the live PTY. The tile state now comes from `pending_plan_review`, and there is no Enter intercept.
- **Nice-to-haves applied:**
  - A separate live check that the `enabledPlugins:false` override works.
  - Settings copy saying the override disables the whole plugin.
  - `GetExternalPlanReviewers` added to the mocks and bridges.
  - The node test run through `go test`.
  - `conn` closed on daemon stop.
  - Wire tests named so the count includes them.
- **Operator sign-off needed:** criterion 5 is narrowed. With Hive selected, only a *plugin* reviewer is suppressed. A reviewer hook written directly in a settings.json file still prompts too, and Settings shows a warning naming the file.

## Decision log

- **2026-09-24** — Scope is the core gate only; Ask AI, HTML review, plan history split out. Why: operator choice at brainstorm.
- **2026-09-24** — Pi plans come through a Hive-provided tool. Why: Pi has no plan mode; operator choice.
- **2026-09-24** — Opt-in, off by default; an external ExitPlanMode reviewer (e.g. plannotator) wins by default, with a setting to make Hive the reviewer. Why: operator choice.
- **2026-09-24** — Markdown: `marked` lexer only, tokens rendered to React elements, no innerHTML. Why: GFM coverage with zero transitive deps; keeps WhatsNew's no-innerHTML stance.
- **2026-09-24** — Pi: nudge only (promptSnippet/guidelines + system-prompt line), no edit/write gate. Why: a hard gate would trap trivial edits; operator choice.
- **2026-09-24** — Review UI: large modal (ModalShell lg), raised like the worktree-choice prompt. Why: simplest focused surface; operator choice.
- **2026-09-24** — Wait cap: 4 days (345600 s, matches plannotator); GUI disconnect cancels immediately. Why: operator choice.
- **2026-09-24** — Assumptions (uncorrected): long-held events-socket request + parked `pending_plan_review` on SessionInfo; every control client except hivebar counts as able to answer (ws-bridge fronts a real browser GUI and is how e2e-real drives the answer path; hivebar has no UI for it); hook reads the setting live per request; external-reviewer detection scans user/project/local/managed Claude settings + enabled plugins' hooks.json; live ExitPlanMode payload captured interactively during IMPLEMENT (headless `-p` disables ExitPlanMode).
- **2026-09-24** — Open question resolved: both "GUI never attached" and "GUI closed mid-review" fall back to the terminal prompt (the latter cancels the park; hook exits silently). Why: never hang is a success criterion; one rule is simpler.
- **2026-09-24** — Criterion 5 narrowed (operator approved at the plan stop): with Hive selected, a *plugin* reviewer is suppressed for new Claude sessions; a reviewer hook written directly in a settings.json cannot be suppressed and gets a Settings warning. The reviewer choice is fixed per session at spawn. Why: Claude runs all matching hooks in parallel with no precedence, and `--settings` hooks concatenate.- **2026-09-24** — Live check 1 (Claude Code 2.1.282, interactive tmux PTY): the payload has `tool_input.{plan, planFilePath}` (fixture `cmd/hived/testdata/hooks/permission_request_exitplanmode.json`). **Claude's own terminal dialog renders while the hook blocks.** If the user answers there, Claude kills the hook: the process is gone and it never wrote its end marker. That makes EOF-cancel the path that closes the Hive modal. Allow with `updatedInput` echoed works ("Allowed by PermissionRequest hook").
- **2026-09-24** — **The deny message wording matters.** A bare "Reviewer comments: … instead" message was treated by Claude as a prompt injection and ignored. The same content, attributed ("The user reviewed your plan in Hive and did not approve it yet. Revise the plan to address their comments, then call ExitPlanMode again." followed by per-passage quotes and the overall feedback), was applied. `FormatPlanFeedback` uses that framing. Why: this is feedback-lost-or-garbled, a named failure signal.
- **2026-09-24** — After an approve through the hook, Claude continues in default mode and asks before each edit. plannotator also sends `updatedPermissions setMode`. Choosing a post-approve mode is not in the spec, so it is left out and noted as a possible follow-up.
- **2026-09-24** — Live check 1b: `--settings '{"enabledPlugins":{"<id>":false}}'` overrides a user-level `enabledPlugins:true`. Tested with superpowers: the plugin is gone from the init plugin list and its SessionStart hook never fires. The plugin-suppression path is viable.
- **2026-09-24** — Live Claude probe (`TestClaudeProbePlanReview`) passes 3 of 3 once it cycles Shift+Tab until "plan mode on" appears. The first attempts never reached plan mode.
- **2026-09-24** — Live Pi probe: the tool and the review work end to end when the model is asked to use them. On a real task, the operator's default local model (Qwen3.6-35B-A3B) wrote the files without calling `hive_submit_plan`, even after quoting both injected rules word for word. Also added a `before_agent_start` system-prompt line; it was in the approved plan and had been dropped. The operator chose to keep the nudge only, with no write gate. The named failure signal depends on the model, and the probe stays as a measurement of it.
- **2026-09-24** — A Hive-owned Pi plan mode (review triggered at `agent_end`) was considered and rejected by the operator as out of scope for Hive. Pi review stays tool-based.
- **2026-09-24** — Implementation deviations from the plan text:
  - The spawn-time reviewer rides the Claude process environment (`HIVE_PLAN_REVIEWER`, inherited by the hook and sent as `PlanReviewRequest.reviewer`), not a registry field. It is fixed at spawn either way, and needs no registry–agent coupling.
  - The re-open affordance is a bar for the active session (`PlanReviewBar`, beside the opening-prompt bar), not a tile-header button. It is reachable for any session via ⌘B jump-to-attention.
  - Plan review controls sit below the agent list in Settings → Agents; `settings.spec` pins the list on screen at open.
  - The e2e-real spec loads `hive.ts` at runtime, because the frontend tsconfig has no node types.
- **2026-09-24** — A stale `GET_PLAN_REVIEW` must drop that session's pending review before moving on. Without that, the queue re-fetched the same stale review forever, and reviews queued behind it were never raised (found by `plan-review-events.test.ts`).

## Progress

- **2026-09-24** — Spec + issue #457 created via /hs-brainstorm; research started.
- **2026-09-24** — Plan approved (HTML review, 1 round) after two second-opinion rounds.
- **2026-09-24** — Implemented. Go, node, unit, dom, e2e (mock) and e2e-real suites green; live Claude probe 3/3; live Pi probe shows the model-dependent skip (see decision log).

## Open questions

_(none)_

## PR convergence ledger

- **2026-09-24 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 2b58e2d24b812027b4afd4b2a60bc6793af50522a561b88681a642d7a50265a2; threads_open: 2; action: autofix+push; head_sha: 745b04bb.
- **2026-09-24 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 51192c2ab605075cc88747a06aa3e61bedf5b430a483566d2664bfbad3fce725; threads_open: 0; action: autofix+push; head_sha: 567eefee.
