# GUI: Replace sidebar footer hints with hive/hived version and build info

- **Issue:** —
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/218-replace-sidebar-footer-hints-with-version-info.md](../exec-plans/completed/218-replace-sidebar-footer-hints-with-version-info.md)

## Problem

The left sidebar's bottom footer (`#sidebar-hints`) shows a static line of keyboard shortcuts, rendered once at boot and never updated. Those same shortcuts are already discoverable through the command palette (⇧⌘K) and the help overlay (⌘/), so the footer spends permanent screen real estate on redundant information.

At the same time there is no always-visible way to tell which build of Hive is running. The `daemon:stale` banner surfaces build identity only when the GUI and daemon *disagree*; in the normal matching case the user sees nothing. That makes two common situations needlessly hard: filing an accurate bug report, and confirming that a rebuild actually took effect.

A further gap blocks the obvious fix: the daemon's human-readable release version (`internal/buildinfo.Version()`) is never sent over the wire. Only its git build ID travels, via `wire.Welcome.BuildID`.

## Desired behavior

The sidebar footer displays the running version and build of both binaries instead of keyboard hints. In the normal case — GUI and daemon built from the same commit — it shows a single compact line. When the two disagree, it expands to two lines so the discrepancy is visible at a glance, reinforcing the existing stale-daemon banner rather than replacing it.

## Success criteria

- The sidebar footer shows `hive <release> (<build>)` on one line when the GUI and daemon builds match.
- The footer expands to two lines — one per binary — when the builds differ, and the existing stale-daemon banner still appears.
- The daemon's release version reaches the GUI over the wire (new `Release` field on `wire.Welcome`), populated from `buildinfo.Version()`.
- A daemon predating the new field degrades gracefully: that half renders build-ID-only rather than showing an empty `()`.
- The footer neither overflows nor clips the sidebar when the sidebar is dragged to its narrow limit.
- The keyboard hints are removed, along with the now-uncalled `footerHints()` helper and the test that pinned it to the markup.

## Non-goals

- Adding a `version` CLI command or a version RPC to the daemon. Version travels on the existing handshake only.
- Changing `wire.PROTOCOL_VERSION` or the protocol-version negotiation. The new field is additive and `omitempty`.
- Showing a version placeholder while the daemon is unreachable. The footer stays empty until the control connection is established; the `daemon:stale` banner already owns the daemon-down experience.
- Relocating the keyboard hints elsewhere in the UI.

## Notes

The relevant plumbing already exists end to end. `emitDaemonVersionStatus` (in `cmd/hivegui/app.go`) emits a `daemon:stale` Wails event on every control connect carrying `guiBuild`, `daemonBuild`, and a computed `severity`; this spec extends that payload rather than adding a new event or a Wails-bound method.

Naming landmine: `wire.Welcome.Version` is already taken — it is the *protocol* version integer (`internal/wire/control.go:65`). The new field is therefore named `Release`.

Numbering note: this spec is local-only and has no GitHub issue. The number 218 is the next free spec/plan prefix and is unrelated to GitHub PR #218, which belongs to spec 217.
