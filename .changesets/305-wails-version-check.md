---
issue: null
pr: 305
type: fixed
bump: patch
---
- `build.sh` now fails with install instructions when the `wails` CLI on
  `PATH` does not match the version pinned in `scripts/ci-bootstrap.sh`,
  instead of silently building against a stale toolchain after a Wails
  bump.
- Added `scripts/check-changeset.sh`, a local mirror of the changesets CI
  gate that can be installed as a `pre-push` hook.
