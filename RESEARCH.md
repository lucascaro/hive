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
