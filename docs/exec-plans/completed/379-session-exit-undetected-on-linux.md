# Session exit is never detected on Linux

- **Spec:** [docs/product-specs/379-session-exit-undetected-on-linux.md](../../product-specs/379-session-exit-undetected-on-linux.md)
- **Issue:** #379
- **Status:** completed
- **PR:** #469
- **Branch:** feature/379-session-exit-undetected-on-linux

## Summary

The bug the spec describes is already fixed on `main`, as a side effect of
PR #402 (the Windows child-exit fix). What is missing is the registry-level
regression test the spec's first success criterion asks for: a session whose
child exits on its own, observed through the registry, running on Linux.

## Research

### Relevant code

- `internal/session/session.go:227` — `Start` launches `go s.reapChild()`
  next to `go s.readLoop()`. Added by #402.
- `internal/session/session.go:262` `reapChild` — `cmd.Wait()`, then waits
  `exitDrainGrace` (250 ms) for `s.done`, then `closePty()`. On Linux go-pty
  keeps the slave open in the parent, so the master read never fails on child
  exit; closing the PTY is what makes it fail. The comment above
  `exitDrainGrace` already documents the Linux case.
- `internal/session/session.go:284` `readLoop` — still the **only** closer of
  `s.done` (`defer close(s.done)`). `reapChild` never touches `done`; the PTY
  close is `sync.Once`-guarded (`closePty`). Success criterion 2 ("done closed
  exactly once") is therefore satisfied by construction.
- `internal/registry/registry.go:1471` `watchSessionExit` — blocks on
  `sess.Done()`, then nils `e.sess`, runs `machine().Exit()` (state
  `exited`), cancels `captureCancel`, drops a pending prompt, cancels a plan
  review, broadcasts. Unchanged by this work.
- `internal/session/session_test.go:228` `TestSessionDoneClosesWhenChildExits`
  — session-level self-exit test (`echo`, no Kill/Close before the assertion).
  Runs on every OS including Windows and Linux. This is the Windows
  measurement criterion 3 asks for (#402's CI legs).
- `internal/registry/*_test.go` — no test ends a session any way other than
  `Kill` or `sess.Close()`. `TestPendingPromptWithdrawnWhenTheSessionEnds`
  (`initial_prompt_test.go:753`) uses `sess.Close()`, which closes the PTY from
  our side and so cannot see this bug.

### Measurements (2026-09-27)

- `docker run golang:1.27.1` (CI toolchain) over current `main`:
  `TestSessionDoneClosesWhenChildExits` passes (0.28 s).
- A new registry test (`/bin/sh -c true`, no Kill/Close) passes on macOS
  (0.02 s) and Linux (0.26 s — the drain grace, as expected), `-count=3`.
- **Mutation:** with `go s.reapChild()` commented out, the same registry test
  times out at 10 s on Linux — the exact failure the spec reports. So the test
  is a real regression guard for #379, not a vacuous one.
- Side observation: on Linux every self-exit logs
  `session <id>: pty read: read /dev/ptmx: input/output error`. `readLoop`
  filters `io.EOF` and `os.ErrClosed` but not `EIO`, which is how a Linux master
  read reports the closed slave side.

### Constraints / dependencies

- Registry tests are POSIX-only (`skipOnWindows`); Windows coverage of the exit
  path lives at the session layer.
- A PR touching `internal/registry/` trips `scripts/check-daemon-contract.sh`;
  a test-only change takes the `daemon-contract-override` label, not a bump.

### Prior lessons

- No prior lessons matched (`brain-search 'session exit linux pty reaper'`).

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh [go|unit|dom|e2e]` (this change: `go`).
- Verify under CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- Static analysis per GOOS: `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`.
- TDD: every change ships with the test that would have caught it. Go tests live beside source.
- Changeset only for user-visible changes; tests-only PRs are exempt from `check-changeset.sh`.

## Approach

No production code change: `reapChild` (#402) already closes the PTY after
the child exits, which ends `readLoop` and closes `Done()` on Linux. The work
is the regression guard the spec asks for, at the registry layer where the
user-visible consequences (`alive`, `exited`) live. Chosen over a new
session-level test because `TestSessionDoneClosesWhenChildExits` already
covers the session layer on every OS; nothing covered `watchSessionExit`
being reached without Kill/Close.

Success criteria mapping:

1. Registry self-exit test on Linux — `TestSessionExitingOnItsOwnIsSeen`,
   `skipOnWindows` only (runs on the Linux CI leg).
2. `done` closed exactly once — holds by construction: `readLoop`'s
   `defer close(s.done)` is the only closer; `reapChild` only calls the
   `sync.Once`-guarded `closePty`. No change.
3. Windows measured — `TestSessionDoneClosesWhenChildExits` runs on the
   Windows leg (from #402) and passes.

### Files to change

- `docs/product-specs/379-session-exit-undetected-on-linux.md` — add a
  `## Resolution` note: fixed by #402's reaper; this PR adds the registry guard.

### New files

- `internal/registry/self_exit_test.go` — `TestSessionExitingOnItsOwnIsSeen`.

### Tests

- `TestSessionExitingOnItsOwnIsSeen` — `/bin/sh -c true`, no Kill/Close;
  waits for `!Alive()`, asserts `State == wire.StateExited`. Mutation-verified:
  fails (10 s timeout) on Linux with `go s.reapChild()` removed.

### Verification

- `docker run --rm -v "$PWD":/src -w /src golang:1.27.1 go test -count=3 -run TestSessionExitingOnItsOwnIsSeen ./internal/registry/`
- `GOTOOLCHAIN=go1.27.1 go test ./internal/registry/ ./internal/session/`
- `for os in darwin linux windows; do GOOS=$os staticcheck ./internal/registry/ ; GOOS=$os go vet ./internal/registry/ ; done`

## Second opinion

Skipped — fast lane (S, 2 files, not user-visible); drafter self-check passed
(coverage mapped above; verification mutation-checked; blast radius: test-only
plus spec note, `daemon-contract-override` label).

## Decision log

- **2026-09-27** — No code change; test + spec note only. Why: #402 already fixed it; Linux mutation check proves the reaper is the fix.
- **2026-09-27** — Leave the Linux `pty read: ... input/output error` log line alone. Why: operator chose test-only scope.
- **2026-09-27** — Reversed after gate PASS at operator request: `readLoop` now treats `syscall.EIO` like EOF/`os.ErrClosed` (no log line). Log-only, so `daemon-contract-override` still holds; not user-visible, so `no-changeset`. Test: `TestChildExitLogsNoPtyReadError` (red on Linux before the change).

## Progress

- **2026-09-27** — Research: bug already fixed by #402; registry regression test drafted and mutation-verified on Linux.
- **2026-09-27** — PR #469 opened.
- **2026-09-27** — Review nits applied: test uses `r.Create` directly (no liveSession sess==nil window for an instant-exit child).

## Open questions

## PR convergence ledger

- **2026-09-27 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 0abf6e9.
- **2026-09-27 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 9bf88f0.

## Gate verdict

- **2026-09-27** — verdict: PASS; phase: —; checks: 3 passed / 0 failed / 0 followups; followups: none; one-line: registry self-exit test passes on Linux and is mutation-proven; done single-closer by construction; Windows session-layer test passes on CI.
  - 2026-09-27 dimensions:
    - acceptance — PASS — Linux docker 3x pass; reaper removed → 10s timeout; readLoop sole closer of done; Windows internal/session ok on main CI
    - non-goals — PASS — 3 files (1 _test.go, 2 docs); no internal/session, wire or client change
    - doc accuracy — PASS — all file:line refs verified; check-changeset exempt; no stale Linux-exit claims
