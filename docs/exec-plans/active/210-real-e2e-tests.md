# 210 — Real end-to-end tests for hive

- **Stage:** IMPLEMENT
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

### New files (this PR — Layer A only)

- `internal/wire/testclient/client.go` — async client over the binary wire protocol with `Dial`, `Handshake`, `WriteStdin`, `WaitForData`, `AwaitReplayBoundary`, `AwaitSessionEvent`, `CreateSession`, `RequireIsolation`. DATA + EVENT share one ordered channel to preserve wire order (separate channels lose select-ordering, returning empty replays).
- `internal/wire/testclient/client_test.go` — unit tests against an in-test fake daemon (no real binary needed).
- `cmd/hived/e2e_test.go` (`//go:build e2e`) — spawns the real `hived` binary in a temp dir; tests below.
- `docs/exec-plans/active/210-real-e2e-tests.md` (this file).

### Tests (Layer A)

- `TestE2E_SessionLifecycle` — attach, type a marker, detach, reattach, replay carries the marker.
- `TestE2E_ScrollbackAtomicityUnderConcurrentFanout` — two attach clients on one session, 200-line burst (with `stty -echo` so input doesn't pollute observers), assert identical clipped windows.
- `TestE2E_MultiSessionIsolation` — 4 sessions, distinct concurrent markers, assert no cross-talk.
- `TestE2E_DaemonRestart` — SIGTERM the daemon, restart against the same state dir, assert the named session persists in the registry.
- `TestE2E_ProtocolVersionMismatch` — wrong wire version → daemon refuses cleanly.
- `TestE2E_IsolationGuard_FailsClosed` — `RequireIsolation` errors when env vars are unset.

## Decision log

- **2026-05-15** — Use build tag `e2e` rather than gating on env / file-name pattern. Why: `go test ./...` stays the same speed; CI opts in explicitly; the tag is the same pattern used by other projects and reads cleanly in workflows.
- **2026-05-15** — Build the `hived` binary once per test process via `sync.Once`. Why: each test spawning a fresh `go build` would push the suite from ~0.4s of work to ~10s × N.
- **2026-05-15** — `testclient` merges DATA + EVENT into a single ordered channel. Why: separate channels caused `AwaitReplayBoundary` to non-deterministically pick the Done event before draining pending DATA, returning empty replays. Wire order must be preserved end-to-end.
- **2026-05-15** — Drive stdin via a dedicated third attach in the fanout test, with `stty -echo` issued before the burst. Why: PTY input echo otherwise injects the START/END markers into observers before the actual output, breaking the clip window.
- **2026-05-15** — Isolation guard recognises `/tmp`, `/private/tmp`, `/var/folders` (macOS `os.TempDir()` returns the latter on CI). Why: bare `/tmp` prefix-matching rejects valid temp paths on macOS runners.

## Progress

- **2026-05-15** — Layer A scaffolding landed: testclient + 6 e2e tests + CI wiring (Linux + macOS). All passing locally on macOS in ~12s (10s of which is the initial `go build hived`).
- **2026-05-15** — Layer B and Layer C remain in plan; each will land as its own PR.

## Open questions

- Per-PR runtime budget for Layer B (~2 min projected). If it slips toward 5 min we may need to split into smoke vs full passes; revisit when the bridge shim lands.
- Should Layer A also build with `-race`? Worth the wall-time? Defer until the suite is broader.
