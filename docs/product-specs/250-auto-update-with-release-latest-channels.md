# Auto-update with release/latest channels

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/250-auto-update-with-release-latest-channels.md](../exec-plans/active/250-auto-update-with-release-latest-channels.md)

## Problem

Hive detects updates but cannot apply them. `cmd/hivegui/update.go` polls the GitHub
releases API every 6 hours and `banners.ts` shows a banner, but its only action opens the
release page in a browser — the user then downloads a zip by hand, drags `hivegui.app`
over the installed bundle, and relaunches. Untagged local builds (`buildinfo.Version() ==
"dev"`) opt out of the check entirely, so anyone running from a checkout gets no update
signal at all and has to remember to `git pull && ./build.sh` themselves.

## Desired behavior

Settings gains an **update channel** choice:

- **release** — check for a newer tagged GitHub release than the running version.
- **latest** — check whether the source checkout is behind its upstream branch.

Checking stays automatic. Applying is one click: an **Update** button stages the new build
in the background (download + verify for `release`; `git pull` + `./build.sh` for
`latest`), showing progress while it works, then becomes **Restart**. Clicking Restart
swaps the new bundle in and relaunches Hive on the new version. The button appears both in
the Settings modal and on the existing update banner, and the two never disagree.

## Success criteria

- Settings persists a channel (`release` / `latest`) across restarts, defaulting to `release`.
- On the `latest` channel the source checkout is found automatically when the binary lives
  inside one, and can be pointed at an explicit directory otherwise.
- On the `release` channel the downloaded artifact is checked against a SHA-256 manifest
  published with the release; a mismatch aborts and leaves the installed app untouched.
- On the `latest` channel a dirty working tree aborts before any `git pull` runs.
- Clicking Update never blocks the UI; progress is visible and the button becomes Restart
  when staging succeeds.
- Clicking Restart replaces the installed `hivegui.app`, restarts `hived`, and the sidebar
  version footer reports the new version/build after relaunch.
- A failed swap leaves a working window and the original bundle in place.

## Non-goals

- Self-update on Windows and Linux. Both keep today's open-the-release-page banner —
  replacing a running `.exe` needs a helper process, and Linux ships no release artifact.
- Code signing or notarization of the macOS bundle. Worth doing, but a separate project;
  this feature must be revisited if it lands.
- Silent background downloading or a fully unattended restart. Hive hosts live PTY
  sessions; restarts stay user-initiated.
- Delta/patch updates. Full bundle replacement only.

## Notes

Reuses `enclosingAppBundle` (`cmd/hivegui/window_unix.go`), `RestartDaemon`
(`cmd/hivegui/app_control.go`), and the `updateURLPrefix` allowlist already in
`cmd/hivegui/update.go`. Persistence follows the `window.json` pattern in
`cmd/hivegui/window_state.go`, not the registry.
