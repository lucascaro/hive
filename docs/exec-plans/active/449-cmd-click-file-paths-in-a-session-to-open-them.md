# Cmd-click file paths in a session to open them

- **Spec:** [docs/product-specs/449-cmd-click-file-paths-in-a-session-to-open-them.md](../../product-specs/449-cmd-click-file-paths-in-a-session-to-open-them.md)
- **Issue:** #449
- **Status:** active
- **PR:** #450
- **Branch:** feature/449-cmd-click-file-paths

## Summary

Make file paths in session output clickable. Cmd-click opens a file with the OS default app, but executables are never launched. Cmd+shift-click opens it in an editor configured in Settings. OSC 8 `file://` links follow the same rules. Today `OpenURL` refuses every `file://` link, so no file can be opened from a click.

## Research

### Relevant code

- `cmd/hivegui/app_calls.go:548-583` — `OpenURL` + `allowedURL`. Only http/https/mailto are allowed; `file://` is logged and dropped. The threat comment says terminal content is attacker-influenced. `open_url_test.go` — `TestAllowedURL` checks ok and bad lists, including `file:///Applications/Utilities/Terminal.app`.
- `cmd/hivegui/os_terminal.go` — `OpenTerminalAt(path)`: one file with a `runtime.GOOS` switch. It checks the path with `os.Stat(...).IsDir()`, then runs `open -a Terminal` on darwin, `wt.exe` / `cmd start` on windows, and a `$TERMINAL` → `x-terminal-emulator` → fixed list on linux. The frontend passes the base dir (`main.tsx:210`, `OpenTerminalAt(activeCwd())`). No test.
- Nothing in the codebase reveals in Finder or opens a file: no `open -R`, `xdg-open`, `explorer /select` or ShellExecute.
- `cmd/hivegui/frontend/src/app/session-term.ts`
  - `:339-343` — the OSC 8 `linkHandler.activate` calls `OpenURL`.
  - `:401-410` — `WebLinksAddon` (0.12.0) calls `OpenURL`.
  - `:412-452` — a capture-phase mousedown/mouseup pair reads the private `_core.linkifier.currentLink`. It swallows the click so the PTY mouse protocol never sees it, and activates the link on mouseup if the pointer moved less than 5px (`isClick`). It checks **no modifiers**, so any click on any hovered link activates it.
  - `:455+` — click-to-position bails when a modifier is held.
  - There is no `registerLinkProvider` anywhere (xterm 6.0.0).
- The session's base dir is `resolveSessionCwd(sess)` (`src/app/selectors.ts:116-125`): `worktree_path ?? worktreePath`, else the project `cwd`. `wire.SessionInfo` has no cwd field (`internal/wire/control.go:154`). The Go `App` keeps no session list, so the frontend must pass the base dir to Go.
- Settings
  - `src/components/modals/Settings.tsx` has four tabs: agents, appearance, menubar, updates.
  - A Go-persisted setting follows `GetAgentSettings`/`SaveAgentSettings` (`app_calls.go:110-138`, `internal/agent/settings.go`) and `update_prefs.go:27-120`: a JSON file in `registry.StateDir()` written atomically. A missing file means defaults. A corrupt file is an error and is never overwritten; `Settings.tsx:331-348` tracks a load-failed flag for this.
  - A localStorage setting follows PR #448: `hive.agentPrefs` (`src/lib/agent-order.ts:89`).
- Errors: there is no toast component. `reportFailure(what)` (`src/app/dom.ts:60`) calls `flashStatus(msg, true)`, the status-bar flash.
- PATH on macOS: a GUI app lacks the user's PATH. `shell_env_darwin.go` provides `lookPathIn(pathOf(envWithLoginPATH(os.Environ())), name)`, which `gitCommandFn` uses at `:74-84`. These helpers are darwin-only.
- Bridge: `src/bridge.ts:36-71` re-exports each method by name from the generated `wailsjs`, which is gitignored. A new method also needs a stub in `test/e2e/wails-mock.ts` (the `maybeFail` pattern at `:973`) and in `test/e2e-real/wails-bridge.ts` (~`:366`), or the app fails to boot under those harnesses.
- Per-OS Go code uses `//go:build` tags plus filename suffixes (`restart_unix.go`, `restart_windows.go`, `update_apply_other.go`). Tests swap out external commands through package-level variables (`runArgv`, `runGitFn`, `loginPATHFn`).

### Constraints / dependencies

- **Every exec must use `proc.Command`, never `os/exec`.** `TestNoDirectExecOnWindows` enforces this. Build argv without a shell. On Windows avoid `cmd /C start`, because cmd re-parses an untrusted path.
- **Security intent must survive.** The path comes from terminal output, which can be attacker-controlled. The executable guard must be enforced in Go, not the frontend. The editor command should be read from Go-side config, never passed per call from JS.
- No daemon or wire change is needed: the work is GUI-only, so no `DaemonContract` bump.
- `scripts/test.sh`, the per-GOOS `staticcheck`/`vet`, and `scripts/ui-lint.sh` must all pass.

### Prior lessons

- Read how xterm itself handles link detection and activation before designing (a lesson from keybindings that applies here).
- GUI changes also update `docs/design-docs/ui/*` where relevant, plus `site/features.json`. Grep `docs/`.
- A settings modal backed by a file must keep "failed to load" distinct from "empty", so Save never overwrites a bad file. Check every path that can reset or discard the draft.

### Conventions card

```
build:  ./build.sh          # macOS .app (GUI + daemon)
test:   scripts/test.sh     # all layers: go · unit · dom · e2e
static: for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done
toolchain: GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod)
lint:   cd cmd/hivegui/frontend && npx biome ci . ; npm run typecheck (needs ./scripts/ci-bootstrap.sh in a fresh worktree) ; scripts/ui-lint.sh
```

- TDD: every behavior change ships with its test. Go tests live next to the source; frontend tests go in `test/{unit,dom,e2e}`.
- Shell out only through `proc.Command`/`proc.CommandContext`.
- A user-visible feature needs a `.changesets/<slug>.md` (`bump: minor`) and a `site/features.json` entry with `since: "Unreleased"`. Never edit `CHANGELOG.md`.
- A keybinding or mouse chord goes through `src/lib/platform.ts` helpers (⌘ on mac, Ctrl elsewhere) and the help overlay and README keybinds table.
- UI uses design tokens and component primitives (`docs/design-docs/ui/`), enforced by `scripts/ui-lint.sh`.

## Approach

Detection runs in the frontend and every decision runs in Go.

- A new xterm **link provider** (`registerLinkProvider`) scans the hovered line, including wrapped continuation lines, for path-shaped tokens. It sends them to Go in one batched call, `ResolveFilePaths(baseDir, candidates)`. Go expands `~`, joins relative paths to `baseDir`, cleans the result and `os.Stat`s it. The provider returns links only for paths that exist, so only real files get the hover underline (criteria 1 and 2).
- On activation, the frontend calls `OpenFile(baseDir, path, line, col, editor bool)`. **Go re-resolves and re-stats the path.** The frontend's resolution is never trusted, and the bridge takes no command.
  - **Editor mode** (⌘⇧-click): run the configured editor. If none is configured, fall through to OS mode (criteria 3–5).
  - **OS mode** (⌘-click):
    1. `filepath.EvalSymlinks`.
    2. A directory is revealed in the file manager (criterion 8).
    3. A file where `isLaunchable(path, info)` is true is revealed, not opened (criterion 6).
    4. Anything else opens with the OS default app.
- **Editor config** lives in `<stateDir>/editor.json`, following `update_prefs.go` exactly: a missing file means defaults, a corrupt file is an error and never overwritten, BOM stripped, atomic write.
  - Shape: `{kind: "" | "vscode" | "cursor" | "zed" | "sublime" | "command" | "app", command: string, app: string}`.
  - Presets build argv in Go: `code -g f:l:c`, `cursor -g f:l:c`, `zed f:l:c`, `subl f:l:c`.
  - `command` is an argv template. A tiny quote-aware splitter tokenizes it; `{file}`, `{line}` and `{col}` are substituted after splitting, so a path with spaces stays one argument and no shell is ever involved. A missing line or column substitutes `1`.
  - `app` (macOS only) runs `open -a <App> <file>`.
  - Binaries are looked up on the login PATH on macOS (`lookPathIn(pathOf(envWithLoginPATH(...)))`) and with `exec.LookPath` elsewhere.
- **OSC 8 `file://`**: xterm's `OscLinkProvider` (`src/browser/OscLinkProvider.ts:72`) drops every non-http(s) OSC 8 link unless `linkHandler.allowNonHttpProtocols: true`. Set that flag. Then `linkHandler.activate` branches. A `file:` URI (host empty or `localhost`) is decoded to a path and sent to `OpenFile`. Everything else still goes to `OpenURL`, which is unchanged, so its allowlist still refuses `file://` (criteria 7 and 9).
- **Modifier rule**: file links, both provider links and OSC 8 `file:`, activate only with `cmdOrCtrl(e)`. Shift selects editor mode.
  - The existing capture-phase mousedown handler (`session-term.ts:412-452`) swallows *every* click on a hovered link. It gains one guard: if the link is a file link and the modifier isn't held, it does not intercept. Plain clicks then fall through to selection and click-to-position.
  - URL links keep their current behavior (criterion 9).
- **Errors**: `OpenFile` returns an `error`. The frontend `.catch(reportFailure('open file'))` shows it in the status bar (criterion 10).
- **Why this beats the obvious alternative**, which is to allow `file://` in `OpenURL` and let `BrowserOpenURL` handle it:
  - That hands an attacker-labelled path to the OS with no executable guard.
  - It can't do line jumps.
  - It doesn't work for relative paths.

## Per-OS behavior

The guard is a **pure, portable function**, `isLaunchable(goos, path string, info fileMeta) bool`, in `file_open_guard.go`. `fileMeta` is `{isDir, execBit, isAlias}`. Because it takes `goos` as an argument, every OS table is tested on every host, so criterion 6's "a unit test covers the guard on each platform" runs in CI on any runner. Only the side-effecting `openDefault` and `reveal` live in the build-tagged `file_open_{darwin,linux,windows,other}.go`, plus a tiny per-OS `statMeta` that fills `fileMeta`.

**Normalization before the check (all OSes, enforced in the pure function):**
- If `goos == "windows"`, reject (treat as launchable, so the file is revealed) any path whose final element ends in `.` or a space, or that contains `:` anywhere after the drive prefix. This blocks the Win32 trailing-dot and trailing-space stripping trick and NTFS alternate data streams such as `evil.exe.`, `evil.exe ` and `evil.exe::$DATA`.
- On Windows, `statMeta` first expands 8.3 short names (`PAYLOA~1.SET`) with `windows.GetLongPathName`. The check then runs on the name the user supplied, before `EvalSymlinks`, and again on the resolved target; either one being launchable means reveal.
- Extensions are matched on the final extension only, case-insensitively. The trailing-dot, trailing-space and stream rules above already close the Windows name tricks, so checking every dotted suffix is unnecessary and would needlessly reveal files like `x.py.bak`.

**Denylist (common to every OS, because an interpreter can be the default handler anywhere):** `.py .pyw .pyz .pl .rb .sh .bash .zsh .fish .csh .ksh .php .jar .jnlp .ps1 .psm1 .applescript .scpt .scptd .command .tool`.

| | open | reveal | extra launchable |
|---|---|---|---|
| darwin | `open <abs>` | `open -R <abs>` | bundle directories: `.app .bundle .framework .plugin .prefPane .saver .workflow .action .kext .qlgenerator .appex`; files: `.terminal .webloc .inetloc .fileloc .pkg .mpkg .dmg .shortcut`; any exec bit; **Finder alias files** (the `com.apple.FinderInfo` xattr has the kIsAlias flag, 0x8000 at bytes 8–9, read with `unix.Getxattr`). A symlink is resolved with `EvalSymlinks` first. |
| linux | `xdg-open <abs>` | `dbus-send` to `org.freedesktop.FileManager1.ShowItems`; on failure, `xdg-open <parent>` | any exec bit; `.desktop .AppImage .run .deb .rpm .flatpakref .snap` |
| windows | `windows.ShellExecute(0, "open", abs, …)` via `golang.org/x/sys/windows`. It is already an indirect dependency at v0.47.0 and becomes direct. No `cmd /C start`. | `explorer.exe /select,<abs>` | `PATHEXT` plus `.exe .com .bat .cmd .vbs .vbe .js .jse .wsf .wsh .hta .msi .msp .msc .scr .cpl .lnk .url .pif .reg .appref-ms .application .scf .inf .gadget .settingcontent-ms .library-ms .diagcab` |

**Editor launch on Windows (the "BatBadBut" class of bug):**
- When the resolved editor binary ends in `.cmd` or `.bat`, which is how the `code`, `cursor` and `subl` shims usually install, Go runs it through cmd.exe without escaping.
- `editorArgv` therefore refuses, with an error that names the character, any argument containing `& | < > ^ % ! "` or a newline when the binary is a `.cmd` or `.bat`.
- The VS Code and Cursor presets first try the real `.exe`: the shim's `..\Code.exe` / `..\Cursor.exe`, run with the `--goto` form they support. They fall back to the shim, under the guard above.

**Renderer-writes-config caveat:** `SaveEditorSettings` lets JS write a command template. This is the same trust level as custom agents (`app_calls.go:72,105`). To stop a compromised renderer from turning the editor into an arbitrary-shell runner, Save rejects a template whose argv[0] basename is a shell or interpreter (`sh bash zsh fish dash cmd cmd.exe powershell pwsh python* perl ruby node osascript env open xdg-open explorer rundll32 mshta wscript cscript`). A code comment states that this list is defense-in-depth, not a boundary: a renderer that can write settings could equally write a custom agent `Cmd`.

All paths are cleaned absolute paths, so no argument begins with `-`. Every exec uses `proc.Command`.

## Files to change

1. `cmd/hivegui/frontend/src/app/session-term.ts`
   - Register the file link provider after `WebLinksAddon`.
   - Branch the OSC 8 `linkHandler` on the `file:` scheme.
   - Add the modifier guard to the capture mousedown handler.
   - The base dir is `resolveSessionCwd(this.info)`, read at activation time.
2. `cmd/hivegui/frontend/src/bridge.ts` — re-export `ResolveFilePaths`, `OpenFile`, `GetEditorSettings`, `SaveEditorSettings`.
3. `cmd/hivegui/frontend/src/components/modals/Settings.tsx`
   - Add an "Editor" section to the Appearance tab (no new tab) that hosts `EditorSettings`.
   - Load it with a load-failed flag, and save it in the existing `saveSettings` chain.
4. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — add help overlay rows "⌘-click path: Open file (executables are revealed)" and "⇧⌘-click path: Open in editor", next to the Double-click row.
5. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — mock the four new methods with `maybeFail`. `ResolveFilePaths` resolves against a mock file set on `__hive`; `OpenFile` records its calls for assertions.
6. `cmd/hivegui/frontend/test/e2e-real/wails-bridge.ts` — stub the four methods.
7. `cmd/hivegui/frontend/test/e2e/hive-global.d.ts` — type the new `__hive` helpers.
8. `README.md` — add the click rows to the Keybinds table.
9. `docs/design-docs/ui/components.md` — add the Settings › Appearance › Editor section to the Settings inventory (near `:112`, which documents Settings → Agents).
10. `site/features.json` — add an entry with `since: "Unreleased"`.

## New files

- `cmd/hivegui/file_open.go` — shared code: `ResolveFilePaths`, `OpenFile`, `resolvePath(base, p)` (expands `~`, joins, cleans, stats) and the mode dispatch. Per-OS work goes through package-level vars (`openDefaultFn`, `revealFn`, `runEditorFn`) so tests never launch anything.
- `cmd/hivegui/file_open_guard.go` — the pure `isLaunchable(goos, path, fileMeta)`, the Windows name normalization and the denylists.
- `cmd/hivegui/file_open_darwin.go`, `file_open_linux.go`, `file_open_windows.go`, and `file_open_other.go` (build tags; other unix systems return an error) — `openDefault`, `reveal` and `statMeta` for each OS.
- `cmd/hivegui/editor_prefs.go` — the `EditorSettings` type, load and save (`update_prefs.go` pattern), `GetEditorSettings` / `SaveEditorSettings` (Save validates kind and template: `command` needs a non-empty template containing `{file}`; `app` is refused off macOS), `editorArgv(settings, file, line, col) ([]string, error)`, and `splitArgv`.
- `cmd/hivegui/frontend/src/lib/file-links.ts` — a pure module:
  - `findPathCandidates(text) → {start, end, path, line?, col?}[]`.
  - It covers absolute POSIX paths, `~/`, `./`, `../`, repo-relative `a/b.ext`, bare `name.ext`, and Windows `C:\…` / `.\…`.
  - It skips URLs (`://`), strips wrapping quotes, backticks, parens and brackets, trims trailing `.,;:!?`, and parses `:line[:col]` and `(line,col)`.
  - `parseFileUri(uri) → path | null` handles host `''` or `localhost` only, and percent-decodes the path.
- `cmd/hivegui/frontend/src/app/file-link-provider.ts`
  - It builds the logical line across `isWrapped` rows and maps string offsets back to buffer ranges.
  - It calls `ResolveFilePaths` once per provideLinks call.
  - Its `ILink` objects carry `hiveFile: true`, and their `activate` enforces the modifier rule and calls `OpenFile`.
- `cmd/hivegui/frontend/src/components/modals/EditorSettings.tsx` — a preset select, plus a template field for `command` and an app-name field for `app` (mac only). A hint shows the placeholders.
- `.changesets/449-cmd-click-file-paths.md` — `type: added`, `bump: minor`.

## Tests

**Go**
- `cmd/hivegui/file_open_test.go`
  - `TestResolvePath`: covers `~`, relative joins, `..`, a missing file (empty), an absolute path ignoring the base, and trailing slashes.
  - `TestOpenFileDispatch`: a table test with stubbed fns that checks each case sends the expected `open` / `reveal` / `editor` call:
    - a regular file in OS mode → open;
    - a directory → reveal;
    - a launchable file → reveal and **never** open;
    - editor mode with no editor configured → the OS path;
    - editor mode with an editor → the editor, with line and col;
    - a missing file → an error;
    - a symlink to a launchable file → reveal; windows-goos `evil.exe.` → reveal, never open.
  - `TestOpenFileNeverTrustsFrontendResolution`: a relative path escaping via `..` still resolves correctly, and a nonexistent path errors without calling any fn.
- `cmd/hivegui/file_open_guard_test.go` is portable and runs on every CI OS. `TestIsLaunchable` is a table over `goos × case`:
  - **darwin:** a `.app` directory, `.command`, an exec-bit script, `.terminal`, `.webloc`, `.py`, `.sh`, `.jnlp` and an alias are true; `.md` and `.ts` are false.
  - **linux:** `.desktop`, `.AppImage`, an exec-bit file and `.py` are true; `.txt` is false.
  - **windows:** `.exe`, `.BAT`, `.lnk`, `.ps1`, `.url`, `.py`, `evil.exe.`, `evil.exe ` (trailing space), `evil.exe::$DATA`, `notes.txt.exe` and `PAYLOA~1.SET` (via a long-name lookup stub) are true; `.txt`, `report.v2.md` and `x.py.bak` are false.
  - Mixed case is tested on every OS.
- `cmd/hivegui/file_open_darwin_test.go` — `TestStatMetaDetectsAlias`: writes a FinderInfo xattr with the alias flag onto a temp file and checks `isAlias` is set. It also checks the exec bit.
- `cmd/hivegui/editor_prefs_test.go`
  - `TestEditorSettingsRoundTrip`, and `TestLoadEditorSettingsCorruptIsError` (Save never overwrites a corrupt file).
  - `TestEditorArgvPresets`: vscode, cursor, zed and sublime, each with and without a line.
  - `TestEditorArgvTemplate`: a path with spaces stays one argument, `{line}` defaults to 1, and shell metacharacters stay literal.
  - `TestSplitArgv`: quotes, empty tokens and an unterminated quote (error).
  - `TestEditorArgvRefusesCmdMetacharsForBatchShims`: a `.cmd` binary with the file `a&calc&.txt` is an error, and a `.exe` binary with the same file is allowed.
  - `TestSaveEditorSettingsRejectsShellArgv0`: `sh -c {file}`, `cmd /c {file}` and `powershell {file}` are rejected.
  - `TestSaveEditorSettingsValidates`: `command` without `{file}` is rejected, and so is an unknown kind.
- `cmd/hivegui/open_url_test.go` — `TestAllowedURL` keeps its `file://` rejection case (unchanged). This is the criterion 9 guard.

**Frontend unit** (`test/unit/file-links.test.ts`)
- `findPathCandidates`: covers every shape above, trailing punctuation, quoted paths, URLs excluded, `:12:5` and `(12,5)`.
- `parseFileUri`: rejects a remote host, and decodes `%20`.

**DOM** (`test/dom/file-link-provider.test.ts`)
- Covers the wrapped-line offset mapping.
- Activation without the modifier is a no-op. With ⌘ it calls `OpenFile(editor=false)`; with ⌘⇧ it calls `OpenFile(editor=true)`. On Linux and Windows, Ctrl plays the role of ⌘.
- A rejection calls `reportFailure`.

**DOM** (`test/dom/settings-editor.test.tsx`)
- Picks presets, shows the template field only for `command`, and shows the app field only on mac.
- If the load fails, Save does not call `SaveEditorSettings`.

**e2e** (`test/e2e/file-links.spec.ts`)
- Writes `src/foo.ts:12:5` and `nope/missing.ts` into a session.
- Hovering the first underlines it; hovering the second does not.
- ⌘-click records `OpenFile(..., editor=false)`, and ⇧⌘-click records `editor=true` with line 12, col 5.
- A plain click records nothing.
- An OSC 8 `file:///…` link routes to `OpenFile`, while an OSC 8 `https:` link still routes to `OpenURL`.
- An `OpenFile` failure through `__hive.failNext('OpenFile')` shows the status-bar error.

## Verification

```
GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod) go test ./cmd/hivegui/ -run 'ResolvePath|OpenFile|IsLaunchable|Editor|SplitArgv|AllowedURL' -v
for os in darwin linux windows; do GOOS=$os go vet ./cmd/hivegui/ && GOOS=$os staticcheck ./cmd/hivegui/ || echo FAIL $os; done
GOOS=windows go test -c -o /dev/null ./cmd/hivegui/   # windows test file compiles
GOOS=linux go test -c -o /dev/null ./cmd/hivegui/
go test ./internal/proc/ -run TestNoDirectExecOnWindows
scripts/test.sh   # go, unit, dom, e2e
cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && cd - && scripts/ui-lint.sh
```

Manual check against the real app, per `docs/verifying-the-gui-by-hand.md`: run `wails dev` and use Playwright on localhost:34115. Hover `README.md` in a shell session and confirm the underline. ⌘-click a `+x` script and confirm Finder reveals it rather than running it. The editor launch itself is covered by the stubbed Go test; don't launch real apps in automation.

## PR convergence ledger

- **2026-09-22 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: a744ef39db81dcde839309394d696d9f7b7c1252e72959ddf99d87130b6ff7a5; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: d4000269.
- **2026-09-22 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: fec55bb3824f8fb4a78d3b18c37aa6dd8ac2ed3d9eee1ea60b3aaeecb8a65871; threads_open: 4; action: escalated:risky-fix-needs-human-decision; head_sha: 125b09ec.
- **2026-09-22 iter 2b** — operator-approved fixes applied (app-kind editor guard, safeEvalSymlinks, in-flight cache, TOCTOU accepted with a ponytail comment); 4 CodeRabbit threads replied to and resolved; macOS e2e flake (activity-grid spec 428, untouched by this diff) passed on rerun; action: autofix+push; head_sha: 5a62db61.
- **2026-09-22 iter 3** — verdict: REQUEST_CHANGES (COMMENT coerced: 8 IMPORTANT, 0 BLOCKING); mergeable: MERGEABLE; findings_hash: 85e965c960cbffdc4a0d05ce7b8814c58e88fe1a4bad81190805d311933e9847; threads_open: 0; action: autofix+push; head_sha: 5a62db61.
- **2026-09-22 iter 3** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 85e965c960cbffdc4a0d05ce7b8814c58e88fe1a4bad81190805d311933e9847; threads_open: 0; action: autofix+push; head_sha: eb365477.
- **2026-09-22 iter 4** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 9c8f3b0fc46f55920ddb9a9207d866fb23bc73902c6fb07db17183c7cefe6bf7; threads_open: 0; action: autofix+push; head_sha: d4a90108.
- **2026-09-22 iter 5** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: b8b1d463.

## Open questions / risks

- **Private xterm API (verified)**: `Linkifier.currentLink` is `{link, state}`, and `link` is the provider's own `ILink` object (`Linkifier.ts:204-262`). So the `hiveFile` flag survives. For OSC 8, `text` is the URI (`OscLinkProvider.ts:56`), so the guard checks `text.startsWith('file:')`.
- **`allowNonHttpProtocols: true`** now lets `mailto:`, `vscode:` and other schemes through as OSC 8 links. Every non-`file:` URI still goes to `OpenURL`, whose Go allowlist refuses anything except http, https and mailto, so the security boundary is unchanged. `TestAllowedURL` stays the guard.
- **Hover cost**: each hovered line costs one bridge call and a few `stat`s. Review added a bounded memo (3s TTL, 64 entries, keyed by base dir plus candidate list) that also shares the in-flight promise, since the pointer re-asks for a row within milliseconds.
- **False positives**: bare `name.ext` tokens are only underlined if they exist, so false positives are cheap.
- **Stale base dir**: relative paths use the launch or worktree dir, not the live cwd. This is a non-goal, and the existence check hides wrong guesses.
- **VS Code without a CLI**: many mac users never install the `code` shell command. The preset then errors with "`code` not found on PATH — install the shell command from VS Code, or choose 'App'". This is a clear error rather than a silent fallback.
- **Windows open primitive**: `ShellExecute` is a direct API call, not an exec, so `TestNoDirectExecOnWindows` is unaffected. It avoids `cmd /C start` re-parsing the path.
- **Linux reveal**: `FileManager1` is missing on some desktops. The fallback opens the parent directory.
- **Deferred**: Spotlight content-type (UTI) checks on macOS. They are not needed: the alias flag, bundle extensions and exec bit cover the launchable cases, and Spotlight metadata is missing in unindexed directories anyway.
- **Race**: the file could be swapped between the stat and the open (time-of-check to time-of-use). This is accepted: the attacker would need local write access, which already means game over.

## Second opinion

- **Round 1 — revise (7/10).** Three security must-fixes, all applied: Windows name normalization (trailing dot/space, alternate data streams), the BatBadBut `.cmd`/`.bat` shim injection guard, and interpreter extensions (`.py`, `.sh`, `.jnlp`) missing from the denylist.
- **Round 2 — approve (8/10).** No must-fixes. Nice-to-haves applied: 8.3 short-name expansion, a wider argv[0] denylist with a comment saying it is defense-in-depth rather than a boundary, and a `evil.exe.` dispatch test. Ruled out: launching `Code.exe` directly (it skips `cli.js`, and the line jump is unverified) and Spotlight UTI checks (redundant, and unreliable in unindexed directories).

## Decision log

- **2026-09-20** — Detect absolute, `~/` and relative paths; relative paths resolve against the session base dir (`resolveSessionCwd`). Why: operator choice in brainstorm; agents print repo-relative paths.
- **2026-09-20** — Cmd-click uses the OS default app with an executable guard that reveals instead of launching. Why: operator choice; this keeps the security line `OpenURL` draws today.
- **2026-09-20** — Cmd+shift-click opens the configured editor at line and column. The editor is a Settings preset or a custom command/app. `$EDITOR` is not used. Why: operator choice; `$EDITOR` is typically a terminal editor.
- **2026-09-20** — Scope covers macOS, Linux and Windows, plus OSC 8 `file://` under the same guard. Live cwd (OSC 7) is a non-goal. Why: operator choice.
- **2026-09-22** — File links require a modifier. A plain click on a file path does nothing and click-to-position still works. URL links are unchanged. Why: operator choice; a stray click in agent output must not open files.
- **2026-09-22** — The editor setting lives in Go JSON (`editor.json` in the state dir). The bridge call takes path, line, col and mode only, never a command. Why: operator choice; JS must never be able to name the binary Go runs.
- **2026-09-22** — Errors use the status-bar flash (`reportFailure`), not a new toast. Spec criterion 10 was amended to match. Why: operator choice; no toast primitive exists.
- **2026-09-22** — A custom editor is either an argv template (`{file}`, `{line}`, `{col}`, split without a shell) or a macOS app name (`open -a`, no line jump). Why: operator choice.

## Progress

- **2026-09-20** — Research complete.
- **2026-09-22** — Implemented; PR #450 opened. All layers green (go, unit, dom, e2e), biome/tsc/ui-lint clean. staticcheck could not run locally (release predates this Go toolchain's export-data version); CI covers it.
- **2026-09-22** — Plan approved (HTML review, round 1; second opinion revise → approve).

## Open questions

- Exact per-platform denylist of executable and launcher types.
