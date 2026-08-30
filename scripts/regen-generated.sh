#!/usr/bin/env bash
# Regenerate hivesmith's centralized aggregate files from per-PR sources.
#
# This is a thin shim around scripts/regen-generated.py. It exists so CI and
# downstream tooling can call a stable shell entrypoint that matches the rest
# of the repo's scripts/* convention; the heavy lifting (YAML parsing, rendering)
# lives in the Python sibling.
#
# Usage:
#   scripts/regen-generated.sh                  # regenerate all aggregates
#   scripts/regen-generated.sh --check          # exit non-zero if any aggregate would change
#   scripts/regen-generated.sh --release X.Y.Z  # promote [Unreleased] to stamped X.Y.Z

set -euo pipefail

SCRIPT_DIR="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

exec python3 "$SCRIPT_DIR/regen-generated.py" "$@"
