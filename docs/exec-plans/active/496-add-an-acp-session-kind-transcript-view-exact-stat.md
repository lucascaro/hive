# Add an ACP session kind: transcript view, exact state, permission prompts, typed results, PTY takeover

- **Spec:** [docs/product-specs/496-add-an-acp-session-kind-transcript-view-exact-stat.md](../../product-specs/496-add-an-acp-session-kind-transcript-view-exact-stat.md)
- **Issue:** #496
- **Status:** active
- **PR:** —
- **Branch:** feature/496-acp-session-kind

## Summary

Add an `acp` session kind that `hived` owns next to `pty`: the daemon speaks ACP (JSON-RPC over stdio) to an adapter, the GUI renders a transcript instead of a terminal, and a session can be taken over in a PTY and handed back. The full *why* lives in the spec and in `docs/design-docs/acp-workflows.md` (spec 492's findings).

## Research

Detailed findings, with line citations, are in [docs/design-docs/acp-session-kind.md](../../design-docs/acp-session-kind.md). The parts that drive the plan:

### Relevant code

- **Spawn path.** `Registry.Create` (`internal/registry/create.go:96`) calls `insertEntry` (:600), which persists the entry before the spawn (:177).
  - A `Kind` field has to be threaded through `Entry`, `MetaFile`, `load()`, `Tombstone`/`Restore`, `Info()`, `wire.SessionInfo` and `wire.CreateSpec`.
  - An empty `Kind` reads as `pty`.
- **Restart paths.** `reviveAll` → `Revive` (registry.go:1315), `Restart` (:1422) and `Restore` all assume a PTY. ACP entries need a `session/load` branch in each.
- **Session coupling.** `internal/session.Session` is PTY-only and concrete. Plan: keep a separate `e.acp` child in `Entry`, and widen `Alive`, `Kill`, `Close`, `Restart`, `watchSessionExit` and `title` to cover it. `serveAttach` refuses ACP sessions.
- **State tier.** `internal/agentstate/machine.go`: feed `Apply` in-process with `Source: "acp"`. The tier must be exempt from the 30s `HookStaleAfter` demotion. Add the constant in `wire/control.go:327`.
- **Wire.** New frames start at 0x3f. Each must be classified, mapped in `controlEvents`, given a row in `docs/plugins.md`, and added to all three clients (GUI, ws-bridge, testclient). `DaemonContract` goes 21 → 22.
- **Child process.** Use `proc.CommandContext` and the plugin manager's supervise and kill-tree pattern (`internal/plugin/manager.go:640-695`).
- **GUI.**
  - The tile seam is `ensureTerm` (`session-term.ts:1872`). The new `AcpTile` implements `TermTile` (`app/state.ts:254`).
  - Focus code hard-codes the xterm textarea (`focus.ts:229`).
  - The launcher toggle copies the worktree toggle (`Launcher.tsx:567`).
  - The stream store copies `store/activity.ts`.
  - The bridge parity test requires mock and real-bridge stubs.

### Constraints and dependencies

- **No per-agent permission or unattended setting exists** (`internal/agent/settings.go:40`). Criterion 5 needs a new setting, a Settings UI for it, and a default.
- **No Node detection exists**, and the daemon's PATH usually lacks `npx`. The login-shell PATH has to be resolved without an interactive shell on the JSON-RPC pipes.
- **`SpawnedBy` exists nowhere in code.** Plan 391 defines it but hasn't started; it waits on 389 and 390. 496 lands it first, with the semantics 391 specifies: the daemon stamps it and the client never sends it.
- **Proven and unproven ACP methods.** Spike 492 proved `initialize`, `session/new`, `session/prompt`, `session/load` and `request_permission`. `cancel` and `set_mode` are unproven, and so is the PTY-to-ACP hand-back.
- **F2.** No known way to release the Codex writer lock.
- **F5.** Pi needs `submit_result` through the Hive Pi extension.
- **Gate B** needs ≥5 *real* tasks per agent (Claude, Pi), each taken over and handed back once. That spends the operator's subscription and needs real CLIs, so CI can't do it.
- **Size.** This is a large change across the daemon, wire, GUI, settings and docs. Phasing is an open question (see the Decision log).

### Prior lessons

- No hive-brain entries matched the ACP or daemon terms.
- Relevant GUI entries:
  - `hive-attach-disconnect-can-precede-opensession`: a Wails event can arrive before the binding's promise resolves, so the transcript subscribe path needs an epoch guard.
  - `hive-macos-menu-owns-cmd-chords`: a ⌘ chord change needs a `menu_darwin.go` change.
- From the 492 decision log:
  - Strip `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION` and `HIVE_SOCKET` from adapter env, and keep user preferences.
  - Match an agent's verbatim reply, not a derived string.

### Conventions card

- Build: `./build.sh`. Tests: `scripts/test.sh [go|unit|dom|e2e]`. Real-daemon e2e: `npm run test:e2e:real`, with `HIVE_SOCKET` and `HIVE_STATE_DIR` isolated.
- Lint gates:
  - `for os in darwin linux windows; do GOOS=$os staticcheck ./...; GOOS=$os go vet ./...; done`
  - `scripts/ui-lint.sh --strict`
  - `biome ci .` (in the frontend)
  - `npm run typecheck`, which needs `./scripts/ci-bootstrap.sh` first
  - `scripts/check-daemon-contract.sh main HEAD`
  - Check under the CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- TDD, so every behaviour change ships with its test. Go tests live beside the source; frontend tests live under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- **Wire changes:** JSON tags are `snake_case`, JS reads `snake_case ?? camelCase`, the three clients change in lock-step, and `DaemonContract` is bumped. Do not bump `PROTOCOL_VERSION`.
- **Children:** spawn only through `proc.Command*`. The GUI never spawns processes; everything goes over the wire.
- **New commands:** a title, a `shortcuts.ts` row, a menu item plus the accelerators JSON, the README Keybinds table and a changeset. A new key scope needs an `every-shortcut` fixture.
- **UI:** tokens and sprite icons only; a mock in `docs/design-docs/ui/mocks/` for any new visual choice; a `components.md` section for a new component.
- **Docs:** add a changeset under `.changesets/`, an entry in `site/features.json` with `since: "Unreleased"`, a `DESIGN.md` update (new package), and updates to `control-plane.md` and `acp-workflows.md`.

## Approach

_Pending._

## Decision log

- **2026-10-03** — Gate B targets Claude + Pi, not Claude + Codex. Why: the operator named Claude and Pi as the primary agents during the 496 brainstorm.
- **2026-10-03** — ACP sessions must survive a `hived` restart in the same conversation. Why: PTY sessions already do (`reviveAll` → `Revive` → `ResumeArgs`), so ACP must match.

## Progress

- **2026-10-03** — Plan created; research started.

## Open questions

## PR convergence ledger

## Gate verdict
