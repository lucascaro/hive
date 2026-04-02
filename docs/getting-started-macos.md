# Getting Started with Hive on macOS

This guide walks you from a fresh macOS machine to running your first AI agent session in Hive.

## Prerequisites

### Go 1.25+

Hive is distributed as source and requires Go to build.

**Option A — Homebrew (recommended):**

```bash
brew install go
```

**Option B — Official installer:**

Download from [go.dev/dl](https://go.dev/dl/) and follow the macOS installer instructions.

**Verify:**

```bash
go version   # should print go1.25 or higher
```

### tmux

Hive uses tmux as its default (and recommended) backend for managing terminal sessions.

```bash
brew install tmux
```

**Verify:**

```bash
tmux -V   # should print tmux 3.x or higher
```

## Install Hive

```bash
git clone https://github.com/lucascaro/hive
cd hive
./build.sh
```

`build.sh` compiles the binary and copies it to `/usr/local/bin/hive`. You may be prompted for your password if `/usr/local/bin` requires elevated privileges.

**Or build manually and install wherever you prefer:**

```bash
go build -o hive .
mv hive /usr/local/bin/   # or any directory on your PATH
```

## Verify the Installation

```bash
hive version
```

## First Launch

```bash
hive start
```

On first launch Hive:
- Creates its config directory at `~/.config/hive/`
- Writes a default `config.json`
- Connects to tmux (starting the tmux server if it isn't already running)

The TUI appears in your terminal. You are now in the Hive session manager.

## Basic Workflow

### 1. Create a project

Press **`n`**, type a project name, press **`Enter`**.

Projects group related agent sessions together.

### 2. Create a session

With a project selected, press **`t`**.

A picker appears — choose your agent:

```
Select Agent
────────────
▶ [claude]   Claude (Anthropic)
  [codex]    Codex (OpenAI)
  [gemini]   Gemini (Google)
  [copilot]  GitHub Copilot CLI
  [aider]    Aider
  [opencode] OpenCode
  [custom]   Custom command
```

### 3. Connect to a session

Navigate to a session with **`j`** / **`k`** (or arrow keys), then press **`a`** or **`Enter`**.

The TUI suspends and you interact with the agent directly in the terminal.

### 4. Detach from a session

Press **`Ctrl+Q`** to detach. You return to the Hive TUI; the agent keeps running in the background.

### 5. Quit Hive

| Key | Behaviour |
|-----|-----------|
| `q` | Quit the TUI — **sessions keep running** in the background |
| `Q` | Quit the TUI **and kill** all managed sessions |

Run `hive start` at any time to reconnect to your running sessions.

## Configuration

The config file lives at `~/.config/hive/config.json`. Common settings:

```json
{
  "theme": "dark",
  "preview_refresh_ms": 500,
  "multiplexer": "tmux"
}
```

Hive uses tmux as the default backend. Detach from sessions with **Ctrl+B D** when inside a tmux session.

> **Native backend (alpha):** Hive includes an experimental built-in PTY backend that requires no external dependencies. It is **not recommended** for general use. To try it: set `"multiplexer": "native"` in `config.json` or pass `--native` to `hive start`.

## Notifications (optional)

Hive supports lifecycle hooks. To get a macOS notification when a new session is created, create this file:

```bash
mkdir -p ~/.config/hive/hooks
cat > ~/.config/hive/hooks/on-session-create << 'EOF'
#!/bin/bash
osascript -e "display notification \"$HIVE_SESSION_TITLE\" with title \"Hive: New $HIVE_AGENT_TYPE session\""
EOF
chmod +x ~/.config/hive/hooks/on-session-create
```

## Next Steps

| Resource | Description |
|----------|-------------|
| [keybindings.md](keybindings.md) | Full keyboard reference |
| [agent-teams.md](agent-teams.md) | Running multi-agent orchestrator + worker teams |
| [hooks.md](hooks.md) | Shell hooks for lifecycle events |
| [features.md](features.md) | Complete feature list |
| [../README.md](../README.md) | Top-level overview and configuration reference |
