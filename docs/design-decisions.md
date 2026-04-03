# Hive: Research Notes

## Similar Projects

| Project | Tech | Key Features |
|---------|------|-------------|
| [Agent Deck](https://github.com/asheshgoplani/agent-deck) | Go + Bubble Tea | Single dashboard for Claude/Gemini/Codex, token tracking, vim-style nav |
| [Claude Squad](https://github.com/smtg-ai/claude-squad) | Go + Bubble Tea + tmux | Multi-agent workspace, git worktrees, tmux backend |
| [CCManager](https://github.com/kbwo/ccmanager) | — | Supports 8+ agents, auto project discovery, vi search |
| [claude-session-manager](https://github.com/Swarek/claude-session-manager) | — | Auto ID assignment, colored status line, unlimited concurrent sessions |
| [cld-tmux](https://github.com/TerminalGravity/cld-tmux) | tmux wrapper | Persistent sessions, SSH-friendly |
| [TmuxAI](https://tmuxai.dev/) | tmux integration | Observes pane content, non-intrusive assistance |
| [CodeMux](https://www.codemux.dev/) | — | Vibe coding with Claude/Gemini/Aider, web UI |

## TUI Framework Comparison

| Framework | Language | Performance | Dev Speed | Notes |
|-----------|----------|-------------|-----------|-------|
| **Bubble Tea** | Go | High | Fast | Elm-MVU, best ecosystem, most existing tools use this |
| Ratatui | Rust | Extreme | Slower | Best for perf-critical UIs, steeper learning curve |
| tview | Go | High | Medium | Widget-based, more boilerplate |
| Textual | Python | Medium | Fastest | CSS-like, dual terminal+web mode |
| ink | Node.js | Medium | Fast | React-based, high overhead |

**Chosen: Go + Bubble Tea** — Elm-inspired MVU, excellent charmbracelet ecosystem (Lip Gloss, Bubbles), used by most existing Claude session managers, lightweight binary.

## Terminal Backend: tmux vs. Embedded PTY

| Approach | Pros | Cons |
|----------|------|------|
| **tmux backend** (chosen) | Battle-tested, SSH-friendly, no VT100 parser, sessions persist | Requires tmux installed |
| Embedded PTY (creack/pty + VT100 parser) | No tmux dependency, full control | Complex, fragile, ~5x more code |

## Key Design Patterns from Research

1. **tmux naming**: `tool-{projectShortID}` for sessions, window per agent session
2. **Attach/detach**: TUI quits → tmux attach → tmux detach → TUI relaunches (clean round-trip)
3. **Session title via OSC 2**: `\033]2;title\007` — standard xterm sequence, works in any terminal
4. **Status aggregation**: Derive team status from member statuses (any `waiting` → team waiting)
5. **Hook system**: Executable scripts in `~/.config/{tool}/hooks/on-{event}` with env vars injected
6. **Atomic config writes**: Write to `.tmp`, then `os.Rename()` — prevents corruption on crash

## Agent Teams (Claude-specific research)

Claude supports multi-agent orchestration via its API/SDK where one Claude instance acts as orchestrator and spawns subagents. Key behaviors:
- Orchestrator directs workers via conversation/files
- Workers can specialize (code, review, docs, tests)
- Shared working directory is the most common coordination mechanism
- Git worktrees can isolate per-worker to prevent branch conflicts

In Hive, this is exposed as a **Team**: orchestrator session + N worker sessions, all in the same project, sharing a work directory. The TUI reflects the team hierarchy visually.

## Multi-Instance State Synchronisation

**Problem**: users often run hive in several terminal tabs or tmux splits at once.
With a single JSON file and no coordination, two simultaneous saves would result
in one instance silently overwriting the other's changes.

**Decision: advisory flock + mtime polling (no fsnotify, no server process)**

Three mechanisms working together:

1. **Exclusive advisory lock** (`syscall.Flock(LOCK_EX)`) on `state.json.lock`
   during every write.  The lock file is separate from the data file so the
   atomic rename used for writes is not interfered with.  Holding the lock for
   only the milliseconds of the write minimises contention.

2. **Atomic rename** (`write tmp → os.Rename`) means readers always see a
   complete file.  No shared or reader lock is needed.

3. **mtime polling** every 500 ms inside the running TUI (`watcher.go`).  When
   the modification time of `state.json` advances past the value stamped after
   our last `persist()` call, we reload from disk, reconcile dead tmux windows,
   and refresh the sidebar — all without restarting the process.

**Why polling instead of `fsnotify`?**
`fsnotify` requires a new dependency and adds OS-specific complexity (kqueue/
inotify/ReadDirectoryChangesW). A 500 ms poll is imperceptible to humans, is
rock-solid on NFS/network volumes, and needs zero extra dependencies.

**Why not a central server/daemon?**
A daemon adds a single point of failure, a crash recovery problem, and IPC
complexity. The stateless polling approach degrades gracefully: if one instance
crashes mid-write, the lock is released by the OS and the atomic rename ensures
the file is either fully written or not at all.

**Trade-off accepted**: the exclusive lock and atomic rename prevent file
corruption and partial writes, but they do not prevent lost updates by
themselves — each instance writes its in-memory snapshot without re-reading
under the lock, so a stale instance can overwrite another's recent changes
(last-writer-wins).  The mtime watcher mitigates this: within 500 ms the
"losing" instance reloads the winner's state from disk.  In practice the
window for conflict is small (two users mutating different instances within
the same poll tick), and no data is silently corrupted — the losing write
is simply superseded.

---

## Go Dependencies Selected

```
github.com/charmbracelet/bubbletea   # TUI MVU framework
github.com/charmbracelet/lipgloss    # Terminal styling + layout
github.com/charmbracelet/bubbles     # textinput, list, key, spinner
github.com/spf13/cobra               # CLI subcommands
github.com/google/uuid               # UUID v4 generation
github.com/mattn/go-runewidth        # CJK-safe string width
golang.org/x/term                    # Terminal size detection
```

No CGO dependencies. tmux is the only external runtime requirement.
