# Feature: Display changelog since last version on open

- **GitHub Issue:** #69
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** 3
- **Branch:** —

## Description

When a user opens Hive after an update, they have no easy way to know what changed. The app should detect when the version has changed since the last launch and display a changelog or summary of what's new. This helps users discover new features and understand recent fixes without having to check release notes manually.

## Research

### Relevant Code

- `cmd/version.go:9` — `const Version = "0.5.1"` — hardcoded version string, bumped by `scripts/release.sh` during releases.
- `internal/config/config.go:4-26` — `Config` struct. No `LastSeenVersion` field exists yet. Config is persisted to `~/.config/hive/config.json` and has a schema migration system (`SchemaVersion`, currently 2).
- `internal/config/migrate.go:1-63` — Config migration system. `currentSchemaVersion = 2`. Precedent for one-shot version-gated behavior: the v1→v2 migration resets `HideAttachHint` to re-show the detach key splash. `MigrateAndPersist()` writes config only when schema version changes.
- `internal/config/load.go:62-79` — `Load()` reads config, `Save()` writes it atomically.
- `cmd/start.go:33-139` — App startup flow: loads config → runs `MigrateAndPersist()` → loads state → creates TUI model → runs Bubble Tea loop. The version comparison and "What's New" trigger should happen here or in Model init.
- `internal/tui/viewstack.go:6-27` — ViewID enum. Add `ViewWhatsNew` here. Stack-based: `PushView`/`PopView`/`TopView`.
- `internal/tui/viewstack.go:89-150` — `syncLegacyFlags()` — must add a case for `ViewWhatsNew`.
- `internal/tui/app.go:342-386` — `View()` method dispatches rendering based on `TopView()`. Add `ViewWhatsNew` case to render the "What's New" overlay.
- `internal/tui/handle_keys.go:16-71` — `handleKey()` routes key events by `TopView()`. Add `ViewWhatsNew` case to handle dismissal (esc/enter/q).
- `internal/tui/views.go:16-31` — `overlayView()` centers any dialog over a dark background. Reuse this for the What's New dialog.
- `internal/tui/views.go:82-170` — `helpView()` and `tmuxHelpView()` — existing full-overlay patterns to follow. Simple bordered content with centered layout.
- `internal/tui/views.go:172-190` — `attachHintView()` — precedent for `d` = "don't show again" pattern.
- `internal/tui/handle_keys.go:767-795` — `handleAttachHint()` — shows exactly how `d` saves `HideAttachHint` to config and dismisses.
- `CHANGELOG.md` — Keep a Changelog format. Version headers: `## [0.6.0] — 2026-04-11`. Categories: `Added`, `Changed`, `Fixed`. Entries are markdown bullet points with bold titles and issue refs.
- `internal/tui/components/settings.go:517-687` — `buildSettingEntries()` — where to add the "Hide What's New" toggle.

### Constraints / Dependencies

- **Version string is compile-time only:** `cmd.Version` is a Go constant. It's not importable from `internal/` packages without creating a circular dependency — pass it through the Model constructor or config.
- **Config is the right home for `LastSeenVersion`:** It's already persisted, has migration support, and is loaded before the TUI starts. No need for a separate file.
- **No schema version bump needed:** Adding new JSON fields with `omitempty` is backward-compatible. Older versions simply ignore them. The migration path is: on first launch after upgrade, `LastSeenVersion` is empty/"" → triggers the What's New dialog → saves the current version. No `currentSchemaVersion` bump required.
- **Changelog parsing complexity:** The CHANGELOG.md format is consistent (Keep a Changelog). Parsing sections between `## [current]` and `## [lastSeen]` is straightforward with line-by-line scanning. Edge cases: first-ever launch (no lastSeen → show latest release only), `[Unreleased]` section (skip it — only show released versions).
- **Changelog file must be embedded:** The binary is distributed as a standalone executable. Use `//go:embed CHANGELOG.md` to bake the changelog into the binary at compile time.
- **Scrolling for long changelogs:** If multiple versions were skipped, the content could be long. Use a `viewport.Model` from `charmbracelet/bubbles` (already a dependency, used by the preview component) for scrollable content.
- **Precedent:** The `HideAttachHint` pattern shows how the codebase handles one-shot informational dialogs — same approach, but version-gated instead of schema-version-gated.
- **"Don't show again" option:** The dialog should offer a "don't show again" key (like `d` in the attach hint pattern). This sets a `HideWhatsNew bool` field in Config. The settings view (`internal/tui/components/settings.go`) should include a toggle to reset this back to false, so users can re-enable it later. The dialog still updates `LastSeenVersion` on dismiss regardless — "don't show again" only suppresses the auto-popup, not the version tracking.

## Plan

On startup, compare the running version against `LastSeenVersion` in config. If they differ and `HideWhatsNew` is false, parse the embedded CHANGELOG.md to extract entries between the current and last-seen versions, and display them in a scrollable "What's New" overlay. The user can dismiss with Enter/Esc, or press `d` to permanently suppress it. A toggle in Settings lets them re-enable it.

### Architecture

**Version flow:**
```
cmd/start.go: cfg.LastSeenVersion vs cmd.Version
  → if different AND !cfg.HideWhatsNew:
      pass changelog content string into New()
  → always: cfg.LastSeenVersion = cmd.Version; config.Save(cfg)
```

**Changelog parsing:** A new `internal/changelog/` package provides `ParseSince(changelog, lastVersion string) string`. It scans for `## [X.Y.Z]` headers, collects lines between the current version's header and the lastSeen version's header, and returns rendered text. If lastSeen is empty, returns only the latest release section. Skips `[Unreleased]`.

**What's New dialog:** A new `ViewWhatsNew` in the view stack. Content is rendered in a `viewport.Model` for scrolling. Pushed during `New()` if changelog content is non-empty.

### Files to Change

1. **`internal/changelog/changelog.go`** (new file) — Changelog parser.
   - `func ParseSince(changelog, lastVersion string) string` — extracts changelog entries between the current version and `lastVersion`. Returns plain text (markdown-lite formatting preserved).
   - Regex-based line scanning for `## [X.Y.Z]` headers.
   - Skips `[Unreleased]` section.
   - If `lastVersion == ""`, returns only the most recent release section.
   - If `lastVersion` is not found in the changelog (downgrade or very old), returns all entries.

2. **`internal/changelog/changelog_test.go`** (new file) — Unit tests for the parser.

3. **`internal/config/config.go`** — Add two fields to `Config`:
   - `LastSeenVersion string  \`json:"last_seen_version,omitempty"\``
   - `HideWhatsNew    bool    \`json:"hide_whats_new,omitempty"\``

4. **`cmd/changelog.go`** (new file) — Embed the changelog.
   - `//go:embed CHANGELOG.md` into a package-level `var Changelog string`.
   - This lives in `cmd/` (alongside `version.go`) because `go:embed` requires the embedded file to be in the same package's directory or below. `CHANGELOG.md` is at the repo root, and `cmd/` is one level down — so we use `//go:embed ../CHANGELOG.md` which is not allowed. **Alternative:** Place the embed directive in `main.go` (root package, same directory as CHANGELOG.md) and pass it down. Or create a top-level `changelog_embed.go` in the root package.

   **Decision:** Add `var Changelog string` with `//go:embed CHANGELOG.md` in `main.go` (root package). Pass it to `cmd.Execute()` as a parameter, or set it on a package-level var in `cmd` before calling `Execute()`. Simplest: add `cmd.EmbeddedChangelog` var, set in `main.go`.

5. **`main.go`** — Add `//go:embed CHANGELOG.md` and set `cmd.EmbeddedChangelog` before `cmd.Execute()`.

6. **`cmd/start.go`** — In `runStart()`, after config load:
   - Call `changelog.ParseSince(EmbeddedChangelog, cfg.LastSeenVersion)` to get what's new content.
   - If content is non-empty and `!cfg.HideWhatsNew`, pass the content to `tui.New()` via a new `WhatsNewContent` field on the options/params.
   - Always update `cfg.LastSeenVersion = Version` and save config (so it's set even if `HideWhatsNew` is true).

7. **`internal/tui/viewstack.go`** — Add `ViewWhatsNew ViewID = "whats-new"` constant. Add case in `syncLegacyFlags()` (no legacy flag needed — just a no-op case to avoid the default log warning).

8. **`internal/tui/app.go`** — Changes to Model:
   - Add `whatsNewContent string` field and `whatsNewViewport viewport.Model` field.
   - In `New()`: accept `whatsNewContent` param. If non-empty, initialize viewport with content and `PushView(ViewWhatsNew)` (after recovery/orphan overlays so it appears on top).
   - In `View()`: add `case ViewWhatsNew: return m.whatsNewView()`.
   - In `Update()`: route `tea.WindowSizeMsg` to resize the viewport.

9. **`internal/tui/views.go`** — Add `whatsNewView()` method:
   - Title: "What's New in Hive vX.Y.Z"
   - Body: `m.whatsNewViewport.View()` (scrollable)
   - Footer hints: `enter/esc: close  d: don't show again  ↑↓: scroll`
   - Wrapped in the standard bordered style, rendered via `overlayView()`.
   - Width: `min(termWidth-10, 70)`, Height: `min(termHeight-8, 30)`.

10. **`internal/tui/handle_keys.go`** — Add `case ViewWhatsNew:` in `handleKey()`:
    - `enter`, `esc`, `q`, ` `: `PopView()` — dismiss.
    - `d`: `PopView()`, set `m.cfg.HideWhatsNew = true`, `config.Save(m.cfg)` — dismiss and suppress future popups.
    - `up`, `k`: scroll viewport up.
    - `down`, `j`: scroll viewport down.

11. **`internal/tui/components/settings.go`** — Add "Hide What's New" toggle in `buildSettingEntries()` under the General section (after "Hide Attach Hint"):
    ```go
    {field: &settingField{
        label:       "Hide What's New",
        description: "When enabled, skip the changelog dialog shown after updates. Disable to see it again on the next version change.",
        kind:        fieldBool,
        get:         func(c config.Config) string { return strconv.FormatBool(c.HideWhatsNew) },
        set:         func(c *config.Config, v string) error { ... },
    }}
    ```

### Test Strategy

**Unit tests — `internal/changelog/changelog_test.go`:**

1. `TestParseSince_ReturnsEntriesBetweenVersions` — Given a multi-version changelog and lastSeen="0.5.0", verify it returns entries for 0.6.0 and 0.5.1 but not 0.5.0 or earlier.
2. `TestParseSince_EmptyLastVersion_ReturnsLatestOnly` — Given lastSeen="", verify only the most recent release section is returned.
3. `TestParseSince_SameVersion_ReturnsEmpty` — Given lastSeen matching the latest version, verify empty string returned.
4. `TestParseSince_UnknownLastVersion_ReturnsAll` — Given a lastSeen not in the changelog, verify all released entries are returned.
5. `TestParseSince_SkipsUnreleased` — Verify the `[Unreleased]` section is never included in output.
6. `TestParseSince_EmptyChangelog` — Given an empty string, verify empty string returned.

**Flow tests — `internal/tui/flow_whatsnew_test.go`:**

7. `TestFlow_WhatsNew_ShownOnVersionChange` — Create a Model with `whatsNewContent` set. Verify `TopView() == ViewWhatsNew` and `View()` contains "What's New".
8. `TestFlow_WhatsNew_DismissWithEnter` — Send Enter, verify `TopView()` is no longer `ViewWhatsNew`.
9. `TestFlow_WhatsNew_DismissWithEsc` — Send Esc, verify dismissed.
10. `TestFlow_WhatsNew_DontShowAgain` — Send `d`, verify `PopView()` called and `cfg.HideWhatsNew == true`.
11. `TestFlow_WhatsNew_NotShownWhenEmpty` — Create Model with empty `whatsNewContent`. Verify `TopView() != ViewWhatsNew`.
12. `TestFlow_WhatsNew_ScrollUpDown` — Send `j`/`k`, verify viewport offset changes.

**Settings test — `internal/tui/components/settings_test.go`:**

13. `TestSettings_HideWhatsNewToggle` — Open settings, navigate to "Hide What's New", toggle it, verify `cfg.HideWhatsNew` flips.

### Risks

- **`go:embed` path restriction:** `go:embed` cannot use `..` paths. The embed must live in `main.go` (root package, same dir as CHANGELOG.md). If the embed is placed in `cmd/`, it won't compile. Mitigation: embed in `main.go`, pass to `cmd` package via a public var.
- **Changelog format changes:** If CHANGELOG.md format drifts from the expected `## [X.Y.Z]` pattern, the parser will fail to extract sections. Mitigation: unit tests with the actual CHANGELOG.md format, and a graceful fallback (show nothing rather than crash).
- **Large changelog with many skipped versions:** If a user upgrades from a very old version, the dialog could have a lot of content. Mitigation: viewport scrolling handles this; the dialog has a max height.
- **First-time users see the dialog unnecessarily:** On first launch, `LastSeenVersion` is empty, so the dialog triggers showing the latest release. This is actually desirable — new users learn what the current version offers. If not desired, we can check if this is a first-ever launch (no state.json exists) and skip.

## Implementation Notes

- Embedded `CHANGELOG.md` via `//go:embed` in `main.go` (root package) — passed to `cmd.EmbeddedChangelog` var.
- New `internal/changelog/` package with `ParseSince()` — regex-based parser for Keep a Changelog format.
- `LastSeenVersion` is updated on every startup regardless of `HideWhatsNew`, so re-enabling the toggle only shows new entries from the *next* version change.
- Viewport size adapts to terminal dimensions via `handleWindowSize()`.
- No deviations from plan.

- **PR:** —
