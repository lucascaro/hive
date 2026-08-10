# Phase 3 — Go consolidation

Ordered by value. Each item is its own PR.

## 3a. One shared daemon wire client (highest value)

The dial/handshake + control-read-loop + attach-read-loop + writeStdin/
closeAttach logic exists **three times**:

- `cmd/hivegui/app.go` — `dialHandshake` :193, `controlReadLoop` :402,
  `attachReadLoop` :792 (production, untested)
- `cmd/hived-ws-bridge/main.go` — `dialHandshake` :464, `controlReadLoop`
  :306, `attachReadLoop` :372 (production, untested)
- `internal/wire/testclient/client.go` — 462 lines, used only by
  `cmd/hived/e2e_test.go`

Extract a shared client into `internal/wire` (the framing already lives
there: `control.go`, `frame.go`). testclient likely becomes a thin wrapper or
disappears. Payoff: the two production protocol clients become one **unit-
tested** implementation; protocol changes stop needing 3 synchronized edits.

**Do this test-first — the two production clients have zero tests, and
refactoring untested protocol code is exactly where silent regressions hide.**
`internal/wire/testclient` already HAS a test (`client_test.go`), so it is the
known-good behavioral reference: lift the shared client from testclient (or
write characterization tests pinning the current `app.go` / bridge behavior)
*before* deleting either production copy. This keeps the repo's own "no patches
without a reproducer" rule (cited in Phase 1) honest for a refactor too.

## 3b. Split `internal/registry/registry.go` (1123 lines)

- Decompose `Create` (registry.go:286–510, 225 lines) into named steps:
  validate spec → create worktree → spawn agent → capture session ID →
  persist → broadcast. Straight-line extraction, no new abstractions.
- Move broadcast/subscriber plumbing and color-picking out of registry.go
  into sibling files in the same package (like `projects.go`/`persist.go`
  already do). Package split not required — file split is enough.
- While there: `persistEntryLocked` (:1007) does disk I/O under the registry
  mutex, so disk latency blocks all session ops. Verify and, if real, snapshot
  under lock + write outside it.

## 3c. Dedupe codex/copilot session capture — **done, rescoped**

The original item claimed `internal/agent/codex.go` and `copilot.go` were
"structurally identical poll-newest-session-file scanners" and proposed one
parameterized scanner. Reading both showed that premise is wrong — only the
poll loop is shared:

| | codex | copilot |
|---|---|---|
| scan | `WalkDir` over `YYYY/MM/DD`, filename regex, file mtime | `ReadDir` one level, dirname UUID, `workspace.yaml` mtime |
| read | JSON first line, `session_meta` check | hand-rolled YAML top-level `cwd:` scan |
| ID source | file body (`payload.id`) | scan (dir basename) |
| result | 2-state `bool` | 3-state (`NotReady` deliberately not cached) |

"Third agent gets it free" doesn't hold either: gemini uses pinning and claude
uses a file-existence probe — 2 of 4 built-ins never poll.

What shipped instead:

1. **A real bug fix.** `readCodexRolloutCwd` returned a bare `false` for both
   "not our cwd" and "unreadable / caught mid-write", and the caller
   negative-cached it permanently. A poll tick landing while codex wrote the
   first `session_meta` line rejected the file for the whole capture window,
   losing the session ID. Codex now has copilot's tri-state.
2. **The poll loop only** — ticker, negative cache, ctx handling, and the
   "only cache a definitive mismatch" rule — extracted to
   `pollCaptureSessionID` in `internal/agent/capture.go`. Both scanners and
   both readers stay separate by design; unifying them would add a candidate
   struct and a two-value callback to remove ~22 lines.

## 3d. Context propagation

`internal/worktree/worktree.go` roots 5 fresh `context.Background()`
timeouts (:131,:145,:158,:168,:175); same at `registry.go:518` and
`cmd/hivegui/update.go:81`. Thread a caller `ctx` down so daemon shutdown
cancels in-flight git/spawn work. Mechanical change; do it while touching
registry in 3b.

## 3e. Conventions (do opportunistically, not as a sweep)

- Logging: 77 `log.Printf` sites, zero `slog`. Adopt `slog` for **new/touched
  code**; don't mass-convert.
- Error wrapping: ~50/50 split between `%w` and non-wrapping. Rule going
  forward: wrap (`%w`) when callers may inspect; plain message otherwise.
- Audit the ~78 `_ =` ignored errors in registry persistence paths only
  (teardown-path Close/Write ignores are fine).

## 3f. Dependency trims — **do not do; closed as documentation**

- **Do NOT drop `atotto/clipboard`.** The original claim ("Wails already
  provides clipboard APIs") is wrong for writes: Wails'
  `runtime.ClipboardSetText` is broken on Windows because the JS-bridged call
  runs on a non-STA goroutine, so `OpenClipboard` silently fails and nothing
  reaches the clipboard. `cmd/hivegui/app.go:58-72` documents this; atotto
  shells out to `clip.exe` to sidestep the threading constraint. Reads
  (`ClipboardGetText`) work because they don't need clipboard ownership —
  which is why the runtime API appears elsewhere in the file. Removing the dep
  re-breaks Windows copy.
- Keep `google/uuid` (3 call sites, stable — not worth churn).

## Later / only-if-it-hurts (JS side, from the same audit)

- Split `cmd/hivegui/frontend/src/app/session-term.ts` (996 lines: lifecycle,
  scroll/replay, wheel, link hit-testing in one class) — do it the next time a
  change there goes wrong, not before.
- Split `cmd/hivegui/frontend/src/style.css` (1488 lines) — cosmetic; only if
  merge conflicts start happening.
- Fire-and-forget Wails-binding calls wrapped in `try/catch` only catch the
  *synchronous* failure (binding absent in tests); an async **rejection** of
  the returned promise still escapes as an unhandled rejection. Affects
  `LogFrontend(...)` in `src/main.ts` and `src/app/events.ts`, and any similar
  fire-and-forget binding call. Fix once with a tiny `fireAndForget(p)` helper
  (`p?.catch(() => {})`) applied at every call site — not per-line. Surfaced by
  CodeRabbit on PR #249 (the Biome PR); deferred out of that formatting PR to
  keep it scoped. Low impact (fire-and-forget logging), so: only-if-it-hurts.
