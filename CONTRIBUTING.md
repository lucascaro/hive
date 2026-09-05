# Contributing to Hive

Thank you for your interest in contributing! This document explains how to build, test, and submit changes.

## Development Setup

### Prerequisites

- **Go 1.25.14+** — [install](https://go.dev/dl/). The patch floor is
  deliberate: `go.mod` pins it because CI's `setup-go` installs the `go`
  directive verbatim, and the earlier 1.25.0 shipped stdlib advisories that
  `govulncheck` reports as reachable.
- **Node 24+** and the **Wails CLI** for the desktop GUI:
  run `scripts/ci-bootstrap.sh` (installs the pinned Wails CLI version)

### Build

```bash
git clone https://github.com/lucascaro/hive
cd hive
./build.sh            # macOS .app at cmd/hivegui/build/bin/hivegui.app
```

See the [README](README.md#build) for `--open` / `--zip` flags and the
Windows/Linux build steps.

### Run Tests

Hive has four test layers, all orchestrated by `scripts/test.sh`:

| Layer | What it covers                                        | Runtime  |
|-------|-------------------------------------------------------|----------|
| `go`  | Go unit + daemon integration tests (`internal/...`); shells out to `node` for the Pi extension's own suite, which skips when node cannot load TypeScript | ~5s      |
| `unit`| Pure JS modules under `cmd/hivegui/frontend/src/lib/` | <1s      |
| `dom` | Vitest jsdom tests (sidebar tree, visibility gate)    | <1s      |
| `e2e` | Playwright vs. the Wails-mock bridge                  | ~5s      |

```bash
scripts/test.sh             # all four
scripts/test.sh go          # just Go
scripts/test.sh unit dom    # frontend only, no browser
```

The frontend layers live in `cmd/hivegui/frontend/test/`. E2E tests run against `vite dev` with `VITE_WAILS_MOCK=1`, which swaps the generated Wails bindings for an in-browser fake (`test/e2e/wails-mock.ts`). No native Wails build is required to run them.

### Lint / Vet

```bash
go vet ./...
```

## Project Layout

See [DESIGN.md](DESIGN.md) for a full description of every package and how they interact.

## Submitting Changes

1. **Fork** the repository and create a branch from `main`.
2. Make your changes.
3. Ensure `go test ./...` and `go vet ./...` pass.
4. **Add a changeset** — for every user-visible change, create `.changesets/<slug>.md` with
   YAML frontmatter (`type`, `bump`, optional `issue` / `pr`) and the changelog bullet as the
   body. See `.changesets/README.md` for the schema. **Do not edit `CHANGELOG.md` directly** —
   it is regenerated on `main` from `.changesets/`, and a PR that touches it fails CI unless
   labelled `regen-override`. A docs- or CI-only PR can skip the changeset with the
   `no-changeset` label. To catch a missing changeset before you push, install the local
   gate as a `pre-push` hook — once per clone, shared by every worktree:
   `cp scripts/hooks/pre-push "$(git rev-parse --git-common-dir)/hooks/pre-push"`.
5. **Update `DESIGN.md`** if your change adds or removes packages, alters a major interface, or otherwise changes the high-level structure described there.
6. Update any relevant files in `docs/` if the subsystem they describe has changed.
7. Open a pull request with a clear description of the problem and solution.

### Commit Style

Use short imperative subject lines, e.g.:

```
fix: prevent socket permission exposure on multi-user systems
feat: add filter shortcut to grid view
docs: document hook environment variables
```

Prefixes: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`.

## Reporting Bugs

Open a [GitHub Issue](https://github.com/lucascaro/hive/issues) and include:

- Hive version (`hive version`)
- OS and terminal emulator
- Steps to reproduce
- Expected vs. actual behaviour

## Design Guidelines

When adding or modifying UI elements, follow the guidelines in [AGENTS.md](AGENTS.md):

- Show key hints inline, next to the action they trigger.
- Never rely on the user memorising bindings from the help screen alone.
- Destructive actions must go through a confirmation overlay.
- Keep status indicators (dots, badges) visible on every row.

## AI-Assisted Development

This project uses [hivesmith](https://github.com/lucascaro/hivesmith) skills for AI-assisted feature development. If you work with Claude Code, the following global `hs-*` skills are available:

- `/hs-feature-ingest` — import a GitHub issue into the local `features/` pipeline
- `/hs-feature-triage` — classify and prioritize
- `/hs-feature-research` — explore the codebase and document findings
- `/hs-feature-plan` — write an implementation plan
- `/hs-feature-implement` — code, test, and open a PR
- `/hs-feature-loop` — drive a feature through the entire pipeline
- `/hs-review-pr` — deep PR review
- `/hs-release` — cut a release

See [AGENTS.md](AGENTS.md) for the full feature pipeline workflow.

## Security

If you discover a security vulnerability, please open a GitHub Issue marked **[security]** or contact the maintainer directly before disclosing publicly. See [SECURITY.md](SECURITY.md) for details.
