---
issue: 359
pr: 360
title: "Bump the Go toolchain to 1.27.1 (1.25 is end of life)"
stage: REVIEW
---

# Bump the Go toolchain to 1.27.1 (1.25 is end of life)

- **Issue:** #359
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/359-bump-go-toolchain-to-1-27-1.md](../exec-plans/active/359-bump-go-toolchain-to-1-27-1.md)

## Problem

`go.mod` names `go 1.25.14`, the final 1.25 patch release. The 1.25 line is end of life and receives no further security fixes, so the toolchain CI and every contributor resolves is frozen on known-vulnerable stdlib code. Reachable stdlib advisories GO-2026-6090, GO-2026-6089, GO-2026-5972 and GO-2026-5856 are fixed only in 1.26.5 and later.

This is finding #5 (MED) of the 2026-09-05 security audit and Task 3 of `.plans/2026-09-05-security-hardening.md`.

## Desired behavior

Hive builds, tests and ships on a supported Go toolchain that carries the current stdlib security fixes, and every place in the repo that names a Go version agrees on that one version.

## Success criteria

- `go.mod`'s `go` directive names 1.27.1; `go.sum` is consistent (`go mod tidy` is a no-op afterwards).
- `go build ./...`, `go vet ./...` and `go test ./... -count=1 -timeout 120s` pass under `GOTOOLCHAIN=go1.27.1`.
- `govulncheck ./...` reports no reachable stdlib vulnerability.
- No stale `1.25.14` reference remains in `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, `scripts/` or `.github/`.
- The `staticcheck` pin in `.github/workflows/ci.yml` (and the cache key that names it) is on a release that supports the new toolchain, and `staticcheck ./...` is clean for darwin, linux and windows.
- The e2e-tagged leg `go test -tags=e2e ./cmd/hived/...` and `./build.sh` (Wails codegen) both succeed under the new toolchain.
- CI is green on all three OS legs, including the `govulncheck` job (which reads `go-version-file: go.mod`).

## Non-goals

- Upgrading beyond the 1.27 line.
- Upgrading any third-party Go module beyond what `go mod tidy` requires. (The `staticcheck` CI *tool* pin is in scope — Go 1.27 export data is unreadable by the old release, so the gate cannot run without it.)
- Any behavior change to hived, hivegui or hivebar.
- Frontend, GitHub Actions or socket hardening (Tasks 2, 7 and 4 of the same plan).

## Notes

Task 3 of `.plans/2026-09-05-security-hardening.md`. Toolchain plumbing only — no user-visible behavior change, so the PR carries the `no-changeset` label, and `daemon-contract-override` since `DaemonContract` is unchanged.
