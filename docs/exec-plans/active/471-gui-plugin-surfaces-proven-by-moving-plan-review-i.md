# GUI plugin surfaces, proven by moving plan review into a bundled plugin

- **Spec:** [docs/product-specs/471-gui-plugin-surfaces-proven-by-moving-plan-review-i.md](../../product-specs/471-gui-plugin-surfaces-proven-by-moving-plan-review-i.md)
- **Issue:** #471
- **Status:** active
- **Phase:** 1 of 2
- **PR:** #472
- **Branch:** feature/471-gui-plugin-surfaces

## Summary

Give plugins four GUI surfaces (session view, palette command, sidebar badge, settings section) through the reserved manifest `ui` field, then prove them by moving the #457 plan-review UI out of core into a bundled, disabled-by-default first-party plugin. The full *why* lives in the spec.

## Research

FE = `cmd/hivegui/frontend/src`.

### Relevant code — plan review today (moves to the plugin)

- UI: `FE/app/modals/plan-review.ts` (213; pending map, one-at-a-time queue, fetch/answer), `FE/components/modals/PlanReview.tsx` (274; modal, passage comments, approve/deny, `capBytes`), `FE/components/PlanReviewBar.tsx` (32), `FE/components/Markdown.tsx` (186; lexer-only, no innerHTML; only PlanReview uses it), `FE/theme/components/plan-review.css` (113).
- Core hooks into it: `FE/store/store.ts:153-199` (`'plan-review'` hard-coded in the closed `ModalId` union), `FE/app/events.ts:72-77,747-752,890,900,944-946,1241`, `FE/app/keyboard.ts:74,360-370,1110`, `FE/main.tsx:85,334`, `FE/components/App.tsx:35-36,118,151,181`, `cmd/hivegui/frontend/index.html:155` (`#plan-review` host), `FE/bridge.ts:52-54`, `FE/app/state.ts:104-127`.
- Settings: `FE/components/modals/Settings.tsx:192-212,393-407,711-712,~940-995` (opt-in checkbox + reviewer select on the Agents tab). Go: `cmd/hivegui/app_calls.go:127-161,212-235,913-947`; values in `agent-settings.json` (`internal/agent/settings.go:57,64,114`), read/written by the GUI directly (not over the wire).
- Wire: `SessionInfo.pending_plan_review` (`internal/wire/control.go:193-202`, set at `internal/registry/registry.go:289`); `GET_PLAN_REVIEW` 0x34 → `PLAN_REVIEW` 0x35 / `plan_review_stale`; `RESOLVE_PLAN_REVIEW` 0x36 (`internal/daemon/daemon.go:1779-1806`). GUI event `planreview:plan` (`internal/wire/client.go:139`). Plugins can already send both control frames (`docs/plugins.md` table; `internal/plugin/limiter.go:42`).
- Daemon decision: `internal/daemon/planreview.go:67-128` — setting off ⇒ Disabled; Claude + external reviewer + reviewer≠hive ⇒ External; else park; `ErrNoAnswerer` ⇒ NoClient (no-GUI fallback). Deny text from `agent.FormatPlanFeedback` (`internal/agent/planreview.go:222`) — stays core.
- **Answerer rule:** `canAnswer` = not `hivebar/` (`internal/daemon/planreview.go:22-24`); applied at `daemon.go:762` with `tag == nil`, so plugin connections are never answerers (`docs/design-docs/control-plane.md:127-155`; `internal/daemon/plugin_test.go:142-150`). Any non-hivebar, non-plugin control client counts as "a GUI" — including one whose plan-review UI is disabled. After the move, that would park reviews nobody can see.

### Relevant code — extension points (none open today)

- Palette: literal array in `FE/main.tsx:~145-299` handed once to `FE/app/modals/command-palette.ts` (`commandTable`); rendered by `FE/components/modals/CommandPalette.tsx`.
- ⌘/ overlay: `FE/components/modals/HelpOverlay.tsx:35` memoizes static `shortcutGroups()` from `FE/lib/shortcuts.ts` (342; its header :1-15 lists the five-file drift surface). Keys: `FE/app/keyboard.ts` (1205) if-chain; `FE/lib/keymap.ts` holds predicates only.
- Sidebar: `FE/components/SessionRow.tsx` (399) computes indicators inline (plan badge `planOf`:42, :225-260); CSS `FE/theme/components/session-row.css`.
- Settings: closed `TabId` union `Settings.tsx:106`, `tabsFor()` :110-117; Plugins tab `FE/components/modals/PluginsPanel.tsx` (286) fed by store `plugins` (`store.ts:136,1279-1300`) via `FE/app/plugins.ts`.
- Session view: no per-session view switcher. `ViewMode` is single/grid (`FE/lib/view.ts:10`). The closest precedent is the activity panel/tile (`FE/components/activity/*`, store toggles `store.ts:99-105`). Terminals must never be React-owned (`App.tsx` header, Invariant 5).
- Every surface needs a small store-backed contribution list. No registry exists to extend.

### Relevant code — loading, plugin runtime, bundling

- Frontend: one Vite bundle, embedded `frontend/dist` (`cmd/hivegui/main.go:16-17`), `AssetServer{Assets}` only (`cmd/hivegui/window_options.go:21`). Wails v2.15 `assetserver.Options.Handler` serves any GET missing from Assets, which is the hook for `/plugins/<id>/…`. No CSP, no dynamic `import()`, no iframes today.
- **Containment: none.** No React error boundary anywhere; a single root (`FE/main.tsx:390`), with every region a portal of it. One plugin render throw unmounts the whole GUI. Hangs are only detected (`FE/lib/freeze-heartbeat.ts`).
- Manifest: `internal/plugin/manifest.go:29` `APIVersion="0.1"` is an exact-match gate (:112), so a bump refuses every 0.1 plugin, including `plugins/webhook`. `UI json.RawMessage` is reserved (:47) and hard-rejected (:101-103, install fails). `main.command` is required.
- Store: `plugins.json` + `plugins/<id>/` + `plugin-data/<id>/` under `registry.StateDir()` (`internal/plugin/store.go:11-30`). `Remove` deletes the install dir (`manager.go:432`).
- `wire.PluginInfo` (`control.go:1593-1611`) has no path/ui field, so the GUI cannot see plugin manifests today.
- **No builtin-plugin concept.** Precedent: `EnsurePiExtension` go:embeds `internal/agent/pi/hive.ts` and materializes it into the state dir on every `hived` start (`internal/agent/pi.go:18`, `cmd/hived/main.go:112`). It works on every platform with no `build.sh` change.
- DaemonContract is 19 (`internal/buildinfo/contract.go:188`). The gate watches wire/daemon/session/registry/cmd/hived, not `internal/plugin` or `internal/agent`.
- `CLIENT_COMMAND` has a closed allowlist and no free-form body. It is not a fit for plugin UI traffic.

### Tests that must follow the move

- DOM: `test/dom/plan-review.test.tsx`, `plan-review-events.test.ts`, `blocked-tile-keyboard.test.ts:123-157`, `settings.test.tsx:405-445` (+ fixtures in settings-editor/-updates).
- e2e mock: `test/e2e/plan-review.spec.ts`, mock `test/e2e/wails-mock.ts:931-970`.
- e2e-real: `test/e2e-real/plan-review.spec.ts` (writes agent-settings.json itself, :24-36), `plugins.spec.ts`, `wails-bridge.ts:239-263,362-374`; `cmd/hived-ws-bridge/main.go:406-458` (control HELLO `ws-bridge/control` counts as answerer).
- Go: `internal/daemon/plan_review_test.go`, `internal/registry/planreview_test.go`, `internal/agent/planreview_test.go`, `cmd/hived/hook_plan_review_test.go`, `plan_review_probe_test.go`; plugins: `internal/plugin/{manager,install,doc,sdk_drift}_test.go`, `internal/daemon/plugin_test.go`, `cmd/hived/plugin_e2e_test.go`.

### Constraints / dependencies

- Criterion 5 (hang containment) vs "no sandbox": plugin JS in the main webview can be contained for throws (error boundary) and async hangs (timeouts), **not** for a synchronous busy loop. WKWebView runs same-page iframes on the same thread.
- Browser e2e (ws-bridge) has no Wails AssetServer, so plugin UI assets must be servable there too.
- The plugin API bump interacts with the exact-match gate and the SDK drift tests.
- The daemon-side answerer semantics change, so bump DaemonContract.

### Prior lessons

- A native keydown listener on a modal root (Settings' Enter-to-save) runs before React `onKeyDown`. Plugin settings inputs with their own Enter must be excluded via `closest()`, with a DOM test that sends Enter (PR 465).
- Deny feedback to Claude must say it came from the user and what to do next, or Claude reads it as injection. Keep it in `agent.FormatPlanFeedback` (core), and keep the probe.
- Moves leave stale pointers. Grep every moved symbol/path, including `docs/`, AGENTS.md and the drift list in `FE/lib/shortcuts.ts:7-15`, in the same diff.

### Conventions card

- Build: `./build.sh` (macOS .app); GUI alone `wails build` (never `-s`).
- Tests: `scripts/test.sh` (layers go · unit · dom · e2e); `npm run test:e2e:real` separately (isolated HIVE_SOCKET/HIVE_STATE_DIR). Local Playwright with `CI=1`.
- Lint/types: `biome ci .`; `npm run typecheck` (fresh worktree: `./scripts/ci-bootstrap.sh` first); `scripts/ui-lint.sh`; per-GOOS `staticcheck` + `go vet` for darwin/linux/windows; Go under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- TDD: every behavior change ships with its test; boil the lake.
- Wire: snake_case JSON, JS reads `snake_case ?? camelCase`; update GUI, ws-bridge and testclient in lock-step; bump `buildinfo.DaemonContract` for daemon-side behavior changes (`scripts/check-daemon-contract.sh`).
- Keybindings: keymap + ⌘/ overlay + palette + README table + changeset. UI tokens/icons per `docs/design-docs/ui/` (enforced by `scripts/ui-lint.sh`). Changesets in `.changesets/`, never edit CHANGELOG.md; user-visible feature ⇒ `site/features.json` entry with `since: "Unreleased"`. DESIGN.md update (new surface/rule).

## Approach

**ES module + React host API.** A plugin's `ui.entry` is `import()`ed from `/plugins/<id>/<entry>?v=<version>-<commit>`. It default-exports `activate(hive)`, which returns (or resolves to) a contributions object. Surfaces are declared at runtime, not in the manifest, which keeps `wire.PluginInfo` small and the daemon UI-agnostic. The cost is that ⌘/ lists a plugin's commands only once it has loaded. Rejected: a DOM `mount(el)→cleanup` contract, because the error boundary couldn't catch render throws and every plugin would duplicate the modal chrome. Plugins get the host's React as `hive.React` (two React copies break hooks). No build step is needed: in-repo plugins use `const h = hive.React.createElement`.

**Host API (plugin API 0.2, documented in docs/plugins.md, one frozen object per plugin):**
```
hive = { apiVersion:'0.2', pluginId, React,
  components: { ModalShell, Button, Markdown, Kbd, Chip },
  useSessions(), getSessions(), useActiveSessionId(),
  on(event, cb) → off,                 // same event names as internal/wire/client.go:120-143
  actions: { getPlanReview(sid,rid), resolvePlanReview(ans), externalPlanReviewers(), switchTo(sid) },
  settings: { get(), use(), set(obj) }, // plugin-data/<id>/ui-config.json via new SET_PLUGIN_CONFIG (UI plugins only)
  openSessionView(sid, props) → Promise<'closed'|'dismissed'>, closeSessionView(), togglePanel() }
contributions = { sessionView?: { modal?, banner?, panel?: {title, component} },
  commands?: [{ id, title, keys?: {key, shift?, alt?}, run }],
  badge?: (session) => ({ text, title, tone }) | null,
  settings?: Component, deactivate?() }
```
- Markdown and ModalShell become public host components. The plugin can't import `marked` without a build step, and the focus trap and a11y behavior should stay uniform. `actions.*` wrap existing Go bindings: these are the generic plugin-facing actions.
- Badges are descriptors, so the host renders them with Chip and tokens stay consistent.

**Mounting.** A new `<PluginSurfaces/>` in App.tsx renders portals only:
- session modal: `#plugin-view` (replaces `#plan-review`, index.html:155-156)
- docked panel: `<aside id="plugin-panel">`, a sibling of `#activity-panel`
- tile banner: `term.overlays` via TileChrome
- badges: SessionRow
- settings section: under the plugin's row in PluginsPanel (`Settings.tsx:771` already early-returns for `#settings-panel-plugins`)

`#terms` is never touched. The modal gets a generic ModalId `{id:'plugin-view', pluginId, sessionId, props}` with one keyboard gate: Escape resolves `'dismissed'`, and focus is trapped.

**Containment.** Each plugin gets a PluginBoundary (error boundary + Suspense).
- Any throw marks the plugin `failed`: its surfaces unmount, its `<link>` is removed, and its Settings row shows the error with a Disable button.
- Timeouts: `import()` 10s, `activate()` 5s, a Suspense fallback still showing after 5s means `failed`. A generation token ignores late resolves.
- `on` callbacks and `run()` are wrapped in try/catch, and a throw means `failed`.
- `settings.set` is coalesced: one in flight, latest wins, capped at 64 KiB.
- A synchronous busy loop is out of scope (only freeze-heartbeat detects it).

**Keybindings.** Plugin chords are dispatched at the tail of the core Mod if-chain (keyboard.ts:~640), so core always wins. A chord whose label collides with `shortcutGroups()` is refused: no hint, plus a console warning. Between plugins, the first enabled wins. Effective chords show in the palette and in a "Plugins" ⌘/ group.

**Reviewer choice (Phase 2).** Stored in `plugin-data/plan-review/ui-config.json` `{"reviewer":"external"|"hive"}`, written by the Manager via SET_PLUGIN_CONFIG. Claude spawn (`HIVE_PLAN_REVIEWER`, claude.go:308,321-326) and the daemon gate both run in hived. The daemon gives the registry `SetPlanReviewSource(func() (on bool, reviewer string))`, which reads `Manager.Enabled("plan-review")` and `Manager.Config`, and `registry.spawnInfo` (registry.go:942-950) overlays it onto the agent.Settings snapshot. The callback runs **before** `r.mu` is taken, in the same place the `SpawnSettings()` file read already happens, so there is no Manager→registry lock inversion. `plan_review`/`plan_reviewer` are dropped from settingsFile. Rejected: keeping them in agent-settings.json, which would need a plan-review-specific core API.

**Answerer rule (Phase 2).** The GUI sends a new control request `SET_CLIENT_UI {plugin_uis:[ids]}` whenever its set of activated plugins changes (failed or disabled plugins drop out). Go stores the last set and re-sends it after every control reconnect (app_control.go:69; ws-bridge main.go:586).
- The daemon counts only `tag==nil && canAnswer` connections whose set contains `plan-review`, and passes that count to `Registry.SetReviewAnswerers(n)`.
- `ParkPlanReview` (registry/planreview.go:87) checks it. When the count reaches zero, pending reviews are withdrawn as `no_client` (moved out of `SetAnswerers`, registry.go:888-899).
- Worktree choice keeps the general count.
- Announces from plugin sockets and hivebar are ignored.

## Files to change

### Phase 1: surfaces, example plugin, containment, docs, API 0.2 (criteria 4–8)
1. `internal/plugin/manifest.go`:
   - `APIVersion="0.2"`
   - `UI *UIEntry{Entry, Style}`. The entry must be relative, clean, with no `..` and no scheme, and end in `.js`/`.mjs`; `style` is optional `.css`.
   - Drop the hard reject (:101-103) and require `main` or `ui`.
2. `internal/plugin/manager.go`:
   - `infoLocked` adds UI, and adds Config only for plugins that declare `ui`, read from `ui-config.json`. A headless plugin's hand-edited `config.json` (webhook URL, often with a secret token) is never read, broadcast or written.
   - A UI-only enable spawns no runner and reports `running`.
   - `SetConfig(id, raw)`: refused for a plugin without `ui`. Validates JSON, caps at 64 KiB, writes `DataPath(id)/ui-config.json` atomically, and emits `updated`. An invalid or oversize file on disk is reported as empty config with a `status_detail` note; it never fails `infoLocked`.
   - Add `Enabled(id)` and `Config(id)`.
3. `internal/wire/control.go`: `PluginInfo` gets `UI *PluginUI` and `Config json.RawMessage`; add `SetPluginConfigReq`.
4. `internal/wire/frame.go`: add `FrameSetPluginConfig` 0x3d, add it to ControlRequestFrames, and add its name and decoder cases.
5. `internal/plugin/limiter.go`: SetPluginConfig costs 10.
6. `internal/daemon/daemon.go`: dispatch SET_PLUGIN_CONFIG.
7. `internal/buildinfo/contract.go`: bump the contract 19→20 and add a history entry.
8. `cmd/hivegui/window_options.go:21`: `Handler: pluginassets.Handler(registry.StateDir())`. Verify that `wails dev` falls through to it when Vite 404s `/plugins/…`; if it does not, add a Vite dev passthrough (docs/verifying-the-gui-by-hand.md).
9. `cmd/hivegui/app_calls.go`: add bindings `SetPluginConfig` and `PluginAssetBase`.
9b. `internal/wire/testclient/client.go`: add a `SetPluginConfig` helper next to `ListPlugins` (:300). Phase 2 adds `SetClientUI`.
10. `cmd/hived-ws-bridge/main.go`: add `mux.Handle("/plugins/", cors(pluginassets.Handler(state)))` and a SetPluginConfig case.
11. `FE/src/store/store.ts`: add the `'plugin-view'` ModalId and entry, a `pluginUI` slice, and `pluginPanel`.
12. `FE/src/components/App.tsx`: add `<PluginSurfaces/>`.
13. `FE/src/components/TileChrome.tsx`: add `<PluginTileBanners>`.
14. `FE/src/components/SessionRow.tsx`: add `<PluginBadges>`.
15. `FE/src/components/modals/PluginsPanel.tsx`:
   - Show each plugin's settings section inside a boundary.
   - Show the failed state with a Disable button.
   - For UI-only plugins, the consent text says "runs code inside the Hive app".
16. `FE/src/app/modals/command-palette.ts`: `paletteCommands()` concatenates plugin commands.
17. `FE/src/components/modals/HelpOverlay.tsx`: add a "Plugins" group, read from the store and not memoized.
18. `FE/src/app/keyboard.ts`: add the plugin-view modal gate, tail `dispatchPluginChord`, and plugin-view in `ideaKeysBlocked`.
19. `FE/src/main.tsx`: call `initPluginHost()` and run `ListPlugins()` at boot.
20. `FE/index.html`: add the `#plugin-view` and `#plugin-panel` hosts.
21. `FE/src/theme/layout.css`: add the plugin-panel column.
22. `FE/src/bridge.ts`: add `SetPluginConfig` and `pluginAssetURL`.
23. `FE/test/e2e/wails-mock.ts`: mock `pluginAssetURL` (fixtures and `/@fs` repo plugins) and `SetPluginConfig`, and set api_version to 0.2.
24. `FE/test/e2e-real/wails-bridge.ts`: `pluginAssetURL` returns the bridge origin; add `SetPluginConfig`.
25. `FE/vite.config.js`: mock mode only; add `fs.allow` for repo `plugins/` and `__HIVE_PLUGINS__`.
26. `plugins/webhook/hive-plugin.json`: api 0.2.
27. `plugins/sdk/hive-plugin.mjs`: add the new frame and typedef, and re-vendor the SDK into webhook.
28. Update `api_version` 0.2 in test fixtures: `cmd/hived/plugin_e2e_test.go:208`, `FE/test/dom/plugin-events.test.ts:129`, `settings-plugins.test.tsx:34`.
29. `scripts/ui-lint.sh`: also lint `plugins/**/*.css`.
30. `scripts/measure-idle.sh`: add a `--gui` mode.
31. Docs:
   - `docs/plugins.md`: the `ui` manifest, surfaces, lifecycle and timeouts, host API, trust, the 0.x promise, chord policy, CSS namespacing, and the SET_PLUGIN_CONFIG row.
   - DESIGN.md: plugins and the GUI reading `plugins/<id>` read-only.
   - `docs/design-docs/ui/README.md`.
   - README shortcut note.
   - A changeset.
   - `site/features.json`.
   - The drift list in `FE/src/lib/shortcuts.ts:7-15`.

### Phase 2: plan-review migration, bundling, answerer rule (criteria 1–3)
32. `internal/plugin/manager.go`: `New` calls `EnsureBuiltins`; `Remove` refuses with `ErrBuiltin`; `PluginInfo.Builtin`.
33. `internal/plugin/store.go`: `record.Source="builtin"`.
34. `internal/wire`: add `PluginInfo.Builtin`, plus `SetClientUIReq{PluginUIs}` as frame 0x3e.
   - 0x3e stays in `ControlRequestFrames`, because every control request is listed there and the drift tests need it complete.
   - It gets a `FrameCost` of 10.
   - When it arrives from a plugin socket or from hivebar it is read and dropped, with no error reply.
   - Lock-step updates: the `plugins/sdk/hive-plugin.mjs` frame table and typedef, the webhook re-vendor, a `docs/plugins.md` row marked "GUI only; ignored from plugins", and testclient `SetClientUI`.
35. `internal/daemon/daemon.go`: seed `reg.SetReviewAnswerers(0)` next to `reg.SetAnswerers(0)` (:374), so the registry never treats "unset" as "assume a client" for reviews. Track each connection's announced set, add the SET_CLIENT_UI case, decrement on disconnect, and call `reg.SetPlanReviewSource`.
36. `internal/daemon/planreview.go`: a disabled plugin means Disabled; add `addReviewAnswerer`.
37. `internal/registry/{registry,planreview}.go`: add `reviewAnswerers` and `SetReviewAnswerers`, make ParkPlanReview check them, and add the spawnInfo overlay.
38. `internal/agent/settings.go`: remove `plan_review`/`plan_reviewer` from settingsFile, keeping them as runtime fields.
39. `cmd/hivegui/app_calls.go`: remove the PlanReview settings fields; add `SetClientUI`, re-sent on reconnect in app_control.go, including an empty set (only a never-set value is skipped). Test: `TestSetClientUI_ResentAfterReconnectIncludingEmpty`.
40. `cmd/hived-ws-bridge/main.go`: store SetClientUI and re-send it after dial.
41. `internal/buildinfo/contract.go`: bump the contract 20→21.
42. Delete the core plan-review UI:
   - `FE/src/app/modals/plan-review.ts`, `PlanReview.tsx`, `PlanReviewBar.tsx`, `plan-review.css`, and the import at `theme/components/index.css:42`
   - the ModalId entry
   - hooks in `keyboard.ts`, `events.ts`, `main.tsx`, `App.tsx`, `index.html:154-156`, `state.ts:124-127` (`pendingPlanReviewId`), `Settings.tsx` (:39,191-212,393-407,711-712,937-995) and `settings.css:138`
43. `PluginsPanel.tsx`: no Remove button for builtins.
44. Docs:
   - control-plane.md :110-155 (gate + announce rule)
   - `docs/plugins.md` limits
   - DESIGN.md:99
   - a spec 457 pointer
   - a changeset noting the accepted regression
   - grep the moved symbols across `docs/` and AGENTS.md

## New files
**Phase 1**
- `internal/plugin/pluginassets/assets.go`: a leaf package with no Manager dependencies, so the GUI does not link the supervisor. `Handler(stateDir)` serves `GET /plugins/<id>/<path>` with `http.ServeFileFS` over `os.DirFS(installDir)`. It stats the file first and returns 404 on `IsDir()` (ServeFileFS would otherwise list the directory), and `..` is refused. It validates the id with `ValidID`, which moves here from `internal/plugin/manifest.go:34` (manifest.go calls it, so the leaf never imports `internal/plugin`), sets `Content-Type: text/javascript` explicitly for `.js`/`.mjs` (WKWebView refuses module scripts with a wrong MIME type), and sends `no-store`.
- `FE/src/app/plugin-host.ts`: the loader (store-driven, no-op when no plugin has UI, `import()` with timeout, activate, style link), the `hive` object builder, chord normalization and conflicts, `settings.set` coalescing, and an injectable importer for tests.
- `FE/src/components/PluginSurfaces.tsx`: PluginBoundary, PluginSurfaces (modal and panel portals), PluginTileBanners, PluginBadges and PluginSettingsSection.
- `plugins/session-notes/`: a UI-only example written against `docs/plugins.md` only (`hive-plugin.json`, `ui.mjs`, `ui.css`, README). Its four surfaces:
  - a note modal, a pinned-note tile banner and a notes panel
  - "Toggle notes panel" (Mod+Alt+N) and "Edit session note"
  - a "Note" badge
  - a settings section with a checkbox and a text input that has its own Enter
- `FE/test/fixtures/plugins/`: `ui-throws/`, `ui-hangs/` (`?mode=` for a top-level await that never settles, an activate that never resolves, and a component that suspends forever), `ui-flood/` (setState interval, a `settings.set` loop, an `on` re-subscribe loop) and `ui-ok/`.

**Phase 2**
- `plugins/embed.go`: `//go:embed all:plan-review`, `var Builtin embed.FS`.
- `internal/plugin/builtin.go`: `EnsureBuiltins(stateDir)`. Materializes `plugins/<id>/` on every start, skipped when content matches. It writes to a temp dir, renames the old dir to `.<id>.old`, renames the new dir in, then removes the old one. At start it removes a leftover `.old` or temp dir from a crash (a plain rename onto a non-empty dir fails with ENOTEMPTY on POSIX and on Windows). It adds `{source:"builtin", enabled:false}` if absent, and never touches an existing `enabled` value.
- `plugins/plan-review/`: `hive-plugin.json` (UI-only, 0.2); `ui.mjs`, a port of `plan-review.ts` and `PlanReview.tsx` (banner replaces the bar, "Plan" badge, "Review pending plan" command, settings section with the reviewer select and external-reviewer warning); `ui.css`; README.

## Tests
**Go, Phase 1**
- install_test:
  - `TestManifest_UIOnlyValid`
  - `TestManifest_RequiresMainOrUI`
  - `TestManifest_UIEntryRejects{Traversal,Absolute,URL,NonJS}`
  - `TestManifest_API01Refused`
- manager_test:
  - `TestManager_UIOnlyEnableSpawnsNothing`
  - `TestManager_SetConfigPersistsAndEmits`
  - `TestManager_SetConfigRejects{Oversize,InvalidJSON,UnknownID}`
  - `TestManager_ConfigSurvivesReinstall`
  - `TestManager_HeadlessConfigNeitherExposedNorClobbered`: a headless plugin's `config.json` is not in PluginInfo, and SetConfig on it is refused.
- assets_test:
  - `TestAssetHandler_ServesModuleWithJSMime`
  - `TestAssetHandler_RefusesTraversal`
  - `TestAssetHandler_BadOrUnknownID404`
  - `TestAssetHandler_DirectoryIs404`
  - `TestHiveguiDoesNotDependOnPluginManager`: `go list -deps ./cmd/hivegui` does not contain `internal/plugin` (only `internal/plugin/pluginassets`).
- The existing SDK drift and doc tests go red until the SDK, webhook and docs are updated.
- daemon plugin_test:
  - `TestSetPluginConfig_BroadcastsToAllControlClients`
  - `TestSetPluginConfig_FromPluginSocketCharged`
- `cmd/hivegui`: `TestAppOptions_HandlerServesPluginAssets`
- ws-bridge:
  - `TestBridge_ServesPluginAssetsWithCORS`
  - `TestBridge_SetPluginConfigForwarded`
- plugin_e2e: `TestPluginE2E_UIOnlyInstallEnable`

**Go, Phase 2**
- builtin_test:
  - `TestEnsureBuiltins_FreshStateInstalledDisabled`
  - `TestEnsureBuiltins_RewritesStaleFilesKeepsEnabled`: runs on an existing, populated, stale install dir.
  - `TestEnsureBuiltins_RecoversLeftoverAsideDir`
  - `TestRemoveBuiltinRefused`
  - `TestInstallBuiltinIDRefused`
  - `TestBuiltinPlanReviewManifestValid`
- plan_review_test: update DisabledByDefault, and add:
  - `ParksOnlyWhenUIAnnounced`
  - `NoClientWhenGUIHasNoPluginUI`: a real control connection through the daemon that never announces. The test must not call `SetReviewAnswerers` itself.
  - `WithdrawnOnUnannounce`
  - `AnnounceIgnoredFrom{PluginSocket,Hivebar}`: the frame is dropped, the connection stays open, and no ERROR is sent.
  - `DisabledWhenPluginDisabledMidSession`
  - Existing tests enable and announce in setup.
- registry:
  - `TestSetReviewAnswerersZeroWithdrawsNoClient`
  - `TestWorktreeChoiceUsesGeneralAnswerers`
  - `TestSpawnInfoReviewerFromPluginConfig`: asserts `HIVE_PLAN_REVIEWER`, disabled Claude plugin reviewers, **and** Pi `HIVE_PI_PLAN_REVIEW` (`internal/agent/settings.go:271`).
  - `TestSpawnInfoSourceCalledOutsideLock`: the callback re-enters the registry without deadlocking.
- agent: `TestLegacyPlanReviewKeyIgnored`
- hook/probe tests switch their setup.

**Unit**
- `plugin-chords.test.ts`: normalization, core collision refused, first plugin wins.
- Phase 2: `no-core-plan-review.test.ts` asserts the deleted files are absent, and greps `src/**` for `plan-review|planReview|PlanReview|planreview` against an exact file:symbol allowlist: the wire field `pending_plan_review`/`pendingPlanReview` in the store and bridge types, and the `actions.*` wrappers in `bridge.ts` and `plugin-host.ts`.

**DOM**
- `plugin-host.test.tsx`:
  - With zero plugins, the importer is not called, no link is added, and no SetClientUI is sent.
  - A throw is contained.
  - Each timeout marks the plugin failed, and late resolves are ignored.
  - A throwing `on` handler marks the plugin failed.
  - Disabling unmounts the plugin and removes its link.
  - A `settings.set` flood leaves at most one set in flight.
  - Phase 2: a GUI that announced `[plan-review]` sends `SetClientUI([])` when the plugin is disabled, and again when it fails.
- `plugin-surfaces.test.tsx`:
  - badge, banner and panel toggle
  - openSessionView: Escape resolves `'dismissed'` and focus returns
  - palette hint
  - the ⌘/ Plugins group
  - a plugin binding ⌘T gets no hint and only the core action runs
- `settings-plugins.test.tsx`:
  - Enter-listener trap: a plugin input's Enter neither saves nor closes Settings.
  - A throwing section is contained, and Disable calls `SetPluginEnabled(false)`.
  - UI-only consent text.
  - Phase 2: builtin rows have no Remove button.
- Phase 2 ports:
  - `plan-review.test.tsx` and `plan-review-events.test.ts` load `plugins/plan-review/ui.mjs` through the host.
  - `blocked-tile-keyboard` uses plugin-view.
  - The settings plan-review assertions move to a plugin settings test.

**e2e mock**
- `plugin-surfaces.spec.ts`: session-notes, all four surfaces.
- `plugin-containment.spec.ts`: for each fixture, switch sessions, type into the mock pty, open Settings with ⌘,, then check the failed row and disable it.
- Phase 2: port `plan-review.spec.ts` (5 tests) and delete the dead mock paths.

**e2e-real**
- `plugin-ui.spec.ts`: install session-notes by directory, accept consent, exercise all four surfaces, and assert `plugin-data/session-notes/ui-config.json`; install ui-throws, check the GUI is still usable, then disable it.
- Phase 2: port `plan-review.spec.ts` to enable the builtin through Settings → Plugins, not by writing agent-settings. Add tests for:
  - on fresh state, builtin listed installed+disabled with no Remove button (criterion 3)
  - plugin disabled: the requester gets disabled
  - plugin enabled but the UI failed (`ui.mjs` corrupted in the isolated state dir): no_client
- Update `plugins.spec.ts`.

**Idle cost (criterion 6).** `scripts/measure-idle.sh --gui [--ref origin/main]`:
- `wails build`, then launch in an isolated HOME / HIVE_SOCKET / HIVE_STATE_DIR.
- Sample `ps %cpu,rss` of hivegui and its WebContent processes at 1 Hz for 60s, reporting mean and max.
- Startup time runs from launch to a new `boot: ready` log line.
- Run 3× on the base and 3× on the branch; the table goes in the PR. Also run the existing daemon `measure-idle.sh --ref`.

## Verification
```sh
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/plugin/... ./internal/daemon/... ./internal/registry/... ./internal/agent/... ./cmd/...
for os in darwin linux windows; do GOOS=$os go vet ./... && GOOS=$os staticcheck ./...; done
scripts/check-daemon-contract.sh origin/main HEAD
cd cmd/hivegui/frontend && ./node_modules/.bin/biome ci . && npm run typecheck
npx vitest run test/unit/plugin-chords.test.ts test/unit/no-core-plan-review.test.ts
npx vitest run test/dom/plugin-host.test.tsx test/dom/plugin-surfaces.test.tsx test/dom/settings-plugins.test.tsx test/dom/plan-review.test.tsx
CI=1 npx playwright test test/e2e/plugin-containment.spec.ts test/e2e/plugin-surfaces.spec.ts test/e2e/plan-review.spec.ts
CI=1 npm run test:e2e:real -- plugin-ui.spec.ts plan-review.spec.ts plugins.spec.ts
scripts/ui-lint.sh --strict && scripts/test.sh
scripts/measure-idle.sh --gui --ref origin/main && scripts/measure-idle.sh --gui
```

## Open questions / risks
- **Lock order.** SET_CLIENT_UI takes `d.mu` then `r.mu`, the same order as addAnswerer. Withdrawal happens under `r.mu` only.
- **Enable-to-announce window.** Roughly 100ms: a review arriving then gets no_client and falls back to the terminal, which is safe.
- **Old GUI with a new daemon.** It never announces, so reviews fall back; the contract bump restarts the daemon.
- **Hard-coded plugin id.** `planReviewPluginID="plan-review"`: a third-party plugin cannot take over the answerer role.
- **Silent skips.** e2e must fail, not skip, without WS_BRIDGE_URL. A wrong `/@fs` fs.allow fails loudly with 403s. Idle cost is a recorded number, not a CI gate.
- **Runaway.** A sync busy loop is out of scope. The flood fixture proves batching and coalescing hold. Plugin `on` handlers must be cheap (documented).
- **Module cache.** `import()` caches per URL, so a same-version reinstall needs a reload (`ponytail:` comment; switch to a content hash if it matters).
- **Plugin CSS** is global and trusted. The docs require `.<id>-` prefixes and tokens, but only in-repo plugins are linted.
- **Keybinding conflicts** are label-based and can miss range labels, but tail dispatch stays safe.
- **Loosened invariant.** The GUI and ws-bridge now read `plugins/<id>/` read-only.

## Success criteria coverage
| # | Covered by |
|---|---|
| 1 | Phase 2 items 32–40; ported dom/e2e/e2e-real tests; TestSpawnInfoReviewerFromPluginConfig; NoClient tests; plugin ui.css theme test |
| 2 | item 42; no-core-plan-review.test.ts |
| 3 | embed.go, builtin.go; TestEnsureBuiltins_*; e2e-real builtin test |
| 4 | plugins/session-notes; plugin-surfaces.spec.ts; plugin-ui.spec.ts |
| 5 | PluginBoundary + timeouts + fixtures; plugin-host.test.tsx; plugin-containment.spec.ts |
| 6 | zero-plugin DOM test; measure-idle.sh --gui numbers in PR |
| 7 | items 1, 26–28, 31; drift + doc tests |
| 8 | items 16–18; plugin-surfaces.test.tsx; README + changeset |

## Second opinion (round 1)
verdict: revise, confidence 7. All 8 must-fix items applied: ui-config.json split from the headless config.json; SetReviewAnswerers(0) seeded at startup; contract-check args; testclient helpers; 0x3e SDK/doc/cost/drop; spawnInfo callback outside r.mu plus the Pi env assertion; aside-rename builtin swap; unannounce-to-[] tests. Adopted nice-to-haves: pluginassets leaf package, ServeFileFS plus explicit JS MIME, wails dev passthrough check, broader no-core grep.

## Second opinion (round 2)
verdict: approve, confidence 8. No must-fix items. Nice-to-haves applied: directory requests return 404, `ValidID` moved into the leaf package plus a dependency guard test, exact allowlist, renumbering.

## Decision log

- **2026-09-28** — Plan review moves to a plugin (not a plugin-provided alternative reviewer). Why: operator choice in /hs-brainstorm; proves the GUI surfaces on a real shipped feature.
- **2026-09-28** — Old in-Hive plan review setting is not migrated; upgrading users find the plugin disabled. Why: operator accepted this regression explicitly (spec Non-goals).
- **2026-09-28** — Plugin owns UI only; agent-side plan review stays in core. Why: plugins injecting agent hooks is a much larger API (spec Non-goals).
- **2026-09-28** — Plugin UI = ES modules loaded by `import()` into the main webview, each surface inside an error boundary with load/render timeouts. Why: operator choice; general-purpose, and plan review ports almost as-is. Consequence: criterion 5's "never finishes" is tested as an async hang (a promise that never resolves, a module that never loads). A synchronous busy loop cannot be contained in WKWebView and is out of scope.
- **2026-09-28** — One switch: plan-review plugin enabled = plan review on. The core `PlanReview` agent setting is removed and the reviewer choice moves to the plugin's Settings section. Why: operator choice; two switches for one feature is noise. Criterion 1's "review turned on" reads as "plugin enabled".
- **2026-09-28** — Plugin API bumps to exact `0.2`; `plugins/webhook` bumps with it and third-party 0.1 plugins are refused. Why: operator choice; 0.x promises no compatibility.
- **2026-09-28** — Session view surface = both a session modal + tile banner and a docked side panel. Why: operator choice.
- **2026-09-28** — Assumptions (uncorrected at round B): plugin UI assets are served from the installed plugin dir by the GUI's Go AssetServer Handler and by `hived-ws-bridge` (no wire file transfer); the bundled plugin is go:embedded in `hived` and materialized into `plugins/plan-review/` on every start (EnsurePiExtension pattern), and it is not removable, only disableable; `main` becomes optional when `ui` is declared; the GUI announces its loaded plugin UIs to the daemon so a review parks only when a GUI can show it, and otherwise takes the no-GUI fallback; `FormatPlanFeedback`, the hook, the Pi tool and external-reviewer detection stay core.
- **2026-09-28** — Dropped `TestHiveguiDoesNotDependOnPluginManager`. Why: `cmd/hivegui` already imports `internal/daemon` (which imports `internal/plugin`), so the guard could never pass. `pluginassets` stays a leaf so the asset handler itself carries no Manager code, and ws-bridge links only the leaf.
- **2026-09-28** — The session banner renders in `#banners` for the active session (like the plan-review bar), not in each tile's overlay. Banner and badge are descriptors (`{text, action}` / `{text, title, tone}`) that the app renders, so the chrome stays uniform. Why: Phase 2's plan-review bar is exactly this shape, and a descriptor cannot break layout.
- **2026-09-28** — `hive.components` exposes `Button`, `Kbd` and `Markdown` only. The host draws the modal's `ModalShell` itself (title, hints, Escape), so plugins never need it, and `Chip` is a tray chip rather than a badge. Why: smallest public surface; ModalShell's root/id contract is app-internal.
- **2026-09-28** — Plugin chords are ⌘/Ctrl (+Shift) plus one letter or digit; Alt is not bindable. Why: Alt rewrites `e.key` on macOS and is AltGr on Windows. Session-notes uses ⇧⌘O (free in core and in the macOS menu).
- **2026-09-28** — Phase 1 `hive.actions` is `switchTo` only. The plan-review actions (`getPlanReview`, `resolvePlanReview`, `externalPlanReviewers`) land in Phase 2 with their only consumer. Why: YAGNI.
- **2026-09-28** — `settings.set` updates the local store immediately and is settled by the daemon's echo (`pluginConfigOverlay`); a failed write drops the local value and re-lists. Why: e2e-real showed a controlled checkbox snapping back for a round trip.
- **2026-09-28** — Criterion 6 is measured in headless Chromium against the mock bridge (`scripts/measure-gui-idle.sh`: boot, main-thread busy ms/s, JS heap) rather than by launching the built app. Why: the GUI change is frontend-only (the Go side adds a request-only asset handler and two bindings); the script compares both sides the same way and needs no app build or window. It is a proxy for WKWebView/WebView2, and the PR says so.

## Progress

- **2026-09-28** — Exec plan created; research started.
- **2026-09-28** — Research complete.
- **2026-09-28** — Phase 1 implemented. Idle cost with zero UI plugins, 3 runs each:
  - GUI frontend (`scripts/measure-gui-idle.sh --seconds 20`): branch boot 349/218/215 ms, busy 0.37/0.36/0.37 ms/s, heap 14.2 MiB; `origin/main` boot 286/226/231 ms, busy 0.37 ms/s, heap 14.0–14.2 MiB.
  - Daemon (`scripts/measure-idle.sh --seconds 30`): branch RSS mean 14678 KiB, CPU 0.00%; `origin/main` RSS mean 14484 KiB, CPU 0.00%.
- **2026-09-28** — Plan approved (chat, after the HTML review page timed out). Stage → IMPLEMENT, Phase 1 of 2.

- **2026-09-28** — Phase 1 PR #472 opened.

## Open questions

- How and where plugin UI code runs inside the Wails webview.
- Revising the control-plane rule that plugins never answer plan reviews.

## PR convergence ledger

- **2026-09-28 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: e8ce955.
