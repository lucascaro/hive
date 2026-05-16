# 210 — Real end-to-end tests for hive

- **Stage:** QA
- **Status:** active

## Summary

Stop the steady drip of GUI / daemon regressions (#195, #198, #200/#203, #208/#209) by adding three new test layers stacked on the existing harness. Layer A spawns the real `hived` binary and drives it over the real wire protocol; Layer B replaces the JS Wails mock with a thin bridge to a real daemon so the frontend exercises actual payloads end-to-end; Layer C systematically expands the existing mock-Wails Playwright suite with invariant tests for the five fragile interaction zones.

All three layers run on every PR. All test runs are isolated — temp `HIVE_SOCKET` and `HIVE_STATE_DIR`, plus a `testclient.RequireIsolation` guard that fail-closes if either is missing or points outside `/tmp`. Production hive state is never touched.

## Research

- Wire protocol: `internal/wire/frame.go` (1-byte type, 4-byte BE length, payload) + `internal/wire/control.go` (Hello/Welcome, control frames, SessionInfo with `snake_case` JSON).
- Existing in-process tests: `internal/daemon/daemon_test.go`, `internal/daemon/integration_test.go` — useful patterns for socket teardown, replay drain, snapshot consumption.
- Frontend mock harness: `cmd/hivegui/frontend/test/e2e/wails-mock.js` (in-browser stand-in for the Wails bridge); `cmd/hivegui/frontend/playwright.config.js` (Vite dev + headless Chromium).
- Isolation knobs already in tree: `HIVE_SOCKET` (`internal/daemon/socket.go:18`), `HIVE_STATE_DIR` (`internal/registry/paths.go:20`), and the daemon's `HIVE_STATE_DIR`-aware orphan-reaper skip (`internal/daemon/daemon.go:86`).
- Regression history that drove the plan: #195 (per-session TextDecoder), #198 (atlas glyph rendering — reverted), #200/#203 (scrollback fanout race + grid-resize wrap), #208/#209 (grid baseline + sidebar focus).

## Approach

Three layers + CI:

- **Layer A** — Go tests that build and spawn the real `hived` binary. Catches daemon/wire bugs (fanout races, protocol mismatches, persistence). Build-tagged `e2e` so default `go test ./...` stays fast.
- **Layer B** — Real daemon ↔ real frontend via a `hived-ws-bridge` shim that translates JSON-RPC ↔ binary wire. Catches payload-shape and end-to-end UX drift the mock can't see.
- **Layer C** — Expand the mock-Wails Playwright suite to cover the five fragile zones with invariant-shaped tests.
- **Layer D / CI** — All three layers run on Linux and macOS, every PR. CI exports `HOME` / `HIVE_STATE_DIR` / `HIVE_SOCKET` to temp paths.

Phasing (one PR per layer): A → C → B, with CI rolling in alongside.

### Files to change

- `.github/workflows/build-linux.yml` — add `Test (Go e2e — real hived binary)` step.
- `.github/workflows/build-macos.yml` — same.

### New files

**Layer A**
- `internal/wire/testclient/client.go` + `client_test.go` — async wire client + isolation guard.
- `cmd/hived/e2e_test.go` (`//go:build e2e`) — daemon binary tests.

**Layer B**
- `cmd/hived-ws-bridge/main.go` — localhost WS shim translating JSON-RPC ↔ wire frames. Fail-closes on missing/escaping isolation env.
- `cmd/hivegui/frontend/test/e2e-real/wails-bridge.js` — browser-side WS client matching the Wails App + runtime surface.
- `cmd/hivegui/frontend/test/e2e-real/globalSetup.mjs` / `globalTeardown.mjs` — Playwright lifecycle that builds binaries, spawns hived + bridge in temp dirs, exports `WS_BRIDGE_URL`.
- `cmd/hivegui/frontend/playwright.real.config.js` — separate Playwright config for real-daemon mode.
- `cmd/hivegui/frontend/test/e2e-real/lifecycle.spec.js` — bootstrap session, attach, type, assert echoed output in xterm buffer.
- `cmd/hivegui/frontend/test/e2e-real/glyph-utf8.spec.js` — multi-byte UTF-8 (2 / 3 / 4 byte) round-trips through real wire path.

**Layer C** (mock-Wails Playwright expansion)
- `cmd/hivegui/frontend/test/e2e/scrollback-invariants.spec.js` — 5 invariants beyond the specific #208 regressions.
- `cmd/hivegui/frontend/test/e2e/focus-invariants.spec.js` — 4 focus-alignment invariants not covered by `focus.spec.js`.
- `cmd/hivegui/frontend/test/e2e/renderer-recovery.spec.js` — WebGL context-loss integration smoke.
- `cmd/hivegui/frontend/test/e2e/payload-shapes.spec.js` — snake_case vs camelCase parity for SessionInfo.

### Modified files

- `cmd/hivegui/frontend/vite.config.js` — add `VITE_WAILS_REAL=1` substitution branch.
- `cmd/hivegui/frontend/package.json` — add `test:e2e:real` script.
- `cmd/hivegui/frontend/test/e2e/smoke.spec.js` — add console-error invariant.
- `cmd/hivegui/frontend/src/main.js` — expose `window.__hive_state = state` gated on `import.meta.env.VITE_WAILS_MOCK/REAL` (test-only).
- `.github/workflows/build-linux.yml` — Layer A + Layer B steps.
- `.github/workflows/build-macos.yml` — Layer A + Layer C (mock) + Layer B steps.

## Decision log

- **2026-05-15** — Use build tag `e2e` rather than gating on env / file-name pattern. Why: `go test ./...` stays the same speed; CI opts in explicitly; the tag is the same pattern used by other projects and reads cleanly in workflows.
- **2026-05-15** — Build the `hived` binary once per test process via `sync.Once`. Why: each test spawning a fresh `go build` would push the suite from ~0.4s of work to ~10s × N.
- **2026-05-15** — `testclient` merges DATA + EVENT into a single ordered channel. Why: separate channels caused `AwaitReplayBoundary` to non-deterministically pick the Done event before draining pending DATA, returning empty replays. Wire order must be preserved end-to-end.
- **2026-05-15** — Drive stdin via a dedicated third attach in the fanout test, with `stty -echo` issued before the burst. Why: PTY input echo otherwise injects the START/END markers into observers before the actual output, breaking the clip window.
- **2026-05-15** — Isolation guard recognises `/tmp`, `/private/tmp`, `/var/folders` (macOS `os.TempDir()` returns the latter on CI). Why: bare `/tmp` prefix-matching rejects valid temp paths on macOS runners.

## Decision log (continued)

- **2026-05-15** — Bundle Layers A + B + C into a single PR after user feedback ("one PR"). Why: original plan to ship three separate PRs was overcautious — the layers are conceptually one contract ("real e2e coverage"), reviewers benefit from seeing the whole story, and the regressions-have-to-stop framing argues for landing the full net at once.
- **2026-05-15** — Layer C specs explicitly avoid duplicating `grid-scroll-regressions.spec.js`, `focus.spec.js`, etc. They target the GAPS in existing coverage, framed as invariants rather than historical-bug regression tests so the next refactor must uphold them generically.
- **2026-05-15** — Layer B uses a single ordered WS JSON-RPC channel (gorilla/websocket already transitive in deps) rather than a separate frame protocol. Why: the bridge surface is ~10 methods; full protocol parity isn't worth the LOC.
- **2026-05-15** — Layer B reads xterm buffer via a tiny `window.__hive_state = state` test hook in main.js, gated on Vite env vars so production drops it. Why: WebGL renderer paints to canvas; `.xterm-rows` DOM scrape returns empty. xterm's `buffer.active.getLine().translateToString(true)` is the source of truth.
- **2026-05-15** — globalSetup uses `.mjs` extension. Why: `cmd/hivegui/frontend/package.json` does not set `"type": "module"`, but Playwright's globalSetup loader requires ESM syntax.

## Progress

- **2026-05-15** — Layer A scaffolding landed locally: testclient + 6 e2e tests + CI wiring (Linux + macOS). 6/6 green in ~12s.
- **2026-05-15** — Layer C scaffolding landed locally: 4 new spec files + smoke.spec.js extension. Full mock suite 32/33 green (1 graceful skip for unavailable `WEBGL_lose_context`).
- **2026-05-15** — Layer B scaffolding landed locally: hived-ws-bridge + browser-side bridge + Playwright real config + globalSetup/Teardown + lifecycle.spec.js + glyph-utf8.spec.js. 2/2 green in ~6s.
- **2026-05-15** — All three layers wired into Linux and macOS CI on every PR.

## Open questions

- Per-PR runtime budget for Layer B (~2 min projected). If it slips toward 5 min we may need to split into smoke vs full passes; revisit when the bridge shim lands.
- Should Layer A also build with `-race`? Worth the wall-time? Defer until the suite is broader.
