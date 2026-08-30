#!/usr/bin/env bash
# One-shot migration: split CHANGELOG.md `[Unreleased]` into `.changesets/*.md`.
#
# Thin shim around scripts/migrate-to-changesets.py — exists so downstream
# projects can run a stable `scripts/migrate-to-changesets.sh` entrypoint after
# upgrading hivesmith templates.
#
# Usage:
#   scripts/migrate-to-changesets.sh             # do the migration
#   scripts/migrate-to-changesets.sh --dry-run   # print planned writes, no I/O

set -euo pipefail

SCRIPT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

exec python3 "$SCRIPT_DIR/migrate-to-changesets.py" "$@"
