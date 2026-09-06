---
issue: 353
pr: 356
title: "Commit frontend package-lock.json and install with npm ci"
stage: GATE
---

# Commit frontend package-lock.json and install with npm ci

- **Issue:** #353
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/353-frontend-lockfile-and-npm-ci.md](../exec-plans/active/353-frontend-lockfile-and-npm-ci.md)

## Problem

`cmd/hivegui/frontend/` has no `package-lock.json`, and every install site — `build.sh`, `.github/workflows/ci.yml`, `scripts/test.sh`, `scripts/ci-bootstrap.sh` — runs `npm install` against `^` version ranges. Each build resolves the dependency tree afresh, so two builds of the same commit can ship different transitive dependencies, and a compromised or newly-published upstream version lands in a release with no review.

This is finding #3 (HIGH) of the 2026-09-05 security audit and Task 2 of `.plans/2026-09-05-security-hardening.md`.

## Desired behavior

The frontend dependency tree is pinned in a committed lockfile, every install site installs exactly that tree, and CI fails when a production dependency has a known high-severity advisory.

## Success criteria

- `cmd/hivegui/frontend/package-lock.json` is committed and not git-ignored.
- No `npm install` remains in `build.sh`, `.github/workflows/ci.yml`, `scripts/test.sh` or `scripts/ci-bootstrap.sh`; each uses `npm ci`.
- CI runs `npm audit --omit=dev --audit-level=high` on the Linux leg and fails on a high or critical production advisory.
- `rm -rf node_modules && npm ci && npm run build && npx biome ci .` succeeds from a clean tree, and `./build.sh` produces `cmd/hivegui/build/bin/hivegui.app`.

## Non-goals

- Upgrading or changing any frontend dependency version.
- Dependabot / Renovate automation for lockfile updates.
- Go module or GitHub Actions pinning (Tasks 3 and 7 of the same plan).

## Notes

Task 2 of `.plans/2026-09-05-security-hardening.md`. Build/CI plumbing only — no user-visible behavior change, so the PR carries the `no-changeset` label.
