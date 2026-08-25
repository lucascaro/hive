# Hive improvement suggestions — 2026-08-24

Follow-up to `docs/analysis/2026-07-19-improvement-plan/`. Most of that
plan shipped (Biome, single CI matrix, Windows frontend leg, shared wire
client, registry split, full TypeScript migration, docs sweep). This file
covers what is left plus what surfaced since. Evidence gathered on this
worktree (`white-spire`, head `11e9088`); numbers are measured, not guessed.

## Health snapshot

**Good:** 25.7k LOC Go / 12.8k LOC TS, zero TODO/FIXME (7 deliberate
`ponytail:` markers), internal packages 68–87% statement coverage, atomic
0600/0700 state writes, Go 1.25 + Wails 2.12, `go vet` clean, 77 frontend
test files across unit/dom/e2e/e2e-real, one-item backlog.

**Not so good, ranked by pain:**

| # | Problem | Evidence | Size |
|---|---------|----------|------|
| 1 | e2e-real flake is still masked by `retries: 1`, not fixed | `playwright.real.config.js:24`; `playwright.config.js:14` does the same for the mock suite | M |
| 2 | Thin binaries are barely tested | `cmd/hived` 24.3%, `hived-ws-bridge` 30.7%, `hivegui` 30.5% coverage | M |
| 3 | God-files kept growing after the July plan | `session-term.ts` 996→1638, `style.css` 1488→1984, `app.go` 912→1101, `registry.go` 1038 | M–L |
| 4 | Long functions in the hot path | `serveControl` 274 lines, `kill` 164, `RenameWorktree` 146, `ListWorktrees` 139, `New` 120, `serveAttach` 112 | M |
| 5 | No Go static analysis beyond `go vet` | no `staticcheck` / `govulncheck` in CI or locally | S |
| 6 | Dependency drift | xterm 5.5→6.0 (major), addons one minor behind; `go list -m -u` shows ~10 stale indirects | S–M |
| 7 | Plan-lifecycle drift (again) | `exec-plans/active/247-…` says Stage REVIEW, PR #248 merged; `142-…` and `in-house-vt-emulator.md` unclear | S |
| 8 | Stray repo-root `node_modules/` (empty, untracked, not ignored) | `ls node_modules` → empty; `git check-ignore` → no | XS |
| 9 | Go tests need a seeded `frontend/dist` or `go vet ./...` fails | `cmd/hivegui/main.go:17: pattern all:frontend/dist: no matching files` on a fresh worktree | XS |

## Status — implemented 2026-08-25

Everything below was carried out on branch `white-spire` unless the row says
otherwise. Numbers are measured before/after, not estimated.

| # | Item | Outcome |
|---|------|---------|
| 1 | e2e-real flake | **Fixed, and it was not a flake.** All three quarantined specs failed *deterministically* (0/10 local runs) because their vacuity guard demanded a `replay-request` in follower scenarios where the code deliberately skips the replay. Guard now counts replay *decisions*; 10/10 green, quarantine removed, CI runs 21 of 21 e2e-real tests instead of 2 of 12. `retries: 1` kept but paired with `failOnFlakyTests`, so a retry buys a trace, not a green check. Root cause and evidence in spec 245. |
| 2 | Thin-binary coverage | **Done.** `hived-ws-bridge` 30.7% → 39.8% (plus a real `requireLoopback` guard — `-addr 0.0.0.0` was accepted while `CheckOrigin` returned true for everything). `cmd/hivegui` 30.5% → 38.2% (the fourteen Wails-bound RPCs had no test at all). `internal/daemon` 73.0% → 78.4%. `cmd/hived` stays at 24.3% and that number is **not** a gap: the binary is exercised as a subprocess by the `-tags=e2e` suite, which coverage cannot attribute. Fixed a real race there instead — `TestE2E_DaemonRestart` asserted `alive:true` on the first snapshot after a restart, which since #282 is legitimately `false`; it failed 3/3 locally and passed CI on luck. |
| 3 | God-files | **Go half done, frontend half deliberately not.** `app.go` 1101 → 192 + four domain files (largest 416). `registry.go`'s `kill` 164 → 128 by lifting `disposeWorktree` out. `session-term.ts` and `style.css` left alone — see "Not done, and why" below. |
| 4 | Long functions | **Done.** `serveControl` 274 → 144, with the 14-case dispatch now `handleControlFrame(...) bool` behind a `controlOps` value, and twelve identical decode prologues collapsed into `decodeReq[T]`. The seam paid for itself: the handlers are now unit-tested without a socket. |
| 5 | Go static analysis | **Done.** `staticcheck` (pinned 2025.1.1) and `govulncheck` run on the Linux CI leg. Baseline was one real finding (an unused assignment `go vet` misses); both are clean now. `govulncheck` is clean on the Go 1.25.x that setup-go resolves from go.mod — a local 1.26.4 toolchain reports four stdlib advisories, which is the toolchain's age, not this repo's. |
| 6 | Dependencies | **Partly.** Go module graph refreshed (`go-pty` 0.2.3 + indirects). Wails held at v2.12.0 and xterm held at 5.5 — both explained below. |
| 7 | Plan-lifecycle drift | **Done.** 247 moved to `completed/`; 142's stub corrected (PR #160 was closed unmerged, the bug is still real, and `in-house-vt-emulator.md` is the route to it). `scripts/check-plan-lifecycle.sh` asks `gh` about every PR referenced from `active/` so this stops recurring. |
| 8 | Housekeeping | **Done.** Stray root `node_modules/` removed (a vitest cache, accidentally committed in #255) and gitignored. The `frontend/dist` seeding is already handled by `scripts/ci-bootstrap.sh`, so no change was needed there. |

### Not done, and why — these are yours to call

**xterm 5.5 → 6.0.** Held. Hive uses none of the removed APIs (`windowsMode`,
`fastScrollModifier`, the canvas addon, `overviewRulerWidth`), so the bump would
*compile* fine. The problem is what 6.0 rewrote: "the viewport and scroll bar
implementation works significantly differently now". `src/` has 87 call sites on
exactly that surface — 29 `baseY`, 25 `viewportY`, 19 `scrollToBottom`, 10
`onScroll`, 4 `scrollLines` — and that surface is the one with the longest bug
history in this repo (scroll-jump, the strand, cap-trim, the freeze). A
regression there reproduces only with a full 5000-line buffer, in a real
WebView, which no CI leg here runs. Worth doing, worth doing alone, and worth
watching a real terminal while you do it.

**Wails v2.12.0 → v2.15.0.** Held. `scripts/ci-bootstrap.sh` pins the Wails
*CLI* to the same version, and the CLI generates the bindings and drives the
build. Bumping the library alone invites skew; bumping both is a real upgrade
that deserves its own commit and a native build to validate.

**Splitting `session-term.ts` (1638) and `style.css` (1984).** Not done, and I
would argue against doing it now. Unlike `app.go` (a 50-method binding surface
with existing section banners) and `serveControl` (an untestable 274-line
closure), these two would be pure file-moving: no seam is unlocked, nothing
becomes testable that was not. The July plan called it "cosmetic until they
cause merge pain", and I have not found evidence of that pain — only that the
files grew. `session-term.ts` in particular is the file behind the whole spec-245
saga; churning it right after establishing that its behaviour is correct is a
poor trade. If you want it split, the seam worth taking is the scrollback/replay
logic into the existing pure `lib/scrollback.ts`, because *that* becomes
unit-testable. The rest is rearrangement.

**Dependabot / Renovate.** Not added — it is a repo-settings decision with
ongoing PR-noise cost, and that is a preference, not a defect.

## Original suggestions

Kept as written on 2026-08-24, so the status table above can be read against them.


### 1. Finish the e2e-real re-gate (carry-over from phase 1)

`retries: 1` on both Playwright configs is the thing phase 1 explicitly said
not to do as "the fix". It hides first-attempt failures behind green checks
and doubles CI time on real regressions.

- Follow `phase-1-unblock-ci.md` step 2: poll-until-condition preconditions
  (the `baseY > 4500` pattern) in `scroll-codex`, `wheel-scroll`,
  `scroll-restream-strand`.
- Track 10 consecutive green first-attempt runs per spec, then set
  `retries: 0`. Playwright's JSON reporter + a tiny script can count
  `retry > 0` results so you know when you are there.
- If a spec cannot be made deterministic, move it to a non-required
  Playwright project rather than retrying it.

### 2. Test the thin binaries where they actually break

Coverage is low in exactly the code that has produced recent bug fixes
(#282 restored sessions, #281 stranded daemon, #243 restart). Don't chase a
number; cover the paths behind those fixes:

- `cmd/hived/main.go`: pid file handling, `--reset`, socket-bind-when-ready
  (the last three fix commits). The e2e-tagged suite exists (`-tags=e2e`) —
  check whether these paths are in it; if they are, `go test ./...` without
  the tag just under-reports and this row is a non-issue for hived.
- `cmd/hivegui/app.go` (50 funcs, 1101 lines): the wire-client half moved to
  the shared client, but the Wails-bound methods have no tests. The
  `control_swap_test.go` pattern (fake daemon) can cover create/kill/
  reorder round-trips cheaply.
- `hived-ws-bridge`: `CheckOrigin` returns `true` unconditionally
  (`main.go:90`). Comment says localhost-only listener — assert that in a
  test (bind address is `127.0.0.1`, not `0.0.0.0`) so a future change
  can't silently expose the daemon to any web page. Small, security-relevant.

### 3. Split the four god-files — now they cause pain, not before

The July plan called splitting "cosmetic until they cause merge pain". They
have grown 20–65% since; most recent fix PRs touch `session-term.ts` and
`app.go`. Proposed seams, each its own small PR:

- `session-term.ts` (1638): scrollback/replay logic → `lib/scrollback.ts`
  already exists; move the remaining replay + resize-anchor code there
  (pure, unit-testable). Tile mouse/focus handling → `app/tile-focus.ts`.
- `style.css` (1984): split by component (`sidebar.css`, `terminal.css`,
  `modals.css`, `grid.css`) and `@import` from `style.css`. Vite inlines
  them; zero runtime cost. Validate in a real browser per repo memory
  (vitest is CSS-blind).
- `app.go` (1101): update/restart/menu are already separate files; what's
  left is Wails bindings + event fan-out. Split `app_sessions.go`,
  `app_worktrees.go`, `app_settings.go` by domain, mirroring the registry
  split.
- `registry.go` (1038): `kill` (164 lines) is the obvious extraction —
  separate "stop session" from "remove worktree" from "persist".

### 4. Shrink the long daemon/registry functions

`serveControl` at 274 lines is a request-type switch. Table-driven dispatch
(`map[wire.Type]func(...)`) or one method per request type makes each
handler unit-testable in isolation and keeps the switch under a screen.
`RenameWorktree` / `ListWorktrees` (146/139) are recent (#279) and mix git
plumbing with registry state; pushing git calls down into
`internal/worktree` keeps the layer rule (`worktree → registry`) honest.

### 5. Add `staticcheck` + `govulncheck` to CI (Linux leg only)

Two lines in `ci.yml` under `if: matrix.biome`. Both are platform-neutral,
fast, and the repo currently has no Go analysis beyond `vet`. Run them once
locally first to size the initial cleanup (neither is installed here, so the
baseline is unknown — could be zero, could be a dozen).

### 6. Dependency pass

- **xterm 6.0** is a major bump; the scroll/replay code is the most
  xterm-coupled part of the app. Do it as its own PR, run e2e-real locally
  on a full (>5000-line) buffer before trusting it. Read the 6.0 changelog
  first — the WebGL addon and `write` callback semantics are the risk.
- `go get -u ./... && go mod tidy` for the indirects; `go-pty` 0.2.3 is the
  only direct one behind.
- Consider Dependabot/Renovate for `go.mod` + `package.json` so this stops
  being a manual sweep (the repo already merged one Dependabot bump).

### 7. Plan-lifecycle hygiene

- Move `exec-plans/active/247-…` to `completed/` (PR #248 merged, feature in
  CHANGELOG).
- Decide `142-vt-snapshot-cjk-wide-char-alignment.md` and
  `in-house-vt-emulator.md`: still planned, or move to `rejected/`?
- This is the second time in five weeks; consider a check in
  `hs-doc-garden` or a tiny script that flags `active/` plans whose PR is
  merged.

### 8. Housekeeping (XS, do in one commit)

- `rmdir node_modules` at repo root and add `/node_modules/` to
  `.gitignore` (something ran `npm` in the wrong directory once).
- Make `go vet ./...` / `go test ./...` work on a fresh clone without
  `ci-bootstrap.sh`: commit `cmd/hivegui/frontend/dist/.gitkeep` (or an
  `index.html` stub) so `go:embed all:frontend/dist` always matches, or
  document the one-liner in AGENTS.md next to `test:`.

## Deliberately not proposed

- Frontend framework, state-singleton replacement, dropping `google/uuid` —
  same reasoning as the July plan; nothing changed.
- Raising coverage on `internal/*` — 68–87% with real behavioural tests is
  fine; more would be number-chasing.
- Rewriting `ponytail:` markers — all 7 are honest ceilings with named
  upgrade paths; leave them until one is hit.

## Suggested order

1 (CI trust) → 8 + 7 (an hour) → 5 (baseline) → 2 (tests) → 3/4 (splits,
one file per PR, interleaved with normal work) → 6 (xterm 6 last, alone).
