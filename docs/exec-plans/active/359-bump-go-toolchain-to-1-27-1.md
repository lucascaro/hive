# Bump the Go toolchain to 1.27.1

- **Spec:** [docs/product-specs/359-bump-go-toolchain-to-1-27-1.md](../../product-specs/359-bump-go-toolchain-to-1-27-1.md)
- **Issue:** #359
- **PR:** #360
- **Branch:** `sec/go-1-27`
- **Status:** active

## Summary

`go.mod` names `go 1.25.14`, the last release of an end-of-life line. CI's
`setup-go` installs the `go` directive verbatim, so that one line decides the
toolchain every gate runs on. Move it to 1.27.1 and bring the three prose
sites that quote the old number along with it. No code changes.

## Research

**The version lives in exactly one machine-read place.**

- `go.mod:3` — `go 1.25.14`. The single source of truth.
- `.github/workflows/ci.yml:68-70` — the only `actions/setup-go` in the repo
  (`pages.yml` and `changesets.yml` have none), and it uses
  `go-version-file: go.mod`. Nothing else to update for CI to follow.
- `scripts/ci-bootstrap.sh` — pins the Wails CLI, not Go. No change.

**Prose that quotes the number and will go stale:**

- `README.md:45` — "Requires Go 1.25.14+, Node 24+…".
- `CONTRIBUTING.md:9-12` — "**Go 1.25.14+**" plus a paragraph explaining why
  the floor is a patch release, citing 1.25.0's advisories as the example.
- `.github/workflows/ci.yml:198-203` — the govulncheck step's comment, which
  uses 1.25.0/1.25.13/1.25.14 to explain verbatim-install and tells a local
  verifier to `GOTOOLCHAIN=go1.25.14`.

**Deliberately not touched:**

- `AGENTS.md:346-350` — already version-agnostic; it derives the value with
  `GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod)`. Correct as written.
- `docs/analysis/2026-07-19-*`, `docs/analysis/2026-08-24-improvements.md`,
  `CHANGELOG.md`, `docs/exec-plans/completed/*` — dated historical records.
  Rewriting them would falsify the record.

**Constraints / risks:**

- Local toolchain is `go1.26.4`; `GOTOOLCHAIN` auto-downloads 1.27.1 on first
  use. Every verification command must name it explicitly, per `AGENTS.md`.
- 1.27 is a *minor* bump, not a patch: `go vet` and `staticcheck` can surface
  findings that 1.25 did not, and Wails v2 codegen runs under the same
  toolchain. This is the whole risk of the task, and the verification step is
  built to catch it (build + vet + staticcheck across all three GOOS + full
  test suite + a real `./build.sh`).
- `go mod tidy` under 1.27 may add a `toolchain` line to `go.mod`. Leave it
  off: with a patch-level `go` directive it is redundant, and a second pinned
  version is a second thing to keep in sync.

**Prior lessons:** no prior lessons matched.

## Approach

Edit the directive, follow the three prose sites, verify under the target
toolchain. The alternative — a bare `go 1.27` directive plus a `toolchain`
line — was rejected: `setup-go` installs the directive verbatim, so the
patch-level directive is what makes CI's toolchain reproducible, and that
property is exactly what this task exists to preserve.

### Files to change

- `go.mod` — `go 1.25.14` → `go 1.27.1`. `go.sum` if `go mod tidy` moves it.
- `README.md:45` — "Requires Go 1.27.1+".
- `CONTRIBUTING.md:9-12` — bump the floor; reword the rationale so it explains
  the patch-level pin without asserting a stale claim about 1.25.0.
- `.github/workflows/ci.yml:198-203` — retarget the comment's example versions
  and the `GOTOOLCHAIN=` hint to 1.27.1.

### New files

None.

### Tests

No new tests. A toolchain bump has no behavior of its own to assert; the
existing suite plus the pinned analyzers *are* the assertion, and they must
pass under the new toolchain. The pinned analyzers are the real risk of a
minor bump — `staticcheck@2025.1.1` and `govulncheck@v1.7.0`
(`.github/workflows/ci.yml:181,191,214`) both predate Go 1.27 and are the
likeliest way this PR goes red — so the block below runs them at CI's pins,
not at whatever is on PATH.

**Verification (run from a clean worktree, in this order):**

```bash
export GOTOOLCHAIN=go1.27.1
go version                                   # confirms 1.27.1 downloaded

# 0. cmd/hivegui/main.go embeds frontend/dist, so no `go build` works
#    in a fresh worktree until the bindings and dist tree exist
#    (mirrors ci.yml:80 and :82-88).
./scripts/ci-bootstrap.sh
(cd cmd/hivegui/frontend && npm ci --no-audit --no-fund && npm run build)

# 1. go.mod/go.sum settled by the edit alone.
go mod tidy && git diff --exit-code go.mod go.sum

# 2. Build + vet, per GOOS. `|| exit 1` — not `|| break`, which would
#    swallow the failure and exit 0.
go build ./...
for os in darwin linux windows; do GOOS=$os go vet ./... || exit 1; done

# 3. Staticcheck at CI's pin, per GOOS (ci.yml:191; staticcheck analyses
#    one platform at a time, per AGENTS.md).
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
for os in darwin linux windows; do GOOS=$os "$(go env GOPATH)/bin/staticcheck" ./... || exit 1; done

# 4. Unit suite, then the e2e-tagged Go leg — untagged `go test ./...`
#    never compiles those files (ci.yml:232). Isolated state, always.
#    TERM=dumb on both: CI sets it so golden-file output carries no ANSI.
#    HOME is relocated for isolation, so GOPATH/GOMODCACHE are carried
#    across explicitly or the run re-downloads the whole module cache.
env TERM=dumb go test ./... -count=1 -timeout 120s
env HOME="$(mktemp -d)" HIVE_STATE_DIR="$(mktemp -d)" \
    HIVE_SOCKET="$(mktemp -d)/hived.sock" TERM=dumb \
    GOPATH="$(go env GOPATH)" GOMODCACHE="$(go env GOMODCACHE)" \
    go test -tags=e2e -timeout 180s ./cmd/hived/...

# 5. Vulnerability gate at CI's pin (ci.yml:214).
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
"$(go env GOPATH)/bin/govulncheck" ./...

# 6. Wails codegen + a real app bundle under 1.27.
./build.sh

# 7. No half-edit left behind, across every path the spec names.
! git grep -nE '1\.25\.[0-9]+' -- go.mod README.md CONTRIBUTING.md .github/ AGENTS.md scripts/
#    (1\.26\.x is deliberately excluded: ci.yml:202 states, accurately, that
#     the advisories were fixed from 1.26.5 on. That is prose, not a pin.)
```

**Contingencies — each is a stop-and-surface, not a silent workaround:**

- *`go mod tidy` writes a `toolchain` line.* Delete it and re-run tidy to
  confirm it stays off. If tidy insists on writing it back, keep it (a
  non-empty `git diff` after tidy is a failed gate otherwise) and record the
  reversal here — do not paper over it by dropping check 1.
- *`staticcheck@2025.1.1` refuses a `go 1.27.1` directive or reports new
  findings.* Bump the pin in `ci.yml:191` and the cache key in `ci.yml:181`
  together — the key names both tool versions, so a bump that misses it
  restores a stale binary. Same shape for `govulncheck` at `ci.yml:214`/`181`.
- *Wails CLI v2.15.0 (`scripts/ci-bootstrap.sh`) cannot generate bindings
  under 1.27.* Stop. Bumping Wails is outside this spec's non-goals and needs
  its own change; the fallback is to retarget this task at 1.26.8.

## Decision log

- **2026-09-06** — Target 1.27.1, not the plan's 1.26.8. Why: operator
  decision at the clarifying round; 1.27.1 is the current release and buys a
  longer support runway. Cost: a minor bump has a wider blast radius than a
  patch bump, absorbed by verifying vet/staticcheck across all three GOOS.
- **2026-09-06** — Keep a patch-level `go` directive; no `toolchain` line.
  Why: `setup-go` installs the directive verbatim, which is what makes CI's
  toolchain reproducible.
- **2026-09-06** — Leave dated analysis docs and `CHANGELOG.md` untouched.
  Why: they are records of what was true then.
- **2026-09-06** — Bump `staticcheck` 2025.1.1 → 2026.2.1 in `ci.yml:191` and
  the cache key at `ci.yml:181`. Why: forced, not optional. 2025.1.1 cannot
  read Go 1.27 export data at all — it aborts with `export data version 4 is
  greater than maximum supported version 2` on stdlib packages, so the
  Staticcheck gate would fail outright. 2026.2.1 is clean across darwin,
  linux and windows. This is the contingency the plan named, taken.
- **2026-09-06** — Keep 1.27.1 despite a local Rosetta deadlock (below).
  Why: operator decision — the defect is an x86_64 Go on an arm64 Mac, not
  Go 1.27, and the fix is a native toolchain. No repo change follows from it.

## Progress

- **2026-09-06** — Spec written, research complete, plan drafted; second
  opinion revise → approve.
- **2026-09-06** — Implemented on `sec/go-1-27`. Verified under
  `GOTOOLCHAIN=go1.27.1`: `go mod tidy` no-op (no `toolchain` line written),
  `go build ./...`, `go vet` × {darwin,linux,windows}, `staticcheck 2026.2.1`
  × {darwin,linux,windows}, `go test ./... -count=1` (exit 0, 12 packages),
  the e2e-tagged `./cmd/hived/...` leg, `govulncheck@v1.7.0` ("No
  vulnerabilities found"; 2 imported + 1 required unreachable, unchanged
  posture), and `./build.sh` producing a universal `hivegui.app`.
- **2026-09-06** — Local-environment finding, no repo impact: on this Apple
  Silicon machine `/usr/local/bin/go` is `Mach-O x86_64` and the shell is
  translated (`sysctl.proc_translated = 1`), so every test binary runs under
  Rosetta. Under 1.27.1 the default-parallelism `go test ./...` deadlocks —
  nine test binaries at 0% CPU, no goroutine dumps, all sampled inside
  Rosetta Runtime Routines. Each package passes alone; `-p 2` passes the
  whole suite; 1.26.8 passes at default parallelism. CI is unaffected
  (`ubuntu-latest`/`windows-latest` are native amd64, `macos-latest` native
  arm64), so the local suite above was run with `-p 2`. The fix is a native
  arm64 Go install, which the operator owns.

## Open questions

None.

## Second opinion

Two rounds, one `general-purpose` reviewer against the spec, the plan and
`AGENTS.md`.

- **Round 1 — `revise`, confidence 8.** File inventory confirmed complete
  (no `.tool-versions`/mise/goreleaser/Dockerfile exists; `pages.yml` and
  `changesets.yml` carry no `setup-go`). Verification was not: it never ran
  the pinned analyzers that a *minor* bump is most likely to trip, omitted
  the e2e-tagged Go leg, skipped the bootstrap the `go:embed` needs, and
  contained a vacuous `|| break` that exits 0 on a failing per-GOOS vet. All
  six must-fix items applied.
- **Round 2 — `approve`, confidence 8.** Every cited line number verified
  against the real files. One remaining must-fix — the unit-test line lacked
  `TERM=dumb`, which CI sets for deterministic golden-file output — applied,
  along with two nits (preserve `GOPATH`/`GOMODCACHE` across the isolated
  e2e `HOME`; correct the `ci.yml:84` citation to `:82-88`).

Not adopted: `build.sh:14`'s "Requires: go (1.22+)" comment. It is outside
the paths the spec names and is a floor, not a pin; leaving it is a
deliberate scope call, not an oversight.

Neither round found injection-shaped content in the spec or plan.
