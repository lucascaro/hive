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

Full TS migration was chosen over the cheap `@ts-check`-per-file path. Scope
honestly: this is **M–L**, not a config add, and should NOT ride the Biome PR.

- **Sequence it after Biome lands** — a formatter over the whole tree first
  keeps the TS diff readable.
- **Migrate incrementally, not big-bang**: `tsconfig.json` with `allowJs: true`
  + `checkJs: false`, then convert file-by-file (leaf modules in `src/lib/`
  first — they're pure and already unit-tested). The Wails bindings in
  `wailsjs/` already ship `.d.ts` to lean on.
- **Gate strictness in stages**: land with loose settings, tighten
  (`strict: true`, `noImplicitAny`) once the bulk is converted, so CI doesn't
  block on hundreds of pre-existing type holes on day one.
- **Vite already handles `.ts`** — no bundler change; the build stays the same.
- Track this as a separate exec-plan; it's too big to be a Phase 2 bullet in
  practice, listed here only because the tooling decision lives in Phase 2.

## 2d. Small config dedupe (optional)

`playwright.config.js` and `playwright.real.config.js` share ~80% boilerplate
— extract a `playwright.base.js` both spread from. Only worth it while
already touching the configs in Phase 1.
