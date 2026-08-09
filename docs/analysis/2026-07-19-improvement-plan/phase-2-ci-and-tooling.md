# Phase 2 — CI dedupe and missing tooling

## 2a. Collapse the three CI workflows

`.github/workflows/build-linux.yml` and `build-macos.yml` are ~95% identical
(seed dist → install Wails CLI → `wails generate module` → npm build → go
build/vet/test → Go e2e → Vitest → Playwright mock → Playwright real →
artifacts). `build-windows.yml` re-implements a subset.

- Extract the shared ladder into **one reusable workflow** (`workflow_call`)
  or a composite action; per-OS files become ~10-line callers with a matrix
  of which legs run.
- Move the "seed empty dist so `go:embed` compiles" workaround into a small
  `scripts/ci-bootstrap.sh` used by both CI and `build.sh` — kills the
  CI↔build.sh drift in one place.
- Pin the Wails CLI version in exactly one place; `build.sh` currently
  installs `@latest` while CI pins `v2.12.0`.

## 2b. Give Windows a frontend CI leg

Windows CI runs only `go build/vet/test` — zero frontend coverage on the
platform with the most platform-specific frontend bugs (product-specs #177,
#183). Windows is "green" because it tests nothing.

Minimum viable: add Vitest (unit+dom) and the **mock** Playwright suite
(`test/e2e`) to the Windows job. Skip e2e-real on Windows (needs the unix
socket bridge; spec 245 already scopes it out).

## 2c. Add JS static analysis

~14k LOC of vanilla JS with no linter, formatter, or type checking.

- **Linter/formatter: Biome** (decided). One dep, one config, lint+format in a
  single Rust binary; runs in well under a second at this size, so oxlint's
  speed edge doesn't buy anything here and its lint-only split (needs a
  separate formatter) loses the "one tool" win. Run it in CI next to Vitest.
- Expect an initial autoformat commit; do it in one dedicated PR so blame
  stays readable.

### TypeScript migration (decided 2026-07-25 — its own workstream)

> **IN PROGRESS 2026-08-07** — now tracked in
> [docs/exec-plans/active/typescript-migration.md](../../exec-plans/active/typescript-migration.md).
> That plan supersedes this section on two points: **tests migrate too** (`test/` as well
> as `src/`), and **`strict: true` lands in PR 1** rather than in a final ramp wave —
> `checkJs: false` already covers the "don't block CI on pre-existing holes" rationale
> below, so staged strictness would only buy a terminal wave that re-touches every
> migrated file at once. Reasoning is in that plan's decision log.

Full TS migration was chosen over the cheap `@ts-check`-per-file path. Scope
honestly: this is **M–L**, not a config add, and should NOT ride the Biome PR.

- **Sequence it after Biome lands** — a formatter over the whole tree first
  keeps the TS diff readable.
- **Migrate incrementally, not big-bang**: `tsconfig.json` with `allowJs: true`
  + `checkJs: false`, then convert file-by-file (leaf modules in `src/lib/`
  first — they're pure and already unit-tested). The Wails bindings in
  `wailsjs/` already ship `.d.ts` to lean on.
- ~~**Gate strictness in stages**: land with loose settings, tighten
  (`strict: true`, `noImplicitAny`) once the bulk is converted, so CI doesn't
  block on hundreds of pre-existing type holes on day one.~~ **Superseded
  2026-08-07** — `strict: true` lands in PR 1. The "pre-existing type holes"
  risk this bullet guards against comes entirely from `checkJs: true`, which
  the bullet above already rules out; staged strictness would only buy a
  terminal wave re-touching every migrated file at once.
- **Vite already handles `.ts`** — no bundler change; the build stays the same.
- Track this as a separate exec-plan; it's too big to be a Phase 2 bullet in
  practice, listed here only because the tooling decision lives in Phase 2.

## 2d. Small config dedupe (optional)

`playwright.config.js` and `playwright.real.config.js` share ~80% boilerplate
— extract a `playwright.base.js` both spread from. Only worth it while
already touching the configs in Phase 1.

## 2e. CI caching and dependency pinning (landed 2026-08-08 in #268; two follow-ups)

Playwright browser downloads were the single biggest cost in CI — **219s of Windows's
6m48s**, versus 25s on Linux and 13s on macOS, so it is a Windows unzip pathology rather
than bandwidth. There was no caching of any kind in `ci.yml`. #268 added an
`actions/cache` entry over a `PLAYWRIGHT_BROWSERS_PATH` pinned to one workspace-relative
path, which is what lets a single cache config serve all three OSes (their defaults differ:
`~/.cache`, `~/Library/Caches`, `%LOCALAPPDATA%`).

Two things that cost a round of review each, recorded so they are not re-derived:

- **The cache key only works because `@playwright/test` is pinned exactly.** It was
  `^1.48.0` while the resolved version was 1.62.1, so `hashFiles(package.json)` was a
  constant across 14 minors of differing browser revisions. That failure is silent and
  self-perpetuating: the stale hit restores revision X, `playwright install` re-downloads
  Y, and `actions/cache` skips the save because the key already exists — so the cost
  returns permanently behind a green check. Keying on `package-lock.json` is **not** an
  option here: it is gitignored (`frontend/.gitignore:3`) and CI runs `npm install`, not
  `npm ci`, so `hashFiles` would return empty and the key would go constant in the other
  direction. The exact pin is what makes the version observable at all.
- **Actions caches are scoped by ref, and a PR-scoped cache is invisible to `main`.**
  Measured: identical keys existed under `refs/pull/268/merge` and `refs/heads/main` as
  separate 299MB entries, and the post-merge `main` run missed on all three legs and
  re-saved. Caches written on the default branch *are* inherited by branches cut from it,
  so the steady state is right — but the first run after any Playwright bump pays full
  price on `main` before any PR benefits. That is inherent to the scoping; no config
  change fixes it. Don't read a cold `main` run as a broken cache.

**Follow-up: finish the pinning convention.** `@biomejs/biome` (2.5.5), `typescript`
(7.0.2), `ws` (8.21.0) and now `@playwright/test` (1.62.1) are exact; `jsdom`, `vite`,
`vitest` and all four `@xterm/*` are still caret ranges (`package.json:20-33`). The reason
the first four are pinned applies verbatim to the rest — the lockfile is gitignored and CI
runs a bare `npm install`, so a caret floats and CI is not reproducible run to run. The
`@xterm/*` ones matter most: they are runtime dependencies of the shipped app, not tooling.

**Follow-up: the browser cache key over-invalidates.** It hashes all of `package.json`, so
bumping `jsdom` evicts the browsers. `restore-keys` absorbs most of the cost and
over-invalidating is the safe direction, so this is deliberate — revisit only if the
partial-restore path turns out to be slow in practice.
