# Testing Hive on Windows from a macOS host

Windows-only behaviour (ConPTY, Explorer, `PATHEXT`, the Ctrl keymap, the
`.exe` updater) cannot be checked by any layer in `scripts/test.sh`. This is
the recipe for running a real Windows build in a local VM and letting an
agent on the Mac drive it: take screenshots, click, type, run PowerShell.

It complements [verifying-the-gui-by-hand.md](verifying-the-gui-by-hand.md),
which drives the real frontend in a browser but cannot reach native
behaviour.

## Pieces

| Piece | Role |
|---|---|
| A Windows 11 VM (UTM, Parallels, VMware…) | Runs the Windows build. On Apple silicon it is Windows on ARM; the amd64 `.exe` runs under x64 emulation, which is also what ARM users get from a release. |
| [Windows-MCP](https://github.com/CursorTouch/Windows-MCP) in the guest | MCP server for the guest desktop, over streamable HTTP. |
| `scripts/windows-vm/setup-windows-mcp.ps1` | Installs it as a logon task, locked to the host. |
| `scripts/windows-vm/modclick.ps1` | Ctrl-/Ctrl+Shift-click and Ctrl-hover, which Windows-MCP's `Click` cannot do. |

## One-time setup

**1. VM networking.** Use a host-only or shared (NAT) network so the host and
guest share a private subnet. Find the host's address on it
(`ifconfig` on the host — on UTM it is the `bridge1xx` interface) and the
guest's (`ipconfig` in the guest).

**2. Generate a token on the host and keep it outside the repo** (e.g.
`openssl rand -hex 24 > ~/.config/<dir>/token; chmod 600 …`). Never commit
it or paste it into an issue or PR.

**3. Install Windows-MCP in the guest.** Copy
`scripts/windows-vm/setup-windows-mcp.ps1` and the token file into the guest
(see *Moving files*) and run the script in an **elevated** PowerShell:

```powershell
powershell -NoExit -ExecutionPolicy Bypass -File setup-windows-mcp.ps1 -HostIp <host-ip> -TokenFile <token-file>
```

The token is read from a file, not passed as an argument, because the setup
log and the process list both record command lines. The script deletes the
token file once it has read it.

It installs `uv`, runs `windows-mcp>=0.7.5` as a logon task in the
interactive session, and adds a firewall rule that admits only `<host-ip>`.
The server also enforces the token and its own IP allowlist.

**4. Register it with Claude Code on the host:**

```bash
claude mcp add --scope user --transport http windows-vm \
  http://<guest-ip>:8000/mcp --header "Authorization: Bearer $(cat <token-file>)"
```

`/mcp` should list `windows-vm` as connected; `Screenshot` returns the
guest desktop.

## Each test run

```bash
./build.sh --platform windows     # hivegui.exe + hived.exe in cmd/hivegui/build/bin
```

Copy both into the guest, then launch `hivegui.exe` with Windows-MCP's `App`
tool (`launch_executable`). It starts `hived` itself.

**Note:** building for macOS afterwards runs `wails build -clean`, which
deletes the `.exe` files. Rebuild before copying.

## Moving files into the guest

Pick one:

- **A one-off HTTP server on the host, bound to the VM network only.** Serve
  a dedicated directory that contains only what the guest needs:
  `python3 -m http.server <port> --bind <host-ip> --directory <dir>`. Pull it
  in the guest with `Invoke-WebRequest … -OutFile …`. Stop it when you're
  done. Never bind to `0.0.0.0` or serve the repo root. Serve a *copy* of
  the token file and delete it as soon as the guest has it.
- **The hypervisor's shared folder** (UTM: SPICE WebDAV from the guest
  tools).
- **SMB** from macOS File Sharing, limited to one folder.

## Driving the GUI

- **Typing into a session.** Windows-MCP's `Type` pastes with Ctrl+V, which
  a Hive terminal passes to the shell as `^V`. Put the text on the clipboard
  instead (`cmd /c 'echo|set /p="…"| clip'`; `Set-Clipboard` fails from the
  MCP's PowerShell) and paste with `Shortcut ctrl+shift+v`. `Type` works in
  ordinary inputs such as Settings.
- **Modifier clicks.** Dot-source `modclick.ps1` in the guest through the
  `PowerShell` tool, then call `[In]::ModClick(x, y, $shift)`. Click a plain
  spot in the terminal first so Hive has focus.
- **Checking side effects.** Prefer PowerShell evidence to screenshots: window
  titles (`Get-Process | ? MainWindowTitle`), Explorer windows
  (`(New-Object -ComObject Shell.Application).Windows()`), a marker file
  written by a script that must *not* run, or a probe program that logs its
  argv (useful for verifying the Ctrl+Shift+click editor command).

## Troubleshooting

| Symptom | Cause |
|---|---|
| Guest gets a `169.254.x.x` address, or reaches the host but not the internet | A host VPN or network-filtering security product intercepting the VM's traffic. Disconnect or pause it, then shut the VM down and start it again. |
| Host `curl`/`ping` to the guest fails with `No route to host` / `EHOSTUNREACH` | macOS Local Network privacy. The *responsible* app needs the permission; inside Hive that is Hive itself. Enable it under System Settings › Privacy & Security › Local Network, then relaunch the app. |
| `irm … \| iex` closes the PowerShell window | Run the script with `-File` as shown above; its log is at `%TEMP%\windows-mcp-setup.log`. |
| A red `NativeCommandError` block during setup | Windows PowerShell labels any native stderr as an error. Harmless when the log continues. |
| Start menu keeps opening | The host's ⌘ key reaching the guest as the Windows key. Press `esc`. |

## Security

- Windows-MCP gives whoever holds the token full control of the guest
  desktop. Use a disposable test VM with no personal accounts signed in.
- Keep the token, guest address and any host-specific notes out of the repo.
- In the guest, the token lives in `%LOCALAPPDATA%\windows-mcp\run.ps1`
  (readable by your user only) and in the running server's command line,
  which other accounts on the guest can list. Keep the guest single-user.
- Keep the three layers of restriction: private VM network, the firewall rule
  scoped to the host, and the server's token plus IP allowlist. Never expose
  the port through port forwarding or a bridged network.
- Stop any file server you started once the files are copied.
