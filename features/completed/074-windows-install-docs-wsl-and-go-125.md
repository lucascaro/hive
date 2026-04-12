# Feature: Windows install docs could better highlight WSL path and Go 1.25+ requirement

- **GitHub Issue:** #74
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

User feedback after installing on Windows 11. Three friction points in the install docs:

1. **Go 1.25+ requirement is easy to miss.** Most Windows users have an older Go toolchain, and `go build` fails with a confusing module error. The requirement is listed but not prominent.
2. **tmux-on-Windows options aren't equivalent.** MSYS2/Chocolatey tmux works but persistent sessions behave differently than WSL. A short "recommended path for Windows: use WSL" note would save evaluation time.
3. **No prebuilt Windows/WSL binaries.** `go install` support or a release artifact would lower the barrier significantly vs. clone + build.

## Research

Three independent sub-tasks: (a) docs emphasis for Go 1.25, (b) docs re-ordering to front-load WSL, (c) release-artifact pipeline.

### Relevant Code / Docs

**Go version requirement:**
- `go.mod:3` — declares `go 1.25.0`. Requirement is real; pre-1.25 toolchains produce a confusing "module requires newer Go" error rather than a friendly message.
- `README.md:27` — "Go 1.25+ (to build from source)" is a single line in a bulleted list, easy to miss.
- `docs/getting-started-windows.md:10-19` — already flags Go 1.25, but one-liner; no warning about how recently 1.25 released or common confusion with system-packaged Go.

**WSL vs MSYS2 recommendation:**
- `README.md:51` — lists "MSYS2, WSL, or Chocolatey" in that order; no "recommended path" callout.
- `docs/getting-started-windows.md:26-55` — currently marks **MSYS2 as recommended** (Option A). User feedback says WSL gives the most equivalent experience, so this should flip.
- No data on which method most users actually succeed with — could ask in issue for confirmation before flipping the default recommendation.

**Why WSL is the better recommendation (to include in the updated docs):**
- **Persistent sessions actually persist.** Under MSYS2/Cygwin, tmux runs as a regular Windows process — closing the MSYS2 terminal or rebooting kills the tmux server and all sessions. In WSL, tmux runs inside a real Linux kernel and survives terminal closure as it does on Linux/macOS. Session persistence is hive's core value prop.
- **PTY semantics match Linux.** MSYS2 emulates Unix PTYs on top of Windows consoles, which leaks edge cases: SIGWINCH on resize, ANSI sequences, signal forwarding. WSL uses real Linux PTYs, so OSC 2 title updates, mouse reporting, and alternate-screen mode behave identically to Linux.
- **Signal handling.** Ctrl+C, SIGTERM, and process-group semantics are native in WSL; MSYS2 translates them and occasionally drops them, breaking agents that rely on clean shutdown.
- **AI agent CLIs are Linux-first.** Claude, Codex, Gemini, etc. test primarily on macOS/Linux. On MSYS2 they sometimes fail on path handling (`/c/Users` vs `C:\Users`), line endings, or subprocess spawning. WSL runs the Linux binary directly.
- **Single ecosystem for dependencies.** `apt install` + native Linux Go in WSL vs. mixing MSYS2 pacman + Windows Go + `hive.exe` communicating with MSYS2 tmux across the Win32/MSYS boundary.
- **Tradeoff:** WSL requires enabling the Windows feature and uses more disk/RAM. For a TUI that depends on tmux behaving like Linux, the cost is worth it.

**Distribution / binary artifacts:**
- `scripts/release.sh` **already cross-compiles and uploads binaries** on every release: `hive-darwin-amd64`, `hive-darwin-arm64`, `hive-linux-amd64`, `hive-linux-arm64`, `hive-windows-amd64.exe`. Verified against `v0.7.1` release assets.
- The user's "no prebuilt binaries" complaint is actually a **discoverability** problem: README and Windows docs only document "build from source" paths and never mention the Releases page.
- `main.go` at repo root; module path `github.com/lucascaro/hive`. `go install github.com/lucascaro/hive@latest` should also work (pulls source, compiles locally — still needs Go 1.25+).
- `build.sh` (Linux/macOS) and `build.ps1` (Windows) — manual build helpers; retain but demote in docs.

### Constraints / Dependencies
- **Docs-only change.** Release pipeline already exists (`scripts/release.sh`) and produces binaries — no goreleaser or new workflow needed.
- **README churn:** all sub-changes touch `README.md` and `docs/getting-started-windows.md` — deliver in one PR.
- **AGENTS.md testing rule:** mandates unit + flow tests for "all changes." Docs-only PRs have no code to test; this must be explicitly acknowledged in the PR description (verification becomes manual: links resolve, code snippets run).

## Plan

Docs-only change. Address all three user friction points by (1) leading the install docs with prebuilt binaries from the Releases page, (2) promoting Go 1.25+ to a visible callout that only applies to source builds, (3) flipping Windows tmux guidance to WSL-first with the rationale captured during research.

### Files to Change

1. **`README.md`** — restructure the "Installation" section:
   - **New top block:** "Install a prebuilt binary (recommended)" with a table mapping platform → asset name (`hive-darwin-arm64`, `hive-darwin-amd64`, `hive-linux-amd64`, `hive-linux-arm64`, `hive-windows-amd64.exe`) and a link to `https://github.com/lucascaro/hive/releases/latest`. Include a one-liner for each OS to download, `chmod +x`, and move onto PATH.
   - **Demote "Build from source" to a subsection** below prebuilt binaries. Preface it with a blockquote: "Only needed if you want the latest unreleased changes or are on a platform without a prebuilt binary. Requires **Go 1.25+** — check with `go version`. Get it from [go.dev/dl](https://go.dev/dl/)."
   - **Requirements section (line 25-30):** split into "Runtime" (tmux) and "Build-time only" (Go 1.25+). Remove Go from runtime requirements — users installing the prebuilt binary don't need Go.
   - **Windows tmux guidance (line 51):** reorder options to **WSL first, recommended**; add a short rationale (persistent sessions survive terminal close; PTY semantics match Linux; agent CLIs are Linux-first). Keep MSYS2 and Chocolatey as alternatives with a note that they work but have caveats.
   - **Add `go install` note** under "Build from source" for users who want Go's module fetch: `go install github.com/lucascaro/hive@latest`.

2. **`docs/getting-started-windows.md`** — mirror the README restructure for Windows-specific flow:
   - **New "Install Hive" section at top (before current `## Prerequisites`):** "Option A — Prebuilt binary (recommended)" with PowerShell snippet downloading `hive-windows-amd64.exe` from the Releases page, moving it onto PATH. "Option B — Build from source" demoted, still shows `build.ps1` + manual `go build`.
   - **Prerequisites section (line 8-61):** reorder as tmux → Git → Go (only if building from source). Make Go 1.25+ a visible `> **Note:**` callout, not a plain bullet.
   - **tmux install section (line 26-55):** promote Option B (WSL) to Option A and mark it recommended. Include the rationale from the Research section, condensed to 3-4 bullets:
     - Sessions survive terminal close and reboots (MSYS2 tmux dies with the terminal).
     - Real Linux PTYs — OSC 2 title updates, mouse, alt-screen behave as on Linux.
     - AI agent CLIs are Linux-first; WSL runs their native builds directly.
     - Single ecosystem (`apt` + native Linux Go), no Win32/MSYS boundary crossings.
   - Demote MSYS2 and Chocolatey to "alternative methods" with a warning that session persistence is limited.

### Test Strategy

This is a docs-only change. Per AGENTS.md the normal unit+flow test rule doesn't apply — there is no Go code being modified, so there is nothing to cover with `go test`. Verification is manual:

- **Manual doc verification checklist (run before opening PR, include results in PR description):**
  1. `go build ./...` still passes (no accidental code changes).
  2. `go test ./...` still passes (confirms no regressions).
  3. Every new URL resolves: `curl -I <url>` returns 2xx/3xx for each link to `github.com/lucascaro/hive/releases/...` and `go.dev/dl`.
  4. Every new shell snippet is syntactically valid: bash snippets run through `bash -n`, PowerShell snippets run through `powershell -NoProfile -Command '$PSVersionTable; [scriptblock]::Create((Get-Content snippet.ps1 -Raw))'` or `pwsh -c` on a Mac.
  5. A `grep` check that `"Go 1.25"` does not appear in the runtime requirements list in README.md (moved to build-from-source only).
  6. Visual render of both files on GitHub (preview in the PR) — confirm the "recommended" callouts display as intended and the Install section reads top-down as "prebuilt → source".
- **Explicit AGENTS.md exception note in PR description:** "Docs-only change; no new Go code. Test Strategy replaced with manual doc-verification checklist above per the nature of the change."

### Risks

- **Release asset naming drift.** If `scripts/release.sh` is ever changed to rename assets (e.g., `.tar.gz` wrappers), README links will break. Mitigation: use `…/releases/latest` links (resolve at click-time) rather than hard-coded versioned URLs where possible; asset names hard-coded in the README table are a known maintenance cost, accept it.
- **Flipping WSL as recommended may alienate MSYS2 users.** Keep MSYS2/Chocolatey as documented alternatives; don't remove them. Frame WSL recommendation as "for the full hive experience", not "only supported path".
- **`go install` pitfall.** The command pulls the latest tagged source and compiles — users still need Go 1.25+. Make sure the doc block places `go install` under "Build from source" (where the Go 1.25 callout lives), not next to "Prebuilt binaries".
- **Overlap with future goreleaser migration.** If the project later switches to goreleaser, asset names may change. Acceptable now; revisit when/if that happens.

## Implementation Notes

Docs-only change executed exactly as planned. Summary:

- **`README.md`** — split Requirements into "Runtime" (tmux only) and "Build-time only" (Go 1.25+). New "Install a prebuilt binary (recommended)" section added at the top of Installation with a platform→asset table and curl/Invoke-WebRequest one-liners. "Build from source" demoted below; includes Windows instructions and a `go install github.com/lucascaro/hive@latest` alternative. New "Windows: install tmux" subsection leads with WSL as recommended (with 4-bullet rationale); MSYS2 and Chocolatey listed as alternatives with a session-persistence warning.
- **`docs/getting-started-windows.md`** — restructured "Install Hive" to lead with prebuilt binary (no Go required). Option B (build from source) moved below and absorbs the Go 1.25+ + Git prerequisites inline. tmux install section flipped: WSL is now the recommended option with the rationale bullets; MSYS2 and Chocolatey are alternatives with caveats. Updated the "First Launch" terminal list to mention the WSL shell first.
- **`CHANGELOG.md`** — new entry under `[Unreleased]` → Changed, summarising the overhaul.

**Deviations from plan:** none. All items in "Files to Change" delivered.

**Verification (docs-only, per planned AGENTS.md exception):**
- `go build ./...`, `go vet ./...`, `go test ./...` all pass — no code was changed, confirms no incidental breakage.
- All new URLs resolved (HTTP 200): `releases/latest`, `releases/latest/download/hive-darwin-arm64`, `releases/latest/download/hive-windows-amd64.exe`, `go.dev/dl/`.
- Grep-checked that "Go 1.25" no longer appears under runtime requirements in README — it is scoped to "Build-time only" and to the "Build from source" section only.

- **PR:** —
