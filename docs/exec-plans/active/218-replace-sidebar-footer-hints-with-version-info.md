# GUI: Replace sidebar footer hints with hive/hived version and build info

- **Spec:** [docs/product-specs/218-replace-sidebar-footer-hints-with-version-info.md](../../product-specs/218-replace-sidebar-footer-hints-with-version-info.md)
- **Issue:** —
- **Stage:** REVIEW
- **Status:** active
- **PR:** #240
- **Branch:** feature/218-replace-sidebar-footer-hints-with-version-info

## Summary

Replace the sidebar footer's static keyboard hints with a live version/build readout for `hive` (the GUI) and `hived` (the daemon). The daemon's release version is not currently on the wire, so `wire.Welcome` gains an additive `Release` field. Everything downstream reuses the existing `daemon:stale` Wails event, which already fires on every control connect with both build IDs and a computed severity — so no new event, no new Wails-bound method, and no polling.

## Research

Authored via plan-first mode. Code references identified during plan-mode iteration:

### The footer today

- `cmd/hivegui/frontend/index.html:27-33` — `<footer id="sidebar-hints" class="hints">` holding hardcoded shortcut text. Last flex child of `<aside id="sidebar">` (index.html:21-40), just above the absolutely-positioned `#sidebar-resizer`.
- `cmd/hivegui/frontend/src/style.css:430-439` — `.hints`: single-line, `white-space: nowrap` with ellipsis truncation, `border-top: 1px solid #1f1f1f`, `font-size: 10.5px`, `color: #555`, `padding: 6px 10px`. The sidebar is a flex column (style.css:72-81), so the footer sits at the bottom by document order — no sticky/absolute positioning.
- `cmd/hivegui/frontend/src/main.js:100-104` — sets `footerHintsEl.textContent` once at boot. Nothing re-renders it.
- `cmd/hivegui/frontend/src/lib/shortcuts.js:157-168` — `footerHints()`, a pure function of `isMac`. `main.js` is its only caller.

### Version plumbing today

- `internal/buildinfo/buildinfo.go` — single source of truth. `BuildID()` at `:69` (link-time override, else Go-embedded `vcs.revision`, else `"dev"`); `Version()` at `:88` (link-time override, else `"dev"` — never returns `""`). Test hooks `SetForTest:47` and `SetVersionForTest:98`.
- `build.sh:87-89` — sets `-X ...buildIDOverride` always, `-X ...versionOverride` only when `--version` is passed. Applied to both `hived` and `hivegui`.
- `internal/wire/control.go:64-74` — `Welcome{Version int, BuildID string, Mode, SessionID, Cols, Rows}`. **`Version` is the protocol integer, not a release string** — the new field must not overload it.
- `internal/daemon/daemon.go:234` (control) and `:408` (attach) — the two `Welcome` construction sites.
- `cmd/hivegui/app.go:249` — `ConnectControl` calls `a.emitDaemonVersionStatus(welcome.BuildID)`. `DaemonStaleEvent` at `:257`, `emitDaemonVersionStatus` at `:263` (severity: `match` / `mismatch` / `unknown`).
- `cmd/hivegui/frontend/src/app/banners.js:74-95` — the existing `daemon:stale` consumer. **Early-returns on `severity === 'match'` (`:77-80`)**, which is precisely the case the footer must render.
- `cmd/hivegui/frontend/src/bridge.js:14-24` — the single re-export choke point. `vite.config.js` substitutes test mocks by matching these literal specifiers, so frontend modules must import `EventsOn` from here, never from `wailsjs` directly.

### Constraints

- `test/unit/shortcuts.test.js:59-76` regex-scrapes the footer body out of `index.html` and asserts it equals `footerHints({isMac:true})` verbatim. This test cannot survive the change.
- `test/e2e/ux-polish.spec.js:141` locates `#sidebar-hints` — verify whether it asserts text or only overflow/visibility.

## Approach

Extend the existing `daemon:stale` event into the footer's data source rather than building a parallel path. The event already fires exactly when the needed data arrives (control connect) and already carries a computed severity that answers the collapse-vs-expand question directly.

The obvious alternative — a new Wails-bound `GetVersions()` method the frontend calls at boot — was rejected: it would race the control connection (the daemon's version isn't known until the handshake completes), and it would add a second source of truth for build identity alongside the event that already exists.

Severity stays **build-ID-based**. Build IDs are git revisions, so equal build IDs imply equal releases; a release-based severity would never differ and would only add a second comparison to keep in sync.

### Files to change

- `internal/wire/control.go` — add `Release string` with tag `json:"release,omitempty"` to `Welcome`, beside `BuildID` (~:69). Single-word name so snake/camel case never arises. `omitempty` so an older daemon still parses. `Hello` is unchanged: the GUI knows its own version locally.
- `internal/daemon/daemon.go` — set `Release: buildinfo.Version()` at both `Welcome` sites (`:234` control, `:408` attach) for wire consistency. Only the control one feeds the footer.
- `cmd/hivegui/app.go` — `DaemonStaleEvent` gains `GuiRelease` / `DaemonRelease` string fields with camelCase json tags (`guiRelease`, `daemonRelease`), matching that struct's existing tags. This is a Wails event struct, not a `SessionInfo`/`ProjectInfo` wire payload, so the repo's snake_case wire convention does not apply. `emitDaemonVersionStatus` takes a second `daemonRelease string` param; `ConnectControl` passes `welcome.Release`. Severity logic untouched.
- `cmd/hivegui/frontend/index.html` — replace the footer's hardcoded text with two spans, keeping the `#sidebar-hints` id and `.hints` class (CSS and the e2e locator key off them): `<span id="ver-gui"></span><span id="ver-daemon" hidden></span>`. Empty by default so the pre-JS paint shows nothing rather than stale text.
- `cmd/hivegui/frontend/src/style.css` — `.hints`: drop `white-space: nowrap` and the ellipsis truncation (content is now short and self-sizing); make the two spans block-level so the mismatch case stacks. Keep border-top, font-size, color, padding. Add a muted-warning color for the mismatch state — visible but not competing with the stale banner.
- `cmd/hivegui/frontend/src/main.js:100-104` — drop the `footerHints` call and import; initialize the new footer module instead.
- `cmd/hivegui/frontend/src/lib/shortcuts.js` — delete `footerHints()` once confirmed uncalled.

### New files

- `cmd/hivegui/frontend/src/app/version-footer.js` — owns the footer. Takes its **own** `EventsOn('daemon:stale', …)` subscription (imported from `bridge.js`) rather than extending the handler in `banners.js`, whose `severity === 'match'` early-return would otherwise leave the daemon half permanently blank. Wails supports multiple listeners per event, so the banner's control flow stays untouched. Export the pure render function separately from the subscription so it is unit-testable.

  Render rules:
  - `severity === 'match'` → one line, `hive <release> (<build>)`; `#ver-daemon` stays hidden.
  - otherwise → two lines, `hive <guiRelease> (<guiBuild>)` and `hived <daemonRelease> (<daemonBuild>)`; `#ver-daemon` unhidden.
  - empty release (older daemon predating the wire change) → render that half build-ID-only, e.g. `hived (b7e220)`, never an empty `()`. Same guard for an empty build ID.
  - daemon unreachable → footer stays empty; the event simply hasn't fired. Accepted, per spec non-goals.

### Tests

- `internal/wire` (or `internal/daemon`) — round-trip test that `Welcome.Release` survives marshal/unmarshal and arrives populated at the client. Use `buildinfo.SetVersionForTest` for determinism.
- `cmd/hivegui` — table test on `emitDaemonVersionStatus`: event carries both releases; `severity` still derives from build IDs only.
- `test/unit/version-footer.test.js` (new) — drive the pure render function with synthetic payloads: match (one line, daemon span hidden), mismatch (two lines, daemon span shown), empty `daemonRelease` (build-ID-only fallback), empty build ID.
- `test/unit/shortcuts.test.js:59-76` — delete alongside `footerHints()`.
- `test/e2e/ux-polish.spec.js:141` — update only if it asserts hint text; unchanged if it only checks overflow.

## Decision log

- **2026-07-18** — Named the new `Welcome` field `Release`, not `Version`. Why: `Welcome.Version` already holds `wire.PROTOCOL_VERSION` (an int); overloading it would break protocol negotiation.
- **2026-07-18** — Footer subscribes to `daemon:stale` in its own module instead of extending `banners.js`. Why: that handler early-returns on `severity === 'match'`, the footer's normal case.
- **2026-07-18** — Kept severity build-ID-based rather than adding release comparison. Why: build IDs are git revisions, so equal builds imply equal releases; a second comparison would be redundant state to keep in sync.
- **2026-07-18** — Collapse to one line when builds match, expand to two on mismatch. Why: the sidebar is narrow and matching is the overwhelmingly common case; the layout shift on mismatch is a useful signal, not a defect.

## Progress

- **2026-07-18** — Plan-first scaffold; Stage = IMPLEMENT. Spec and exec plan written; no GitHub issue (local-only per operator choice).
- **2026-07-18** — Implemented. Go suite, 191 JS unit tests, 59 e2e all pass. Visually confirmed in Chromium (match / mismatch / narrow sidebar).
- **2026-07-18** — Follow-up commit: `[hidden]` was inert against the author-origin `display: block`; added an explicit rule and an e2e assertion that fails without it.
- **2026-07-18** — Pushed; PR #240 opened. Stage = REVIEW.

## PR convergence ledger

<Append-only. One line per /hs-review-loop iteration.>

## Open questions

None. Format, scope, and wire-field naming were resolved during plan-mode iteration.
