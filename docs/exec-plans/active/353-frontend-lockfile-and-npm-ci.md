# Commit frontend package-lock.json and install with npm ci

- **Spec:** [docs/product-specs/353-frontend-lockfile-and-npm-ci.md](../../product-specs/353-frontend-lockfile-and-npm-ci.md)
- **Issue:** #353
- **PR:** #356
- **Branch:** `sec/frontend-lockfile`
- **Status:** active

## Summary

Pin the `cmd/hivegui/frontend/` dependency tree: un-ignore and commit `package-lock.json`, pin every remaining `^` range to an exact version, switch all four install sites to `npm ci`, and add a blocking `npm audit --omit=dev --audit-level=high` step to the Linux CI leg. Build/CI plumbing only; no runtime code changes.

## Research

**Install sites** (`grep -rn "npm install"`):

- `build.sh:88` — `npm install --no-audit --no-fund`, guarded by a `node_modules` freshness check against `package.json`.
- `.github/workflows/ci.yml:87` — "Build frontend" step, `npm install --no-audit --no-fund && npm run build`.
- `scripts/test.sh:33` — `run_frontend()` lazy install, `npm install --silent`, only when `node_modules` is absent.
- `cmd/hivegui/wails.json:5` — `"frontend:install": "npm install"`. **Not in the task spec**, but `wails build` and `wails dev` run it, so leaving it unpinned would defeat the change on the release path (`build.sh` shells into `wails build`).
- `scripts/ci-bootstrap.sh` — **no** npm install (it seeds `dist/`, installs the Wails CLI, generates bindings). No change needed; the spec's mention of it is stale.
- `.github/workflows/pages.yml:42` — already `npm ci`, and it is the docs site, not this package. Out of scope.

**Blocker the task spec got wrong:** `cmd/hivegui/frontend/.gitignore:3` explicitly ignores `package-lock.json`. The spec assumed no such rule existed. That line must be deleted or `git add` silently does nothing.

**Version ranges** — `package.json` is already half-pinned: `react`, `react-dom`, `zustand`, `@biomejs/biome`, `@playwright/test`, `@testing-library/*`, `@types/*`, `typescript`, `ws` are exact; `@xterm/*` (4), `jsdom`, `vite`, `vitest` carry `^`.

**Ordering constraint:** `cmd/hivegui/main.go` embeds `frontend/dist` via `//go:embed`, so `ci-bootstrap.sh` (seeds `dist/`, generates `wailsjs/`) must stay ahead of the frontend build, and the frontend build ahead of every Go step. The new audit step is independent of that chain and can sit right after "Build frontend".

**Prior lessons:** `brain-search "npm lockfile frontend ci install"` returned no hits. From the operator's standing notes: a fresh worktree needs `./scripts/ci-bootstrap.sh` before `npm run typecheck` or `tsc` reports ~20 errors in untouched files (confirmed during implementation — the first `npm run build` failed on unresolved `../wailsjs/go/main/App`); verify the frontend with `biome ci .`, not `biome lint .`; run Playwright locally with `CI=1`; build with plain `wails build`, never `-s`.

## Approach

Lockfile plus exact versions, `npm ci` everywhere, audit as a hard gate.

`npm ci` is the whole point: it refuses to run without a lockfile and refuses to reconcile a lockfile that disagrees with `package.json`, so a hand-edited dependency that was never locked fails CI instead of silently resolving. The obvious cheaper alternative — commit the lockfile but keep `npm install` — was rejected because `npm install` will happily mutate the lockfile mid-build, which is exactly the non-reproducibility being fixed.

Pinning the remaining `^` ranges is belt-and-braces on top of the lockfile (operator decision, recorded below): the lockfile alone governs `npm ci`, but a developer running `npm install` by hand still floats a caret and rewrites the lockfile. Exact versions make that a no-op.

The audit step is blocking (`--audit-level=high`, no `continue-on-error`). It runs `--omit=dev` so the gate covers only what ships in the app bundle; dev-only advisories (vitest, playwright) never wedge the repo. The audit is run against the freshly-resolved tree *before* the CI step is written. If it reports a high or critical production advisory (prod deps are `react`, `react-dom`, `zustand`, `@xterm/*`), the fix is a dependency bump in this PR, not a weaker gate — operator decision, taken with the knowledge that it stretches non-goal 1.

### Files to change

1. `cmd/hivegui/frontend/.gitignore` — delete the `package-lock.json` line.
2. `cmd/hivegui/frontend/package.json` — pin `@xterm/addon-fit`, `@xterm/addon-web-links`, `@xterm/addon-webgl`, `@xterm/xterm`, `jsdom`, `vite`, `vitest` to the exact versions `npm install` resolves.
3. `build.sh:81-91` — `npm install` → `npm ci`; freshness check compares `node_modules` against `package-lock.json` (not `package.json`), since the lockfile is what now determines the tree; update the stale comment.
4. `.github/workflows/ci.yml:82-88` — "Build frontend" runs `npm ci --no-audit --no-fund`; add a following `npm audit --omit=dev --audit-level=high` step gated on `if: matrix.biome` (Linux leg only — the audit is platform-independent, same precedent as the typecheck at `:91`, staticcheck at `:160` and govulncheck at `:184`).
5. `.github/workflows/ci.yml:238-263` — the Playwright browser cache key is `hashFiles('cmd/hivegui/frontend/package.json')`, and the comment at `:248-250` justifies it with "package-lock.json is gitignored here, so it cannot be the key." That premise dies with this PR. Switch the key to `hashFiles('cmd/hivegui/frontend/package-lock.json')` and rewrite the comment paragraph accordingly. This is a **deliberate trade**, not a free win: `:243` calls this cache the single biggest cost in CI, and a lockfile hash churns on transitive-only regenerations that leave `@playwright/test` untouched, so hit rate drops. Accepted because the key then reflects the tree actually installed, and `restore-keys` still warms a partial directory on every miss.
6. `scripts/test.sh:30-35` — `run_frontend()` uses `npm ci --silent`, and its guard gains the same lockfile freshness check as `build.sh`: reinstall when `node_modules` is absent **or** `package-lock.json` is newer than it. Without this the dev path keeps testing a stale tree after any pull that moves the lockfile — the exact non-reproducibility the spec exists to close.
7. `cmd/hivegui/wails.json:5` — `"frontend:install": "npm ci"`.
8. `FRONTEND.md:166` — the "run `npm install` first" instruction becomes `npm ci`.

**What item 7 does and does not buy.** Wails skips `frontend:install` when the cached MD5 of `package.json` is unchanged (`build.sh:81-83` documents this; the cache file is ignored at `cmd/hivegui/frontend/.gitignore:4`). So a lockfile-only change does **not** re-trigger it. The release path is pinned because `build.sh` installs before shelling into `wails build`; item 7 only ensures that when Wails *does* install — a fresh tree, or `wails dev` — it uses the lockfile.

### New files

- `cmd/hivegui/frontend/package-lock.json` — generated by `npm install`, committed. `lockfileVersion` per the Node 24 npm shipped in CI.

No `.changesets/*.md` file: this is build/CI plumbing with no user-visible change, so the PR carries the `no-changeset` label (`.github/workflows/changesets.yml` `verify-generated` hard-fails without one or the other).

### Tests

No unit or integration test is added. This change has no runtime behaviour: nothing imports it, and there is no function to assert on. The verification is the build itself, and the standing regression test is CI — `npm ci` hard-fails when `package.json` and `package-lock.json` disagree, so the lockfile cannot silently rot.

Existing suites must keep passing unchanged, and because generating the lockfile moves four `^`-ranged packages (below), that now includes the browser suites: `go test ./...`, `vitest run test/unit`, `vitest run test/dom`, `npm run typecheck`, `npx biome ci .`, `npm run test:e2e`.

**Deliberately not tested:** the audit gate has no known-bad fixture, unlike this repo's other blocking gates (`ci.yml:107-116` ui-lint, `:125-151` ui-contrast, `scripts/check-daemon-contract-selftest.sh`). Building a poisoned fixture package to prove `npm audit` can fail is more machinery than the gate is worth; it shares this with govulncheck (`ci.yml:184`), which also has no negative test.

### The version move this necessarily makes

Generating a lockfile from `^` ranges resolves `@xterm/addon-fit`, `@xterm/addon-web-links`, `@xterm/addon-webgl`, `@xterm/xterm`, `jsdom`, `vite ^8.0.10` and `vitest ^4.1.8` to today's latest-in-range. That is a real, one-time dependency bump, and it brushes the spec's non-goal "Upgrading or changing any frontend dependency version" — the non-goal means *no deliberate upgrades*, not *no resolution*, and there is no way to write a lockfile without resolving. The PR body must list the resolved versions. A vite or vitest minor is exactly what breaks a browser suite, which is why `test:e2e` joins the verification below.

### Verification

```bash
# 1. Lockfile exists and is tracked, not ignored
test -f cmd/hivegui/frontend/package-lock.json
git check-ignore cmd/hivegui/frontend/package-lock.json; test $? -eq 1
git ls-files --error-unmatch cmd/hivegui/frontend/package-lock.json

# 2. No `npm install` command left in any install site. Greps only executable
#    lines -- `build.sh`'s comment must keep saying "Wails skips `npm install`",
#    because that is still true of Wails' own MD5-cached behaviour.
! grep -n "npm install" build.sh scripts/test.sh scripts/ci-bootstrap.sh \
    .github/workflows/ci.yml cmd/hivegui/wails.json FRONTEND.md | grep -v '^\S*:[0-9]*: *#'

# 3. Clean install from the lockfile alone, then the full frontend gate
cd cmd/hivegui/frontend
rm -rf node_modules
npm ci --no-audit --no-fund
npm audit --omit=dev --audit-level=high     # must exit 0
npm run build && npm run typecheck && npx biome ci . && npm test
CI=1 npm run test:e2e                       # vite/vitest moved; the browser suite must still pass
cd ../../..

# 4. Playwright cache key points at a file that exists
grep -n "hashFiles('cmd/hivegui/frontend/package-lock.json')" .github/workflows/ci.yml

# 5. Full release path (macOS)
./build.sh && test -d cmd/hivegui/build/bin/hivegui.app

# 6. Go side untouched
go test ./... -count=1 -timeout 120s
```

Check 3 is the real gate, and `npm ci`'s own exit code is the sync assertion — it errors out rather than reconciling when the lockfile is missing or disagrees with `package.json`, and by design it never rewrites the lockfile. The e2e run catches the version move. Check 5 fails if `wails.json`'s `npm ci` cannot run in the build sandbox.

## Decision log

- **2026-09-05** — Audit step is blocking, not `continue-on-error`. Why: operator decision; an advisory found today is fixed by bumping the dependency in this PR, so the gate has no standing exception.
- **2026-09-05** — Pin all remaining `^` ranges to exact versions. Why: operator decision; the lockfile only binds `npm ci`, and a manual `npm install` would otherwise float a caret and rewrite the lockfile.
- **2026-09-05** — Also change `cmd/hivegui/wails.json` and `FRONTEND.md`, which the task spec did not list. Why: `wails build` runs `frontend:install` on the release path, so leaving it as `npm install` would keep the release unpinned.
- **2026-09-05** — No change to `scripts/ci-bootstrap.sh`. Why: it contains no npm install; the task spec's mention of it was speculative.
- **2026-09-05** — `build.sh` *and* `scripts/test.sh` freshness checks compare `node_modules` against `package-lock.json` rather than `package.json`. Why: the lockfile is now the input that determines the installed tree, and `test.sh`'s old absent-only guard would keep testing a stale tree after a lockfile change.
- **2026-09-05** — Accept the one-time version resolve of the seven `^`-ranged packages as part of this PR, rather than pinning them to their currently-installed versions first. Why: there is no installed tree to read them from (no lockfile exists), and pinning to a guess would be fiction; the resolved versions get listed in the PR body and `test:e2e` covers the risk.
- **2026-09-05** — Repoint the Playwright cache key at `package-lock.json` (`ci.yml:262`). Why: its comment explicitly picked `package.json` because the lockfile was gitignored; that reason is removed by this PR, and the lockfile hash is the more accurate key.
- **2026-09-05** — No negative test for the audit gate. Why: it would need a deliberately-vulnerable fixture package; govulncheck (`ci.yml:184`) sets the precedent for accepting a blocking gate with a live advisory DB and no self-test.

## Progress

- **2026-09-05** — Spec and exec plan created; research complete.
- **2026-09-05** — Second opinion round 1 returned `revise`; four must-fix items applied. Round 2 returned `revise`; two items applied, one resolved by operator decision.
- **2026-09-05** — Operator approved the plan at the Phase 4 stop.
- **2026-09-05** — Implemented on `sec/frontend-lockfile`. Lockfile resolve moved `jsdom` 25.0.0→25.0.1, `vite` 8.0.10→8.2.2, `vitest` 4.1.8→4.1.11; the four `@xterm/*` packages resolved to their range floors, so they did not move. `npm audit --omit=dev` reports 0 vulnerabilities, so no dependency bump was needed.
- **2026-09-05** — Review loop converged on iteration 1: COMMENT, no BLOCKING, 0 unresolved threads. One IMPORTANT finding applied by hand rather than deferred — `docs/exec-plans/active/ui-design-system-phase6.md:871` regenerated Playwright baselines with `npm install` inside a bind-mounted container, which would both install an unvalidated tree and rewrite the committed lockfile from inside the container. Now `npm ci`. Three MINOR findings declined: two are stale premises in a completed plan and a dated analysis snapshot (historical record), and `cache: 'npm'` on `setup-node` is a real follow-up but outside this spec's scope.
- **2026-09-05** — All verification checks pass: `npm ci` clean-tree install, audit, build, typecheck, `biome ci` (0 errors), vitest 1051/1051, Playwright e2e 273 passed / 31 skipped, `go test ./...`, `./build.sh` producing a universal `hivegui.app`.

## Second opinion

Two rounds, `general-purpose` reviewer, per `/hs-feature-loop` Phase 4.

**Round 1 — `revise`, confidence 8.** Four must-fix items, all applied:
1. Verification check `git diff --exit-code` on the lockfile was vacuous (`npm ci` never writes it; diff on a new file reports nothing). Removed.
2. Blast-radius miss: `.github/workflows/ci.yml:238-263`, whose cache-key comment asserts the lockfile is gitignored. Added as file 5.
3. `scripts/test.sh:31` guards on `node_modules` *absent*, so `npm ci` would never re-run after a lockfile change. Given the same freshness check as `build.sh`.
4. Generating a lockfile from `^` ranges is a real version move that the verification never exercised with Playwright. Documented, and `test:e2e` added.

**Round 2 — `revise`, confidence 7.** Three items, disposition:
- *"The Playwright cache-key swap is not strictly better; `:243` calls that cache the biggest CI cost."* Correct on the facts; the "strictly better" claim is removed. Operator chose to switch to the lockfile hash anyway, with the hit-rate cost stated.
- *"The blocking audit has no stop rule; an in-PR bump collides with non-goal 1."* Operator confirmed: bump in this PR. The audit now runs before the CI step is written so the bump is visible rather than discovered in CI.
- *"Check 2's grep omits `ci-bootstrap.sh`, which the spec names verbatim; the `git status` line proves nothing."* Both applied.

Per the skill, the loop presents after one revise cycle rather than looping. The operator approved with the two decisions above.

## PR convergence ledger

<!-- one line per /hs-review-loop iteration -->

- **2026-09-05 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 5b4ed5d90a69090684fa32ecbc96ae68b628607d34b13dee0487a0ff6317ab70; threads_open: 0; action: stop; head_sha: 4b4af3e.

## Open questions

None.

## Gate verdict

- **2026-09-05** — verdict: FAIL; checks: 2 passed / 1 failed / 0 followups; followups: none; one-line: spec success criterion named `scripts/ci-bootstrap.sh` as an install site converted to `npm ci`, but that file contains no npm install at all.
  - 2026-09-05 dimensions:
    - acceptance — (see next entry; dimension still running at the time of the FAIL)
    - non-goals — PASS — production deps showed zero version movement (caret dropped, same version); the three dev-only moves (jsdom 25.0.1, vite 8.2.2, vitest 4.1.11) are each the highest version satisfying the prior `^` range, i.e. forced by resolution rather than a deliberate upgrade. No go.mod/Go source/Actions-pinning/dependabot bleed.
    - doc accuracy — FAIL — spec:29 asserted `scripts/ci-bootstrap.sh` "uses `npm ci`"; grep shows the file has zero npm references. Changeset policy (`no-changeset` label) verified authorized by `AGENTS.md:228` and enforced by `changesets.yml:113-129`. Developer docs sweep clean; remaining `npm install` hits are a dated analysis snapshot and a completed exec plan, correctly preserved as historical record. Rewritten comments in `build.sh`, `scripts/test.sh` and the `ci.yml` Playwright cache-key block are factually accurate.
