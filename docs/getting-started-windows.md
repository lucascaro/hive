# Getting Started with Hive on Windows

This guide walks you from a fresh Windows machine to running your first AI agent session in Hive.

> **Important:** The built-in PTY multiplexer backend is not available on Windows.
> Hive on Windows uses **tmux** as its backend. You must install tmux before running Hive.

## Prerequisites

### 1. Go 1.25+

Download the Windows installer from [go.dev/dl](https://go.dev/dl/) and run it.
The installer adds `go` to your `PATH` automatically.

**Verify in PowerShell or Command Prompt:**

```powershell
go version   # should print go1.25 or higher
```

### 2. Git

If you don't already have Git, download it from [git-scm.com](https://git-scm.com/download/win).
Git Bash (included with Git for Windows) is a convenient terminal for running Hive.

### 3. tmux

Choose **one** of these installation methods:

**Option A — MSYS2 (recommended):**

1. Download and install [MSYS2](https://www.msys2.org/).
2. Open the MSYS2 terminal and run:
   ```bash
   pacman -S tmux
   ```

**Option B — WSL (Windows Subsystem for Linux):**

1. Enable WSL from PowerShell (run as Administrator):
   ```powershell
   wsl --install
   ```
2. Inside the WSL terminal:
   ```bash
   sudo apt install tmux   # Ubuntu/Debian
   ```

**Option C — Chocolatey:**

```powershell
choco install msys2
```

Then open the MSYS2 terminal and install tmux as in Option A.

**Verify tmux is available** from the terminal you plan to use:

```bash
tmux -V
```

## Install Hive

Open PowerShell (or Git Bash) and run:

```powershell
git clone https://github.com/lucascaro/hive
cd hive
.\build.ps1
```

`build.ps1` compiles `hive.exe` and installs it to `%ProgramFiles%\hive\`.
Run the script from an **elevated (Administrator) PowerShell** window to install system-wide.
It also adds the install directory to your user `PATH` automatically.

**Or build manually:**

```powershell
go build -o hive.exe .
```

Then copy `hive.exe` to any directory already on your `PATH`.

## Verify the Installation

Open a new terminal (to pick up the updated `PATH`) and run:

```powershell
hive version
```

## Configure the tmux Backend

On first launch, Hive creates its config directory at `%APPDATA%\hive\`
(e.g. `C:\Users\You\AppData\Roaming\hive\`) with a default `config.json`.

**You must set the multiplexer to `tmux`** before Hive will work on Windows:

1. Open `%APPDATA%\hive\config.json` in a text editor.
2. Set `"multiplexer": "tmux"`:

```json
{
  "multiplexer": "tmux"
}
```

> **Tip:** Run `hive start` once (it will fail or warn), then edit the config, and restart.
> Alternatively create `%APPDATA%\hive\config.json` with the content above before the first launch.

## First Launch

Open the terminal that has `tmux` on its `PATH` (MSYS2, Git Bash, or WSL) and run:

```bash
hive start
```

The TUI appears. You are now in the Hive session manager.

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

When using the tmux backend, detach with **`Ctrl+B D`** (the standard tmux detach chord).

You return to the Hive TUI; the agent keeps running in the background.

### 5. Quit Hive

| Key | Behaviour |
|-----|-----------|
| `q` | Quit the TUI — **sessions keep running** in the background |
| `Q` | Quit the TUI **and kill** all managed sessions |

Run `hive start` at any time to reconnect to your running sessions.

## Configuration Reference

Config file: `%APPDATA%\hive\config.json`

```json
{
  "schema_version": 1,
  "theme": "dark",
  "preview_refresh_ms": 500,
  "multiplexer": "tmux",
  "agents": {
    "claude":   { "cmd": ["claude"] },
    "codex":    { "cmd": ["codex"] },
    "gemini":   { "cmd": ["gemini"] },
    "copilot":  { "cmd": ["copilot"] },
    "aider":    { "cmd": ["aider"] },
    "opencode": { "cmd": ["opencode"] }
  }
}
```

## Data Storage

| Path | Contents |
|------|----------|
| `%APPDATA%\hive\config.json` | User configuration |
| `%APPDATA%\hive\state.json` | Projects, teams, sessions |
| `%APPDATA%\hive\hooks\` | Lifecycle hook scripts |
| `%APPDATA%\hive\hive.log` | Error and hook output log |

## Next Steps

| Resource | Description |
|----------|-------------|
| [keybindings.md](keybindings.md) | Full keyboard reference |
| [agent-teams.md](agent-teams.md) | Running multi-agent orchestrator + worker teams |
| [hooks.md](hooks.md) | Shell hooks for lifecycle events |
| [features.md](features.md) | Complete feature list |
| [../README.md](../README.md) | Top-level overview and configuration reference |
