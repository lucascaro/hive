# Contributing to Hive

Thank you for your interest in contributing! This document explains how to build, test, and submit changes.

## Development Setup

### Prerequisites

- **Go 1.25+** — [install](https://go.dev/dl/)
- No other runtime dependencies for the native backend. The tmux backend additionally requires `tmux`.

### Build

```bash
git clone https://github.com/lucascaro/hive
cd hive
go build -o hive .
```

Or use the build script, which also installs the binary to `/usr/local/bin`:

```bash
bash build.sh
```

### Run Tests

```bash
go test ./...
```

### Lint / Vet

```bash
go vet ./...
```

## Project Layout

See [ARCHITECTURE.md](ARCHITECTURE.md) for a full description of every package and how they interact.

## Submitting Changes

1. **Fork** the repository and create a branch from `main`.
2. Make your changes.
3. Ensure `go test ./...` and `go vet ./...` pass.
4. **Update `CHANGELOG.md`** — add an entry under `[Unreleased]` for every user-visible change (`Added`, `Changed`, `Fixed`, `Removed`, or `Security`).
5. **Update `ARCHITECTURE.md`** if your change adds or removes packages, alters a major interface, or otherwise changes the high-level structure described there.
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

## Security

If you discover a security vulnerability, please open a GitHub Issue marked **[security]** or contact the maintainer directly before disclosing publicly. See [SECURITY.md](SECURITY.md) for details.
