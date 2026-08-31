#!/usr/bin/env bash
# check-changeset.sh — local mirror of the `changesets` CI gate.
#
# Fails when the current branch adds no `.changesets/*.md` entry relative
# to main, so you find out before pushing instead of from a red check.
#
# Install as a pre-push hook (works from any worktree; --git-common-dir
# resolves to the shared hooks directory, so the graphify post-* hooks
# living there are left alone — do NOT set core.hooksPath, that would
# disable them):
#
#   ln -sf ../../scripts/check-changeset.sh \
#     "$(git rev-parse --git-common-dir)/hooks/pre-push"
#
# Bypass a single push with `git push --no-verify` — CI still requires
# either a changeset or the `no-changeset` label.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

base="$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || true)"
# Detached from any main, or nothing new on this branch: nothing to gate.
if [[ -z "$base" || "$base" == "$(git rev-parse HEAD)" ]]; then
  exit 0
fi

# Same glob and exclusions as .github/workflows/changesets.yml.
if git diff --name-only "$base"...HEAD -- ':(glob).changesets/*.md' \
  | grep -vE '^\.changesets/(README\.md|\.gitkeep)$' | grep -q .; then
  exit 0
fi

echo "error: this branch has no .changesets/*.md entry." >&2
echo "  Add one (see CONTRIBUTING.md for the schema), e.g.:" >&2
echo "    .changesets/<pr-or-slug>.md" >&2
echo "  Docs/CI-only change? Push with --no-verify and apply the" >&2
echo "  'no-changeset' label on the PR." >&2
exit 1
