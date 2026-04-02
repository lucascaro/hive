# build.ps1 — build and install hive on Windows
# Run from the repository root: .\build.ps1
#
# Requires Go to be installed and on PATH.
# To install hive system-wide, run this script from an elevated (Administrator) terminal.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

Write-Host "Building hive..."
go build -o hive.exe .

$installDir = "$env:ProgramFiles\hive"
Write-Host "Installing hive to $installDir ..."

if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir | Out-Null
}
Copy-Item -Path hive.exe -Destination "$installDir\hive.exe" -Force

# Add install dir to the user PATH if not already present.
$userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable('PATH', "$userPath;$installDir", 'User')
    Write-Host "Added $installDir to your user PATH. Restart your terminal for the change to take effect."
}

Write-Host "Done. Run 'hive' to start."
