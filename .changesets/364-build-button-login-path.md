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
  The build script now runs with the PATH a login shell would have.
