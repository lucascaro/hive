# Hive — Enterprise Security Assessment

**Document type:** Security assessment for enterprise deployment evaluation  
**Subject:** Hive (`github.com/lucascaro/hive`) — AI agent session multiplexer  
**Analysis basis:** Static code review of full source tree (Go 1.25, all packages)  
**Intended audience:** Enterprise security teams, IT governance, risk and compliance

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [What Hive Is (and Is Not)](#2-what-hive-is-and-is-not)
3. [Telemetry & Network Traffic](#3-telemetry--network-traffic)
4. [Data Handling & Privacy](#4-data-handling--privacy)
5. [Process Architecture & Isolation](#5-process-architecture--isolation)
6. [Hook System & Extensibility](#6-hook-system--extensibility)
7. [Deployment Environments](#7-deployment-environments)
8. [Hive vs. Direct Agent Use](#8-hive-vs-direct-agent-use)
9. [Dependencies & Supply Chain](#9-dependencies--supply-chain)
10. [Risk Matrix & Findings](#10-risk-matrix--findings)
11. [Recommendations](#11-recommendations)

---

## 1. Executive Summary

**Bottom line for security teams:** Hive is a local-only terminal multiplexer. It makes **no outbound network connections**, sends **no telemetry**, and does not intercept or store AI conversation content. The security boundary is the developer's workstation; Hive does not introduce cloud-side risk.

### Quick-Reference Scorecard

| Concern | Finding |
|---|---|
| Outbound network calls | ✅ None — fully local |
| Telemetry / analytics | ✅ None |
| Conversation storage | ✅ None (512 KB in-memory preview buffer only) |
| AI API access | ✅ Not applicable — Hive does not call AI APIs |
| Credential storage | ✅ None in Hive's config by default |
| Shell injection risk | ✅ Mitigated (direct exec native backend; POSIX-escaped tmux backend) |
| IPC socket exposure | ✅ Owner-only (mode `0o600`) |
| Multi-user data exposure | ✅ Fixed — `state.json`, `usage.json`, `hive.log` now written `0o600`; existing files re-chmoded at startup |
| Hook code execution | ⚠️ Hooks run arbitrary user scripts — same trust level as `.bashrc`; user-controlled only |
| Supply chain | ✅ All dependencies are established, pure UI/terminal libraries with no network access |

**Overall risk level:**
- Single-user machine: 🟢 Low
- Multi-user / shared server: 🟢 Low (permissions hardened; no credential leakage)
- Air-gapped environment: 🟢 Low (no phone-home behavior)

---

## 2. What Hive Is (and Is Not)

### 2.1 Core Purpose

Hive is a **terminal UI (TUI) session multiplexer** for AI coding agents. Its job is to:

1. Launch and manage multiple AI agent CLI processes (Claude, Codex, Gemini, Copilot Workspace, Aider, etc.) as child processes
2. Organize those processes into named **projects** and **teams**
3. Keep sessions alive across TUI restarts
4. Provide a live preview pane of session output
5. Fire lifecycle **hook scripts** on session/project/team events

### 2.2 What Hive is NOT

| Misconception | Reality |
|---|---|
| Hive is an AI API gateway | ❌ Hive never calls Anthropic, OpenAI, Google, or any AI API |
| Hive is a proxy for AI traffic | ❌ AI traffic flows directly from the agent CLI to its API |
| Hive captures or stores conversations | ❌ Conversation content is never written to disk by Hive |
| Hive modifies agent commands | ❌ Commands are passed through verbatim; tmux backend adds only a shell detach handler |
| Hive requires cloud connectivity | ❌ Hive runs entirely offline |

### 2.3 Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│  Developer Workstation                                   │
│                                                         │
│  ┌──────────┐       Unix socket        ┌─────────────┐  │
│  │  hive    │◄────────────────────────►│ mux-daemon  │  │
│  │  (TUI)   │    ~/.config/hive/       │  (Go PTY)   │  │
│  └──────────┘    mux.sock (0o600)      └──────┬──────┘  │
│                                               │         │
│                              ┌────────────────┼──────┐  │
│                              ▼                ▼      ▼  │
│                         ┌────────┐  ┌───────┐  ┌──────┐ │
│                         │ claude │  │ codex │  │ ...  │ │
│                         │  CLI   │  │  CLI  │  │      │ │
│                         └───┬────┘  └───┬───┘  └──┬───┘ │
└───────────────────────────┼────────────┼──────────┼─────┘
                             │            │          │
                         HTTPS        HTTPS       HTTPS
                             │            │          │
                       Anthropic       OpenAI    Google
                          API           API       API
```

**Key insight:** Hive manages processes. All AI communication occurs _outside_ Hive's scope, between the agent CLI and its respective provider API. Hive never sees tokens, never sees raw prompts, and never touches the network.

### 2.4 Supported Backends

Hive supports two session backends:

| Backend | Description | Default |
|---|---|---|
| **Native** | Built-in Go PTY daemon, zero external dependencies | Yes |
| **tmux** | Delegates to the system `tmux` binary | Optional |

---

## 3. Telemetry & Network Traffic

### 3.1 Outbound Network Activity

**Hive makes zero outbound network connections.** This was verified by auditing every Go source file for network-related imports and call sites.

**Network code found in the codebase:**

```go
// internal/mux/native/daemon.go
l, err := net.Listen("unix", sockPath)       // local Unix socket listener
conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)  // local only
```

Both calls use the `"unix"` network type (Unix domain socket), which is local IPC — not a TCP/IP connection. No `"tcp"`, `"tcp4"`, `"tcp6"`, or `"udp"` network types appear anywhere in the codebase.

### 3.2 Telemetry

| Type | Present | Notes |
|---|---|---|
| Usage analytics | ❌ None | |
| Crash/error reporting | ❌ None | |
| Performance metrics | ❌ None | |
| Update checks | ❌ None | No auto-update mechanism exists |
| License validation | ❌ None | Open-source, no license server |
| AI API calls | ❌ None | Hive spawns agents; it doesn't call them |

### 3.3 DNS and External Hosts

No DNS lookups (`net.Lookup*`, `net.ResolveIPAddr`, etc.) appear in the codebase. There are no hardcoded external hostnames or IP addresses in any source file.

### 3.4 Firewall Posture

For enterprise environments with egress filtering:

- **No firewall rules are required for Hive itself.** 
- Firewall rules for AI APIs (Anthropic, OpenAI, Google, etc.) are determined by which agent CLIs are deployed — that is independent of Hive.
- Hive's only socket is a local Unix domain socket (`~/.config/hive/mux.sock`); it does not bind to any TCP port.

---

## 4. Data Handling & Privacy

### 4.1 What Hive Stores

Hive maintains four files under `~/.config/hive/` (the directory can be overridden via `HIVE_CONFIG_DIR`):

| File | Content | Mode | Notes |
|---|---|---|---|
| `config.json` | Agent commands, keybindings, theme, multiplexer choice | `0o600` | Owner-only |
| `state.json` | Project names, team names, session titles, working dirs | `0o600` | ✅ Fixed |
| `usage.json` | Agent type usage frequency and recency | `0o600` | ✅ Fixed |
| `mux-daemon.log` | Daemon startup events, IPC errors | `0o600` | Owner-only |
| `hive.log` | TUI errors, hook failures | `0o600` / `0o644`* | ⚠️ Inconsistent |
| `mux.sock` | Native daemon IPC socket | `0o600` | Owner-only |

\* `hive.log` is created with `0o600` by the main app but opened with `0644` by some TUI components. The effective mode is set at file creation, so the first opener wins — but the inconsistency is a code quality issue. See [Risk Matrix](#10-risk-matrix--findings).

### 4.2 What Hive Does NOT Store

| Data | Stored? | Notes |
|---|---|---|
| AI conversation content | ❌ Never | |
| Prompts or messages | ❌ Never | |
| Agent output (persistent) | ❌ Never | 512 KB in-memory circular buffer only |
| API keys / credentials | ❌ Not by default | User _could_ put them in agent cmd args (see §4.5) |
| User code or files | ❌ Never | Only working directory _paths_ are stored |

### 4.3 Session Output Handling

Each session has a 512 KB in-memory circular buffer used to populate the preview pane. This buffer:

- Exists only in the daemon process's memory
- Is never written to disk
- Is discarded when the session is killed
- Is not accessible to other OS users (isolated in daemon memory)

```go
// internal/mux/native/pane.go
const bufSize = 512 * 1024  // 512 KB circular buffer per session
```

### 4.4 Log Contents

**`mux-daemon.log`** contains only operational events:
- Daemon start/stop messages
- IPC connection lifecycle (`"daemon: write to ... failed: ..."`)
- No session output, no conversation content

**`hive.log`** contains only operational events:
- TUI startup errors
- Hook execution failures and timeouts
- Component errors
- No session output, no conversation content

### 4.5 Credentials and API Keys

**Hive does not store credentials.** API keys used by agent CLIs (e.g., `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) are handled by the user's shell environment and passed to agent processes through the standard Unix environment inheritance mechanism — the same as running the agent CLI directly.

**Risk:** A user _could_ embed credentials directly in `config.json` agent command arguments:
```json
{
  "agents": {
    "my-llm": { "cmd": ["my-llm", "--api-key", "sk-..."] }
  }
}
```
If they do, the key is stored in plaintext in `config.json` (mode `0o600`, owner-only). This is the user's choice and is equivalent to putting a key in `~/.bashrc`. Enterprise deployments should establish policy against this practice (see §11).

**Recommended pattern:**
```bash
# ~/.bashrc or ~/.zshrc — do NOT put keys in config.json
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
```

### 4.6 Environment Variable Handling

Hive reads two environment variables for its own use:

| Variable | Purpose |
|---|---|
| `HIVE_CONFIG_DIR` | Overrides the configuration directory (`~/.config/hive/`) |
| `HOME` | Used as fallback for resolving the config directory |

Agent child processes **inherit the full environment** of the Hive process (standard Unix `execve` behavior). This is the same behavior as running the agent CLI directly from the terminal.

Hook scripts receive the parent environment plus a set of `HIVE_*` variables (see §6.3).

---

## 5. Process Architecture & Isolation

### 5.1 Daemon Model

Hive uses a **separate daemon process** (`hive mux-daemon`) to manage agent sessions. The TUI communicates with the daemon over a local Unix socket.

```
hive (TUI process)  ◄──── mux.sock (0o600) ────►  hive mux-daemon
                                                        │
                                                   ┌────┴────┐
                                                   │ PTY     │
                                                   │ sessions│
                                                   └─────────┘
```

**Benefits of this model:**
- Agent sessions survive TUI restart or crash
- A crash in a single agent session does not affect other sessions
- The daemon runs with `Setsid` (new Unix session group), fully detached from the terminal

**Daemon startup:**
```go
// cmd/start.go — daemon is spawned if socket does not exist
cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}  // detach from terminal
cmd.Stdin = nil
cmd.Stdout = nil
cmd.Stderr = nil
```

### 5.2 Agent Process Spawning

#### Native Backend (default)

```go
// internal/mux/native/pane.go
cmd := exec.Command(args[0], args[1:]...)
cmd.Dir = workDir
ptm, err := pty.Start(cmd)
```

- **No shell invocation** — the binary is executed directly via `execve`
- No shell expansion, no glob expansion, no variable substitution
- No command injection risk from user-controlled session names or project names
- Working directory explicitly set from stored configuration

#### tmux Backend

```go
// internal/mux/tmux/backend.go
func (b *Backend) wrapCmd(cmd []string) []string {
    quoted := make([]string, len(cmd))
    for i, arg := range cmd {
        quoted[i] = shellQuote(arg)
    }
    return []string{"sh", "-c", strings.Join(quoted, " ") + "; tmux detach-client"}
}

func shellQuote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

- A shell (`sh -c`) is used to append `; tmux detach-client` after the agent exits
- Every argument is wrapped in POSIX single quotes with proper `'\''` escaping
- This is the established, safe method for constructing shell commands from arbitrary strings

### 5.3 IPC Socket Security

The native daemon's Unix socket is created with mode `0o600`:

```go
// internal/mux/native/daemon.go
l, err := net.Listen("unix", sockPath)
// ...
if err := os.Chmod(sockPath, 0o600); err != nil {
    return fmt.Errorf("chmod socket: %w", err)
}
```

- Only the file owner can connect
- Other OS users on the same machine cannot communicate with the daemon
- No TCP port is exposed; the socket is invisible to network scanners

### 5.4 Process Permissions

Hive runs with the **invoking user's permissions** — no privilege escalation, no `setuid`/`setgid` bits, no `sudo` requirements. Agent child processes run with the same UID/GID as Hive.

### 5.5 Signal and Resource Handling

- Sessions are terminated via `Process.Kill()` (SIGKILL)
- No custom signal handlers that could mask OS security signals
- No shared memory, no POSIX message queues, no System V IPC
- File descriptors: one PTY master per session, closed on session kill

---

## 6. Hook System & Extensibility

### 6.1 Overview

Hive fires lifecycle hooks at key events. Hooks are executable files placed in `~/.config/hive/hooks/` named `on-{event}`. This is an **opt-in** system — no hooks exist by default.

**Events:**

| Event | Trigger |
|---|---|
| `on-session-create` | New agent session started |
| `on-session-kill` | Session terminated |
| `on-session-attach` | User attaches to session |
| `on-session-detach` | User detaches from session |
| `on-session-title-changed` | Session title changed |
| `on-project-create` | New project created |
| `on-project-kill` | Project deleted |
| `on-team-create` | New team created |
| `on-team-kill` | Team deleted |
| `on-team-member-add` | Session added to team |
| `on-team-member-remove` | Session removed from team |

### 6.2 Execution Model

```go
// internal/hooks/runner.go
func runScript(path string, extraEnv []string) error {
    ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
    defer cancel()
    cmd := exec.CommandContext(ctx, path)           // direct execution, no shell
    cmd.Env = append(os.Environ(), extraEnv...)     // parent env + HIVE_* vars
    return cmd.Run()
}
```

**Security controls on hook execution:**

| Control | Detail |
|---|---|
| Executable check | Only files passing `os.Stat` + executable bit check are run |
| Direct execution | Hook path is executed directly — no shell wrapper, no glob expansion |
| Timeout | 5-second hard timeout via `context.WithTimeout` |
| Error isolation | Hook errors are logged; they do not crash Hive or affect sessions |
| No auto-install | Hooks are never downloaded or updated remotely |

### 6.3 Hook Environment Variables

Hooks receive the full parent environment plus these `HIVE_*` variables:

```
HIVE_VERSION         Hive version string
HIVE_EVENT           Event name (e.g., "session-create")
HIVE_PROJECT_ID      UUID of the project
HIVE_PROJECT_NAME    Human-readable project name
HIVE_SESSION_ID      UUID of the session
HIVE_SESSION_TITLE   Current session title
HIVE_TEAM_ID         UUID of the team (if applicable)
HIVE_TEAM_NAME       Team name
HIVE_TEAM_ROLE       Role within team (orchestrator / worker)
HIVE_AGENT_TYPE      Agent type (claude, codex, etc.)
HIVE_AGENT_CMD       Full agent command string
HIVE_TMUX_SESSION    tmux session name (tmux backend only)
HIVE_TMUX_WINDOW     tmux window index (tmux backend only)
HIVE_WORK_DIR        Working directory of the session
```

**Note on `HIVE_AGENT_CMD`:** This is the command used to launch the agent (e.g., `claude --no-update`). If a user has embedded credentials in their agent command (see §4.5), those credentials would appear here. Enterprise deployments should audit hook scripts to ensure they do not log `HIVE_AGENT_CMD` to shared locations.

### 6.4 Trust Model

Hook scripts run with **the invoking user's full permissions**. This is intentional and equivalent to code in `~/.bashrc` or a git hook. The trust boundary is:

- ✅ Only the user who launched Hive can place scripts in `~/.config/hive/hooks/`
- ✅ Scripts are never fetched from the network
- ✅ Scripts are never modified by Hive
- ⚠️ Scripts can do anything the user can do (write files, make network calls, etc.)

Enterprise deployments on shared machines should apply the standard directory permission controls (`chmod 700 ~/.config/hive/hooks/`) and review hook scripts as part of developer onboarding.

---

## 7. Deployment Environments

### 7.1 Supported Platforms

Hive builds for Linux, macOS, and Windows. The native PTY daemon uses platform-specific process management:

- **Unix (Linux/macOS):** `Setsid` for daemon detachment, `/dev/ptmx` for PTY
- **Windows:** `CREATE_NEW_PROCESS_GROUP` for daemon detachment, ConPTY

### 7.2 Single-User Machine (Recommended)

**Risk:** 🟢 Low

This is the intended deployment model. All files are owned by one user, so the world-readable permission issue in `state.json` has no practical impact.

**Typical setup:** Developer's laptop or dedicated cloud VM.

### 7.3 Multi-User / Shared Development Server

**Risk:** 🟢 Low

On machines with multiple OS users:

| Concern | Detail |
|---|---|
| **Daemon socket** | `0o600` — other users cannot connect |
| **`state.json`** | `0o600` — owner-only (fixed) |
| **`usage.json`** | `0o600` — owner-only (fixed) |
| **Log files** | `hive.log` and `mux-daemon.log` both `0o600` (fixed) |
| **Session output** | Confined to daemon memory; other users cannot read it |

All sensitive files are now owner-only. On startup, `config.Ensure()` calls `FixPermissions()` to retroactively tighten permissions on files created by older versions.

### 7.4 Container / Docker Environments

**Risk:** 🟢 Low

Single-user containers are the lowest-risk deployment:

- No other OS users share the container
- All config files are owner-only
- Daemon socket is isolated within the container namespace

Hive produces a single static binary — no runtime dependencies beyond the agent CLIs it is configured to launch.

### 7.5 Air-Gapped Environments

**Risk:** 🟢 Low (Hive side)

Hive itself makes no network calls, so it functions fully in air-gapped environments. Whether AI agents work in air-gapped environments depends on whether the agent CLI can reach its API — that is independent of Hive.

### 7.6 CI/CD and Automated Pipelines

Hive is a TUI-first tool and not designed for headless CI use. It requires a terminal. Running it in a CI pipeline is not a supported use case and not recommended.

---

## 8. Hive vs. Direct Agent Use

This section answers the enterprise question: _"What changes when a developer uses Hive instead of running `claude` directly in a terminal?"_

### 8.1 What Does NOT Change

| Aspect | Direct Use | Via Hive |
|---|---|---|
| AI API endpoint | Agent's configured endpoint | Same — Hive is not in the path |
| Authentication | Agent's own auth (env var / config) | Same — Hive passes environment through |
| Network traffic | Agent to AI provider | Same |
| Data residency | Governed by agent/provider | Same |
| Content filtering | Governed by provider | Same |
| Agent capabilities | Full agent feature set | Same — Hive does not restrict agent |
| Terminal access | User's working directory | Same — Hive sets `cmd.Dir` from config |

### 8.2 What Hive Adds

| Feature | Security Implication |
|---|---|
| **Session persistence** | Sessions survive terminal restart; output preserved in memory (not disk) |
| **Multi-session management** | More agent processes running simultaneously; each independently scoped |
| **Daemon process** | Additional long-running process owned by the user |
| **Unix socket IPC** | New local IPC channel (owner-only, `0o600`) |
| **Output preview buffering** | 512 KB of recent output per session in daemon memory |
| **Hook scripts** | User-defined code runs on lifecycle events |
| **Config file** | Agent launch commands stored in `~/.config/hive/config.json` (`0o600`) |
| **State/usage files** | Project metadata stored in `~/.config/hive/state.json` (`0o600`) |
| **Multi-agent teams** | Orchestrator + worker pattern for coordinated agent work |

### 8.3 What Hive Removes / Improves

| Risk | Direct Use | Via Hive |
|---|---|---|
| **Session loss on disconnect** | Sessions die on terminal close | Sessions survive (daemon keeps them alive) |
| **Credential in shell history** | `claude --api-key sk-...` in `.bash_history` | Key stays in config file (`0o600`), not shell history |
| **Untracked agent proliferation** | Easy to lose track of running agents | Named sessions in UI; visible what's running |

### 8.4 Net Security Assessment

Using Hive introduces a **small, well-bounded increase in local attack surface** compared to direct CLI use:

- One additional long-running process (daemon)
- One additional IPC socket (owner-only)
- One additional config/state directory (`~/.config/hive/`)
- Hook scripts (optional, user-controlled)

It introduces **no new network attack surface** and **no new data exfiltration paths**. An attacker who has already compromised the user's account gains no meaningful additional capability from Hive being present — the agent CLIs themselves are the higher-value target for such an attacker.

---

## 9. Dependencies & Supply Chain

### 9.1 Direct Dependencies

| Package | Purpose | Network Access | Notes |
|---|---|---|---|
| `charmbracelet/bubbletea` | TUI event loop framework | ❌ None | Established, widely used |
| `charmbracelet/bubbles` | TUI input components | ❌ None | UI library |
| `charmbracelet/lipgloss` | Terminal styling | ❌ None | Rendering only |
| `charmbracelet/x/ansi` | ANSI escape parsing | ❌ None | Local parsing |
| `creack/pty` | PTY allocation (Unix) | ❌ None | Standard Go PTY library |
| `google/uuid` | UUID v4 generation | ❌ None | Cryptographic RNG only |
| `spf13/cobra` | CLI argument parsing | ❌ None | Standard CLI framework |
| `golang.org/x/term` | Terminal I/O | ❌ None | Platform terminal utils |

### 9.2 Indirect Dependencies

All indirect dependencies are UI/terminal utilities (ANSI, clipboard, color, string width, fuzzy search). None make network calls.

Full dependency list is pinned in `go.sum` with cryptographic hashes, providing standard Go module supply chain integrity.

### 9.3 Notable Absence

There are **no dependencies** on:
- Cloud SDKs (AWS, GCP, Azure)
- AI provider SDKs (Anthropic, OpenAI, Google GenAI)
- HTTP client libraries (`resty`, `go-retryablehttp`, etc.)
- Telemetry SDKs (OpenTelemetry, Datadog, Sentry, etc.)
- Authentication libraries

---

## 10. Risk Matrix & Findings

### 10.1 Severity Definitions

| Level | Description |
|---|---|
| 🔴 Critical | Immediate exploitation risk, data loss, or remote code execution |
| 🟠 High | Significant security impact requiring prompt remediation |
| 🟡 Medium | Meaningful risk under specific conditions; remediation recommended |
| 🔵 Low | Limited impact; remediate in normal course of work |
| ⚪ Info | No direct impact; documentation/process gap |

### 10.2 Findings

---

#### Finding #1 — World-Readable State and Usage Files ✅ Fixed

| Field | Value |
|---|---|
| **Severity** | 🔵 Low / 🟡 Medium (on multi-user systems) |
| **Status** | **Fixed** — `internal/tui/persist.go` now uses `0o600`; `config.Ensure()` chmods existing files at startup |
| **Files** | `~/.config/hive/state.json`, `~/.config/hive/usage.json` |

**Description:**  
Both `state.json` (project/team/session metadata) and `usage.json` (agent usage statistics) were written with mode `0o644`, making them readable by all OS users on the same machine.

**Fix applied:**
- `internal/tui/persist.go`: both `saveUsage` and `saveState` now write with `0o600`
- `internal/config/load.go`: `config.Ensure()` now calls `FixPermissions()` at every startup to retroactively chmod these files for users upgrading from older versions

**Data that was exposed (now protected):**
- Project names and working directory paths
- Team names and roles
- Session titles and agent types
- Agent usage frequency (which agents are used, how often)

**Data NOT exposed (unchanged):**
- Conversation content
- Source code
- Credentials or API keys

---

#### Finding #2 — Inconsistent Log File Permissions ✅ Fixed

| Field | Value |
|---|---|
| **Severity** | 🔵 Low |
| **Status** | **Fixed** — all three component files now use `0o600` |
| **File** | `~/.config/hive/hive.log` |

**Description:**  
`hive.log` was created with mode `0o600` in `app.go`, but three TUI components opened the same log file with mode `0o644` for append operations. This was a code defect that could cause the log to be created world-readable in edge cases.

**Fix applied:** All `hive.log` open calls in `sidebar.go`, `preview.go`, and `statusbar.go` now use `0o600`. Additionally, `config.FixPermissions()` at startup ensures any existing world-readable log file is corrected retroactively.

---

#### Finding #3 — Full Environment Inheritance by Agent Processes

| Field | Value |
|---|---|
| **Severity** | ⚪ Info (expected behavior) |
| **Component** | `internal/mux/native/pane.go` |

**Description:**  
Agent child processes inherit the complete environment of the Hive process. This is standard Unix behavior and identical to running agent CLIs directly from a terminal. If a developer has set sensitive environment variables (e.g., `AWS_SECRET_ACCESS_KEY`, `DATABASE_URL`) in their shell, those variables are accessible to the agent process.

This is expected and intentional — it is how agent CLIs receive their API keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`). No remediation is required in Hive, but organizations should have policies about which environment variables are set in developer shells.

---

#### Finding #4 — Hook Scripts Have Full User Privileges

| Field | Value |
|---|---|
| **Severity** | ⚪ Info (by design) |
| **Component** | `internal/hooks/runner.go` |

**Description:**  
Hook scripts execute with the full permissions of the invoking user. This is the intended behavior — hooks are an extension mechanism, not a sandbox.

Mitigating controls already present:
- Scripts must be explicitly placed by the user in `~/.config/hive/hooks/`
- No remote delivery or auto-installation of hooks
- Executable bit check required
- 5-second execution timeout
- Errors are logged and isolated; hooks cannot crash Hive

**Enterprise consideration:** Treat hook scripts like `~/.bashrc` entries. Review them during developer onboarding if your organization reviews dotfiles.

---

#### Finding #5 — `HIVE_AGENT_CMD` in Hook Environment

| Field | Value |
|---|---|
| **Severity** | ⚪ Info (conditional) |
| **Component** | `internal/hooks/env.go` |

**Description:**  
The `HIVE_AGENT_CMD` hook environment variable contains the full agent command string. If a user has embedded an API key in their agent command (see §4.5), that key will be present in `HIVE_AGENT_CMD` and accessible to all hook scripts.

**Remediation:** Enterprise policy should prohibit embedding credentials in agent command arguments. Use environment variables instead. Document this in onboarding materials.

---

### 10.3 Summary Table

| # | Finding | Severity | Remediation |
|---|---|---|---|
| 1 | ~~World-readable `state.json` / `usage.json`~~ | 🔵 ~~Low / 🟡 Medium~~ | ✅ Fixed — `0o600` writes + startup chmod |
| 2 | ~~Inconsistent `hive.log` permissions~~ | 🔵 ~~Low~~ | ✅ Fixed — standardized to `0o600` |
| 3 | Full environment inheritance by agents | ⚪ Info | Enterprise shell policy; no code change |
| 4 | Hook scripts have full user privileges | ⚪ Info | By design; review hooks in onboarding |
| 5 | `HIVE_AGENT_CMD` may contain credentials | ⚪ Info | Policy: no credentials in agent cmd args |

---

## 11. Recommendations

### 11.1 For Enterprise Security Teams (Evaluating Deployment)

1. **Approve Hive for single-user workstations and containers without restriction.** The tool has no network attack surface and no telemetry.

2. **On shared multi-user servers:** Apply the `0o600` fix (Finding #1) or instruct developers to manually tighten permissions after first run:
   ```bash
   chmod 600 ~/.config/hive/state.json ~/.config/hive/usage.json
   ```

3. **Establish policy: no credentials in `config.json`.** Ensure developer documentation specifies that API keys must be set as environment variables, not embedded in `config.json` agent command arguments.

4. **Apply standard hook script review** as part of developer environment onboarding — same as reviewing `~/.bashrc` or git hooks.

5. **No special firewall rules required for Hive.** Firewall rules for AI provider APIs are determined by which agent CLIs are in use, not by Hive.

6. **No DLP rules required for Hive.** Hive does not transmit conversation content; DLP posture for AI conversations is governed by the agent CLI and provider.

### 11.2 For Development Teams Using Hive

1. **Never put API keys in config.json agent command arguments.** Use environment variables:
   ```bash
   export ANTHROPIC_API_KEY="sk-ant-..."
   export OPENAI_API_KEY="sk-..."
   ```

2. **Review your hook scripts** before deploying to shared systems. Ensure they do not write sensitive data to world-readable locations.

3. **Use the `HIVE_CONFIG_DIR` environment variable** to point Hive at a directory with `0o700` permissions for additional defence-in-depth on shared systems:
   ```bash
   export HIVE_CONFIG_DIR="$HOME/.local/hive"
   mkdir -p "$HIVE_CONFIG_DIR"
   chmod 700 "$HIVE_CONFIG_DIR"
   ```

### 11.3 For Maintainers

**Findings #1 and #2 are resolved.** Remaining documentation improvements:

- Add "Credential Handling" section to README
- Expand SECURITY.md with file permission table and credential guidance
- Add note to hooks documentation: avoid logging `HIVE_AGENT_CMD` to shared paths

---

## Appendix A: Files and Directories Reference

```
~/.config/hive/               (0o755) Config directory
├── config.json               (0o600) Agent commands, keybindings, theme
├── state.json                (0o600) Projects/teams/sessions metadata
├── usage.json                (0o600) Agent usage statistics
├── hive.log                  (0o600) TUI error log
├── mux-daemon.log            (0o600) Daemon operational log
├── mux.sock                  (0o600) Native daemon Unix socket (IPC)
└── hooks/                    (0o755) User lifecycle hook scripts
    ├── on-session-create      (user-set) Called on new session
    ├── on-session-kill        (user-set) Called on session termination
    └── on-session-create.d/   (0o755) Multiple hooks per event
        ├── 01-notify.sh
        └── 02-log.sh
```

## Appendix B: Binary and Process Reference

```
hive               Main binary (TUI + CLI)
├── hive start     Starts TUI, ensures daemon is running
├── hive kill      Kills daemon and all sessions
├── hive mux-daemon  Internal: daemon process (not for direct use)
└── hive version   Prints version
```

Processes at runtime:
```
hive (TUI)         PID owned by user, exits when TUI closes
└── hive mux-daemon  PID owned by user, survives TUI exit
    ├── claude     Agent process (PTY session)
    ├── codex      Agent process (PTY session)
    └── ...
```

## Appendix C: Key Code Locations

| Topic | File |
|---|---|
| Config loading and writing | `internal/config/load.go` |
| State/usage persistence | `internal/tui/persist.go` |
| Native daemon socket | `internal/mux/native/daemon.go` |
| Agent process spawning | `internal/mux/native/pane.go` |
| Shell escaping (tmux) | `internal/mux/tmux/backend.go` |
| Hook execution | `internal/hooks/runner.go` |
| Hook environment variables | `internal/hooks/env.go` |
| Log file initialization | `internal/tui/app.go` |
| Daemon auto-start | `cmd/start.go` |

---

*This report is based on static analysis of the Hive source code. It does not constitute a formal penetration test or dynamic security assessment. Organizations with heightened security requirements should conduct their own independent review.*
