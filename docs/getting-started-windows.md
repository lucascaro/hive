# Getting Started with Hive on Windows

This guide walks you from a fresh Windows 11 machine to running your first AI agent session in Hive.

> **Hive on Windows runs inside WSL (Windows Subsystem for Linux).** WSL gives hive the same behaviour it has on Linux/macOS — sessions persist across terminal close and reboots, real Linux PTYs, and AI agent CLIs run as their native Linux builds. See [Why WSL?](#why-wsl) below for the full rationale.

## Install Hive (WSL path, recommended)

This flow uses PowerShell exactly once, to bootstrap WSL. Every subsequent step runs in the WSL shell.

### 1. Install WSL

Open **PowerShell as Administrator** (right-click Start → *Terminal (Admin)* on Windows 11) and run:

```powershell
wsl --install
```

This enables the WSL feature and installs Ubuntu (the default distro). Reboot if prompted.

On first launch, WSL will ask for a Linux username and password. These are independent of your Windows account — pick anything memorable; the password is used for `sudo`.

**Verify:**

```powershell
wsl --status
```

You should see a default distribution (e.g. `Ubuntu`) listed.

### 2. Launch the WSL shell

You now have a real Linux shell. Any of the following opens it:

- Open **Ubuntu** from the Start menu.
- Open **Windows Terminal** and choose the *Ubuntu* tab from the dropdown.
- Run `wsl` in any PowerShell or Command Prompt window.

**Everything from here on runs inside the WSL (Linux) shell.** If a prompt in this guide looks like `$`, you're in WSL.

### 3. Install tmux

Inside the WSL shell:

```bash
sudo apt update && sudo apt install -y tmux curl
```

**Verify:**

```bash
tmux -V
```

You should see a version string like `tmux 3.x`.

### 4. Download the hive binary

Still inside WSL. Use the **Linux** binary — not the Windows `.exe`:

```bash
curl -L -o hive https://github.com/lucascaro/hive/releases/latest/download/hive-linux-amd64
chmod +x hive
sudo mv hive /usr/local/bin/
```

**Verify:**

```bash
hive version
```

### 5. Launch Hive

Still inside WSL:

```bash
hive start
```

The TUI appears. You're done with the install — [skip to Basic Workflow](#basic-workflow).

**Config directory (WSL path):** `~/.config/hive/` inside WSL (e.g. `/home/you/.config/hive/`), exactly like Linux.

## Why WSL?

- **Sessions survive** closing the terminal and reboots (tmux under MSYS2 dies with its terminal).
- **Real Linux PTYs** — OSC 2 title updates, mouse reporting, and alt-screen behave exactly as on Linux.
- **AI agent CLIs** (Claude, Codex, Gemini, Copilot, Aider, OpenCode) are Linux-first; WSL runs their native builds directly.
- **Single toolchain** (`apt install` + native Linux Go), no Win32/MSYS boundary crossings.

## Native Windows alternative (MSYS2)

> ⚠️ **Caveat:** tmux sessions under MSYS2 **do not survive closing the MSYS2 terminal or rebooting**. Session persistence is hive's core value prop — use this path only if you genuinely can't run WSL.

### 1. Install tmux via MSYS2

Download and install [MSYS2](https://www.msys2.org/), then in the MSYS2 terminal:

```bash
pacman -S tmux
```

Alternatively, via Chocolatey in PowerShell: `choco install msys2`, then open the MSYS2 terminal and run `pacman -S tmux` as above.

**Verify** (from the MSYS2 terminal): `tmux -V`.

### 2. Install Hive

**Option A — Prebuilt binary.** In PowerShell:

```powershell
Invoke-WebRequest -Uri "https://github.com/lucascaro/hive/releases/latest/download/hive-windows-amd64.exe" -OutFile "hive.exe"
```

Move `hive.exe` to a directory already on your `PATH` (or create one, e.g. `C:\Tools\hive\`, and add it to your user `PATH` via **System Properties → Environment Variables**).

**Option B — Build from source.** Requires **Go 1.25+** from [go.dev/dl](https://go.dev/dl/) and **Git** from [git-scm.com](https://git-scm.com/download/win).

```powershell
git clone https://github.com/lucascaro/hive
cd hive
.\build.ps1          # builds hive.exe and installs to %ProgramFiles%\hive\ (run as Administrator)
```

Or build manually: `go build -o hive.exe .`, then move `hive.exe` onto your `PATH`.

### 3. Launch Hive

Open the **MSYS2 terminal** (or any shell that has `tmux` on `PATH`, such as Git Bash when MSYS2's `bin` is on `PATH`) and run:

```bash
hive start
```

**Config directory (native Windows path):** `%APPDATA%\hive\` (e.g. `C:\Users\You\AppData\Roaming\hive\`). No manual config change is needed — tmux is already the default backend.

## Basic Workflow

Once Hive is running, the workflow is identical across platforms.

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

Config file:

- **WSL path:** `~/.config/hive/config.json` (inside WSL)
- **Native Windows path:** `%APPDATA%\hive\config.json`

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

### WSL path (recommended)

| Path | Contents |
|------|----------|
| `~/.config/hive/config.json` | User configuration |
| `~/.config/hive/state.json` | Projects, teams, sessions |
| `~/.config/hive/hooks/` | Lifecycle hook scripts |
| `~/.config/hive/hive.log` | Error and hook output log |

### Native Windows path

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
