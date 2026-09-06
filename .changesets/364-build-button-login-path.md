---
issue: null
pr: 364
type: fixed
bump: patch
---
- The in-app build button no longer fails with "build.sh failed" on
  Apple Silicon Macs. An app launched from Finder inherits only the
  system PATH, which does not include Homebrew's /opt/homebrew prefix or
  a Node version manager's directory, so the build died looking for npm.
  The build script now runs with the PATH the user's own login shell
  reports, probed the way VS Code does it — interactive login shell,
  marker-delimited output, per-shell argument forms. When the toolchain
  still cannot be found, the button now names the missing tool instead
  of reporting a bare "build.sh failed".
