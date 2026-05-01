# Hive Native Rewrite — Overall Plan

**Branch:** `silent-light`
**Status:** Planning
**Owner:** Lucas

## Motivation

Hive currently relies on tmux as its session backend. tmux has been a steady source of bugs, workarounds, and friction:

- `send-keys` quoting / escape-sequence drift
- Window/pane state diverging from Hive's model (see `8827e77` — dead window cleanup)
- Sequential subprocess startup latency (see `9027168`)
- Platform-specific tmux behavior, version differences
- Limited control over rendering (we are a guest in tmux's UI)

This plan replaces tmux with a native Hive-owned solution and, in the same effort, transitions Hive from a TUI app to a proper desktop application that hosts terminal sessions itself — like iTerm.

## Goals

1. **Own the session lifecycle.** Replace tmux with `hived`, a Hive-owned daemon that manages PTYs and persists sessions across GUI restarts.
2. **Own the rendering.** Move from Bubble Tea to a desktop GUI hosting real terminal widgets (xterm.js via Wails).
3. **Preserve feature parity.** Every feature in current Hive must be reachable in the new app before old Hive is sunset.
4. **Cross-platform.** macOS, Linux, Windows — verified per release (per AGENTS.md / cross-platform release rule).
5. **Multi-agent core remains first-class.** Claude, Codex, Gemini, Copilot, agent teams.

## Non-goals

- Beating iTerm/Alacritty on raw terminal performance. We need *good enough*; differentiation is multi-agent UX.
- Mobile/web clients in v1.
- Plugin SDK in v1.

## Architecture (target)

```
+-------------------------+         +-----------------------+
|   Hive GUI (Wails)      |  IPC    |   hived (Go daemon)   |
|   - Frontend: xterm.js  |<------->|   - Owns PTYs         |
|   - Sidebar, layout,    |  Unix   |   - Scrollback buffer |
|     agent UI            |  socket |   - Session registry  |
+-------------------------+         |   - Persistence       |
                                    +-----------+-----------+
                                                |
                                                v
                                         child processes
                                         (shells, agents)
```

- **`hived`** runs independent of the GUI. It survives GUI close.
- **GUI** is a thin client; it attaches to `hived` over a versioned protocol.
- **Protocol** is the contract — versioned from day one.

## Framework decision (locked at end of Phase 0)

Working assumption: **Wails v2 + xterm.js**.
- Keeps Go for daemon and business logic.
- xterm.js is the de-facto terminal widget (VS Code, Hyper). Eliminates years of VT/ANSI work.
- Cross-platform out of the box.

Alternatives considered: Fyne/Gio (would need to write the emulator — no), Tauri (Rust rewrite — no), Swift/AppKit (macOS-only — violates cross-platform goal).

## Phases

| Phase | Theme                                  | Est.     |
|-------|----------------------------------------|----------|
| 0     | Decision & spike                       | 1 wk     |
| 1     | Single persistent session, GUI client  | 2–3 wks  |
| 2     | Multiple sessions + sidebar            | 2 wks    |
| 3     | Agent integration                      | 2 wks    |
| 4     | Layout: tabs, splits, grid             | 3 wks    |
| 5     | Hive-specific features port            | 2–3 wks  |
| 6     | Platform polish & release              | 2 wks    |
| 7     | Sunset old Hive                        | 1 wk     |

**Total: ~3–4 months focused.**

### Phase 0 — Decision & Spike
De-risk the two scariest pieces. See `phase-0.md` for the detailed plan.
- Spike A: Go daemon + single PTY + reattach over Unix socket.
- Spike B: Wails app + xterm.js + Go-side PTY round-trip.
- Exit: both work on macOS + Linux. Framework locked.

### Phase 1 — Single persistent session
- `hived` daemon owning one shell.
- Wire protocol v0 (JSON-framed over Unix socket): `attach`, `detach`, `write`, `resize`, `scrollback`.
- Disk-backed scrollback ring buffer (~10k lines).
- Wails GUI attaches and renders.
- **Acceptance:** open vim, quit GUI, relaunch, vim is intact.

### Phase 2 — Multiple sessions
- Daemon supports N sessions; protocol gains `list / create / kill / rename / reorder / color`.
- Sidebar with click-to-switch and keybinds.
- Title-gradient color UX (per session-color feedback).
- **Acceptance:** 5 named/colored sessions survive GUI restart with scrollback.

### Phase 3 — Agent integration
- Port agent launchers (Claude, Codex, Gemini, Copilot) — they're process spawns inside a session, logic transfers.
- Per-session env, cwd, command template.
- **Acceptance:** Claude + Codex side-by-side, persist across restart.

### Phase 4 — Layout: tabs, splits, grid
- Tabs, splits (xterm.js per pane + GUI splitter widget).
- Grid view; reorder via Shift+Left/Right (per grid-horizontal-reorder feedback).
- **Acceptance:** parity with current grid/tabs UX.

### Phase 5 — Feature port
Walk current feature list, port one per sub-PR:
- Session templates / workflows
- Agent teams
- Status bar, notifications
- Keybindings (preserve existing — muscle memory)
- Replace any current `tmux send-keys` workarounds with clean daemon RPCs.

### Phase 6 — Platform polish & release
- Daemon lifecycle: launchd (macOS), systemd-user (Linux), Task Scheduler (Windows).
- Code signing + notarization (macOS), MSI/installer (Windows).
- Auto-update.
- Cross-platform smoke test (per release rule).

### Phase 7 — Sunset old Hive
- Migration tool: read old config/state → new daemon session list.
- Final tmux-backed release tagged; branch frozen.
- Bug-fix-only mode for 1–2 releases on old branch.

## Cross-cutting concerns

- **Protocol versioning** from day one — daemon and client will rev independently.
- **Test strategy:** daemon is unit-testable end-to-end; GUI gets thin smoke tests. No tests touching real config/state (per AGENTS.md).
- **AGENTS.md** governs AI rules — never CLAUDE.md.
- **Repo strategy:** `silent-light` branch is long-lived. Do not interleave with `main` feature work; the two architectures don't share enough to merge cleanly. Old Hive on `main` stays in bug-fix mode during the rewrite.
- **Backout:** Phases 0–1 are throwaway-cheap. From Phase 2 onward, sunk cost grows. Phase 0 exit criteria are the real go/no-go gate.

## Risks

| Risk                                              | Mitigation                                          |
|---------------------------------------------------|-----------------------------------------------------|
| xterm.js gaps for advanced features (sixel, etc.) | Phase 0 Spike B exercises edge cases                |
| Wails maturity on Windows                         | Phase 0 must cross-build & smoke-test on Windows    |
| Daemon protocol churn breaks clients              | Version negotiation in handshake; never break v0    |
| Scope creep on agent UX during rewrite            | Phase 5 is port-only; new features after sunset     |
| Reliance on a single GUI framework                | Keep daemon framework-agnostic; GUI is replaceable  |

## Success criteria (epic-level)

1. Zero `tmux` references in the shipping binary.
2. All current Hive features reachable in new app.
3. Sessions survive GUI close on all three platforms.
4. Startup latency ≤ current Hive (target: faster, given no tmux subprocess chain).
5. Agent UX (Claude/Codex/Gemini/Copilot) at parity or better.
