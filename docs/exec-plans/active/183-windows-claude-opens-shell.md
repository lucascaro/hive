# Hive opens only a shell on Windows, not Claude

- **Spec:** [docs/product-specs/183-windows-claude-opens-shell.md](../../product-specs/183-windows-claude-opens-shell.md)
- **Issue:** #183
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

On Windows, sessions configured with an agent command (e.g. `claude`) drop into a bare shell instead of running the agent. The session spawner unconditionally wraps the command in Unix login-interactive shell flags (`-l -i -c`) that `cmd.exe` doesn't understand. Fix: branch on platform when building the spawn args.

## Research

### Relevant code

- `internal/session/session.go:107-121` — sole spawn site. When `opts.Cmd` is non-empty, builds `ptmx.Command(shell, "-l", "-i", "-c", line)` for **all** platforms. The comment above it (lines 109-114) only makes sense for zsh/bash login + interactive — it explicitly references `.zprofile`, `.zshrc`, `fnm`, `nvm`, `asdf`.
- `internal/session/session.go:311-322` — `defaultShell()` falls back to `$ComSpec` or `cmd.exe` on Windows when `$SHELL` is unset. So on a fresh Windows box, `shell = cmd.exe` and the command line becomes `cmd.exe -l -i -c "claude"`. `cmd.exe` treats `-l -i -c` as args/garbage and ends up presenting its prompt — exactly the reported symptom.
- `internal/session/session.go:267-300` — `shellEscape` is POSIX-style quoting (single quotes, `'\''` escape). Not safe for `cmd.exe`, which uses different quoting rules. Needs a Windows-aware sibling.
- `internal/session/session.go:13` — uses `github.com/aymanbagabas/go-pty`, which supports ConPTY on Windows, so the underlying spawn primitive is fine.
- `internal/agent/agent.go:63-69` — uses `exec.LookPath` for agent binary detection. On Windows that already includes `PATHEXT` (`.cmd`, `.exe`, …), so `claude.cmd` will be found. The lookup itself is not the bug.
- `internal/session/session_test.go:13` — existing test already skips on Windows for the default-shell path. Cmd-driven path has no Windows coverage today.
- Prior Windows fixes for context: PR #179 (restart/grid/ctrl-arrow), PR #176/#178 (xterm canvas resize). No prior change to the agent spawn path.

### Constraints / dependencies

- `claude` on Windows is normally a `claude.cmd` shim. CreateProcessW can't `exec` a `.cmd` directly — it must go through `cmd.exe /C`. So the Windows fix path is naturally "use cmd.exe as the wrapper", not "spawn the binary directly".
- We don't want to regress the macOS/Linux behavior, which relies on login+interactive rc sourcing for PATH (fnm/nvm/asdf).
- PowerShell is available on modern Windows but is not guaranteed; `cmd.exe` is. Stick with `cmd.exe` to minimize variability.
- Argument quoting: cmd.exe needs `"..."` quoting with `^` escaping for metacharacters (`&`, `|`, `<`, `>`, `^`, `%`). Agent argv is typically simple (`claude` plus a few flags) but the escape helper must be Windows-correct.

## Approach

Branch the agent-command spawn on `runtime.GOOS` inside `Session.New`. On Unix, keep the current `<shell> -l -i -c <line>` behavior — it's load-bearing for fnm/nvm/asdf PATH setup. On Windows, build the command line as `cmd.exe /C <line>` with cmd.exe-aware quoting. Prefer `$ComSpec` when set; fall back to `cmd.exe`.

This is the smallest correct fix. Alternatives considered and rejected:

- **Spawn the binary directly via `LookPath` on Windows.** Rejected: `claude` ships as `claude.cmd` (a batch shim). CreateProcessW cannot exec `.cmd` files directly — it requires `cmd.exe /C`. So going through `cmd.exe` is mandatory anyway.
- **Use PowerShell.** Rejected: not guaranteed on all Windows installs; cmd.exe is. PS adds startup latency and extra quoting rules with no benefit for the simple "run this command" case.
- **Build-tag split into `spawn_unix.go` / `spawn_windows.go`.** Reasonable but overkill for ~15 lines. A `runtime.GOOS` switch keeps the spawn logic in one readable block. Revisit if Windows handling grows.

The current `shellEscape` (POSIX single-quote) is unchanged. A new `cmdExeEscape` helper handles cmd.exe quoting: wrap each argv element in `"..."`, double-up embedded `"`, and prefix cmd metacharacters (`& | < > ^ %`) with `^` outside quotes — but since we're inside quotes, only `"` and `\` near a `"` need care. For typical agent argv (`claude`, plus simple flags) plain `"arg"` wrapping is sufficient; the helper covers the general case so future agents with metacharacters in args don't break.

`defaultShell()` is left alone — its `cmd.exe` fallback is still the right wrapper; the bug was in how it was invoked, not which shell was picked.

### Files to change

1. `internal/session/session.go` — at the `len(opts.Cmd) > 0` branch (lines 107-121), switch on `runtime.GOOS`. On Windows, build `wrapper := os.Getenv("ComSpec")` (fallback `"cmd.exe"`) and invoke `ptmx.Command(wrapper, "/C", cmdExeLine(opts.Cmd))`. Adjust the log line to record the actual argv used. Add `cmdExeEscape(argv []string) string` near `shellEscape`. Import `runtime`.

### New files

None.

### Tests

- `internal/session/session_test.go` — add `TestCmdExeEscape` (runs on all platforms) covering: simple argv `[]string{"claude"}` → `"claude"`; argv with spaces `[]string{"claude", "--model", "claude opus 4.7"}` → `"claude" "--model" "claude opus 4.7"`; argv with embedded `"` → escaped via doubled quotes; argv with cmd metacharacters (`&`, `|`, `%`) → safely quoted; empty string argv → `""`.
- `internal/session/session_test.go` — add `TestNewSpawnsCmdOnWindows`, gated by `runtime.GOOS == "windows"` (skip elsewhere). Calls `Session.New` with `Cmd: []string{"cmd.exe", "/C", "echo hivetest"}` (something self-contained that doesn't require a third-party CLI), waits for the read loop to capture output via `SubscribeAtomicSnapshot`, asserts `hivetest` appears in the snapshot.
- `internal/session/session_test.go` — add `TestNewSpawnsCmdOnUnix` symmetric counterpart (skip on Windows) using `echo hivetest` so the Unix path stays covered too.

### Manual verification

Build `hivegui` on Windows, create a session with the Claude agent profile, confirm the `claude` REPL launches inside the terminal (not a cmd.exe prompt). Repeat for gemini and copilot agents if their CLIs are installed.

## Decision log

- **2026-05-10** — Triage: bug / S / P1. Why: Windows is a supported platform, this breaks the primary agent flow, single-file fix.
- **2026-05-10** — Used CommandLineToArgvW backslash-quoting rules in `cmdExeEscape`, not the simpler "double quotes only" approach. Why: keeps future agents that include paths like `C:\foo\bar.exe` correct without a follow-up fix.

## Progress

- **2026-05-10** — Spec + exec plan created. Stage TRIAGE → RESEARCH.
- **2026-05-10** — Research done; root cause confirmed at session.go:116. Stage RESEARCH → PLAN.
- **2026-05-10** — Plan approved. Stage PLAN → IMPLEMENT.
- **2026-05-10** — Implemented GOOS branch + `cmdExeEscape` in session.go; added 3 tests in session_test.go; CHANGELOG `[Unreleased]` entry added. All session package tests pass; full suite green except pre-existing `cmd/hivegui` frontend embed (unrelated).

## Open questions

- Should we keep `cmd.exe` as the wrapper, or detect PowerShell and prefer it? Recommendation: `cmd.exe` for simplicity and ubiquity.
