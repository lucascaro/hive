# Installs Windows-MCP in a Windows test VM so an agent on the host can
# drive the desktop (screenshots, clicks, keys, PowerShell).
# See docs/testing-on-windows.md. Run in an elevated PowerShell in the guest:
#
#   powershell -NoExit -ExecutionPolicy Bypass -File setup-windows-mcp.ps1 `
#     -HostIp <host address on the VM network> -Token <random secret>
#
# Test VMs only. Anyone who reaches the port with the token controls the
# guest's desktop as the logged-in user.
param(
  [Parameter(Mandatory)][string]$HostIp,
  [Parameter(Mandatory)][ValidatePattern('^.{32,}$')][string]$Token,
  [int]$Port = 8000
)
$ErrorActionPreference = 'Stop'
Start-Transcript "$env:TEMP\windows-mcp-setup.log" -Force | Out-Null

if (-not (Get-Command uv -ErrorAction SilentlyContinue)) {
  # Child process: the installer must not share (or end) this session.
  powershell -NoProfile -ExecutionPolicy Bypass -c "irm https://astral.sh/uv/install.ps1 | iex"
  if ($LASTEXITCODE) { throw "uv install failed ($LASTEXITCODE)" }
  $env:Path = "$env:USERPROFILE\.local\bin;$env:Path"
}

# Only the host may reach the port.
Get-NetFirewallRule -DisplayName 'Windows-MCP' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
New-NetFirewallRule -DisplayName 'Windows-MCP' -Direction Inbound -Protocol TCP -LocalPort $Port -RemoteAddress $HostIp -Action Allow | Out-Null

# A logon task, not a service: screenshots and input need the interactive desktop.
# >=0.7.5 carries the fix for GHSA-vrxg-gm77-7q5g (wildcard CORS on HTTP transports).
$Dir = "$env:LOCALAPPDATA\windows-mcp"; New-Item -ItemType Directory -Force $Dir | Out-Null
$Uvx = (Get-Command uvx).Source
@"
& '$Uvx' --python 3.13 --from 'windows-mcp>=0.7.5' windows-mcp serve --transport streamable-http --host 0.0.0.0 --port $Port --auth-key '$Token' --ip-allowlist '$HostIp' *>> '$Dir\server.log'
"@ | Set-Content "$Dir\run.ps1"
icacls "$Dir\run.ps1" /inheritance:r /grant:r "${env:USERNAME}:F" | Out-Null

$Action  = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$Dir\run.ps1`""
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
Register-ScheduledTask -TaskName 'Windows-MCP' -Action $Action -Trigger $Trigger -RunLevel Limited -Force | Out-Null
Start-ScheduledTask -TaskName 'Windows-MCP'

Start-Sleep 20
Get-NetIPAddress -AddressFamily IPv4 | Where-Object PrefixOrigin -eq 'Dhcp' | Select-Object InterfaceAlias, IPAddress
Get-Content "$Dir\server.log" -Tail 20
Stop-Transcript | Out-Null
