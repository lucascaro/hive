# Getting Started with Hive on Windows

This guide walks you from a fresh Windows machine to running your first AI agent session in Hive.

> **Important:** The built-in PTY multiplexer backend is not available on Windows.
> Hive on Windows uses **tmux** as its backend. You must install tmux before running Hive.

## Install Hive

### Option A — Prebuilt binary (recommended)

No Go toolchain required.

```powershell
Invoke-WebRequest -Uri "https://github.com/lucascaro/hive/releases/latest/download/hive-windows-amd64.exe" -OutFile "hive.exe"
```

Move `hive.exe` to any directory already on your `PATH` (or create one, e.g. `C:\Tools\hive\`, and add it to your user `PATH` via **System Properties → Environment Variables**).

Then install tmux — see [Install tmux](#install-tmux) below.

### Option B — Build from source

Only needed if you want the latest unreleased changes.

1. Install **Go 1.25+** — download the Windows installer from [go.dev/dl](https://go.dev/dl/) and run it. The installer adds `go` to your `PATH` automatically. Verify in PowerShell:
   ```powershell
   go version   # should print go1.25 or higher
   ```
2. Install **Git** from [git-scm.com](https://git-scm.com/download/win) if you don't already have it.
3. Clone and build:
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

## Install tmux

Hive on Windows uses the `tmux` backend. Pick **one** of the following.

### Recommended: WSL (Windows Subsystem for Linux)

WSL gives hive the same behaviour it has on Linux/macOS:

- Sessions survive closing the terminal and reboots (tmux under MSYS2 dies with its terminal).
- Real Linux PTYs — OSC 2 title updates, mouse reporting, and alt-screen behave exactly as on Linux.
- AI agent CLIs (Claude, Codex, Gemini, Copilot, Aider, OpenCode) are Linux-first; WSL runs their native builds directly.
- Single toolchain (`apt` + native Linux Go), no Win32/MSYS boundary crossings.

1. Enable WSL from PowerShell (run as Administrator):
   ```powershell
   wsl --install
   ```
2. Inside the WSL terminal:
   ```bash
   sudo apt install tmux   # Ubuntu/Debian
   ```

### Alternative: MSYS2

Works, but tmux sessions do not survive closing the MSYS2 terminal or rebooting.

1. Download and install [MSYS2](https://www.msys2.org/).
2. Open the MSYS2 terminal and run:
   ```bash
   pacman -S tmux
   ```

### Alternative: Chocolatey

Same caveats as MSYS2.

```powershell
choco install msys2
```

Then open the MSYS2 terminal and install tmux: `pacman -S tmux`.

### Verify

From the terminal you plan to use:

```bash
tmux -V
```

## Verify the Installation

Open a new terminal (to pick up the updated `PATH`) and run:

```powershell
hive version
```

## Configure the tmux Backend

On first launch, Hive creates its config directory at `%APPDATA%\hive\`
(e.g. `C:\Users\You\AppData\Roaming\hive\`) with a default `config.json` that already
uses the tmux backend. No manual configuration change is required.

> **Tip:** If you need to customise other settings, open `%APPDATA%\hive\config.json` in a text editor.
> The default config already contains `"multiplexer": "tmux"` — do not remove it.

## First Launch

Open the terminal that has `tmux` on its `PATH` (WSL shell, or MSYS2 / Git Bash if you chose an alternative) and run:

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
