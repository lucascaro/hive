#!/usr/bin/env bash
# dev-setup.sh — one-time local dev environment setup, shared by every worktree.
#
# Installs pre-push into the shared .git/hooks dir (not core.hooksPath: that
# would relocate hook lookup and disable the graphify post-checkout /
# post-commit hooks that already live there — see scripts/hooks/pre-push).
set -euo pipefail
cd "$(dirname "$0")/.."

hooks_dir="$(git rev-parse --git-common-dir)/hooks"
cp scripts/hooks/pre-push "$hooks_dir/pre-push"
chmod +x "$hooks_dir/pre-push"

echo "Installed pre-push hook into $hooks_dir"
