# UI design system — Phase 6: fonts, all presets, contrast gate, CSS split

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the design system. Bundle the two brand webfonts so `hive-dark`/`hive-light` render as designed on macOS, Windows and Linux; ship the remaining presets (`native-dark`, `native-light`, `terminal`) with a full ANSI 16 per preset; make WCAG contrast a CI gate; flip the default from `classic` to `system`; dissolve `style.css` into `base.css` + `layout.css` + `components/*.css` and retire the off-scale font sizes that Phase 1 parked behind `/* ui-lint: allow */`.

**Architecture:** No new runtime code paths. `src/theme/base.css` gains six `@font-face` rules pointing at `src/theme/fonts/*.woff2`; Vite rewrites those `url()`s into `dist/assets/*.woff2` at build time, and `//go:embed all:frontend/dist` in `cmd/hivegui/main.go` carries them into the binary unchanged on every platform — a Go test asserts that. `themes.css` grows three preset blocks and a `--ansi-0 … --ansi-15` set for all six; `xtermTheme()` reads them into xterm's `theme` object alongside the existing five keys. `scripts/ui-contrast.mjs` (node, zero deps) parses `tokens.css`/`themes.css`, resolves one level of `var()`, and computes WCAG 2.1 contrast per preset; `scripts/ui-lint.sh --contrast` shells out to it. The `style.css` split is a pure file move: byte-identical rule bodies, new files, one `components/index.css` that `@import`s them in a fixed order so `index.html` keeps five `<link>`s and the cascade stays `tokens → themes → base → layout → components`.

**Tech Stack:** Vanilla TS, Vite 8, vitest 4, Playwright 1.62, bash + node (stdlib only) for lint. No new dependencies — npm, Go, or otherwise.

**Spec:** `docs/design-docs/ui/themes.md` (preset table, "Adding a preset" checklist, xterm mapping, contrast rule), `tokens.md` (type scale, bundled fonts), `mocks/identity-options.html` (`.t-native`, `.t-mono` token values), `README.md` principle 3.

## Global Constraints

- **Depends on Phases 1–5 being merged.** This plan assumes: `src/theme/{tokens,themes}.css` + `theme.ts` (Phase 1), `src/ui/` primitives and the SVG sprite (Phases 2–4), the data-driven `PRESETS` array and Settings › Appearance picker (Phase 5), and `scripts/ui-lint.sh` running `--strict` in CI. If any is missing, stop and land it first.
- No new npm dependencies. The contrast checker is a `.mjs` script on node's stdlib; the fonts are committed binaries, not a package.
- Tokens only. Every value this phase adds lives in `tokens.css` or `themes.css`. The only new literals outside them are `url()` font paths in `base.css`.
- Every preset re-values **every** token (themes.md: partial presets fall through to `hive-dark` and look broken in light). The single exception already documented in `themes.css` is `hive-light`, which deliberately shares hive-dark's type/space/radius scales.
- Gates, all green before the PR: `npx biome ci .`, `npm run typecheck`, `npx vitest run`, `npx playwright test`, `scripts/ui-lint.sh --strict`, `scripts/ui-lint.sh --contrast`, `go build ./...`, `go test ./cmd/hivegui/...`.
- Cross-platform: fonts must be present in the Windows zip and the Linux build, not just the macOS `.app`. Task 3 Step 5 is the check that proves it.
- Commits: conventional, one per task. Run every frontend command from `cmd/hivegui/frontend/`. Fresh worktree → `./scripts/ci-bootstrap.sh` first, or `npm run typecheck` fails on missing `wailsjs/`.

---

### Task 1: Split `style.css` into `base` / `layout` / `components/*`

Mechanical, zero visual change. Done first, while `classic` is still the default and Phase 1's `maxDiffPixels: 0` baselines are still valid — they are the proof that the move changed nothing.

**Files:**
- Create: `cmd/hivegui/frontend/src/theme/base.css`, `src/theme/layout.css`, `src/theme/components/index.css`, `src/theme/components/*.css` (list below)
- Delete: `cmd/hivegui/frontend/src/style.css`
- Modify: `cmd/hivegui/frontend/index.html` (link block)

**Interfaces:**
- Produces: cascade order `tokens.css → themes.css → base.css → layout.css → components/index.css`; `components/index.css` is the single place that fixes intra-component order.

- [ ] **Step 1: Cut the file along the boundaries below**

Procedure, per target file: `sed -n 'A,Bp' src/style.css >> src/theme/<target>.css` for each contiguous range, then delete the range from `style.css`. Rule bodies are moved **byte-identical** — no reformatting, no re-ordering, no token substitutions in this task (that is Task 2). Section comments move with their rules. When `style.css` is empty, `git rm` it.

| Target | What moves (by current `style.css` line ranges — re-derive them, the file drifts) |
|---|---|
| `base.css` | `html, body` (1–11), global scrollbar block (12–32), `.xterm-viewport` scrollbar hack (33–43) → keep here only if it is UA-level; the `.xterm-*` rules go to `components/xterm-overrides.css` |
| `layout.css` | `#app` grid + all grid-placement rules (44–71), `#sidebar-resizer` (83–101), `#terms.single` / `#terms.grid` placement (641–747, geometry only), `#minimized-tray` box (846–858) |
| `components/sidebar.css` | `#sidebar` chrome (72–82), `#sidebar header`, `.brand`, `#new-project-btn` (102–133), `#sessions, #projects` (134–145) |
| `components/project-card.css` | `.project*` (237–364) |
| `components/session-row.css` | `.session-item*`, `.name`, `.session-title`, `.dot`, `.swatch`, drag/drop indicators, attention keyframes (365–601) |
| `components/minimized.css` | `#minimized-projects`, `.min-project-*` (143–236), `#minimized-tray .min-chip*` (859–895) |
| `components/hints.css` | `.hints*` (602–640) |
| `components/terminal-host.css` | `.term-host`, `.tile-*`, tile attention keyframes (708–845, 896–908) |
| `components/xterm-overrides.css` | every rule whose selector contains `.xterm` |
| `components/empty-state.css` | `#empty-state`, `#boot-state`, `.boot-state-card` (909–997) |
| `components/launcher.css` | `#launcher`, `.launcher-*`, `.worktree-rename`, `.worktree-glyph` (998–1148) |
| `components/dialog.css` | shared dialog shells: `#project-editor`, `#settings`, `#worktrees`, `#help-overlay`, `.choice-dialog` backdrops/panels/headers/`.actions` |
| `components/form-field.css` | `label`, `input[type=text]`, `input[type=color]`, `.settings-agent-row` field rules |
| `components/banner.css` | `#daemon-banner*`, `#update-banner*` (1434–1501) |
| `components/command-palette.css` | 1502–1552 |
| `components/phase-overlay.css` | lifecycle/dead-session overlays (1553–1717) |
| `components/help-overlay.css` | 1718–1850 (content rules; shell in `dialog.css`) |
| `components/worktrees.css` | 1851–2083 (content rules; shell in `dialog.css`) |
| `components/choice-dialog.css` | 2084–end (content rules; shell in `dialog.css`) |
| `components/status-bar.css` | `#status` |

If a rule plausibly belongs to two files, put it where its **selector's first token** lives (`.session-item .worktree-glyph` → `session-row.css`, not `worktrees.css`). Don't split a single rule.

- [ ] **Step 2: Write `components/index.css`**

```css
/* Component layer. Import order IS cascade order — later files win ties, so
   surface-specific files come after the primitives they override.
   Vite inlines these @imports at build; index.html links only this file. */
@import './icon.css';
@import './icon-button.css';
@import './kbd.css';
@import './chip.css';
@import './form-field.css';
@import './dialog.css';
@import './banner.css';
@import './status-bar.css';
@import './empty-state.css';
@import './sidebar.css';
@import './project-card.css';
@import './session-row.css';
@import './minimized.css';
@import './hints.css';
@import './terminal-host.css';
@import './phase-overlay.css';
@import './launcher.css';
@import './command-palette.css';
@import './help-overlay.css';
@import './worktrees.css';
@import './choice-dialog.css';
@import './xterm-overrides.css';
```

(`icon.css`, `icon-button.css`, `kbd.css`, `chip.css` already exist from Phases 2–4 — move them under `components/` too if they landed elsewhere.)

- [ ] **Step 3: Rewrite the `index.html` link block**

```html
    <link rel="stylesheet" href="./src/theme/tokens.css"/>
    <link rel="stylesheet" href="./src/theme/themes.css"/>
    <link rel="stylesheet" href="./src/theme/base.css"/>
    <link rel="stylesheet" href="./src/theme/layout.css"/>
    <link rel="stylesheet" href="./src/theme/components/index.css"/>
```

- [ ] **Step 4: Prove the move is inert**

Run: `npm run build && npx playwright test test/e2e/theme.spec.ts && scripts/ui-lint.sh --strict`
Expected: build succeeds; Phase 1's `maxDiffPixels: 0` snapshots still pass; lint unchanged (same violation count as before the split — the `ui-lint: allow` comments travelled with their lines).

If a snapshot moves, the cause is cascade order, not values: two rules with equal specificity swapped places. Find the pair in the diff PNG, and fix by moving one file in `index.css`, never by adding `!important`.

- [ ] **Step 5: Commit**

```bash
git add src/theme index.html && git rm src/style.css
git commit -m "refactor(theme): split style.css into base/layout/components (no visual change)"
```

---

### Task 2: Collapse the off-scale font sizes onto the type scale

Phase 1 parked 30 `font-size` literals behind `/* ui-lint: allow */` with a `TODO(phase-6)`. This is that phase. Sizes present today: 8, 10, 10.5, 11.5, 12.5, 15, 18px.

**Files:**
- Modify: `cmd/hivegui/frontend/src/theme/components/*.css`
- Modify: `cmd/hivegui/frontend/test/e2e/theme.spec.ts-snapshots/*` (regenerated — this task *does* move pixels)

**Interfaces:**
- Consumes: `--text-xs: 11px … --text-xl: 16px` from `tokens.css`.
- Produces: zero `px-size` violations from `scripts/ui-lint.sh --strict`.

- [ ] **Step 1: Apply the mapping — one rule per size, no per-site judgement**

| Literal | → token | Rationale / sites |
|---|---|---|
| `8px` | **delete the declaration** | Only on `.session-item.attention .name::before` and `#terms.grid .term-host.in-grid.attention .tile-name::before`. Phases 2–3 replaced those glyphs with sprite icons sized by `width`/`height`; a `font-size` on an SVG-bearing pseudo-element does nothing. If either rule still renders text, the phase-2 migration is incomplete — fix it there, not here. |
| `10px` | `var(--text-xs)` (11) | `.project-header .caret`, `.term-host .tile-project`, `#minimized-tray .min-chip-project`, `.worktree-meta` — all tertiary metadata, exactly `--text-xs`'s role. |
| `10.5px` | `var(--text-xs)` (11) | `.session-item .session-title`, `.hints`, `.launcher-item .agent-num`, `.launcher-item .install-tag`. Subtitles/hints; rounds up, so nothing shrinks. |
| `11.5px` | `var(--text-sm)` (12) | `.launcher-worktree`, `.launcher-branch`, `.settings-hint`, `.settings-error`, `.dead-subtitle`, `.worktrees-hint`, `.worktree-actions button`, `.choice-dialog-detail`, `.choice-dialog-note`. All "secondary text / chips" = `--text-sm`. |
| `12.5px` | `var(--text-md)` (13) | `.session-item`, `.session-item .name-input`, `.boot-state-card`, `#boot-state-retry`, `#empty-state .empty-hint`, `#empty-state .empty-actions button`, `.launcher-search`, `.worktrees-empty-card`. Body text and buttons = `--text-md`. |
| `15px` | `var(--text-xl)` (16) | `#empty-state .empty-title` — tokens.md assigns the empty-state title to `--text-xl` by name. |
| `18px` | **delete the declaration** | Both sites are `#daemon-banner-dismiss` / `#update-banner-dismiss`, which are `iconButton()`s after Phase 4; the 18px was sizing a `×` glyph that no longer exists. If text remains, use `var(--text-xl)`. |

Delete the `/* ui-lint: allow */` comment on every line you touch. Leave the **colour** allows alone — they are a separate backlog and out of scope here.

- [ ] **Step 2: Sanity-check the two rows that get taller**

`.session-item` 12.5 → 13px and `.session-item .session-title` 10.5 → 11px both live inside the 40px two-line row from Phase 3. Run the app (`wails dev` or `npx vite` + the mock) and confirm no clipping at the 220px minimum sidebar width with a long session name.

- [ ] **Step 3: Regenerate the baselines**

Run: `npx playwright test test/e2e/theme.spec.ts --update-snapshots`
Then **read the diff** (`git diff --stat` on the snapshot dir + open the new PNGs). Expected: text one pixel larger in the listed places, nothing re-flowed onto a new line, no scrollbar appearing.

- [ ] **Step 4: Lint + commit**

Run: `scripts/ui-lint.sh --strict`
Expected: `px-size` violations = 0.

```bash
git add src/theme/components test/e2e/theme.spec.ts-snapshots
git commit -m "refactor(theme): collapse off-scale font sizes onto the type scale"
```

---

### Task 3: Bundle IBM Plex Sans + JetBrains Mono as woff2

**Files:**
- Create: `cmd/hivegui/frontend/src/theme/fonts/IBMPlexSans-{Regular,Medium,SemiBold}.woff2`
- Create: `cmd/hivegui/frontend/src/theme/fonts/JetBrainsMono-{Regular,Medium,Bold}.woff2`
- Create: `cmd/hivegui/frontend/src/theme/fonts/LICENSE-IBMPlexSans.txt`, `LICENSE-JetBrainsMono.txt`, `README.md`
- Create: `cmd/hivegui/assets_test.go`
- Modify: `cmd/hivegui/frontend/src/theme/base.css`

**Interfaces:**
- Produces: font families `"IBM Plex Sans"` (400/500/600) and `"JetBrains Mono"` (400/500/700), already named by `--font-ui` / `--font-mono` in `tokens.css` — no token changes needed.

**Licensing (both are SIL OFL 1.1 — redistribution inside a binary is permitted):**

| Family | Licence | Upstream | Obligations we must meet |
|---|---|---|---|
| IBM Plex Sans | SIL Open Font License 1.1 (`IBM/plex` `LICENSE.txt`) | https://github.com/IBM/plex — release assets under tag `v6.4.0`: `https://github.com/IBM/plex/releases/download/v6.4.0/WOFF2.zip` (fallback: `npm pack @ibm/plex-sans@6.4.1`, files under `fonts/complete/woff2/`; `npm pack` downloads a tarball without adding a dependency) | Ship the licence text alongside the fonts; keep the Reserved Font Name ("IBM Plex") — do not rename or subset-and-rename the files |
| JetBrains Mono | SIL Open Font License 1.1 | https://github.com/JetBrains/JetBrainsMono — `https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip`, files under `fonts/webfonts/` | Same; JetBrains additionally asks that the name be kept intact, which OFL already requires |

Neither licence requires attribution in the app UI. Both require the licence text to travel with the fonts, which is why `LICENSE-*.txt` are committed next to the `.woff2` files rather than referenced.

- [ ] **Step 1: Fetch, verify, place**

```bash
cd cmd/hivegui/frontend/src/theme/fonts
curl -fL -o plex.zip https://github.com/IBM/plex/releases/download/v6.4.0/WOFF2.zip
curl -fL -o jbm.zip  https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip
shasum -a 256 plex.zip jbm.zip   # record both digests in the PR body
unzip -j plex.zip '*IBMPlexSans-Regular.woff2' '*IBMPlexSans-Medium.woff2' '*IBMPlexSans-SemiBold.woff2'
unzip -j jbm.zip  'fonts/webfonts/JetBrainsMono-Regular.woff2' \
                  'fonts/webfonts/JetBrainsMono-Medium.woff2' \
                  'fonts/webfonts/JetBrainsMono-Bold.woff2'
unzip -p plex.zip '*LICENSE*' > LICENSE-IBMPlexSans.txt
unzip -p jbm.zip  'OFL.txt'   > LICENSE-JetBrainsMono.txt
rm plex.zip jbm.zip
ls -la   # expect 6 × .woff2, ~30–60KB each; total under 350KB
```

> **If an asset name or path differs from the above**, do not improvise a different font source. Take the woff2 files from that release's own layout, and record the exact URL + sha256 you used in `fonts/README.md` and the PR body. The plan's URLs were written from the published release layout and are the thing most likely to have drifted.

- [ ] **Step 2: Write `fonts/README.md`** — provenance is the point of this file:

```markdown
# Bundled fonts

Wails has no CDN; the brand presets need these files inside the binary.

| File | Family / weight | Version | Licence | Source |
|---|---|---|---|---|
| IBMPlexSans-Regular.woff2 | IBM Plex Sans 400 | 6.4.0 | OFL 1.1 (LICENSE-IBMPlexSans.txt) | github.com/IBM/plex releases |
| IBMPlexSans-Medium.woff2 | IBM Plex Sans 500 | 6.4.0 | " | " |
| IBMPlexSans-SemiBold.woff2 | IBM Plex Sans 600 | 6.4.0 | " | " |
| JetBrainsMono-Regular.woff2 | JetBrains Mono 400 | 2.304 | OFL 1.1 (LICENSE-JetBrainsMono.txt) | github.com/JetBrains/JetBrainsMono releases |
| JetBrainsMono-Medium.woff2 | JetBrains Mono 500 | 2.304 | " | " |
| JetBrainsMono-Bold.woff2 | JetBrains Mono 700 | 2.304 | " | " |

Files are unmodified upstream releases (no subsetting — the OFL Reserved Font
Name rules make a renamed subset more paperwork than the ~200KB it would save).
sha256 of the source archives is in the PR that added them.
```

- [ ] **Step 3: `@font-face` at the top of `base.css`**

```css
/* Bundled brand fonts. Vite rewrites these url()s to hashed files under
   dist/assets/ at build time; //go:embed all:frontend/dist carries them into
   the binary on every platform. Licences: src/theme/fonts/LICENSE-*.txt.
   font-display: swap — the system fallback in --font-ui/--font-mono paints
   first, so a slow font decode never shows an empty sidebar. */
@font-face {
  font-family: "IBM Plex Sans";
  src: url("./fonts/IBMPlexSans-Regular.woff2") format("woff2");
  font-weight: 400; font-style: normal; font-display: swap;
}
@font-face {
  font-family: "IBM Plex Sans";
  src: url("./fonts/IBMPlexSans-Medium.woff2") format("woff2");
  font-weight: 500; font-style: normal; font-display: swap;
}
@font-face {
  font-family: "IBM Plex Sans";
  src: url("./fonts/IBMPlexSans-SemiBold.woff2") format("woff2");
  font-weight: 600; font-style: normal; font-display: swap;
}
@font-face {
  font-family: "JetBrains Mono";
  src: url("./fonts/JetBrainsMono-Regular.woff2") format("woff2");
  font-weight: 400; font-style: normal; font-display: swap;
}
@font-face {
  font-family: "JetBrains Mono";
  src: url("./fonts/JetBrainsMono-Medium.woff2") format("woff2");
  font-weight: 500; font-style: normal; font-display: swap;
}
@font-face {
  font-family: "JetBrains Mono";
  src: url("./fonts/JetBrainsMono-Bold.woff2") format("woff2");
  font-weight: 700; font-style: normal; font-display: swap;
}
```

Do **not** add `unicode-range`: we ship one file per weight and the app renders arbitrary user text (project names, window titles) in any script — a latin-only range would silently drop the fallback chain's coverage decision onto the browser.

- [ ] **Step 4: Point xterm at the bundled mono font**

`src/app/session-term.ts:308` still hard-codes `fontFamily: 'Menlo, "DejaVu Sans Mono", monospace'`. Replace with the token, read the same way the theme is:

```ts
// Terminal font follows --font-mono so the bundled JetBrains Mono (and any
// user override from Settings › Appearance) reaches xterm too. Read once at
// construction; a theme change re-applies it via applyThemeToTerminals().
fontFamily: getComputedStyle(document.documentElement)
  .getPropertyValue('--font-mono').trim() || 'Menlo, monospace',
```

and add `t.options.fontFamily = …` next to the existing `t.options.theme = …` in the Phase-5 theme-change handler.

- [ ] **Step 5: Prove the fonts reach `dist` and the binary**

```bash
npm run build
ls dist/assets/*.woff2                 # expect 6 hashed files
grep -o 'woff2' dist/assets/*.css | wc -l   # expect >= 6 url() rewrites
```

Then a real embed test — `cmd/hivegui/main.go` declares `//go:embed all:frontend/dist` into `assets`, so ask the embedded FS directly:

```go
// cmd/hivegui/assets_test.go
package main

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// The brand presets are unusable without the bundled webfonts, and the failure
// mode is silent: the app falls back to a system font and only looks slightly
// wrong. Vite hashes the filenames, so match on extension, not name.
func TestEmbeddedAssetsIncludeWebfonts(t *testing.T) {
	var n int
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(path.Ext(p), ".woff2") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	if n < 6 {
		t.Fatalf("embedded .woff2 files = %d, want >= 6 (3 Plex Sans + 3 JetBrains Mono); "+
			"did `npm run build` run before `go build`?", n)
	}
}
```

Run: `npm run build && cd ../../.. && go test ./cmd/hivegui/...`
Expected: PASS. This runs on all three CI legs, which is the Windows/Linux coverage the constraint asks for — no OS-specific packaging step touches `dist`, so if the test is green the Windows zip has the fonts.

- [ ] **Step 6: Commit**

```bash
git add cmd/hivegui/frontend/src/theme/fonts cmd/hivegui/frontend/src/theme/base.css \
        cmd/hivegui/frontend/src/app/session-term.ts cmd/hivegui/assets_test.go
git commit -m "feat(theme): bundle IBM Plex Sans and JetBrains Mono (OFL 1.1) as woff2"
```

---

### Task 4: `native-dark`, `native-light`, `terminal` presets + ANSI 16 everywhere

**Files:**
- Modify: `cmd/hivegui/frontend/src/theme/tokens.css` (ANSI defaults = hive-dark)
- Modify: `cmd/hivegui/frontend/src/theme/themes.css` (three new blocks + ANSI per preset + two contrast fixes)
- Modify: `cmd/hivegui/frontend/src/theme/theme.ts` (`PRESETS`), `index.html` (inline pre-paint script)

**Interfaces:**
- Produces: `ThemeName` = `'classic' | 'hive-dark' | 'hive-light' | 'native-dark' | 'native-light' | 'terminal' | 'system'`; `--ansi-0 … --ansi-15` on every preset.

- [ ] **Step 1: Two contrast fixes to existing presets first** (Task 6's checker fails without them; numbers computed against the WCAG 2.1 formula):

```css
/* tokens.css / hive-dark: --fg-subtle #5f6273 on --surface is 3.05:1 — inside
   the ≥3 rule but with no margin for the --surface-raised case. Leave as is;
   it passes. Recorded here so the next person doesn't "fix" it into a colour
   that breaks the mock. */

/* themes.css / hive-light: --on-accent was #ffffff, which is 3.42:1 on
   --accent #c47a12 — a primary button's own label failing AA. Dark text on the
   amber is 5.02:1. */
:root[data-theme="hive-light"] { --on-accent: #1a1b22; }
```

Update `docs/design-docs/ui/themes.md`'s `hive-light` row in Task 9 to match.

- [ ] **Step 2: ANSI defaults in `tokens.css`** (append inside the existing `:root`):

```css
  /* ANSI 16 for xterm. Derived from the hive-dark mock palette: 1/3/2 are the
     state colours (error/accent/running) so program output agrees with the
     chrome; 4/5/6 come from the project-swatch hues in the mock. Bright rows
     are the same hues at higher lightness, never a different hue. */
  --ansi-0: #0b0c10;  --ansi-1: #ff6b6b;  --ansi-2: #5fd7a5;  --ansi-3: #ffb454;
  --ansi-4: #6c9cff;  --ansi-5: #c084fc;  --ansi-6: #5fd0d7;  --ansi-7: #a5a8b8;
  --ansi-8: #5f6273;  --ansi-9: #ff8f8f;  --ansi-10: #86e5bd; --ansi-11: #ffc97a;
  --ansi-12: #93b7ff; --ansi-13: #d3a6fd; --ansi-14: #8fe3e8; --ansi-15: #e6e7ee;
```

- [ ] **Step 3: ANSI for the existing presets in `themes.css`**

```css
/* classic: xterm's own defaults, unchanged. classic exists so upgraders see
   no difference, and today's terminal has no ANSI overrides at all. */
:root[data-theme="classic"] {
  --ansi-0: #000000;  --ansi-1: #cd0000;  --ansi-2: #00cd00;  --ansi-3: #cdcd00;
  --ansi-4: #0000ee;  --ansi-5: #cd00cd;  --ansi-6: #00cdcd;  --ansi-7: #e5e5e5;
  --ansi-8: #7f7f7f;  --ansi-9: #ff0000;  --ansi-10: #00ff00; --ansi-11: #ffff00;
  --ansi-12: #5c5cff; --ansi-13: #ff00ff; --ansi-14: #00ffff; --ansi-15: #ffffff;
}

/* hive-light: same hues as hive-dark, darkened until each clears 4.5:1 on a
   white terminal ground. "Bright" is DARKER here, not lighter — on a light
   ground, higher contrast is the point of the bright row. */
:root[data-theme="hive-light"] {
  --ansi-0: #1a1b22;  --ansi-1: #d64545;  --ansi-2: #1f9d6a;  --ansi-3: #c47a12;
  --ansi-4: #3b6fd4;  --ansi-5: #8b46c9;  --ansi-6: #12808c;  --ansi-7: #4f5262;
  --ansi-8: #8a8d9c;  --ansi-9: #b83232;  --ansi-10: #178055; --ansi-11: #a05f08;
  --ansi-12: #2d59ad; --ansi-13: #7136a8; --ansi-14: #0d6771; --ansi-15: #1a1b22;
}
```

- [ ] **Step 4: The three new preset blocks** — values from `mocks/identity-options.html` (`.t-native` for both natives, `.t-mono` for terminal), extended to the full token set:

```css
/* native-dark — mock option A. Lifted neutral greys, system font, filled
   selection, 6px radii; VS Code / Zed lineage, so its ANSI is the Dark+ set
   users of those tools already read fluently. */
:root[data-theme="native-dark"] {
  --bg: #1e1e1f; --surface: #252527; --surface-raised: #27272a; --border: #3a3a3d;
  --fg: #ececed; --fg-muted: #b8b8bc; --fg-subtle: #7a7a80;
  --accent: #e6a23c; --on-accent: #1a1a1a; --sel: #3b3b40;
  --hover: color-mix(in srgb, var(--sel) 60%, transparent);
  --btn: #323235; --btn-border: #454548;
  --state-running: #4cc38a; --state-attention: #f0b44c; --state-error: #e06c6c;
  --term-bg: #151516; --term-fg: #e2e2e4;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  --font-mono: "SF Mono", Menlo, Consolas, monospace;
  --radius-sm: 4px; --radius-md: 6px;
  --shadow-popover: 0 8px 24px rgba(0, 0, 0, 0.45);
  --ansi-0: #000000;  --ansi-1: #cd3131;  --ansi-2: #0dbc79;  --ansi-3: #e5e510;
  --ansi-4: #2472c8;  --ansi-5: #bc3fbc;  --ansi-6: #11a8cd;  --ansi-7: #e5e5e5;
  --ansi-8: #666666;  --ansi-9: #f14c4c;  --ansi-10: #23d18b; --ansi-11: #f5f543;
  --ansi-12: #3b8eea; --ansi-13: #d670d6; --ansi-14: #29b8db; --ansi-15: #e5e5e5;
}

/* native-light — A inverted the same way hive-light inverts B: borders lighten,
   shadow softens, accent darkens until white-on-accent clears AA (#a35f0d is
   5.00:1; the mock's #e6a23c would be 1.9:1 and unusable for button labels).
   ANSI is the VS Code Light+ set. */
:root[data-theme="native-light"] {
  --bg: #ffffff; --surface: #f3f3f3; --surface-raised: #ffffff; --border: #e0e0e0;
  --fg: #1c1c1e; --fg-muted: #4c4c52; --fg-subtle: #6e6e76;
  --accent: #a35f0d; --on-accent: #ffffff; --sel: #dfe6f5;
  --hover: color-mix(in srgb, var(--sel) 60%, transparent);
  --btn: #ffffff; --btn-border: #cdcdd2;
  --state-running: #197f52; --state-attention: #a35f0d; --state-error: #c02b2b;
  --term-bg: #ffffff; --term-fg: #1c1c1e;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  --font-mono: "SF Mono", Menlo, Consolas, monospace;
  --radius-sm: 4px; --radius-md: 6px;
  --shadow-popover: 0 8px 24px rgba(20, 20, 40, 0.15);
  --ansi-0: #000000;  --ansi-1: #cd3131;  --ansi-2: #00bc00;  --ansi-3: #949800;
  --ansi-4: #0451a5;  --ansi-5: #bc05bc;  --ansi-6: #0598bc;  --ansi-7: #555555;
  --ansi-8: #666666;  --ansi-9: #cd3131;  --ansi-10: #14ce14; --ansi-11: #b5ba00;
  --ansi-12: #0451a5; --ansi-13: #bc05bc; --ansi-14: #0598bc; --ansi-15: #a5a5a5;
}

/* terminal — mock option C. Everything mono, radius 0, no fills; --accent ==
   --state-attention by design (themes.md allows it only for this preset).
   --fg-subtle is #6f6f6f, not the mock's #5c5c5c: the mock value is 2.96:1 on
   #0a0a0a and fails the ≥3 rule by a hair. */
:root[data-theme="terminal"] {
  --bg: #0a0a0a; --surface: #0a0a0a; --surface-raised: #0f0f0f; --border: #232323;
  --fg: #d6d6d6; --fg-muted: #9c9c9c; --fg-subtle: #6f6f6f;
  --accent: #e8c170; --on-accent: #000000; --sel: #1a1a1a;
  --hover: color-mix(in srgb, var(--sel) 60%, transparent);
  --btn: transparent; --btn-border: #333333;
  --state-running: #9c9c9c; --state-attention: #e8c170; --state-error: #c07070;
  --term-bg: #0a0a0a; --term-fg: #d6d6d6;
  --font-ui: "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
  --font-mono: "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
  --radius-sm: 0; --radius-md: 0;
  --shadow-popover: 0 0 0 1px var(--border);
  /* Near-monochrome ANSI: the greys carry structure, amber carries attention.
     Red is kept as a desaturated brick rather than a grey — build output that
     says "error" in colour must still read as error; a fully monochrome ramp
     would delete information the user's tools are trying to send. */
  --ansi-0: #0a0a0a;  --ansi-1: #c07070;  --ansi-2: #8f8f8f;  --ansi-3: #e8c170;
  --ansi-4: #7c7c7c;  --ansi-5: #8a8a8a;  --ansi-6: #9c9c9c;  --ansi-7: #d6d6d6;
  --ansi-8: #6f6f6f;  --ansi-9: #d98d8d;  --ansi-10: #b4b4b4; --ansi-11: #f0d094;
  --ansi-12: #a0a0a0; --ansi-13: #adadad; --ansi-14: #c2c2c2; --ansi-15: #f2f2f2;
}
```

- [ ] **Step 5: Register the presets in the two places that list them**

`theme.ts` (Phase 5 made this a data-driven array — extend it, keep `label` prose short since it renders in the Appearance dropdown):

```ts
export const PRESETS = [
  { id: 'system', label: 'System' },
  { id: 'hive-dark', label: 'Hive Dark' },
  { id: 'hive-light', label: 'Hive Light' },
  { id: 'native-dark', label: 'Native Dark' },
  { id: 'native-light', label: 'Native Light' },
  { id: 'terminal', label: 'Terminal' },
  { id: 'classic', label: 'Classic (v2.4)' },
] as const;
```

and `index.html`'s pre-paint script, which duplicates the list by design (module scripts are deferred and would flash the wrong preset):

```js
        var t = localStorage.getItem('hive.theme');
        var dark = matchMedia('(prefers-color-scheme: dark)').matches;
        var known = ['classic','hive-dark','hive-light','native-dark','native-light','terminal'];
        if (t === 'system' || known.indexOf(t) < 0) t = dark ? 'hive-dark' : 'hive-light';
        document.documentElement.dataset.theme = t;
```

- [ ] **Step 6: Verify**

Run: `npx vitest run test/unit/theme.test.ts && npm run typecheck && npx biome ci . && scripts/ui-lint.sh --strict`
Also extend `test/unit/theme.test.ts` with `it.each(PRESETS)` asserting `resolveTheme(id, true) === id` for every non-`system` id, so a preset added to `themes.css` but not to `PRESETS` fails a unit test rather than silently falling back.

- [ ] **Step 7: Commit**

```bash
git add src/theme/tokens.css src/theme/themes.css src/theme/theme.ts index.html test/unit/theme.test.ts
git commit -m "feat(theme): add native-dark/native-light/terminal presets and ANSI 16 for all six"
```

---

### Task 5: Wire ANSI into `xtermTheme()`

**Files:**
- Modify: `cmd/hivegui/frontend/src/theme/theme.ts`
- Test: `cmd/hivegui/frontend/test/dom/xterm-theme.test.ts`

**Interfaces:**
- Produces: `xtermTheme()` return grows the 16 xterm ANSI keys (`black … brightWhite`).

- [ ] **Step 1: Extend the failing test**

```ts
it('maps --ansi-0..15 onto xterm ANSI keys', () => {
  const root = document.documentElement;
  for (let i = 0; i < 16; i++) {
    root.style.setProperty(`--ansi-${i}`, `#${i.toString(16).repeat(6)}`);
  }
  const t = xtermTheme(document);
  expect(t.black).toBe('#000000');
  expect(t.red).toBe('#111111');
  expect(t.brightWhite).toBe('#ffffff');
});

it('omits ANSI keys entirely when a preset does not define them', () => {
  const doc = document.implementation.createHTMLDocument();
  expect(xtermTheme(doc).black).toBeUndefined();
});
```

- [ ] **Step 2: Run → FAIL**, then implement:

```ts
// xterm's ANSI key order is fixed and matches SGR 30-37 / 90-97, i.e.
// --ansi-0..15 positionally. Keys are omitted when the custom property is
// empty so xterm keeps its own default rather than being handed ''.
const ANSI_KEYS = [
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow',
  'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const;

export function xtermTheme(doc: Document = document): Record<string, string> {
  const cs = (doc.defaultView ?? window).getComputedStyle(doc.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  const accent = v('--accent');
  const theme: Record<string, string> = {
    background: v('--term-bg'),
    foreground: v('--term-fg'),
    cursor: accent,
    cursorAccent: v('--on-accent'),
    // color-mix isn't resolvable via getPropertyValue; xterm accepts 8-digit hex.
    selectionBackground: accent.length === 7 ? `${accent}4d` : accent,
  };
  ANSI_KEYS.forEach((key, i) => {
    const c = v(`--ansi-${i}`);
    if (c) theme[key] = c;
  });
  return theme;
}
```

`Record<string, string>` rather than a hand-written 21-key interface: xterm's `ITheme` already types the consumer side, and the only caller passes this straight into `new Terminal({ theme })`.

- [ ] **Step 3: Eyeball it once** — `wails dev`, run `for i in $(seq 0 15); do printf "\e[38;5;${i}m%3d\e[0m " $i; done; echo` in a session, switch presets in Settings › Appearance, confirm all 16 change and stay legible on each ground.

- [ ] **Step 4: Run + commit**

Run: `npx vitest run && npm run typecheck && npx biome ci .`

```bash
git add src/theme/theme.ts test/dom/xterm-theme.test.ts
git commit -m "feat(theme): feed per-preset ANSI 16 to xterm"
```

---

### Task 6: `scripts/ui-contrast.mjs` + `ui-lint.sh --contrast` + CI

**Files:**
- Create: `scripts/ui-contrast.mjs`
- Modify: `scripts/ui-lint.sh`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `node scripts/ui-contrast.mjs` — exit 1 on any failing pair, one line per pair checked with `--verbose`.
- Produces: `scripts/ui-lint.sh --contrast` — same exit code, so CI has one entry point.

- [ ] **Step 1: Write the checker**

```js
#!/usr/bin/env node
// WCAG 2.1 contrast gate for the theme presets. No dependencies: it parses the
// two token files with a regex rather than a CSS AST because the input is a
// flat list of `--name: value;` declarations we control, and a real parser
// would be a dependency for no extra correctness.
//
// Rules (docs/design-docs/ui/themes.md):
//   --fg        on --surface  >= 4.5   body text
//   --fg-muted  on --surface  >= 4.5   subtitles/labels are real text too
//   --fg-subtle on --surface  >= 3.0   decorative/disabled
//   --fg        on --bg       >= 4.5   the app ground, not just panels
//   --term-fg   on --term-bg  >= 4.5   terminal default pair
//   --on-accent on --accent   >= 4.5   primary buttons must read their own label
// color-mix()/var() values that don't resolve to a hex are skipped, loudly.
import { readFileSync } from 'node:fs';

const FILES = [
  'cmd/hivegui/frontend/src/theme/tokens.css',
  'cmd/hivegui/frontend/src/theme/themes.css',
];
const PAIRS = [
  ['--fg', '--surface', 4.5],
  ['--fg-muted', '--surface', 4.5],
  ['--fg-subtle', '--surface', 3],
  ['--fg', '--bg', 4.5],
  ['--term-fg', '--term-bg', 4.5],
  ['--on-accent', '--accent', 4.5],
];

// Every :root / :root[data-theme="x"] block, in source order. The base :root
// block in tokens.css seeds every preset, matching the cascade: a preset that
// omits a token inherits the default, and that inherited value is what the
// browser will actually paint, so it is what we must check.
function blocks(css) {
  const out = [];
  const re = /:root(?:\[data-theme=["']([^"']+)["']\])?\s*\{([^}]*)\}/g;
  for (let m; (m = re.exec(css)); ) {
    const decls = {};
    for (const d of m[2].matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/gi)) {
      decls[d[1]] = d[2].trim();
    }
    out.push({ name: m[1] || null, decls });
  }
  return out;
}

function hex(value, decls, depth = 0) {
  if (!value) return null;
  const v = value.trim();
  if (/^#[0-9a-f]{3}$/i.test(v)) return '#' + [...v.slice(1)].map((c) => c + c).join('');
  if (/^#[0-9a-f]{6}$/i.test(v)) return v.toLowerCase();
  const ref = v.match(/^var\((--[a-z0-9-]+)\)$/i);
  if (ref && depth < 4) return hex(decls[ref[1]], decls, depth + 1);
  return null; // color-mix, rgba, transparent, keywords
}

const lum = (h) => {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

const verbose = process.argv.includes('--verbose');
const all = FILES.flatMap((f) => blocks(readFileSync(f, 'utf8')));
const base = all.find((b) => b.name === null)?.decls ?? {};
const presets = all.filter((b) => b.name);
if (!presets.length) {
  console.error('ui-contrast: no [data-theme] blocks found — did the file move?');
  process.exit(1);
}

let failed = 0;
for (const p of presets) {
  const decls = { ...base, ...p.decls };
  for (const [fgName, bgName, min] of PAIRS) {
    const fg = hex(decls[fgName], decls);
    const bg = hex(decls[bgName], decls);
    if (!fg || !bg) {
      if (verbose) console.log(`skip  ${p.name} ${fgName}/${bgName} (not a hex value)`);
      continue;
    }
    const r = ratio(fg, bg);
    const ok = r >= min;
    if (!ok) failed++;
    if (!ok || verbose) {
      console.log(
        `${ok ? 'ok  ' : 'FAIL'}  ${p.name.padEnd(13)} ${fgName} on ${bgName}` +
          `  ${fg}/${bg}  ${r.toFixed(2)}:1 (need ${min})`,
      );
    }
  }
}
console.log(`ui-contrast: ${presets.length} preset(s), ${failed} failure(s)`);
process.exit(failed ? 1 : 0);
```

- [ ] **Step 2: Hook it into `ui-lint.sh`** — insert right after the `strict` flag parsing, before the grep rules:

```bash
if [[ "${1:-}" == "--contrast" ]]; then
  exec node scripts/ui-contrast.mjs "${@:2}"
fi
```

and mention `--contrast` in the script's header comment block.

- [ ] **Step 3: Run it**

Run: `scripts/ui-lint.sh --contrast --verbose`
Expected, with Task 4 applied — these are the numbers the plan was written against, so treat a mismatch as a value drift, not a checker bug:

| preset | fg/surface | fg-muted/surface | fg-subtle/surface |
|---|---|---|---|
| classic | 14.58 | 5.58 | 3.45 |
| hive-dark | 14.90 | 7.78 | 3.05 |
| hive-light | 17.16 | 7.73 | 3.30 |
| native-dark | 12.96 | 7.74 | 3.59 |
| native-light | 15.33 | 7.68 | 4.55 |
| terminal | 13.62 | 7.21 | 3.94 |

`0 failure(s)`, exit 0. If `hive-light --on-accent` still fails at 3.42, Task 4 Step 1 was skipped.

- [ ] **Step 4: CI step** — next to the existing ui-lint step, same `if: matrix.biome` (values are platform-independent):

```yaml
      - name: UI contrast (WCAG AA per preset)
        if: matrix.biome
        run: ./scripts/ui-lint.sh --contrast
```

- [ ] **Step 5: Commit**

```bash
chmod +x scripts/ui-contrast.mjs
git add scripts/ui-contrast.mjs scripts/ui-lint.sh .github/workflows/ci.yml
git commit -m "chore(ci): gate theme presets on WCAG AA contrast"
```

---

### Task 7: Flip the default to `system`

**Files:**
- Modify: `cmd/hivegui/frontend/src/theme/theme.ts` (`DEFAULT_THEME`)
- Modify: `cmd/hivegui/frontend/test/unit/theme.test.ts`
- Modify: `cmd/hivegui/frontend/test/e2e/theme.spec.ts` (Phase 1's assertions)

**Interfaces:**
- Consumes: `resolveTheme()`'s existing `system` branch — no logic change, one constant.

- [ ] **Step 1: One-line change**

```ts
// Phase 6: new installs follow the OS. Users who set a preset keep it —
// readTheme() only falls back when the stored value is absent or garbage.
export const DEFAULT_THEME: ThemeName = 'system';
```

`resolveTheme` already handles `DEFAULT_THEME === 'system'` (Phase 1 wrote that branch ahead of time); the `index.html` script was updated in Task 4 Step 5. No migration for existing users: their `hive.theme` is already written, and someone who never opened Settings has no key and *should* move to the new default — that is the intended visible change of this phase.

- [ ] **Step 2: Fix the Phase 1 unit test**

```ts
it('defaults to the OS preference when nothing is stored', () => {
  expect(resolveTheme(null, true)).toBe('hive-dark');
  expect(resolveTheme(null, false)).toBe('hive-light');
});
```

- [ ] **Step 3: Fix the Phase 1 screenshot tests**

`theme.spec.ts` asserts "classic is pixel-identical to v2.4.0" against the *default* render. Classic is no longer the default, so the test must ask for it explicitly. Add to both cases in that describe block, before `goto`:

```ts
await page.addInitScript(() => localStorage.setItem('hive.theme', 'classic'));
```

The baselines themselves do **not** change — the point of the classic guard survives this phase, it just needs to name its preset now. Task 2 already regenerated them for the type-scale change; nothing here should move a pixel.

- [ ] **Step 4: Run + commit**

Run: `npx vitest run && npx playwright test test/e2e/theme.spec.ts`
Expected: green, snapshots unchanged (`git status` clean under `theme.spec.ts-snapshots`).

```bash
git add src/theme/theme.ts test/unit/theme.test.ts test/e2e/theme.spec.ts
git commit -m "feat(theme): default new installs to the OS colour scheme"
```

---

### Task 8: Per-preset screenshot baselines

themes.md's "Adding a preset" checklist item 4: sidebar + dialog under every preset. Six presets × 2 surfaces = 12 snapshots.

**Files:**
- Modify: `cmd/hivegui/frontend/test/e2e/theme.spec.ts`
- Create (generated): `test/e2e/theme.spec.ts-snapshots/{sidebar,dialog}-<preset>-*.png`

**Interfaces:**
- Consumes: `PRESETS` from `src/theme/theme.ts` — the loop is generated from it, so a seventh preset gets baselines by existing.

- [ ] **Step 1: Add the parametrised block**

```ts
import { PRESETS } from '../../src/theme/theme';

// One sidebar + one dialog per preset. These are the only thing that catches a
// preset that parses fine, passes contrast, and still looks broken — a missing
// token falling through to hive-dark's dark surface inside a light preset.
//
// Playwright's default snapshot path includes the platform, so baselines
// captured on macOS are ABSENT (not failing) on the Linux and Windows legs.
// Pinned to Linux and generated in the official container — see Step 2.
test.describe('preset baselines', () => {
  test.skip(process.platform !== 'linux', 'baselines are captured on Linux');

  for (const { id } of PRESETS.filter((p) => p.id !== 'system')) {
    test(`${id}: sidebar`, async ({ page }) => {
      await page.setViewportSize({ width: 1100, height: 700 });
      await page.addInitScript((t) => localStorage.setItem('hive.theme', t), id);
      await page.goto('/');
      // seed: same helper calls as minimize.spec.ts (2 projects, 3 sessions,
      // one attention, one minimized) — the states are what the preset colours.
      await expect(page.locator('#projects .project')).toHaveCount(2);
      await expect(page.locator('#sidebar')).toHaveScreenshot(`sidebar-${id}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
      });
    });

    test(`${id}: dialog`, async ({ page }) => {
      await page.setViewportSize({ width: 1100, height: 700 });
      await page.addInitScript((t) => localStorage.setItem('hive.theme', t), id);
      await page.goto('/');
      await page.keyboard.press('Meta+,'); // check keymap.ts for the real binding
      await expect(page.locator('#settings-panel')).toBeVisible();
      await expect(page.locator('#settings-panel')).toHaveScreenshot(`dialog-${id}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
      });
    });
  }
});
```

Element-scoped screenshots, not full-page: the terminal area is live content and would need masking on every shot.

- [ ] **Step 2: Generate the baselines in the Linux container**

Font rasterisation differs per OS, so a macOS-generated PNG is not a valid Linux baseline — and with the bundled woff2 the *same* file is now used everywhere, which makes the container the honest capture environment:

```bash
docker run --rm -it -v "$PWD":/w -w /w/cmd/hivegui/frontend \
  mcr.microsoft.com/playwright:v1.62.1-noble \
  bash -c 'npm ci --no-audit --no-fund && npx playwright test test/e2e/theme.spec.ts --update-snapshots'
```

Open all 12 PNGs before committing. Look for: light presets with a dark panel (a missing token), `terminal` with rounded corners (`--radius-*` not zeroed), any preset where the attention row is indistinguishable from the selected row.

- [ ] **Step 3: Confirm the suite is still discovered and green**

Run: `node scripts/check-spec-discovery.mjs && npx playwright test test/e2e/theme.spec.ts`
Expected on macOS: the 12 preset tests report skipped, the Phase 1 tests pass. On Linux CI: all pass.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/theme.spec.ts test/e2e/theme.spec.ts-snapshots
git commit -m "test(theme): per-preset sidebar and dialog baselines"
```

---

### Task 9: Docs, changeset, README screenshots, plan bookkeeping

**Files:**
- Modify: `docs/design-docs/ui/README.md` (status), `themes.md` (preset table, contrast note), `tokens.md` (fonts line)
- Modify: `docs/exec-plans/active/ui-design-system.md` (progress, decision log)
- Modify: `README.md` (screenshots)
- Create: `.changesets/<pr>-ui-design-system-phase6.md`

- [ ] **Step 1: Spec updates**

- `README.md` (ui): Status → `**implemented** (v2.5.0). Changes to the UI now follow "How to change the UI from now on" below.` Drop the "Implementation is tracked in…" sentence's *not yet* framing; keep the link.
- `themes.md`: preset table — `native` row splits into `native-dark` / `native-light` with the shipped values; `hive-light` row gains `--on-accent: #1a1b22` (was `#ffffff`, failed AA on the amber); `terminal` row notes `--fg-subtle: #6f6f6f` (mock's `#5c5c5c` is 2.96:1). Add under Contrast: "`--on-accent` on `--accent` is checked too (≥4.5) — a primary button must read its own label." Add to "Adding a preset": "2b. Add the preset to `PRESETS` in `theme.ts` and to the inline pre-paint script in `index.html`."
- `tokens.md`: change "Presets `native` and `classic` set `--font-ui` to the system stack and load nothing" to name `native-dark`/`native-light`, and add: "Bundled: IBM Plex Sans 400/500/600 and JetBrains Mono 400/500/700, OFL 1.1, provenance in `src/theme/fonts/README.md`."

- [ ] **Step 2: Refresh the repo README screenshots**

The README's screenshots show the v2.4 pure-black UI, which is now neither the default nor reachable without picking `classic`. Recapture at the same crop/size with the default (`hive-dark` on a dark-mode machine), replacing the existing files in place so no README link changes. If a light-theme shot is added, add exactly one, captioned "Hive Light — Settings › Appearance", not a gallery.

- [ ] **Step 3: Changeset**

Create via `/hs-changelog-update`. User-visible text:

```markdown
### Changed

- **New look.** Hive now follows your system light/dark setting out of the box,
  with a redesigned dark and light theme, bundled IBM Plex Sans and JetBrains
  Mono, and consistent iconography. The v2.4 appearance is still available as
  **Settings › Appearance › Classic**, alongside Native Dark, Native Light and
  Terminal presets.
- Terminal colours (including the ANSI 16) now follow the selected theme.

### Added

- Settings › Appearance: theme picker and custom token overrides.
```

- [ ] **Step 4: Master plan bookkeeping**

`docs/exec-plans/active/ui-design-system.md`: tick Phase 6; add to the decision log:

```markdown
- **<date>** — Fonts ship as unmodified upstream woff2 (no subsetting). Why: both are OFL 1.1 with Reserved Font Names; a subset would need renaming and the paperwork costs more than the ~200KB it saves.
- **<date>** — `terminal` keeps a desaturated red at ANSI 1/9 instead of a pure grey ramp. Why: monochrome chrome is a design choice; deleting the error colour out of *program output* would destroy information the user's tools are sending.
- **<date>** — Per-preset screenshot baselines are Linux-only (`test.skip` elsewhere). Why: Playwright keys snapshots by platform and font rasterisation differs; three sets of baselines to maintain buys nothing the contrast gate and the Linux set don't already catch.
- **<date>** — `hive-light --on-accent` changed from `#ffffff` (spec) to `#1a1b22`. Why: 3.42:1 fails AA on `--accent #c47a12`; the spec value was never contrast-checked.
```

Then move the master plan and all six phase plans to `docs/exec-plans/completed/` if `scripts/check-plan-lifecycle.sh` requires it for a fully-ticked plan — run it and follow what it says.

- [ ] **Step 5: Full local gate**

```bash
cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && npx vitest run \
  && npx playwright test && cd ../../.. \
  && scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast \
  && go build ./... && go test ./cmd/hivegui/... && scripts/check-plan-lifecycle.sh
```

- [ ] **Step 6: Commit and open the PR**

```bash
git add docs README.md .changesets
git commit -m "docs(ui): mark the design system implemented"
```

PR title: `feat(theme): design-system phase 6 — fonts, all presets, contrast gate, CSS split`. Body: the six preset baseline screenshots, the `ui-contrast --verbose` table, the font source URLs + sha256 from Task 3 Step 1, and the `dist/assets/*.woff2` listing.

---

## Self-review

- **Spec coverage:** themes.md preset table → Task 4 (all six blocks, full token sets); ANSI 16 per preset → Task 4 + Task 5 (xterm side); contrast rule + `ui-lint.sh --contrast` → Task 6; "Adding a preset" checklist items 1–5 → Tasks 4 (1,2), 6 (3), 8 (4), 9 (5). tokens.md bundled fonts + type scale → Tasks 3 and 2. Master plan's Phase 6 row → Tasks 1, 2 (split), 3 (fonts), 4 (presets), 6 (contrast), 7 (default), 8 (baselines).
- **Ordering rationale:** the CSS split runs first, while `classic` is still the default and Phase 1's zero-diff baselines are still a valid oracle for "this move changed nothing". Every later task is a deliberate visual change measured against that.
- **Type consistency:** `PRESETS` is consumed by `theme.ts`, `test/unit/theme.test.ts` and `theme.spec.ts`; `resolveTheme`/`readTheme`/`applyTheme`/`xtermTheme` signatures are unchanged from Phase 1 except `xtermTheme`'s widened return type, whose only caller passes it straight to `new Terminal({ theme })`.
- **Placeholders:** none. The two font URLs are the one thing that can be stale — Task 3 Step 1 carries an explicit "if the asset name differs" instruction and requires recording the actual URL + sha256, rather than letting an agent substitute a different font source.
- **Known risk — snapshot platform keying:** Playwright's default snapshot path includes the platform, so baselines captured on a dev Mac are silently *absent* on Linux/Windows CI rather than failing. Task 8 pins the preset baselines to Linux and generates them in the official container. If Phase 1's existing baselines were captured on macOS, they have the same latent problem and should be re-captured the same way in Task 1 Step 4 — check before assuming the suite is meaningful in CI.
- **Known risk — `hive-dark --fg-subtle` at 3.05:1** clears the ≥3 decorative rule with almost no margin, and against `--surface-raised` the margin is thinner still. It passes as specified; if a future tweak darkens either value the gate will catch it, which is the point.
- **Deliberately out of scope:** the ~90 remaining `/* ui-lint: allow */` **colour** literals. They belong to the primitives migrated in Phases 2–5 and are a separate backlog; this phase only clears the `font-size` allows it explicitly owns. Also out of scope: font subsetting, variable-font builds, and a `prefers-contrast: more` preset.
